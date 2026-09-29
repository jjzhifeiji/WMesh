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
	mu   sync.Mutex        // 保护下面的表，避免并发写坏
	data map[string][]byte // 按键存放的正文副本
}

// NewMemory 新建空的内存对象存根。
func NewMemory() *Memory {
	return &Memory{data: map[string][]byte{}}
}

// Put 按键覆盖写入；正文拷一份，避免调用方事后改脏。
func (m *Memory) Put(_ context.Context, key string, body []byte) error {
	// 先拷一份，调用方事后改不到库存。
	cp := make([]byte, len(body))
	// 把正文抄进这份副本。
	copy(cp, body)
	// 写表前先独占，避免并发写坏。
	m.mu.Lock()
	// 写完就放开，含提前返回。
	defer m.mu.Unlock()
	// 按键覆盖成这份副本。
	m.data[key] = cp
	return nil
}

// Get 读出该键的正文副本。
func (m *Memory) Get(_ context.Context, key string) ([]byte, error) {
	// 读表前先独占，避免读到写了一半的值。
	m.mu.Lock()
	// 读完就放开，含提前返回。
	defer m.mu.Unlock()
	// 按键取出库存正文。
	body, ok := m.data[key]
	// 没有该键就明确告诉调用方。
	if !ok {
		return nil, ErrNotFound
	}
	// 再拷一份交回，调用方改不到库存。
	cp := make([]byte, len(body))
	// 把库存抄进这份副本。
	copy(cp, body)
	return cp, nil
}

// Delete 按键删掉；没有该键也算成功。
func (m *Memory) Delete(_ context.Context, key string) error {
	// 删表前先独占，避免和写入交错。
	m.mu.Lock()
	// 删完就放开这把锁。
	defer m.mu.Unlock()
	// 没有该键也当成功，删除可重复。
	delete(m.data, key)
	return nil
}

// Usage 合计内存里的字节和份数。
func (m *Memory) Usage(_ context.Context) (used, objects int64, err error) {
	// 统计前先独占，避免数到一半被改。
	m.mu.Lock()
	// 数完就放开这把锁。
	defer m.mu.Unlock()
	// 每份正文都计入字节和份数。
	for _, body := range m.data {
		// 这一份计入对象数。
		objects++
		// 这份字节数累进已用。
		used += int64(len(body))
	}
	return used, objects, nil
}
