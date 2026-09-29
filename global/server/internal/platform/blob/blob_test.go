package blob

import (
	"context"
	"testing"
)

// 空库存应是零，写入两笔后字节和份数要对上。
func TestMemoryUsage(t *testing.T) {
	// 用一份空的内存存根。
	m := NewMemory()
	// 测试不取消，上下文只为满足接口。
	ctx := context.Background()
	// 空库应读出零字节、零份。
	used, objects, err := m.Usage(ctx)
	// 空库对不上就中止。
	if err != nil || used != 0 || objects != 0 {
		// 数字不对就中止并带上原值。
		t.Fatalf("empty %d %d %v", used, objects, err)
	}
	// 先写入五字节的第一笔。
	if err := m.Put(ctx, "a", []byte("hello")); err != nil {
		// 写入失败就中止本用例。
		t.Fatal(err)
	}
	// 再写入两字节的第二笔。
	if err := m.Put(ctx, "b", []byte("!!")); err != nil {
		// 写入失败就中止本用例。
		t.Fatal(err)
	}
	// 两笔合计应是七字节、两份。
	used, objects, err = m.Usage(ctx)
	// 合计对不上就中止。
	if err != nil || used != 7 || objects != 2 {
		// 数字不对就中止并带上原值。
		t.Fatalf("got %d %d %v", used, objects, err)
	}
}
