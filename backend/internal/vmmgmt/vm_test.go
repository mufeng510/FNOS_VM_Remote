package vmmgmt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRunner 是脚本化的 Runner：按 action 返回队列中的响应。
type fakeRunner struct {
	mu      sync.Mutex
	calls   []string
	status  []string // 每次 status 调用依次取出；耗尽后重复最后一个
	startOK bool
	shutOK  bool
}

func (f *fakeRunner) Run(ctx context.Context, action string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, action)

	switch action {
	case "status":
		if len(f.status) == 0 {
			return "unknown", nil
		}
		out := f.status[0]
		if len(f.status) > 1 {
			f.status = f.status[1:]
		}
		return out, nil
	case "start":
		if !f.startOK {
			return "", errors.New("start failed")
		}
		return "OK", nil
	case "shutdown":
		if !f.shutOK {
			return "", errors.New("shutdown failed")
		}
		return "OK", nil
	default:
		return "", fmt.Errorf("unexpected action %q", action)
	}
}

func newTestController(f *fakeRunner) *Controller {
	c := NewController(f)
	c.PollInterval = time.Millisecond
	c.StartWait = 50 * time.Millisecond
	return c
}

func TestNormalizeState(t *testing.T) {
	cases := map[string]string{
		"running":     "running",
		"shut off":    "shut off",
		"in shutdown": "shutting_down",
		"paused":      "paused",
		"crashed":     "unknown",
		"":            "unknown",
		" RUNNING ":   "running",
	}
	for in, want := range cases {
		if got := normalizeState(in); got != want {
			t.Errorf("normalizeState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStartAlreadyRunning(t *testing.T) {
	f := &fakeRunner{status: []string{"running"}}
	c := newTestController(f)
	res, err := c.Start(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Already {
		t.Error("expected Already=true")
	}
	if res.Started {
		t.Error("expected Started=false")
	}
	for _, call := range f.calls {
		if call == "start" {
			t.Error("start must NOT be called when VM is already running")
		}
	}
}

func TestStartFromShutOff(t *testing.T) {
	// 第一次 status → shut off（执行前），之后 status → running
	f := &fakeRunner{status: []string{"shut off", "running"}, startOK: true}
	c := newTestController(f)
	res, err := c.Start(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Started {
		t.Error("expected Started=true")
	}
	if res.Status.State != "running" {
		t.Errorf("expected state running, got %q", res.Status.State)
	}
	foundStart := false
	for _, call := range f.calls {
		if call == "start" {
			foundStart = true
		}
	}
	if !foundStart {
		t.Error("expected start to be called")
	}
}

func TestStartFailureReturnsError(t *testing.T) {
	f := &fakeRunner{status: []string{"shut off"}, startOK: false}
	c := newTestController(f)
	_, err := c.Start(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "启动虚拟机失败") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestShutdownAlreadyOff(t *testing.T) {
	f := &fakeRunner{status: []string{"shut off"}}
	c := newTestController(f)
	res, err := c.Shutdown(context.Background(), 50*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Already {
		t.Error("expected Already=true")
	}
	for _, call := range f.calls {
		if call == "shutdown" {
			t.Error("shutdown must NOT be called when VM is already off")
		}
	}
}

func TestShutdownSuccess(t *testing.T) {
	f := &fakeRunner{status: []string{"running", "in shutdown", "shut off"}, shutOK: true}
	c := newTestController(f)
	res, err := c.Shutdown(context.Background(), 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Sent {
		t.Error("expected Sent=true")
	}
	if res.TimedOut {
		t.Error("expected TimedOut=false")
	}
	if res.Status.State != "shut off" {
		t.Errorf("expected shut off, got %q", res.Status.State)
	}
}

func TestShutdownTimeoutDoesNotDestroy(t *testing.T) {
	f := &fakeRunner{status: []string{"running", "running", "running", "running"}, shutOK: true}
	c := newTestController(f)
	res, err := c.Shutdown(context.Background(), 30*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.TimedOut {
		t.Error("expected TimedOut=true")
	}
	for _, call := range f.calls {
		if call != "status" && call != "shutdown" {
			t.Errorf("forbidden helper call: %q", call)
		}
	}
}

func TestShutdownWhenInShutdown(t *testing.T) {
	f := &fakeRunner{status: []string{"in shutdown"}}
	c := newTestController(f)
	res, err := c.Shutdown(context.Background(), 50*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Pending {
		t.Error("expected Pending=true")
	}
	for _, call := range f.calls {
		if call == "shutdown" {
			t.Error("shutdown must NOT be sent again while already shutting down")
		}
	}
}

// 关键约束：永远不调用 destroy 类子命令。fakeRunner 的 default 分支会报错，
// 上面 TestShutdownTimeoutDoesNotDestroy 已覆盖该约束。
