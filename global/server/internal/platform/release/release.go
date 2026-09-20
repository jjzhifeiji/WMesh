// Package release 管本进程版本号和版本名。不管构建串，也不管软件更新包。
package release

const (
	Code int64 = 22       // 版本号，只向前比较
	Name       = "1.0.21" // 给人看的版本名
)
