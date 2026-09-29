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
	// 包住后续处理，记下状态并兜住崩溃。
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 记下开始时刻，用来计算这次耗时。
		start := time.Now()
		// 包一层只为记下状态码，不改响应正文。
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		// 返回前记访问日志，崩溃则回笼统错误。
		defer func() {
			// 抓住崩溃并记下栈，尽量回一个笼统错误。
			if rec := recover(); rec != nil {
				// 崩溃只记日志，响应里不带栈。
				log.Error("panic", "method", r.Method, "path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
				// 已经写过响应就不再补头，避免重复写。
				if !sw.wrote {
					// 通过之后把结果回给调用方。
					writeJSON(sw, http.StatusInternalServerError, errorBody{Error: "internal error"})
				}
			}
			// 默认按普通级别记录，探活和静态再降。
			level := slog.LevelInfo
			// 按路径和结果决定这条访问怎么记。
			switch {
			// 探活和发现不记访问日志，避免刷屏。
			case r.URL.Path == "/healthz", r.URL.Path == "/v1/discover":
				return
			// 静态资源降到调试级，普通访问仍保留。
			case strings.HasPrefix(r.URL.Path, "/assets/"):
				// 静态资源降到调试，避免日志刷屏。
				level = slog.LevelDebug
			// 服务端失败升到错误级，便于事后排查。
			case sw.status >= http.StatusInternalServerError:
				// 服务端失败升到错误级，便于排查。
				level = slog.LevelError
			}
			// 按刚才定的级别写一行访问日志。
			log.Log(r.Context(), level, "http", nodeLogAttrs(r, sw.status, time.Since(start).Milliseconds())...)
		}()
		// 交给后面的路由，状态由这一层记下。
		next.ServeHTTP(sw, r)
	})
}

// nodeLogAttrs 访问日志带上工厂和本机身份，便于按节点查。
func nodeLogAttrs(r *http.Request, status int, ms int64) []any {
	// 先记下方法、路径、状态和耗时。
	attrs := []any{
		"method", r.Method, "path", r.URL.Path, "status", status,
		"ms", ms, "remote", r.RemoteAddr,
	}
	// 路径里有工厂才写进日志，便于按厂追查。
	if v := r.PathValue("id"); v != "" {
		// 补上工厂或设备，便于按它们查日志。
		attrs = append(attrs, "factory", v)
	}
	// 路径里有设备才写进日志，便于按台追查。
	if v := r.PathValue("clientId"); v != "" {
		// 补上工厂或设备，便于按它们查日志。
		attrs = append(attrs, "client", v)
	}
	return attrs
}

// statusWriter 只为记下响应码，不改响应内容。
type statusWriter struct {
	http.ResponseWriter      // 底层响应，外层只额外记下状态
	status              int  // 已经写出的状态码
	wrote               bool // 是否已经开始写响应
}

// 记下状态码再转发。
func (w *statusWriter) WriteHeader(code int) {
	// 第一次写状态才记下，避免被后面覆盖。
	if !w.wrote {
		// 记下这次真正写出的状态码。
		w.status = code
		// 标记状态已经写出，避免后面再改码。
		w.wrote = true
	}
	// 先落下状态码，再写正文。
	w.ResponseWriter.WriteHeader(code)
}

// 记下已写再转发。
func (w *statusWriter) Write(b []byte) (int, error) {
	// 写了正文也算已经响应，崩溃时不再补头。
	w.wrote = true
	// 把这一步的结果交回调用方。
	return w.ResponseWriter.Write(b)
}

// Unwrap 让 ServeContent 的 Range/Flush 落到真实连接。
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Flush 大包边写边推，避免缓冲到超时。
func (w *statusWriter) Flush() {
	// 底层能推送才立刻刷出，免得大包被缓冲拖住。
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		// 把已经写下的内容推出去，避免一直攒着。
		f.Flush()
	}
}
