// fnos-vm-remote-helper 是 FNOS VM Remote 的受限特权辅助程序。
//
// 设计约束（安全模型的一部分）：
//   - 仅允许 status / start / shutdown 三个子命令，任何其他参数直接拒绝；
//   - 虚拟机名称与 UUID 硬编码，不接受来自调用方的任何虚拟机标识；
//   - 执行 start / shutdown 前先向 libvirt 校验名称对应的 UUID，防止重名虚机被误操作；
//   - 不通过 Shell 调用 virsh，而是直接 exec 固定路径的 virsh 并传递固定参数；
//   - 不提供 destroy / reboot / undefine 等任何第一阶段不需要的能力。
//
// 该程序由 sudoers 授权给应用包用户免密执行，必须保持最小实现。
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	vmName        = "3f1jeea2"
	vmUUID        = "1ce21ce8-0bd1-4ab3-a9c0-064abf81381f"
	libvirtURI    = "qemu:///system"
	virshDefault  = "/usr/bin/virsh"
	virshEnv      = "FNOS_VM_REMOTE_HELPER_VIRSH"
	helperVersion = "0.1.0"
	virshTimeout  = 15 * time.Second
)

func virshPath() string {
	if p := os.Getenv(virshEnv); p != "" {
		return p
	}
	return virshDefault
}

func virsh(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), virshTimeout)
	defer cancel()

	full := append([]string{"-c", libvirtURI}, args...)
	cmd := exec.CommandContext(ctx, virshPath(), full...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("virsh %s 超时", strings.Join(args, " "))
	}
	if err != nil {
		return "", fmt.Errorf("virsh %s 失败: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// verifyUUID 确认 libvirt 中名为 vmName 的虚拟机 UUID 与预期一致。
func verifyUUID() error {
	out, err := virsh("domuuid", vmName)
	if err != nil {
		return fmt.Errorf("查询虚拟机 UUID 失败: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(out), vmUUID) {
		return fmt.Errorf("虚拟机 %s 的 UUID 与预期不符，已拒绝操作", vmName)
	}
	return nil
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s {status|start|shutdown|version}\n", os.Args[0])
	os.Exit(2)
}

func main() {
	if len(os.Args) != 2 {
		usage()
	}

	if os.Args[1] == "version" {
		fmt.Println(helperVersion)
		return
	}

	switch os.Args[1] {
	case "status":
		out, err := virsh("domstate", vmName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Println(out)
	case "start":
		if err := verifyUUID(); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		if _, err := virsh("start", vmName); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Println("OK")
	case "shutdown":
		if err := verifyUUID(); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		if _, err := virsh("shutdown", vmName); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Println("OK")
	default:
		usage()
	}
}
