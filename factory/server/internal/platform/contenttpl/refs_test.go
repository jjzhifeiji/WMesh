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

// 测试用的自定义模版身份，不是空库三份之一。
const customTplID = "0199aaaa-0000-7000-8000-000000000001"

// 测试用的第二份自定义模版身份，工艺键写成枚举。
const customEnumID = "0199aaaa-0000-7000-8000-000000000002"

// customItem 不带工艺槽的自定义模版，只声明名称。
func customItem() ProjectItemSchema {
	// 交回只有名称字段的一份模版。
	return ProjectItemSchema{ID: customTplID, Name: "自定义", Fields: []Field{str("name", "名称", "")}}
}

// 收集、改写、对照表缺项三件事一起核对。
func assertStandardSlots(t *testing.T, items []ProjectItemSchema, raw []byte) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 按各份字段表收集工艺身份。
	ids, err := CollectProcessIDsFromItems(items, raw)
	// 没能收集工艺身份就停住本用例。
	if err != nil {
		// 没能收集工艺身份就停住本用例。
		t.Fatal(err)
	}
	// 拷一份再排序，免得改到原结果。
	got := append([]string(nil), ids...)
	// 排好序再和预期比。
	slices.Sort(got)
	// 标准槽上的六处引用少一处就失败。
	if !slices.Equal(got, []string{"A", "B", "C", "D", "E", "F"}) {
		// 和预期不符就停住本用例。
		t.Fatalf("collect %v", ids)
	}
	// 准备每个旧身份对应的新身份。
	idMap := map[string]string{"A": "A2", "B": "B2", "C": "C2", "D": "D2", "E": "E2", "F": "F2"}
	// 按对照表改写工艺身份。
	out, err := RewriteProcessIDsFromItems(items, raw, idMap)
	// 没能按对照表改写工艺身份就停住本用例。
	if err != nil {
		// 没能按对照表改写工艺身份就停住本用例。
		t.Fatal(err)
	}
	// 改写后的正文再收一遍，看旧身份是否还在。
	next, err := CollectProcessIDsFromItems(items, out)
	// 没能收集改写后的引用就停住本用例。
	if err != nil {
		// 没能收集改写后的引用就停住本用例。
		t.Fatal(err)
	}
	// 排好序再和预期比。
	slices.Sort(next)
	// 有一处没换成新身份就失败。
	if !slices.Equal(next, []string{"A2", "B2", "C2", "D2", "E2", "F2"}) {
		// 和预期不符就停住本用例。
		t.Fatalf("rewrite %s", out)
	}
	// 去掉附加槽那一条，对照表就缺项了。
	delete(idMap, "D")
	// 对照表缺项时改写要报找不到。
	if _, err := RewriteProcessIDsFromItems(items, raw, idMap); !errors.Is(err, errUnknownProcessID) {
		// 和预期不符就停住本用例。
		t.Fatalf("missing map %v", err)
	}
}

// 自定义模版没声明工艺槽，三种焊缝标准槽上的引用照样能收能改。
func TestCustomTemplateStandardSlots(t *testing.T) {
	// 空库三份加一份不带工艺槽的自定义模版。
	items := append(SeedProjectItems(), customItem())
	// 三种焊缝都挂在自定义模版下，工艺填在各自标准槽。
	raw := []byte(`[
		{"templateId":"` + customTplID + `","kind":"multi","basePath":{"processId":"A"},"passes":[{"processId":"B"}]},
		{"templateId":"` + customTplID + `","kind":"single","processId":"C","extraProcesses":[{"id":"x","processId":"D"}]},
		{"templateId":"` + customTplID + `","kind":"tbar","gapBands":[{"rootProcessId":"E","capProcessId":"F"}]}
	]`)
	// 收集、改写、缺项报错一起核对。
	assertStandardSlots(t, items, raw)
}

