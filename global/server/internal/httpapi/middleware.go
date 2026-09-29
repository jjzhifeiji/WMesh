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
	// 包住下游，记下状态，崩溃也要写日志。
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 记下开始时刻，结束时算这次耗时。
		start := time.Now()
		// 包一层以便记下状态码，不改响应正文。
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		// 请求结束时记访问日志，崩溃也要兜住。
		defer func() {
			// 崩溃要记栈，并且尽量仍回笼统失败。
			if rec := recover(); rec != nil {
				// 留下崩溃栈，只进日志不出网。
				log.Error("panic", "method", r.Method, "path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
				// 还没写出才补笼统失败，避免写出两份头。
				if !sw.wrote {
					// 把结果交回调用方。
					writeJSON(sw, http.StatusInternalServerError, errorBody{Error: "internal error"})
				}
			}
			// 默认按普通访问记，探活和静态再降。
			level := slog.LevelInfo
			// 按路径和状态决定这条访问记多响。
			switch {
			// 探活不写访问日志，避免刷屏。
			case r.URL.Path == "/healthz":
				return
			// 静态资源降到调试，避免把访问日志刷满。
			case strings.HasPrefix(r.URL.Path, "/assets/"):
				// 静态资源降到调试，避免刷屏。
				level = slog.LevelDebug
			// 服务端失败升到错误，方便事后排查。
			case sw.status >= http.StatusInternalServerError:
				// 服务端失败升到错误，方便排查。
				level = slog.LevelError
			}
			// 按级别记这一次访问，带上耗时和身份。
			log.Log(r.Context(), level, "http", nodeLogAttrs(r, sw.status, time.Since(start).Milliseconds())...)
		}()
		// 交给下游处理，状态由包装层记下。
		next.ServeHTTP(sw, r)
	})
}

// nodeLogAttrs 访问日志带上工厂或本机身份，便于按节点查。
func nodeLogAttrs(r *http.Request, status int, ms int64) []any {
	// 访问日志先记方法、路径和耗时。
	attrs := []any{
		"method", r.Method, "path", r.URL.Path, "status", status,
		"ms", ms, "remote", r.RemoteAddr,
	}
	// 留下路径，用来判断是厂还是现场设备。
	path := r.URL.Path
	// 路径里有身份才补进日志，没有就算了。
	if v := r.PathValue("id"); v != "" {
		// 按路径补上工厂或现场设备身份。
		switch {
		// 路径里是工厂，日志带上工厂身份。
		case strings.Contains(path, "/factories/"):
			// 日志补上工厂身份，便于按厂追查。
			attrs = append(attrs, "factory", v)
		// 路径里是现场设备，日志带上设备身份。
		case strings.Contains(path, "/clients/"):
			// 日志补上现场设备，便于按机追查。
			attrs = append(attrs, "client", v)
		}
	}
	return attrs
}

// statusWriter 只为记下响应码，不改响应内容。
type statusWriter struct {
	http.ResponseWriter      // 真正写出响应的底层，这里只包一层。
	status              int  // 已经写下的状态码，默认按成功。
	wrote               bool // 是否已经写过头或正文，避免重复写。
}

// 记下状态码再写出。
func (w *statusWriter) WriteHeader(code int) {
	// 还没写过才记状态，避免被后一次覆盖。
	if !w.wrote {
		// 记下真正的状态码，供访问日志使用。
		w.status = code
		// 标记已经写出，崩溃后不要再写一次头。
		w.wrote = true
	}
	// 状态记下之后再交给真正的写出。
	w.ResponseWriter.WriteHeader(code)
}

// 记下已写再转发。
func (w *statusWriter) Write(b []byte) (int, error) {
	// 标记已经写出，崩溃后不要再写一次头。
	w.wrote = true
	// 正文交给底层，字数回给调用方。
	return w.ResponseWriter.Write(b)
}

// 交给底层 writer。
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// 把套接字交给通道升级；不支持则握手失败。
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	// 看底层能否把连接交出去升级通道。
	h, ok := w.ResponseWriter.(http.Hijacker)
	// 底层不支持劫持，通道升级会失败。
	if !ok {
		// 不能升级就失败，不假装已经劫持。
		return nil, nil, errors.New("hijack not supported")
	}
	// 把连接交给通道升级。
	return h.Hijack()
}

// 立刻刷出缓冲，给心跳用。
func (w *statusWriter) Flush() {
	// 支持才立刻刷出，心跳才不会堵在缓冲里。
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		// 立刻刷出缓冲，心跳才不会被攒住。
		f.Flush()
	}
}
