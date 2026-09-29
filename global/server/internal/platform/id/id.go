// Package id 发放不可复用的稳定身份（UUIDv7），不由名称或路径生成。
package id

import "github.com/google/uuid"

// New 创建新的稳定身份。
func New() uuid.UUID {
	// 按时间序发新的稳定身份。
	v, err := uuid.NewV7()
	// 发号失败不能继续，否则会用上空身份。
	if err != nil {
		// 发号是底座，失败就停住进程。
		panic(err)
	}
	return v
}
