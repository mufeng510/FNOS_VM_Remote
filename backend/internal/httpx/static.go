package httpx

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed web
var webFS embed.FS

// staticHandler 提供内嵌的 Web 管理界面。
// 页面本身不含敏感数据；所有数据均通过需鉴权的 API 获取。
func (s *Server) staticHandler() http.Handler {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := r.URL.Path
		if !strings.HasPrefix(upath, "/") {
			upath = "/" + upath
		}
		clean := path.Clean(upath)
		if clean == "/" || clean == "." {
			serveIndex(w, r, sub)
			return
		}
		name := strings.TrimPrefix(clean, "/")
		// /api/* 未匹配的路径一律返回 JSON 404，避免静态回退吞掉 API 语义
		if strings.HasPrefix(name, "api/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"API 路径不存在"}`))
			return
		}
		if f, err := sub.Open(name); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// 未匹配的路径回退到首页（SPA 兜底），但不暴露目录结构
		serveIndex(w, r, sub)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "index.html 缺失", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}
