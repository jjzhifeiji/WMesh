// 工艺引用：按模版收集、改写身份，不扫正文里多出来的键。
package contenttpl

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

// 读出测试用的工程字段表。
func projectSchema(t *testing.T) []byte {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 取出默认的那一份配置或字段。
	raw, err := Marshal(Default(KindProject))
	// 没能取出默认的那一份配置或字段就停住本用例。
	if err != nil {
		// 没能取出默认的那一份配置或字段就停住本用例。
		t.Fatal(err)
	}
	return raw
}

// 按模版收集非空工艺身份，多余的键不算。
func TestCollectProcessIDs(t *testing.T) {
	// 读出测试要用的工程字段表。
	sch := projectSchema(t)
	// 准备一份测试或签名用的字节。
	raw := []byte(`[
		{"kind":"single","processId":"A","extraProcesses":[{"id":"x","processId":"C"}]},
		{"kind":"multi","basePath":{"processId":"A"},"passes":[{"processId":"B"}]}
	]`)
	// 按模版收集工艺身份。
	ids, err := CollectProcessIDs(sch, raw)
	// 没能按模版收集工艺身份就停住本用例。
	if err != nil {
		// 没能按模版收集工艺身份就停住本用例。
		t.Fatal(err)
	}
	// 把这一段接进结果，顺序要保持住。
	got := append([]string(nil), ids...)
	// 做完这一步，再交给后面。
	slices.Sort(got)
	// 结果和预期不符就进入失败。
	if !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Fatalf("%v", ids)
	}
	// 准备一份测试或签名用的字节。
	ids, err = CollectProcessIDs(sch, []byte("job"))
	// 出错或结果对不上就停住本用例。
	if err != nil || len(ids) != 0 {
		// 按模版收集工艺身份和预期不符就停住。
		t.Fatalf("%v %v", ids, err)
	}
	// 工艺身份和预期不符就进入失败。
	if _, err := CollectProcessIDs(sch, []byte(`[{"processId":1}]`)); !errors.Is(err, errBadProcessID) {
		// 结果和预期不符就停住。
		t.Fatalf("got %v", err)
	}
}

// 正文里出现路径键就要拒绝。
func TestRejectProcessPath(t *testing.T) {
	// 读出测试要用的工程字段表。
	sch := projectSchema(t)
	// 路径键和预期不符就进入失败。
	if _, err := CollectProcessIDs(sch, []byte(`[{"processId":"A","processPath":"x.json"}]`)); !errors.Is(err, ErrProcessPath) {
		// 结果和预期不符就停住。
		t.Fatalf("got %v", err)
	}
	// 没能看是不是这一类业务错误就停住本用例。
	if err := RejectProcessPath([]byte(`{"basePath":{"processPath":""}}`)); !errors.Is(err, ErrProcessPath) {
		// 结果和预期不符就停住。
		t.Fatalf("got %v", err)
	}
	// 没能发现路径键就拒绝这份正文就停住本用例。
	if err := RejectProcessPath([]byte(`[{"processId":"A"}]`)); err != nil {
		// 没能发现路径键就拒绝这份正文就停住本用例。
		t.Fatal(err)
	}
}

// 没选中的种类不该被收进引用。
func TestCollectSkipsUnselectedKind(t *testing.T) {
	// 把结构收成字节，再交给后面。
	sch, err := Marshal(Schema{Root: RootArray, Kinds: []string{ItemSingle}})
	// 没能把结构收成字节就停住本用例。
	if err != nil {
		// 没能把结构收成字节就停住本用例。
		t.Fatal(err)
	}
	// 准备一份测试或签名用的字节。
	ids, err := CollectProcessIDs(sch, []byte(`[{"processId":"KEEP","extraProcesses":[{"processId":"SKIP"}],"basePath":{"processId":"SKIP2"}}]`))
	// 出错或结果对不上就停住本用例。
	if err != nil || len(ids) != 1 || ids[0] != "KEEP" {
		// 按模版收集工艺身份和预期不符就停住。
		t.Fatalf("%v %v", ids, err)
	}
}

// 收集时只顺着字段表走，不扫多余的键。
func TestCollectFollowsSchema(t *testing.T) {
	// 把结构收成字节，再交给后面。
	sch, err := Marshal(Schema{Root: RootArray, Item: &Field{Type: TypeObject, Label: "焊缝", Fields: []Field{
		{Key: "alt", Label: "另", Type: TypeObject, Fields: []Field{str("processId", "工艺", "")}},
	}}})
	// 没能文本，再交给后面就停住本用例。
	if err != nil {
		// 没能文本，再交给后面就停住本用例。
		t.Fatal(err)
	}
	// 准备一份测试或签名用的字节。
	ids, err := CollectProcessIDs(sch, []byte(`[{"processId":"SKIP","alt":{"processId":"KEEP"}}]`))
	// 出错或结果对不上就停住本用例。
	if err != nil || len(ids) != 1 || ids[0] != "KEEP" {
		// 按模版收集工艺身份和预期不符就停住。
		t.Fatalf("%v %v", ids, err)
	}
}

