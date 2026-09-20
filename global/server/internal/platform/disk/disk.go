// Package disk 读本机一块盘的已用和剩余，不管 Docker、也不管对象存储。
package disk

import "golang.org/x/sys/unix"

// Space 是某一挂载点的容量。
type Space struct {
	Path  string `json:"path"`  // 统计的挂载点
	Total int64  `json:"total"` // 总容量，字节
	Used  int64  `json:"used"`  // 已用
	Avail int64  `json:"avail"` // 普通用户还能写多少
}

// Of 读 path 所在文件系统的容量；path 空则看根盘。
func Of(path string) (Space, error) {
	if path == "" {
		path = "/"
	}
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return Space{Path: path}, err
	}
	bsize := int64(st.Bsize)
	total := int64(st.Blocks) * bsize
	avail := int64(st.Bavail) * bsize
	free := int64(st.Bfree) * bsize
	return Space{Path: path, Total: total, Used: total - free, Avail: avail}, nil
}
