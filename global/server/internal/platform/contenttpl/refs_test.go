// 工艺引用：按模版收集、改写身份，不扫正文里多出来的键。
package contenttpl

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

// 收一份默认工程模版，失败行指到调用用例。
func projectSchema(t *testing.T) []byte {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 先把模版收成规范正文。
	raw, err := Marshal(Default(KindProject))
	// 收不成规范正文就停。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	return raw
}

// 三处引用去重收集；非法正文和数字身份要分开。
func TestCollectProcessIDs(t *testing.T) {
	// 拼一份只覆盖这一处的模版。
	sch := projectSchema(t)
	// 准备一段工程正文。
	raw := []byte(`[
		{"kind":"single","processId":"A","extraProcesses":[{"id":"x","processId":"C"}]},
		{"kind":"multi","basePath":{"processId":"A"},"passes":[{"processId":"B"}]}
	]`)
	// 按模版把工艺引用收出来。
	ids, err := CollectProcessIDs(sch, raw)
	// 收集失败则引用不可信。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 拷一份再排序，不改收集结果。
	got := append([]string(nil), ids...)
	// 排好序再比，避免顺序误伤。
	slices.Sort(got)
	// 收集到的身份必须是这三份。
	if !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Fatalf("%v", ids)
	}
	// 按模版把工艺引用收出来。
	ids, err = CollectProcessIDs(sch, []byte("job"))
	// 结果或错误对不上就中止。
	if err != nil || len(ids) != 0 {
		// 对不上就中止并带上原值。
		t.Fatalf("%v %v", ids, err)
	}
	// 错误种类必须对上这一条。
	if _, err := CollectProcessIDs(sch, []byte(`[{"processId":1}]`)); !errors.Is(err, errBadProcessID) {
		// 对不上就中止并带上原值。
		t.Fatalf("got %v", err)
	}
}

// 路径键无论在哪一层都要拒绝。
func TestRejectProcessPath(t *testing.T) {
	// 拼一份只覆盖这一处的模版。
	sch := projectSchema(t)
	// 错误种类必须对上这一条。
	if _, err := CollectProcessIDs(sch, []byte(`[{"processId":"A","processPath":"x.json"}]`)); !errors.Is(err, ErrProcessPath) {
		// 对不上就中止并带上原值。
		t.Fatalf("got %v", err)
	}
	// 本该通过的检查失败了。
	if err := RejectProcessPath([]byte(`{"basePath":{"processPath":""}}`)); !errors.Is(err, ErrProcessPath) {
		// 对不上就中止并带上原值。
		t.Fatalf("got %v", err)
	}
	// 本该通过的检查失败了。
	if err := RejectProcessPath([]byte(`[{"processId":"A"}]`)); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
}

// 没选的种类里面的引用不要收进来。
func TestCollectSkipsUnselectedKind(t *testing.T) {
	// 先把模版收成规范正文。
	sch, err := Marshal(Schema{Root: RootArray, Kinds: []string{ItemSingle}})
	// 收不成规范正文就停。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 按模版把工艺引用收出来。
	ids, err := CollectProcessIDs(sch, []byte(`[{"processId":"KEEP","extraProcesses":[{"processId":"SKIP"}],"basePath":{"processId":"SKIP2"}}]`))
	// 结果或错误对不上就中止。
	if err != nil || len(ids) != 1 || ids[0] != "KEEP" {
		// 对不上就中止并带上原值。
		t.Fatalf("%v %v", ids, err)
	}
}

// 只收模版走到的那一处引用。
func TestCollectFollowsSchema(t *testing.T) {
	// 先把模版收成规范正文。
	sch, err := Marshal(Schema{Root: RootArray, Item: &Field{Type: TypeObject, Label: "焊缝", Fields: []Field{
		{Key: "alt", Label: "另", Type: TypeObject, Fields: []Field{str("processId", "工艺", "")}},
	}}})
	// 这一步失败则引用不可信。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 按模版把工艺引用收出来。
	ids, err := CollectProcessIDs(sch, []byte(`[{"processId":"SKIP","alt":{"processId":"KEEP"}}]`))
	// 结果或错误对不上就中止。
	if err != nil || len(ids) != 1 || ids[0] != "KEEP" {
		// 对不上就中止并带上原值。
		t.Fatalf("%v %v", ids, err)
	}
}

