package httpapi

import (
	"bufio"
	"errors"
	"log/slog"
	"net"
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
			case r.URL.Path == "/healthz":
				return
			case strings.HasPrefix(r.URL.Path, "/assets/"):
				level = slog.LevelDebug
			case sw.status >= http.StatusInternalServerError:
				level = slog.LevelError
			}
			log.Log(r.Context(), level, "http",
				"method", r.Method, "path", r.URL.Path, "status", sw.status,
				"ms", time.Since(start).Milliseconds(), "remote", r.RemoteAddr)
		}()
		next.ServeHTTP(sw, r)
	})
}

// statusWriter 只为记下响应码，不改响应内容。
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

// 记下状态码再写出。
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

// 交给底层 writer。
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// 把套接字交给通道升级；不支持则握手失败。
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijack not supported")
	}
	return h.Hijack()
}

// 立刻刷出缓冲，给心跳用。
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
