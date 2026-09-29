package disk

import "testing"

// 根盘容量应能读到，而且不能是负数。
func TestOfRoot(t *testing.T) {
	// 读这块盘现在的容量。
	got, err := Of("/")
	// 没能读这块盘现在的容量就停住本用例。
	if err != nil {
		// 没能读这块盘现在的容量就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if got.Total <= 0 || got.Avail < 0 || got.Used < 0 {
		// 和预期不符就停住本用例。
		t.Fatalf("%+v", got)
	}
}
