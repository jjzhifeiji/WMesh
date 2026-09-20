package blob

import (
	"context"
	"testing"
)

func TestMemoryUsage(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	if err := m.Put(ctx, "a", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	used, objects, err := m.Usage(ctx)
	if err != nil || used != 5 || objects != 1 {
		t.Fatalf("got %d %d %v", used, objects, err)
	}
}
