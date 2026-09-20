package blob

import (
	"context"
	"testing"
)

func TestMemoryUsage(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	used, objects, err := m.Usage(ctx)
	if err != nil || used != 0 || objects != 0 {
		t.Fatalf("empty %d %d %v", used, objects, err)
	}
	if err := m.Put(ctx, "a", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := m.Put(ctx, "b", []byte("!!")); err != nil {
		t.Fatal(err)
	}
	used, objects, err = m.Usage(ctx)
	if err != nil || used != 7 || objects != 2 {
		t.Fatalf("got %d %d %v", used, objects, err)
	}
}
