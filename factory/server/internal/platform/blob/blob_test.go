package blob

import (
	"context"
	"testing"
)

// 写入后用量要涨，删掉之后回到空。
func TestMemoryUsage(t *testing.T) {
	// 建一个空的内存对象表。
	m := NewMemory()
	// 拿出不会被取消的上下文。
	ctx := context.Background()
	// 没能按键覆盖写进一份字节就停住本用例。
	if err := m.Put(ctx, "a", []byte("hello")); err != nil {
		// 没能按键覆盖写进一份字节就停住本用例。
		t.Fatal(err)
	}
	// 合计已经存下的字节和个数。
	used, objects, err := m.Usage(ctx)
	// 出错或结果对不上就停住本用例。
	if err != nil || used != 5 || objects != 1 {
		// 结果和预期不符就停住。
		t.Fatalf("got %d %d %v", used, objects, err)
	}
}