// 类型标成工艺的槽也要收进引用。
func TestCollectFollowsProcessType(t *testing.T) {
	// 把结构收成字节，再交给后面。
	sch, err := Marshal(Schema{Root: RootArray, Item: &Field{Type: TypeObject, Label: "焊缝", Fields: []Field{
		{Key: "foo", Label: "工艺", Type: TypeProcess, Default: ""},
	}}})
	// 这一步失败就停住本用例。
	if err != nil {
		// 这一步失败就停住本用例。
		t.Fatal(err)
	}
	// 准备一份测试或签名用的字节。
	ids, err := CollectProcessIDs(sch, []byte(`[{"foo":"KEEP","processId":"SKIP"}]`))
	// 出错或结果对不上就停住本用例。
	if err != nil || len(ids) != 1 || ids[0] != "KEEP" {
		// 按模版收集工艺身份和预期不符就停住。
		t.Fatalf("%v %v", ids, err)
	}
}

// 改写只动字段表里的引用，别的键不动。
func TestRewriteFollowsSchema(t *testing.T) {
	// 把结构收成字节，再交给后面。
	sch, err := Marshal(Schema{Root: RootArray, Item: &Field{Type: TypeObject, Label: "焊缝", Fields: []Field{
		{Key: "alt", Label: "另", Type: TypeObject, Fields: []Field{str("processId", "工艺", "")}},
	}}})
	// 没能文本，再交给后面就停住本用例。
	if err != nil {
		// 没能文本，再交给后面就停住本用例。
		t.Fatal(err)
	}
	// 准备一份测试或签名用的字节。
	raw := []byte(`[{"processId":"SKIP","alt":{"processId":"KEEP"}}]`)
	// 按对照表改写工艺身份。
	out, err := RewriteProcessIDs(sch, raw, map[string]string{"KEEP": "NEXT"})
	// 没能按对照表改写工艺身份就停住本用例。
	if err != nil {
		// 没能按对照表改写工艺身份就停住本用例。
		t.Fatal(err)
	}
	// 准备承接解出来的对象。
	var welds []map[string]any
	// 没能把字节还原成结构就停住本用例。
	if err := json.Unmarshal(out, &welds); err != nil {
		// 没能把字节还原成结构就停住本用例。
		t.Fatal(err)
	}
	// 工艺身份和预期不符就进入失败。
	if welds[0]["processId"] != "SKIP" {
		// 和预期不符就停住本用例。
		t.Fatalf("template-extra %s", out)
	}
	// 收成对象再往下看，对不上就当不是对象。
	alt := welds[0]["alt"].(map[string]any)
	// 工艺身份和预期不符就进入失败。
	if alt["processId"] != "NEXT" {
		// 和预期不符就停住本用例。
		t.Fatalf("alt %v", alt)
	}
}

// 对得上的身份换成新的，对不上要失败。
func TestRewriteProcessIDs(t *testing.T) {
	// 读出测试要用的工程字段表。
	sch := projectSchema(t)
	// 准备一份测试或签名用的字节。
	raw := []byte(`[{"kind":"multi","name":"w","keep":true,"basePath":{"processId":"fac-1"},"passes":[{"processId":"fac-1"}]}]`)
	// 按对照表改写工艺身份。
	out, err := RewriteProcessIDs(sch, raw, map[string]string{"fac-1": "plat-1"})
	// 没能按对照表改写工艺身份就停住本用例。
	if err != nil {
		// 没能按对照表改写工艺身份就停住本用例。
		t.Fatal(err)
	}
	// 准备承接解出来的对象。
	var welds []map[string]any
	// 没能把字节还原成结构就停住本用例。
	if err := json.Unmarshal(out, &welds); err != nil {
		// 没能把字节还原成结构就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if welds[0]["keep"] != true {
		// 和预期不符就停住本用例。
		t.Fatalf("%s", out)
	}
	// 收成对象再往下看，对不上就当不是对象。
	base, _ := welds[0]["basePath"].(map[string]any)
	// 工艺身份和预期不符就进入失败。
	if base["processId"] != "plat-1" {
		// 和预期不符就停住本用例。
		t.Fatalf("base %v", base)
	}
	// 收成数组再往下走，对不上就当不是数组。
	passes, _ := welds[0]["passes"].([]any)
	// 收成对象再往下看，对不上就当不是对象。
	p0 := passes[0].(map[string]any)
	// 工艺身份和预期不符就进入失败。
	if p0["processId"] != "plat-1" {
		// 口令比对结果不对就停住。
		t.Fatalf("pass %v", p0)
	}
	// 准备一份测试或签名用的字节。
	same, err := RewriteProcessIDs(sch, []byte("job"), map[string]string{"fac-1": "plat-1"})
	// 出错或结果对不上就停住本用例。
	if err != nil || string(same) != "job" {
		// 按对照表改写工艺身份和预期不符就停住。
		t.Fatalf("%s %v", same, err)
	}
	// 结果和预期不符就进入失败。
	if _, err := RewriteProcessIDs(sch, raw, map[string]string{"other": "plat-1"}); !errors.Is(err, errUnknownProcessID) {
		t.Fatalf("got %v", err)
	}
}
