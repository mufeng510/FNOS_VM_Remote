// Package httpx 提供 fnos-vm-remote 的 HTTP API 与内嵌 Web UI。
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mufeng510/FNOS_VM_Remote/backend/internal/config"
	"github.com/mufeng510/FNOS_VM_Remote/backend/internal/vmmgmt"
)

// VMController 抽象虚拟机控制能力，便于测试替换。
type VMController interface {
	Status(ctx context.Context) (vmmgmt.VMStatus, error)
	Start(ctx context.Context) (vmmgmt.StartResult, error)
	Shutdown(ctx context.Context, timeout time.Duration) (vmmgmt.ShutdownResult, error)
}

// Server 持有依赖并实现 http.Handler。
type Server struct {
	store   *config.Store
	vm      VMController
	version string
	port    int

	logf func(format string, args ...any)
}

func New(store *config.Store, vm VMController, version string, port int) *Server {
	return &Server{
		store:   store,
		vm:      vm,
		version: version,
		port:    port,
		logf:    func(format string, args ...any) { log.Printf("[fnos-vm-remote] "+format, args...) },
	}
}

// Routes 注册路由。
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/setup/status", s.handleSetupStatus)
	mux.HandleFunc("POST /api/v1/setup", s.handleSetup)
	mux.Handle("GET /api/v1/vm/status", s.requireAuth(http.HandlerFunc(s.handleVMStatus)))
	mux.Handle("POST /api/v1/vm/start", s.requireAuth(http.HandlerFunc(s.handleVMStart)))
	mux.Handle("POST /api/v1/vm/shutdown", s.requireAuth(http.HandlerFunc(s.handleVMShutdown)))
	mux.Handle("GET /api/v1/config", s.requireAuth(http.HandlerFunc(s.handleGetConfig)))
	mux.Handle("POST /api/v1/auth/token/rotate", s.requireAuth(http.HandlerFunc(s.handleTokenRotate)))
	mux.Handle("GET /api/v1/auth/token/meta", s.requireAuth(http.HandlerFunc(s.handleTokenMeta)))
	mux.Handle("GET /api/v1/logs", s.requireAuth(http.HandlerFunc(s.handleGetLogs)))
	mux.Handle("DELETE /api/v1/logs", s.requireAuth(http.HandlerFunc(s.handleClearLogs)))
	mux.Handle("/", s.staticHandler())

	return mux
}

// Handler 返回带安全响应头的 Handler。
func (s *Server) Handler() http.Handler {
	return securityHeaders(s.Routes())
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// 注意：FNOS 桌面通过 iframe 嵌入本应用，不能发送 X-Frame-Options: DENY，
		// 否则桌面入口显示空白页（真机已验证）。权限由 Token 鉴权把守。
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// ---------- 统一 JSON 响应 ----------

type apiResponse struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, resp apiResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(resp)
}

func writeOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiResponse{Success: false, Error: msg})
}

func clientIP(r *http.Request) string {
	// 仅用于日志展示，不参与任何鉴权决策。
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.SplitN(ip, ",", 2)[0]
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

func (s *Server) audit(r *http.Request, action, result string, detail map[string]any) {
	entry := map[string]any{
		"action":    action,
		"result":    result,
		"client_ip": clientIP(r),
	}
	for k, v := range detail {
		entry[k] = v
	}
	if err := s.store.AppendLog(entry); err != nil {
		s.logf("写入审计日志失败: %v", err)
	}
}

// ---------- 鉴权 ----------

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 首次安装尚未初始化时，除健康检查与初始化接口外一律拒绝
		if !s.store.HasToken() {
			writeJSON(w, http.StatusForbidden, apiResponse{Success: false, Error: "应用尚未初始化，请先完成初始化设置"})
			return
		}
		hdr := r.Header.Get("Authorization")
		token := ""
		if strings.HasPrefix(hdr, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(hdr, "Bearer "))
		}
		if token == "" || !s.store.ValidateToken(token) {
			s.audit(r, "auth", "denied", map[string]any{"path": r.URL.Path})
			writeErr(w, http.StatusUnauthorized, "未提供有效的 Token，请检查 Authorization: Bearer <TOKEN>")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- 健康检查（无需鉴权） ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeOK(w, map[string]any{"app": "fnos-vm-remote", "status": "ok", "version": s.version})
}

// ---------- 首次初始化（无需鉴权，仅限未初始化时） ----------

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	writeOK(w, map[string]any{
		"setup_required": !s.store.HasToken(),
		"version":        s.version,
	})
}

// handleSetup 在首次初始化时由服务端生成高强度 Token，明文仅返回一次。
// 该接口在应用完成初始化后永久失效。
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if s.store.HasToken() {
		writeErr(w, http.StatusConflict, "应用已初始化，如需重置请先卸载并删除数据")
		return
	}
	token, err := config.GenerateToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "生成 Token 失败")
		return
	}
	if err := s.store.SetToken(token); err != nil {
		s.audit(r, "setup.init", "error", map[string]any{"reason": err.Error()})
		writeErr(w, http.StatusInternalServerError, "保存 Token 失败")
		return
	}
	s.audit(r, "setup.init", "ok", nil)
	writeOK(w, map[string]any{
		"new_token": token,
		"message":   "初始 Token 已生成，此明文仅显示这一次，请立即复制并妥善保存",
	})
}

// ---------- 虚拟机状态 ----------

