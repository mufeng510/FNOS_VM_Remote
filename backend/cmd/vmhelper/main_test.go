// vmhelper 集成测试：用假 virsh 脚本验证安全约束。
// 仅当 FNOS_VM_REMOTE_HELPER_INTEGRATION=1 时运行（CI 中启用）。
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func helperBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fnos-vm-remote-helper")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, out)
	}
	return bin
}

// fakeVirsh 生成一个假 virsh：domuuid 输出给定值；其他参数记录到文件。
func fakeVirsh(t *testing.T, uuid, logFile string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "virsh")
	content := fmt.Sprintf(`#!/bin/bash
set -e
echo "$@" >> %q
if [ "$1" = "-c" ] && [ "$2" = "qemu:///system" ] && [ "$3" = "domuuid" ]; then
  echo %q
  exit 0
fi
if [ "$3" = "domstate" ]; then
  echo "shut off"
  exit 0
fi
echo "OK"
`, logFile, uuid)
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func runHelper(t *testing.T, bin, virsh string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "FNOS_VM_REMOTE_HELPER_VIRSH=" + virsh}
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), string(exitErr.Stderr), err
	}
	return string(out), "", err
}

func TestHelperRejectsInvalidArgs(t *testing.T) {
	bin := helperBinary(t)
	logFile := filepath.Join(t.TempDir(), "calls.log")
	virsh := fakeVirsh(t, vmUUID, logFile)

	for _, args := range [][]string{{}, {"destroy"}, {"reboot"}, {"start", "evil-vm"}, {"a", "b"}} {
		_, stderr, err := runHelper(t, bin, virsh, args...)
		if err == nil {
			t.Errorf("args %v must be rejected", args)
		}
		if strings.Contains(stderr, "destroy") && len(args) == 1 {
			// 期望被拒绝而不是被执行
			_ = stderr
		}
	}
	// 假 virsh 不应被调用任何销毁类命令
	data, _ := os.ReadFile(logFile)
	for _, forbidden := range []string{"destroy", "reboot", "undefine"} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("virsh log contains forbidden command: %s", forbidden)
		}
	}
}

func TestHelperStartVerifiesUUID(t *testing.T) {
	bin := helperBinary(t)

	// UUID 不匹配：start 必须被拒绝
	logFile := filepath.Join(t.TempDir(), "calls.log")
	virsh := fakeVirsh(t, "00000000-0000-0000-0000-000000000000", logFile)
	_, stderr, err := runHelper(t, bin, virsh, "start")
	if err == nil {
		t.Fatal("start must be rejected when UUID mismatches")
	}
	if !strings.Contains(stderr, "UUID") {
		t.Errorf("error should mention UUID, got: %s", stderr)
	}
	data, _ := os.ReadFile(logFile)
	if strings.Contains(string(data), " start 3f1jeea2") && !strings.HasSuffix(strings.TrimSpace(string(data)), "domuuid 3f1jeea2") {
		// 只要日志里出现了对 start 的正式调用即违规
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "start 3f1jeea2") && !strings.Contains(line, "domuuid") {
				t.Errorf("virsh start must NOT be invoked on UUID mismatch; log line: %s", line)
			}
		}
	}

	// UUID 匹配：正常执行
	logFile2 := filepath.Join(t.TempDir(), "calls.log")
	virsh2 := fakeVirsh(t, vmUUID, logFile2)
	out, _, err := runHelper(t, bin, virsh2, "start")
	if err != nil {
		t.Fatalf("start with matching UUID must succeed: %v", err)
	}
	if strings.TrimSpace(out) != "OK" {
		t.Errorf("expected OK, got %q", out)
	}
}

func TestHelperStatus(t *testing.T) {
	bin := helperBinary(t)
	logFile := filepath.Join(t.TempDir(), "calls.log")
	virsh := fakeVirsh(t, vmUUID, logFile)
	out, _, err := runHelper(t, bin, virsh, "status")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "shut off" {
		t.Errorf("expected 'shut off', got %q", out)
	}
}
