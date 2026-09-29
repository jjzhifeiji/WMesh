// Package web 托管已构建的管理端静态页；不做业务，也不碰会话。
package web

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// Handler 从目录提供静态资源；找不到的路径回落到 index.html，交给前端路由处理。
func Handler(dir string) http.Handler {
	// 把目录当只读文件树，避免页面代码直接碰磁盘。
	fsys := os.DirFS(dir)
	// 命中真实文件时按静态资源送出。
	files := http.FileServerFS(fsys)
	// 找不到文件就回落首页，交给前端路由。
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 只接受读取，写入直接拒绝。
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// 非读请求回拒绝，避免被当成接口调用。
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// 去掉越界路径，防止读到目录外面。
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		// 根路径没有文件名，改送首页。
		if name == "" {
			// 空路径没有具体文件，改落入口页。
			name = "index.html"
		}
		// 磁盘上有且不是目录才当静态文件。
		if st, err := fs.Stat(fsys, name); err == nil && !st.IsDir() {
			// 带哈希的构建产物长期缓存；其余文件每次回源核对。
			if strings.HasPrefix(name, "assets/") {
				// 带哈希的产物可以长期缓存。
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else { // 入口和普通文件不长期缓存，避免旧壳套新脚本。
				// 每次回源核对，避免浏览器钉住旧页。
				w.Header().Set("Cache-Control", "no-cache")
			}
			// 命中的文件按原样送出。
			files.ServeHTTP(w, r)
			return
		}
		// 未命中也不缓存，避免把回落页钉死。
		w.Header().Set("Cache-Control", "no-cache")
		// 其余路径回落首页，由前端路由接住。
		http.ServeFileFS(w, r, fsys, "index.html")
	})
}
