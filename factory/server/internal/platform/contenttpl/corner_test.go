package contenttpl

import (
	"slices"
	"testing"
)

// 只认顶层 kind 为 cornerGeometry 的正文是包角工艺。
func TestIsCornerProcess(t *testing.T) {
	// 各种正文和期望。
	cases := []struct {
		body string // 工艺正文
		want bool   // 是不是包角
	}{
		{`{"kind":"cornerGeometry","name":"包角","passes":[{"length":5}]}`, true},
		{` {"name":"a","kind":"cornerGeometry"} `, true},
		{`{"current":170,"voltage":20}`, false},
		{`{"kind":"process"}`, false},
		{`{"kind":1}`, false},
		{`{"oscillation":{"kind":"cornerGeometry"}}`, false},
		{`[{"kind":"cornerGeometry"}]`, false},
		{`not json`, false},
		{``, false},
	}
	// 逐条核对。
	for _, c := range cases {
		// 和期望不符就报。
		if got := IsCornerProcess([]byte(c.body)); got != c.want {
			t.Errorf("%q => %v", c.body, got)
		}
	}
}

// 包角组 processId 和焊道一样算工艺引用，和示教器存法一致：两处挂同一条焊接参数。
func TestCornerGroupRefCollected(t *testing.T) {
	// 两条单层带同一包角组，组上 processId 和焊道相同；空的不算引用。
	raw := []byte(`[
		{"templateId":"` + SeedTplSingle + `","kind":"single","processId":"A","cornerGroupParams":{"groupId":"g","isMaster":true,"processId":"A"}},
		{"templateId":"` + SeedTplSingle + `","kind":"single","processId":"B","cornerGroupParams":{"groupId":"g","processId":"C"}},
		{"templateId":"` + SeedTplSingle + `","kind":"single","processId":"","cornerGroupParams":{"groupId":"h","processId":""}}
	]`)
	// 按空库三份收引用，包角组上的也要收进来。
	ids, err := CollectProcessIDsFromItems(SeedProjectItems(), raw)
	// 收不到或漏了包角组上的 C 就失败。
	if err != nil || !slices.Equal(ids, []string{"A", "B", "C"}) {
		t.Fatalf("ids %v %v", ids, err)
	}
}

// 任一工艺槽（含包角组）挂包角工艺就算挂错；认不出的身份不管。
func TestHasCornerProcess(t *testing.T) {
	// A、B 普通，C 包角，X 认不出。
	kinds := map[string]bool{"A": false, "B": false, "C": true}
	// 按表查。
	look := func(id string) (bool, bool) {
		// 表里有才认得出。
		v, ok := kinds[id]
		return v, ok
	}
	// 各种挂法和期望。
	cases := []struct {
		ids  []string // 工程里收到的引用
		want bool     // 挂错没有
	}{
		{[]string{"A", "B"}, false},
		{[]string{"A", "C"}, true},
		{[]string{"C"}, true},
		{[]string{"X"}, false},
		{nil, false},
	}
	// 逐条核对。
	for _, c := range cases {
		// 和期望不符就报。
		if got := HasCornerProcess(c.ids, look); got != c.want {
			t.Errorf("%v => %v", c.ids, got)
		}
	}
}