// 模版身份对不上时退回并集，并集没有工艺槽也按种类照收。
func TestUnknownTemplateStandardSlots(t *testing.T) {
	// 只有一份不带工艺槽的自定义模版，并集里也没有工艺槽。
	items := []ProjectItemSchema{customItem()}
	// 身份对不上或没写，种类靠形状推断。
	raw := []byte(`[
		{"templateId":"nope","basePath":{"processId":"A"},"passes":[{"processId":"B"}]},
		{"processId":"C","extraProcesses":[{"processId":"D"}]},
		{"templateId":"nope","gapBands":[{"rootProcessId":"E","capProcessId":"F"}]}
	]`)
	// 收集、改写、缺项报错一起核对。
	assertStandardSlots(t, items, raw)
}

// 自定义字段和标准槽同键时只走一遍，改写出的新身份不再查表。
func TestMergedSlotRewrittenOnce(t *testing.T) {
	// 第一份自己也声明了同键的工艺槽、对象和数组。
	custom := ProjectItemSchema{ID: customTplID, Name: "自定义", Fields: []Field{
		procRef("processId", "工艺"),
		{Key: "basePath", Label: "基准层", Type: TypeObject, Fields: []Field{procRef("processId", "工艺"), str("name", "名", "")}},
		{Key: "passes", Label: "填充层", Type: TypeArray, Items: &Field{Type: TypeObject, Label: "层", Fields: []Field{procRef("processId", "工艺")}}},
	}}
	// 第二份把工艺键写成枚举，标准槽仍按工艺算。
	enum := ProjectItemSchema{ID: customEnumID, Name: "枚举", Fields: []Field{
		{Key: "processId", Label: "工艺", Type: TypeString, Options: []string{"甲", "乙"}},
	}}
	// 两份自定义模版，种子不在。
	items := []ProjectItemSchema{custom, enum}
	// 多层和单层各挂一份，同键槽上都填了工艺。
	raw := []byte(`[
		{"templateId":"` + customTplID + `","kind":"multi","basePath":{"processId":"A"},"passes":[{"processId":"B"}]},
		{"templateId":"` + customTplID + `","kind":"single","processId":"C"},
		{"templateId":"` + customEnumID + `","kind":"single","processId":"D"}
	]`)
	// 对照表不含新身份，走两遍就会报找不到。
	idMap := map[string]string{"A": "A2", "B": "B2", "C": "C2", "D": "D2"}
	// 按对照表改写工艺身份。
	out, err := RewriteProcessIDsFromItems(items, raw, idMap)
	// 没能按对照表改写工艺身份就停住本用例。
	if err != nil {
		// 同键被走了两遍或枚举没算工艺都会停在这里。
		t.Fatal(err)
	}
	// 改写后的正文再收一遍，看是否都换成了新身份。
	ids, err := CollectProcessIDsFromItems(items, out)
	// 没能收集改写后的引用就停住本用例。
	if err != nil {
		// 没能收集改写后的引用就停住本用例。
		t.Fatal(err)
	}
	// 排好序再和预期比。
	slices.Sort(ids)
	// 有一处没换或换错就失败。
	if !slices.Equal(ids, []string{"A2", "B2", "C2", "D2"}) {
		// 和预期不符就停住本用例。
		t.Fatalf("rewrite once %v %s", ids, out)
	}
}

// T 排盖面工艺组和包角组工艺都算引用，能收能改，模版没声明也照收。
func TestCapListAndCornerRefs(t *testing.T) {
	// 空库三份加一份不带工艺槽的自定义模版。
	items := append(SeedProjectItems(), customItem())
	// 盖面 2、3 写在数组里，包角组工艺挂在包角组上。
	raw := []byte(`[
		{"templateId":"` + SeedTplTBar + `","kind":"tbar","gapBands":[{"rootProcessId":"A","capProcessId":"B","capProcessIds":["B","C",""]}]},
		{"templateId":"` + SeedTplSingle + `","kind":"single","processId":"D","cornerGroupParams":{"groupId":"g","isMaster":true,"processId":"E"}},
		{"templateId":"` + customTplID + `","kind":"single","processId":"","cornerGroupParams":{"groupId":"g","processId":"F"}}
	]`)
	// 收集、改写、缺项报错一起核对。
	assertStandardSlots(t, items, raw)
	// 改写只换身份，包角组其它键要原样留下。
	out, err := RewriteProcessIDsFromItems(items, raw, map[string]string{"A": "A", "B": "B", "C": "C", "D": "D", "E": "E2", "F": "F"})
	// 没能改写就停住本用例。
	if err != nil {
		// 没能改写就停住本用例。
		t.Fatal(err)
	}
	// 读回结构看包角组。
	var got []map[string]any
	// 读不回就停住本用例。
	if err := json.Unmarshal(out, &got); err != nil {
		// 读不回就停住本用例。
		t.Fatal(err)
	}
	// 包角组要保留组身份，引用换成新身份。
	g, _ := got[1]["cornerGroupParams"].(map[string]any)
	// 组身份丢了或引用没换就失败。
	if g["groupId"] != "g" || g["isMaster"] != true || g["processId"] != "E2" {
		// 和预期不符就停住本用例。
		t.Fatalf("corner %s", out)
	}
}