func (s *Server) handleVMStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	st, err := s.vm.Status(ctx)
	if err != nil {
		s.audit(r, "vm.status", "error", map[string]any{"reason": err.Error()})
		writeErr(w, http.StatusBadGateway, "查询虚拟机状态失败："+err.Error())
		return
	}
	writeOK(w, statusPayload(st, time.Now().UTC()))
}

// statusPayload 组装状态响应（含服务端时间，便于前端展示）。
func statusPayload(st vmmgmt.VMStatus, at time.Time) map[string]any {
	return map[string]any{
		"name":       st.Name,
		"uuid":       st.UUID,
		"state":      st.State,
		"raw_state":  st.RawState,
		"updated_at": at.Format(time.RFC3339),
	}
}

// ---------- 开机 ----------

func (s *Server) handleVMStart(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	result, err := s.vm.Start(ctx)
	if err != nil {
		if errors.Is(err, vmmgmt.ErrUnavailable) {
			s.audit(r, "vm.start", "rejected", map[string]any{"reason": result.Message})
			writeJSON(w, http.StatusConflict, apiResponse{Success: false, Error: result.Message, Data: statusPayload(result.Status, time.Now().UTC())})
			return
		}
		s.audit(r, "vm.start", "error", map[string]any{"reason": err.Error()})
		writeErr(w, http.StatusBadGateway, "启动虚拟机失败："+err.Error())
		return
	}
	s.audit(r, "vm.start", "ok", map[string]any{"state": result.Status.State, "started": result.Started, "already": result.Already})
	writeOK(w, map[string]any{
		"status":  statusPayload(result.Status, time.Now().UTC()),
		"started": result.Started,
		"already": result.Already,
		"pending": result.Pending,
		"message": result.Message,
	})
}

// ---------- 正常关机 ----------

func (s *Server) handleVMShutdown(w http.ResponseWriter, r *http.Request) {
	timeout := 120 * time.Second
	if q := r.URL.Query().Get("timeout"); q != "" {
		secs, err := strconv.Atoi(q)
		if err != nil || secs < 5 || secs > 300 {
			writeErr(w, http.StatusBadRequest, "timeout 参数必须为 5—300 之间的秒数")
			return
		}
		timeout = time.Duration(secs) * time.Second
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout+10*time.Second)
	defer cancel()
	result, err := s.vm.Shutdown(ctx, timeout)
	if err != nil {
		if errors.Is(err, vmmgmt.ErrUnavailable) {
			s.audit(r, "vm.shutdown", "rejected", map[string]any{"reason": result.Message})
			writeJSON(w, http.StatusConflict, apiResponse{Success: false, Error: result.Message, Data: statusPayload(result.Status, time.Now().UTC())})
			return
		}
		s.audit(r, "vm.shutdown", "error", map[string]any{"reason": err.Error()})
		writeErr(w, http.StatusBadGateway, "关闭虚拟机失败："+err.Error())
		return
	}
	s.audit(r, "vm.shutdown", "ok", map[string]any{"state": result.Status.State, "sent": result.Sent, "timed_out": result.TimedOut})
	payload := map[string]any{
		"status":    statusPayload(result.Status, time.Now().UTC()),
		"sent":      result.Sent,
		"already":   result.Already,
		"pending":   result.Pending,
		"timed_out": result.TimedOut,
		"message":   result.Message,
	}
	if result.TimedOut {
		// 明确失败，但绝不强制断电
		writeJSON(w, http.StatusOK, apiResponse{Success: false, Data: payload, Message: result.Message})
		return
	}
	writeOK(w, payload)
}

// ---------- 配置信息（不含敏感数据） ----------

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.LoadConfig()
	if err != nil {
		cfg = config.Config{Port: s.port}
	}
	writeOK(w, map[string]any{
		"port":        cfg.Port,
		"listen_port": s.port,
		"version":     s.version,
		"api_base":    "/api/v1",
		"vm":          map[string]string{"name": "3f1jeea2", "uuid": "1ce21ce8-0bd1-4ab3-a9c0-064abf81381f"},
		"setup_done":  s.store.HasToken(),
	})
}

// ---------- Token 管理 ----------

func (s *Server) handleTokenMeta(w http.ResponseWriter, r *http.Request) {
	meta, err := s.store.TokenMeta()
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeOK(w, meta)
}

func (s *Server) handleTokenRotate(w http.ResponseWriter, r *http.Request) {
	token, err := config.GenerateToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "生成 Token 失败")
		return
	}
	if err := s.store.SetToken(token); err != nil {
		s.audit(r, "token.rotate", "error", map[string]any{"reason": err.Error()})
		writeErr(w, http.StatusInternalServerError, "保存 Token 失败")
		return
	}
	s.audit(r, "token.rotate", "ok", nil)
	writeOK(w, map[string]any{
		"new_token": token, // 仅此一次完整返回
		"message":   "Token 已重新生成，此明文仅显示这一次，请立即复制并妥善保存",
	})
}

// ---------- 日志 ----------

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	entries, err := s.store.ReadLogs(limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取日志失败："+err.Error())
		return
	}
	writeOK(w, map[string]any{"entries": entries, "count": len(entries)})
}

func (s *Server) handleClearLogs(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ClearLogs(); err != nil {
		writeErr(w, http.StatusInternalServerError, "清空日志失败："+err.Error())
		return
	}
	s.audit(r, "logs.clear", "ok", nil)
	writeOK(w, map[string]any{"cleared": true})
}
