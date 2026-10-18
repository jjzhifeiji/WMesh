// Package release 管本进程版本号和版本名。不管构建串，也不管 WAN 下发的软件包。
package release

const (
	Code    int64 = 29       // 服务版本号，只向前比较
	Name          = "1.1.27" // 服务版本名
	WebCode int64 = 29       // 前端版本号，与 frontend/src/shared/version.ts 对齐
	WebName       = "1.1.27" // 前端版本名
)
