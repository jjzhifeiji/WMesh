package disk

import "testing"

// 根盘必须能读出非负容量，且已用加剩余不超过总量。
func TestOfRoot(t *testing.T) {
	// 读根盘，确认这台机器问得到容量。
	got, err := Of("/")
	// 读失败则本用例不成立。
	if err != nil {
		// 读失败就中止，避免后面误判。
		t.Fatal(err)
	}
	// 总量必须为正，已用和剩余不能为负。
	if got.Total <= 0 || got.Avail < 0 || got.Used < 0 {
		// 数字不合理就中止并带上原值。
		t.Fatalf("%+v", got)
	}
	// 已用加剩余不应超过总容量。
	if got.Used+got.Avail > got.Total {
		// 超过总容量就中止并带上原值。
		t.Fatalf("used+avail > total %+v", got)
	}
}
