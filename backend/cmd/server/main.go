// fnos-vm-remote-server：FNOS VM Remote 的常驻 HTTP 服务。
//
// 运行身份：以应用包用户（非 root）运行；VM 操作经 sudo -n 调用受限 helper。
// 数据目录（TRIM_PKGVAR）由 FNOS 在升级间保留。
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/mufeng510/FNOS_VM_Remote/backend/internal/config"
	"github.com/mufeng510/FNOS_VM_Remote/backend/internal/httpx"
	"github.com/mufeng510/FNOS_VM_Remote/backend/internal/vmmgmt"
)

// version 由构建时 -ldflags "-X main.version=x.y.z" 注入
var version = "0.1.0-dev"

func main() {
	// 数据目录：默认 TRIM_PKGVAR，可用环境变量覆盖（本地开发/测试用）
	dataDir := getenv("FNOS_VM_REMOTE_DATA", "/var/apps/fnos-vm-remote/var")
	if v := os.Getenv("TRIM_PKGVAR"); v != "" && os.Getenv("FNOS_VM_REMOTE_DATA") == "" {
		dataDir = v
	}

	// Helper 路径：默认 $TRIM_APPDEST/server/fnos-vm-remote-helper
	helperPath := getenv("FNOS_VM_REMOTE_HELPER", "/var/apps/fnos-vm-remote/target/server/fnos-vm-remote-helper")
	if appDest := os.Getenv("TRIM_APPDEST"); appDest != "" && os.Getenv("FNOS_VM_REMOTE_HELPER") == "" {
		helperPath = filepath.Join(appDest, "server", "fnos-vm-remote-helper")
	}

	store := config.NewStore(dataDir)
	if err := store.EnsureDirs(); err != nil {
		fatal("创建数据目录失败: %v", err)
	}

	// 端口优先级：FNOS_VM_REMOTE_PORT > config.json > 默认 8321
	cfg, err := store.LoadConfig()
	if err != nil {
		// 配置损坏时拒绝启动并给出明确信息，避免监听错误端口
		fatal("读取配置失败: %v", err)
	}
	port := cfg.Port
	if v := os.Getenv("FNOS_VM_REMOTE_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p >= 1 && p <= 65535 {
			port = p
		} else {
			fatal("FNOS_VM_REMOTE_PORT 非法: %q", v)
		}
	}

	runner := vmmgmt.SudoRunner{HelperPath: helperPath, Timeout: 20 * time.Second}
	controller := vmmgmt.NewController(runner)

	srv := httpx.New(store, controller, version, port)

	httpServer := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", port),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// 关机接口可能长轮询，写超时需覆盖最长关机等待（300s）+ 余量
		WriteTimeout:   320 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 16,
	}

	go func() {
		fmt.Printf("[fnos-vm-remote] 版本 %s，监听 0.0.0.0:%d，数据目录 %s\n", version, port, dataDir)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("HTTP 服务监听失败: %v", err)
		}
	}()

	// 等待退出信号，优雅关闭并清理资源
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh
	fmt.Println("[fnos-vm-remote] 收到退出信号，正在优雅关闭…")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "优雅关闭失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("[fnos-vm-remote] 已退出")
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[fnos-vm-remote] 致命错误: "+format+"\n", args...)
	os.Exit(1)
}
