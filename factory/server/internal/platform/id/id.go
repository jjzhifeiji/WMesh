// Package id 发放不可复用的稳定身份（UUIDv7），不由名称或路径生成。
package id

import "github.com/google/uuid"

// New 创建新的稳定身份。
func New() uuid.UUID {
	// 发一个新的稳定身份。
	v, err := uuid.NewV7()
	// 没能发一个新的稳定身份就停，避免带着残缺继续。
	if err != nil {
		// 发号失败只能中断进程，不能交回空身份。
		panic(err)
	}
	return v
}
