package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mufeng510/FNOS_VM_Remote/backend/internal/config"
	"github.com/mufeng510/FNOS_VM_Remote/backend/internal/vmmgmt"
)

// stubVM 是可脚本化的 VMController。
type stubVM struct {
	status    vmmgmt.VMStatus
	statusErr error

	startRes vmmgmt.StartResult
	startErr error

	shutdownRes vmmgmt.ShutdownResult
	shutdownErr error
}

func (s *stubVM) Status(ctx context.Context) (vmmgmt.VMStatus, error) {
	return s.status, s.statusErr
}
func (s *stubVM) Start(ctx context.Context) (vmmgmt.StartResult, error) {
	return s.startRes, s.startErr
}
func (s *stubVM) Shutdown(ctx context.Context, timeout time.Duration) (vmmgmt.ShutdownResult, error) {
	return s.shutdownRes, s.shutdownErr
}

func newTestServer(t *testing.T, vm VMController) (*httptest.Server, *config.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store := config.NewStore(dir)
	if err := store.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	token, err := config.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetToken(token); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(store, vm, "0.1.0-test", 8321).Handler())
	t.Cleanup(srv.Close)
	return srv, store, token
}

func doReq(t *testing.T, client *http.Client, method, url, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func parseBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestHealthNoAuth(t *testing.T) {
	srv, _, _ := newTestServer(t, &stubVM{})
	resp := doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/v1/health", "")
	body := parseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if body["success"] != true {
		t.Fatal("expected success")
	}
}

func TestStatusRequiresAuth(t *testing.T) {
	vm := &stubVM{status: vmmgmt.VMStatus{Name: "3f1jeea2", UUID: "1ce21ce8-0bd1-4ab3-a9c0-064abf81381f", State: "shut off", RawState: "shut off"}}
	srv, _, token := newTestServer(t, vm)

	// 无 Token
	resp := doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/v1/vm/status", "")
	if resp.StatusCode != http.StatusUnauthorized {
		resp.Body.Close()
		t.Fatalf("no token: status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 错误 Token
	resp = doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/v1/vm/status", "wrong-token")
	if resp.StatusCode != http.StatusUnauthorized {
		resp.Body.Close()
		t.Fatalf("wrong token: status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 正确 Token
	resp = doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/v1/vm/status", token)
	body := parseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	data, _ := body["data"].(map[string]any)
	if data["state"] != "shut off" {
		t.Errorf("state = %v", data["state"])
	}
}

func TestStartAlreadyRunning(t *testing.T) {
	vm := &stubVM{
		startRes: vmmgmt.StartResult{
			Status:  vmmgmt.VMStatus{Name: "3f1jeea2", UUID: "1ce21ce8-0bd1-4ab3-a9c0-064abf81381f", State: "running", RawState: "running"},
			Already: true,
			Message: "虚拟机已在运行中",
		},
	}
	srv, _, token := newTestServer(t, vm)
	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/vm/start", token)
	body := parseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if body["success"] != true {
		t.Fatal("expected success")
	}
	data, _ := body["data"].(map[string]any)
	if data["already"] != true {
		t.Error("expected already=true")
	}
}

func TestShutdownTimeoutReturnsExplicitFailure(t *testing.T) {
	vm := &stubVM{
		shutdownRes: vmmgmt.ShutdownResult{
			Status:   vmmgmt.VMStatus{Name: "3f1jeea2", UUID: "1ce21ce8-0bd1-4ab3-a9c0-064abf81381f", State: "running", RawState: "running"},
			Sent:     true,
			TimedOut: true,
			Message:  "已发送正常关机指令，但 Windows 未在 120 秒内关闭。未执行强制断电",
		},
	}
	srv, _, token := newTestServer(t, vm)
	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/vm/shutdown", token)
	body := parseBody(t, resp)
	if body["success"] != false {
		t.Fatal("timeout must report success=false")
	}
	data, _ := body["data"].(map[string]any)
	if data["timed_out"] != true {
		t.Error("expected timed_out=true")
	}
}

func TestShutdownTimeoutParamValidation(t *testing.T) {
	vm := &stubVM{}
	srv, _, token := newTestServer(t, vm)

	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/vm/shutdown?timeout=abc", token)
	if resp.StatusCode != http.StatusBadRequest {
		resp.Body.Close()
		t.Fatalf("non-numeric timeout: status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/vm/shutdown?timeout=9999", token)
	if resp.StatusCode != http.StatusBadRequest {
		resp.Body.Close()
		t.Fatalf("out-of-range timeout: status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestStartConflictWhenUnavailable(t *testing.T) {
	vm := &stubVM{
		startRes: vmmgmt.StartResult{
			Status:  vmmgmt.VMStatus{State: "paused", RawState: "paused"},
			Message: "当前状态为 paused，无法启动，请稍后再试",
		},
		startErr: vmmgmt.ErrUnavailable,
	}
	srv, _, token := newTestServer(t, vm)
	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/vm/start", token)
	if resp.StatusCode != http.StatusConflict {
		resp.Body.Close()
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["success"] != false {
		t.Fatal("expected success=false")
	}
}

func TestTokenRotate(t *testing.T) {
	vm := &stubVM{}
	srv, store, token := newTestServer(t, vm)

	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/auth/token/rotate", token)
	body := parseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	data, _ := body["data"].(map[string]any)
	newToken, _ := data["new_token"].(string)
	if len(newToken) != 48 {
		t.Fatalf("expected 48-char new token, got %d", len(newToken))
	}
	if newToken == token {
		t.Fatal("new token must differ")
	}
	if !store.ValidateToken(newToken) {
		t.Fatal("new token must be stored")
	}
	if store.ValidateToken(token) {
		t.Fatal("old token must be invalidated")
	}
}

func TestLogsNeverContainToken(t *testing.T) {
	vm := &stubVM{status: vmmgmt.VMStatus{Name: "3f1jeea2", UUID: "1ce21ce8-0bd1-4ab3-a9c0-064abf81381f", State: "running", RawState: "running"}}
	srv, store, token := newTestServer(t, vm)

	resp := doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/v1/vm/status", token)
	resp.Body.Close()
	// 触发一次失败鉴权
	resp = doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/v1/vm/status", "super-secret-wrong-token")
	resp.Body.Close()

	entries, err := store.ReadLogs(50)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, _ := json.Marshal(e)
		if strings.Contains(string(b), token) {
			t.Fatal("log entry contains the plaintext token")
		}
		if strings.Contains(string(b), "super-secret-wrong-token") {
			t.Fatal("log entry contains the rejected token")
		}
	}
}

func TestUnknownRoutesAndMethods(t *testing.T) {
	vm := &stubVM{}
	srv, _, token := newTestServer(t, vm)

	// 破坏性接口不存在
	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/vm/destroy", token)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("destroy endpoint must not exist")
	}

	// 其他虚拟机无法被指定
	vm2 := &stubVM{startRes: vmmgmt.StartResult{
		Status:  vmmgmt.VMStatus{Name: "3f1jeea2", UUID: "1ce21ce8-0bd1-4ab3-a9c0-064abf81381f", State: "shut off", RawState: "shut off"},
		Message: "开机指令已执行",
	}}
	srv2, _, token2 := newTestServer(t, vm2)
	resp = doReq(t, srv2.Client(), http.MethodPost, srv2.URL+"/api/v1/vm/start?name=othervm", token2)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("query params must not change behavior; status = %d", resp.StatusCode)
	}
	parsed := parseBody(t, resp)
	data, _ := parsed["data"].(map[string]any)
	st, _ := data["status"].(map[string]any)
	if st["name"] != "3f1jeea2" {
		t.Fatalf("VM identity must be fixed; got %v", st["name"])
	}
}

func TestSetupFlow(t *testing.T) {
	// 未初始化的全新实例：所有受保护接口 403，setup/status 报告 setup_required
	dir := t.TempDir()
	store := config.NewStore(dir)
	if err := store.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	vm := &stubVM{status: vmmgmt.VMStatus{Name: "3f1jeea2", UUID: "1ce21ce8-0bd1-4ab3-a9c0-064abf81381f", State: "shut off", RawState: "shut off"}}
	srv := httptest.NewServer(New(store, vm, "0.2.0-test", 8321).Handler())
	defer srv.Close()

	// setup/status 无需鉴权
	resp := doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/v1/setup/status", "")
	body := parseBody(t, resp)
	data := body["data"].(map[string]any)
	if data["setup_required"] != true {
		t.Fatal("fresh instance must report setup_required=true")
	}
	// 受保护接口：403
	resp = doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/v1/vm/status", "")
	if resp.StatusCode != http.StatusForbidden {
		resp.Body.Close()
		t.Fatalf("uninitialized: status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()

	// 初始化：服务端生成 Token，明文仅返回一次
	resp = doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/setup", "")
	body = parseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: status = %d", resp.StatusCode)
	}
	setupData := body["data"].(map[string]any)
	token, _ := setupData["new_token"].(string)
	if len(token) != 48 {
		t.Fatalf("expected 48-char token, got %d", len(token))
	}

	// 重复初始化：409
	resp = doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/setup", "")
	if resp.StatusCode != http.StatusConflict {
		resp.Body.Close()
		t.Fatalf("double setup: status = %d, want 409", resp.StatusCode)
	}
	resp.Body.Close()

	// 受保护接口用新 Token 可正常工作
	resp = doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/v1/vm/status", token)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("authorized after setup: status = %d", resp.StatusCode)
	}
	body = parseBody(t, resp)
	if data, _ := body["data"].(map[string]any); data["state"] != "shut off" {
		t.Fatalf("state = %v", data["state"])
	}
}