// 套模版不给普通单层补包角组，T 排盖面组缺了补成空数组。
func TestApplyKeepsCornerOff(t *testing.T) {
	// 一条普通单层和一条没写盖面组的 T 排。
	raw := []byte(`[
		{"templateId":"` + SeedTplSingle + `","kind":"single","processId":"A"},
		{"templateId":"` + SeedTplTBar + `","kind":"tbar","gapBands":[{"rootProcessId":"B","capProcessIds":["C","D"]},{"capProcessId":"E"}]}
	]`)
	// 按空库三份套正文。
	out, err := ApplyProjectItems(SeedProjectItems(), raw)
	// 套不上就停住本用例。
	if err != nil {
		// 套不上就停住本用例。
		t.Fatal(err)
	}
	// 读回结构逐项看。
	var got []map[string]any
	// 读不回就停住本用例。
	if err := json.Unmarshal(out, &got); err != nil {
		// 读不回就停住本用例。
		t.Fatal(err)
	}
	// 普通单层不该多出包角组。
	if _, ok := got[0]["cornerGroupParams"]; ok {
		// 多出包角组就失败。
		t.Fatalf("single %s", out)
	}
	// 取出两条间隙带。
	bands, _ := got[1]["gapBands"].([]any)
	// 第一条带的盖面组。
	first, _ := bands[0].(map[string]any)["capProcessIds"].([]any)
	// 第二条带的盖面组。
	second, _ := bands[1].(map[string]any)["capProcessIds"].([]any)
	// 盖面组要原样留下，缺的补成空数组。
	if len(first) != 2 || first[1] != "D" || second == nil || len(second) != 0 {
		// 和预期不符就停住本用例。
		t.Fatalf("tbar %s", out)
	}
}

// 旧种类表也要收、改包角组上的工艺。
func TestKindsCornerRefs(t *testing.T) {
	// 只选单层的旧种类表。
	sch, err := Marshal(Schema{Root: RootArray, Kinds: []string{ItemSingle}})
	// 收不成字段表就停住本用例。
	if err != nil {
		// 收不成字段表就停住本用例。
		t.Fatal(err)
	}
	// 包角组工艺挂在包角组上。
	raw := []byte(`[{"processId":"A","cornerGroupParams":{"groupId":"g","isMaster":true,"processId":"B"}}]`)
	// 按旧种类表收引用。
	ids, err := CollectProcessIDs(sch, raw)
	// 漏了包角组工艺就失败。
	if err != nil || !slices.Equal(ids, []string{"A", "B"}) {
		// 和预期不符就停住本用例。
		t.Fatalf("%v %v", ids, err)
	}
	// 按对照表改写，包角组工艺换成新身份。
	out, err := RewriteProcessIDs(sch, raw, map[string]string{"A": "A", "B": "B2"})
	// 没能改写就停住本用例。
	if err != nil {
		// 没能改写就停住本用例。
		t.Fatal(err)
	}
	// 读回结构看包角组。
	var got []map[string]any
	// 读不回就停住本用例。
	if err := json.Unmarshal(out, &got); err != nil {
		// 读不回就停住本用例。
		t.Fatal(err)
	}
	// 包角组其它键原样留下，引用换成新身份。
	g, _ := got[0]["cornerGroupParams"].(map[string]any)
	// 组身份丢了或引用没换就失败。
	if g["groupId"] != "g" || g["isMaster"] != true || g["processId"] != "B2" {
		// 和预期不符就停住本用例。
		t.Fatalf("corner %s", out)
	}
}