// 按工艺类型收，同名文本键不算引用。
func TestCollectFollowsProcessType(t *testing.T) {
	// 先把模版收成规范正文。
	sch, err := Marshal(Schema{Root: RootArray, Item: &Field{Type: TypeObject, Label: "焊缝", Fields: []Field{
		{Key: "foo", Label: "工艺", Type: TypeProcess, Default: ""},
	}}})
	// 这一步失败则引用不可信。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 按模版把工艺引用收出来。
	ids, err := CollectProcessIDs(sch, []byte(`[{"foo":"KEEP","processId":"SKIP"}]`))
	// 结果或错误对不上就中止。
	if err != nil || len(ids) != 1 || ids[0] != "KEEP" {
		// 对不上就中止并带上原值。
		t.Fatalf("%v %v", ids, err)
	}
}

// 只改模版走到的引用，旁边的键不动。
func TestRewriteFollowsSchema(t *testing.T) {
	// 先把模版收成规范正文。
	sch, err := Marshal(Schema{Root: RootArray, Item: &Field{Type: TypeObject, Label: "焊缝", Fields: []Field{
		{Key: "alt", Label: "另", Type: TypeObject, Fields: []Field{str("processId", "工艺", "")}},
	}}})
	// 这一步失败则引用不可信。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 准备一段工程正文。
	raw := []byte(`[{"processId":"SKIP","alt":{"processId":"KEEP"}}]`)
	// 按对照表改写工艺身份。
	out, err := RewriteProcessIDs(sch, raw, map[string]string{"KEEP": "NEXT"})
	// 改写失败则不能核对结果。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 准备接套开之后的结构。
	var welds []map[string]any
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(out, &welds); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 结果和预期不一致就中止。
	if welds[0]["processId"] != "SKIP" {
		// 对不上就中止并带上原值。
		t.Fatalf("template-extra %s", out)
	}
	// 钻进这一层再核对。
	alt := welds[0]["alt"].(map[string]any)
	// 结果和预期不一致就中止。
	if alt["processId"] != "NEXT" {
		// 对不上就中止并带上原值。
		t.Fatalf("alt %v", alt)
	}
}

// 对照表里的身份换掉，没有的就失败。
func TestRewriteProcessIDs(t *testing.T) {
	// 拼一份只覆盖这一处的模版。
	sch := projectSchema(t)
	// 准备一段工程正文。
	raw := []byte(`[{"kind":"multi","name":"w","keep":true,"basePath":{"processId":"fac-1"},"passes":[{"processId":"fac-1"}]}]`)
	// 按对照表改写工艺身份。
	out, err := RewriteProcessIDs(sch, raw, map[string]string{"fac-1": "plat-1"})
	// 改写失败则不能核对结果。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 准备接套开之后的结构。
	var welds []map[string]any
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(out, &welds); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 结果和预期不一致就中止。
	if welds[0]["keep"] != true {
		// 对不上就中止并带上原值。
		t.Fatalf("%s", out)
	}
	// 钻进这一层再核对。
	base, _ := welds[0]["basePath"].(map[string]any)
	// 结果和预期不一致就中止。
	if base["processId"] != "plat-1" {
		// 对不上就中止并带上原值。
		t.Fatalf("base %v", base)
	}
	// 钻进这一层再核对。
	passes, _ := welds[0]["passes"].([]any)
	// 钻进这一层再核对。
	p0 := passes[0].(map[string]any)
	// 结果和预期不一致就中止。
	if p0["processId"] != "plat-1" {
		// 对不上就中止并带上原值。
		t.Fatalf("pass %v", p0)
	}
	// 按对照表改写工艺身份。
	same, err := RewriteProcessIDs(sch, []byte("job"), map[string]string{"fac-1": "plat-1"})
	// 结果或错误对不上就中止。
	if err != nil || string(same) != "job" {
		// 对不上就中止并带上原值。
		t.Fatalf("%s %v", same, err)
	}
	// 错误种类必须对上这一条。
	if _, err := RewriteProcessIDs(sch, raw, map[string]string{"other": "plat-1"}); !errors.Is(err, errUnknownProcessID) {
		t.Fatalf("got %v", err)
	}
}
