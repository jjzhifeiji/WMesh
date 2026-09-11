// Package blob 是进程内对象存根：按键存取字节，不决定谁能传什么。
package blob

import (
	"context"
	"errors"
	"sync"
)

// ErrNotFound 表示该键没有对象。
var ErrNotFound = errors.New("blob not found")

// Store 按键存取不透明正文。
type Store interface {
	Put(ctx context.Context, key string, body []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
}

// Memory 是测试与本阶段默认用的内存对象存根。
type Memory struct {
	mu   sync.Mutex
	data map[string][]byte
}

// NewMemory 新建空的内存对象存根。
func NewMemory() *Memory {
	return &Memory{data: map[string][]byte{}}
}

// Put 按键覆盖写入；正文拷一份，避免调用方事后改脏。
func (m *Memory) Put(_ context.Context, key string, body []byte) error {
	cp := make([]byte, len(body))
	copy(cp, body)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = cp
	return nil
}

// Get 读出该键的正文副本。
func (m *Memory) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	body, ok := m.data[key]
	if !ok {
		return nil, ErrNotFound
	}
	cp := make([]byte, len(body))
	copy(cp, body)
	return cp, nil
}
