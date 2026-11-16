// Package vmmgmt 负责通过受限辅助程序（vmhelper，经 sudo 授权）读取和控制目标虚拟机。
//
// 设计约束：
//   - 永远不直接拼接 Shell 命令；
//   - 只执行固定的 helper 子命令，调用方无法影响虚拟机标识；
//   - 状态查询以 libvirt 实时结果（domstate）为准；
//   - 关机超时后绝不强制断电（不调用 destroy）。
package vmmgmt

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// VMStatus 是面向 API 的规范化虚拟机状态。
type VMStatus struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
	// State 为 API 暴露的规范化状态：
	//   running | shut off | starting | shutting_down | paused | unknown
	State string `json:"state"`
	// RawState 为 libvirt domstate 的原始输出。
	RawState string `json:"raw_state"`
}

// Runner 抽象对 helper 的调用，便于测试注入。
type Runner interface {
	// Run 执行 helper 子命令（status/start/shutdown），返回 stdout。
	Run(ctx context.Context, action string) (string, error)
}

// SudoRunner 通过固定 sudo 命令调用 helper。
type SudoRunner struct {
	HelperPath string        // helper 二进制绝对路径
	Timeout    time.Duration // 单次调用超时
}

func (r SudoRunner) Run(ctx context.Context, action string) (string, error) {
	if action != "status" && action != "start" && action != "shutdown" {
		return "", fmt.Errorf("非法的 helper 子命令: %q", action)
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sudo", "-n", r.HelperPath, action)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("helper %s 超时", action)
	}
	var stderr string
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stderr = strings.TrimSpace(string(exitErr.Stderr))
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if stderr != "" {
			msg = stderr
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("helper %s 失败: %s", action, msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// Controller 串行化虚拟机操作并实现幂等语义。
type Controller struct {
	runner Runner

	mu          sync.Mutex // 串行化 start/shutdown 等变更操作
	pendingFrom time.Time  // 当前 pending 操作开始时间（零值表示无）
	pendingKind string     // "start" | "shutdown"

	// PollInterval 是关机/开机等待期间的轮询间隔（默认 2 秒，测试可调小）。
	PollInterval time.Duration
	// StartWait 是开机后等待 domstate 变为 running 的最长时间。
	StartWait time.Duration
}

func NewController(runner Runner) *Controller {
	return &Controller{runner: runner, PollInterval: 2 * time.Second, StartWait: 15 * time.Second}
}

// normalizeState 将 libvirt domstate 原始输出映射为 API 状态。
func normalizeState(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "running":
		return "running"
	case "shut off":
		return "shut off"
	case "in shutdown":
		return "shutting_down"
	case "paused":
		return "paused"
	case "":
		return "unknown"
	default:
		return "unknown"
	}
}

// Status 查询当前状态。pending 操作会被用于推导 starting / shutting_down。
func (c *Controller) Status(ctx context.Context) (VMStatus, error) {
	c.mu.Lock()
	pendingKind, pendingFrom := c.pendingKind, c.pendingFrom
	c.mu.Unlock()

	out, err := c.runner.Run(ctx, "status")
	if err != nil {
		return VMStatus{}, err
	}
	raw := firstLine(out)
	state := normalizeState(raw)

	if pendingKind != "" && time.Since(pendingFrom) < 2*time.Minute {
		switch {
		case pendingKind == "start" && state == "shut off":
			state = "starting"
		case pendingKind == "shutdown" && state == "running":
			state = "shutting_down"
		}
	}

	return VMStatus{Name: vmNameConst, UUID: vmUUIDConst, State: state, RawState: raw}, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

const (
	vmNameConst = "3f1jeea2"
	vmUUIDConst = "1ce21ce8-0bd1-4ab3-a9c0-064abf81381f"
)

// StartResult 为开机操作结果。
type StartResult struct {
	Status  VMStatus `json:"status"`
	Started bool     `json:"started"` // 本次调用是否真正下发了启动指令
	Already bool     `json:"already"` // 已处于运行状态，未重复启动
	Pending bool     `json:"pending"` // 已在启动流程中
	Message string   `json:"message"`
}

// Start 幂等开机：已运行直接返回；已关闭则执行启动。
func (c *Controller) Start(ctx context.Context) (StartResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	out, err := c.runner.Run(ctx, "status")
	if err != nil {
		return StartResult{}, fmt.Errorf("查询虚拟机状态失败: %w", err)
	}
	raw := firstLine(out)
	state := normalizeState(raw)

	switch state {
	case "running":
		return StartResult{Status: vmStatus(raw, "running"), Already: true, Message: "虚拟机已在运行中"}, nil
	case "starting":
		return StartResult{Status: vmStatus(raw, "starting"), Pending: true, Message: "虚拟机正在启动中"}, nil
	case "shutting_down", "paused":
		return StartResult{Status: vmStatus(raw, state), Message: fmt.Sprintf("当前状态为 %s，无法启动，请稍后再试", raw)}, ErrUnavailable
	case "shut off":
	default:
		return StartResult{Status: vmStatus(raw, state), Message: fmt.Sprintf("无法识别的虚拟机状态: %s", raw)}, ErrUnavailable
	}

	if _, err := c.runner.Run(ctx, "start"); err != nil {
		return StartResult{}, fmt.Errorf("启动虚拟机失败: %w", err)
	}
	c.pendingKind, c.pendingFrom = "start", time.Now()

	// virsh start 返回即表示 QEMU 进程已启动；domstate 通常立即为 running。
	state, raw = c.waitForStateLocked(ctx, func(s string) bool { return s == "running" }, c.StartWait)
	if state == "" {
		state, raw = "starting", "unknown"
	}
	c.clearPendingLocked()

	return StartResult{Status: vmStatus(raw, state), Started: true, Message: "开机指令已执行"}, nil
}

// ShutdownResult 为正常关机操作结果。
type ShutdownResult struct {
	Status   VMStatus `json:"status"`
	Sent     bool     `json:"sent"`    // 本次是否真正下发了关机指令
	Already  bool     `json:"already"` // 已处于关机状态
	Pending  bool     `json:"pending"` // 已在关机流程中
	TimedOut bool     `json:"timed_out"`
	Message  string   `json:"message"`
}

// Shutdown 幂等正常关机。超时后绝不强制断电。
func (c *Controller) Shutdown(ctx context.Context, timeout time.Duration) (ShutdownResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	out, err := c.runner.Run(ctx, "status")
	if err != nil {
		return ShutdownResult{}, fmt.Errorf("查询虚拟机状态失败: %w", err)
	}
	raw := firstLine(out)
	state := normalizeState(raw)

	switch state {
	case "shut off":
		return ShutdownResult{Status: vmStatus(raw, "shut off"), Already: true, Message: "虚拟机已处于关闭状态"}, nil
	case "shutting_down":
		return ShutdownResult{Status: vmStatus(raw, "shutting_down"), Pending: true, Message: "虚拟机正在关机中"}, nil
	case "running":
	default:
		return ShutdownResult{Status: vmStatus(raw, state), Message: fmt.Sprintf("无法识别的虚拟机状态: %s", raw)}, ErrUnavailable
	}

	if _, err := c.runner.Run(ctx, "shutdown"); err != nil {
		return ShutdownResult{}, fmt.Errorf("发送正常关机指令失败: %w", err)
	}
	c.pendingKind, c.pendingFrom = "shutdown", time.Now()

	finalState, finalRaw := c.waitForStateLocked(ctx, func(s string) bool { return s == "shut off" }, timeout)
	c.clearPendingLocked()

	switch {
	case finalState == "shut off":
		return ShutdownResult{Status: vmStatus(finalRaw, "shut off"), Sent: true, Message: "虚拟机已正常关机"}, nil
	case finalState == "":
		return ShutdownResult{Status: vmStatus(raw, "running"), Sent: true, TimedOut: true,
			Message: fmt.Sprintf("已发送正常关机指令，但 Windows 未在 %d 秒内关闭。未执行强制断电，请检查 Windows 是否正常响应后重试", int(timeout.Seconds()))}, nil
	default:
		return ShutdownResult{Status: vmStatus(finalRaw, finalState), Sent: true, TimedOut: true,
			Message: fmt.Sprintf("已发送正常关机指令，当前状态: %s。未执行强制断电，请稍后再次查询", finalRaw)}, nil
	}
}

// waitForStateLocked 轮询状态直到 match 为真或超时；超时返回 ("", "")。
func (c *Controller) waitForStateLocked(ctx context.Context, match func(string) bool, timeout time.Duration) (string, string) {
	deadline := time.Now().Add(timeout)
	for {
		out, err := c.runner.Run(ctx, "status")
		if err == nil {
			raw := firstLine(out)
			state := normalizeState(raw)
			if match(state) {
				return state, raw
			}
			if time.Now().After(deadline) {
				return state, raw
			}
		} else if time.Now().After(deadline) {
			return "", ""
		}
		select {
		case <-ctx.Done():
			return "", ""
		case <-time.After(c.pollInterval()):
		}
	}
}

func (c *Controller) clearPendingLocked() {
	c.pendingKind, c.pendingFrom = "", time.Time{}
}

func (c *Controller) pollInterval() time.Duration {
	if c.PollInterval > 0 {
		return c.PollInterval
	}
	return 2 * time.Second
}

func vmStatus(raw, state string) VMStatus {
	return VMStatus{Name: vmNameConst, UUID: vmUUIDConst, State: state, RawState: raw}
}

// ErrUnavailable 表示当前虚拟机状态不允许执行该操作。
var ErrUnavailable = errors.New("虚拟机当前状态不允许执行该操作")
