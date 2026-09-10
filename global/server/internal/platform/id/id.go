// Package id 发放不可复用的稳定身份（UUIDv7），不由名称或路径生成。
package id

import "github.com/google/uuid"

// New 创建新的稳定身份。
func New() uuid.UUID {
	v, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return v
}
