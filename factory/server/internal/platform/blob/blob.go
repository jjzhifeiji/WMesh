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
	// Put 按键覆盖写入。
	Put(ctx context.Context, key string, body []byte) error
	// Get 按键读出正文。
	Get(ctx context.Context, key string) ([]byte, error)
	// Delete 按键删掉；没有该键也算成功。
	Delete(ctx context.Context, key string) error
	// Usage 合计已存字节和对象数，不含磁盘配额。
	Usage(ctx context.Context) (used, objects int64, err error)
}

// Memory 是测试与本阶段默认用的内存对象存根。
type Memory struct {
	mu   sync.Mutex        // 挡住并发，避免两路同时改这份状态。
	data map[string][]byte // 键到正文的内存表，不落磁盘。
}

// NewMemory 新建空的内存对象存根。
func NewMemory() *Memory {
	return &Memory{data: map[string][]byte{}}
}

// Put 按键覆盖写入；正文拷一份，避免调用方事后改脏。
func (m *Memory) Put(_ context.Context, key string, body []byte) error {
	// 按需要的长度把缓冲准备好。
	cp := make([]byte, len(body))
	// 拷出一份，调用方事后改切片不会弄脏库存。
	copy(cp, body)
	// 先占住锁，避免并发写乱。
	m.mu.Lock()
	// 离开时放开锁，免得把别人堵住。
	defer m.mu.Unlock()
	// 定下这份对象，再交给后面。
	m.data[key] = cp
	return nil
}

// Get 读出该键的正文副本。
func (m *Memory) Get(_ context.Context, key string) ([]byte, error) {
	// 先占住锁，避免并发写乱。
	m.mu.Lock()
	// 离开时放开锁，免得把别人堵住。
	defer m.mu.Unlock()
	// 先当还没找到，找到再改成命中。
	body, ok := m.data[key]
	// 没有命中就走另一路，不用零值冒充有值。
	if !ok {
		return nil, ErrNotFound
	}
	// 按需要的长度把缓冲准备好。
	cp := make([]byte, len(body))
	// 拷出一份，调用方事后改切片不会弄脏库存。
	copy(cp, body)
	return cp, nil
}

// Delete 按键删掉；没有该键也算成功。
func (m *Memory) Delete(_ context.Context, key string) error {
	// 先占住锁，避免并发写乱。
	m.mu.Lock()
	// 离开时放开锁，免得把别人堵住。
	defer m.mu.Unlock()
	// 从这份集合里拿掉这一项。
	delete(m.data, key)
	return nil
}

// Usage 合计内存里的字节和份数。
func (m *Memory) Usage(_ context.Context) (used, objects int64, err error) {
	// 先占住锁，避免并发写乱。
	m.mu.Lock()
	// 离开时放开锁，免得把别人堵住。
	defer m.mu.Unlock()
	// 逐项处理，空的就不进入循环。
	for _, body := range m.data {
		// 把这个值定下来，后面的判断才有依据。
		objects++
		// 看有多长，空的和超限的要分开处理。
		used += int64(len(body))
	}
	return used, objects, nil
}
