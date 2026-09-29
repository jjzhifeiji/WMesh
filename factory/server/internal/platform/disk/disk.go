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
	// 路径空着就改去看根盘。
	if path == "" {
		// 路径空着就改去看根盘。
		path = "/"
	}
	// 准备放下状态，再交给后面。
	var st unix.Statfs_t
	// 没能读这块文件系统的容量就停，避免带着残缺继续。
	if err := unix.Statfs(path, &st); err != nil {
		return Space{Path: path}, err
	}
	// 把块大小收成整数，才能换算成字节。
	bsize := int64(st.Bsize)
	// 把总块数收成整数，用来算总容量。
	total := int64(st.Blocks) * bsize
	// 把可用块数收成整数，用来算还能写多少。
	avail := int64(st.Bavail) * bsize
	// 把空闲块数收成整数，用来算未占用。
	free := int64(st.Bfree) * bsize
	return Space{Path: path, Total: total, Used: total - free, Avail: avail}, nil
}
