package httpapi

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// Wrap 给整棵路由加上 panic 兜底和一行访问日志；探活与静态资源不刷屏。
func Wrap(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic", "method", r.Method, "path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
				if !sw.wrote {
					writeJSON(sw, http.StatusInternalServerError, errorBody{Error: "internal error"})
				}
			}
			level := slog.LevelInfo
			switch {
			case r.URL.Path == "/healthz", r.URL.Path == "/v1/discover":
				return
			case strings.HasPrefix(r.URL.Path, "/assets/"):
				level = slog.LevelDebug
			case sw.status >= http.StatusInternalServerError:
				level = slog.LevelError
			}
			log.Log(r.Context(), level, "http", nodeLogAttrs(r, sw.status, time.Since(start).Milliseconds())...)
		}()
		next.ServeHTTP(sw, r)
	})
}

// nodeLogAttrs 访问日志带上工厂和本机身份，便于按节点查。
func nodeLogAttrs(r *http.Request, status int, ms int64) []any {
	attrs := []any{
		"method", r.Method, "path", r.URL.Path, "status", status,
		"ms", ms, "remote", r.RemoteAddr,
	}
	if v := r.PathValue("id"); v != "" {
		attrs = append(attrs, "factory", v)
	}
	if v := r.PathValue("clientId"); v != "" {
		attrs = append(attrs, "client", v)
	}
	return attrs
}

// statusWriter 只为记下响应码，不改响应内容。
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

// 记下状态码再转发。
func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

// 记下已写再转发。
func (w *statusWriter) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}
