// 套用字段表：填缺省、丢掉未知键、非法 JSON 回退缺省。
package contenttpl

import (
	"encoding/json"
	"testing"
)

// 缺的字段要补上，多出来的要删掉。
func TestApplyFillsAndStrips(t *testing.T) {
	// 取出默认的那一份配置或字段。
	raw, err := Marshal(Default(KindProcess))
	// 没能取出默认的那一份配置或字段就停住本用例。
	if err != nil {
		// 没能取出默认的那一份配置或字段就停住本用例。
		t.Fatal(err)
	}
	// 准备一份测试或签名用的字节。
	out, err := Apply(raw, []byte(`{"name":"1K","current":250,"extra":1,"offsetX":"1.5"}`))
	// 没能按字段表套用正文就停住本用例。
	if err != nil {
		// 没能按字段表套用正文就停住本用例。
		t.Fatal(err)
	}
	// 准备承接解出来的对象。
	var m map[string]any
	// 没能把字节还原成结构就停住本用例。
	if err := json.Unmarshal(out, &m); err != nil {
		// 没能把字节还原成结构就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if _, ok := m["extra"]; ok {
		// 多余字段不该留下来。
		t.Fatalf("extra kept: %s", out)
	}
	// 结果和预期不符就进入失败。
	if m["name"] != "1K" {
		// 和预期不符就停住本用例。
		t.Fatalf("name %v", m["name"])
	}
	// 结果和预期不符就进入失败。
	if m["current"] != 250.0 {
		// 和预期不符就停住本用例。
		t.Fatalf("current %v", m["current"])
	}
	// 结果和预期不符就进入失败。
	if m["offsetX"] != 1.5 {
		// 和预期不符就停住本用例。
		t.Fatalf("offsetX %v", m["offsetX"])
	}
	// 电压和预期不符就进入失败。
	if m["voltage"] != 24.0 {
		// 电压没有落到预期。
		t.Fatalf("voltage default %v", m["voltage"])
	}
	// 收成对象再往下看，对不上就当不是对象。
	osc, _ := m["oscillation"].(map[string]any)
	// 结果和预期不符就进入失败。
	if osc["frequency"] != 2.0 {
		// 和预期不符就停住本用例。
		t.Fatalf("osc %v", osc)
	}
}

// 正文不是合法结构时改用默认值。
func TestApplyInvalidJSONUsesDefaults(t *testing.T) {
	// 取出默认的那一份配置或字段。
	raw, err := Marshal(Default(KindProcess))
	// 没能取出默认的那一份配置或字段就停住本用例。
	if err != nil {
		// 没能取出默认的那一份配置或字段就停住本用例。
		t.Fatal(err)
	}
	// 准备一份测试或签名用的字节。
	out, err := Apply(raw, []byte("not-json"))
	// 没能按字段表套用正文就停住本用例。
	if err != nil {
		// 没能按字段表套用正文就停住本用例。
		t.Fatal(err)
	}
	// 准备承接解出来的对象。
	var m map[string]any
	// 没能把字节还原成结构就停住本用例。
	if err := json.Unmarshal(out, &m); err != nil {
		// 没能把字节还原成结构就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if m["current"] != 200.0 {
		// 和预期不符就停住本用例。
		t.Fatalf("%v", m["current"])
	}
}

// 工程正文按数组套每一份对象。
func TestApplyProjectArray(t *testing.T) {
	// 取出默认的那一份配置或字段。
	raw, err := Marshal(Default(KindProject))
	// 没能取出默认的那一份配置或字段就停住本用例。
	if err != nil {
		// 没能取出默认的那一份配置或字段就停住本用例。
		t.Fatal(err)
	}
	// 准备一份测试或签名用的字节。
	out, err := Apply(raw, []byte(`[{"name":"焊道 1","points":[{"type":"START","pose":{"x":1},"junk":true}],"secret":1}]`))
	// 没能按字段表套用正文就停住本用例。
	if err != nil {
		// 没能按字段表套用正文就停住本用例。
		t.Fatal(err)
	}
	// 准备承接解出来的对象。
	var arr []map[string]any
	// 没能把字节还原成结构就停住本用例。
	if err := json.Unmarshal(out, &arr); err != nil {
		// 没能把字节还原成结构就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if len(arr) != 1 || arr[0]["name"] != "焊道 1" || arr[0]["kind"] != ItemSingle {
		// 和预期不符就停住本用例。
		t.Fatalf("%s", out)
	}
	// 结果和预期不符就进入失败。
	if _, ok := arr[0]["secret"]; ok {
		// 秘密原文不该留在结果里。
		t.Fatalf("secret kept")
	}
	// 结果和预期不符就进入失败。
	if _, ok := arr[0]["basePath"]; ok {
		// 和预期不符就停住本用例。
		t.Fatalf("stuffed multi")
	}
	// 结果和预期不符就进入失败。
	if _, ok := arr[0]["cornerGroupParams"]; ok {
		// 和预期不符就停住本用例。
		t.Fatalf("stuffed corner")
	}
	// 结果和预期不符就进入失败。
	if _, ok := arr[0]["passes"]; ok {
		// 口令比对结果不对就停住。
		t.Fatalf("stuffed passes")
	}
	// 模版身份和预期不符就进入失败。
	if arr[0]["templateId"] != defaultTplSingle {
		// 模版身份和预期不符。
		t.Fatalf("templateId %v", arr[0]["templateId"])
	}
	// 收成数组再往下走，对不上就当不是数组。
	pts, _ := arr[0]["points"].([]any)
	// 结果和预期不符就进入失败。
	if len(pts) != 1 {
		// 和预期不符就停住本用例。
		t.Fatalf("points %v", pts)
	}
	// 收成对象再往下看，对不上就当不是对象。
	p0 := pts[0].(map[string]any)
	// 结果和预期不符就进入失败。
	if _, ok := p0["junk"]; ok {
		// 和预期不符就停住本用例。
		t.Fatalf("point junk kept")
	}
	// 收成对象再往下看，对不上就当不是对象。
	pose := p0["pose"].(map[string]any)
	// 结果和预期不符就进入失败。
	if pose["x"] != 1.0 || pose["y"] != 0.0 {
		// 和预期不符就停住本用例。
		t.Fatalf("pose %v", pose)
	}
}

// 空的工程正文要补成默认条目。
func TestApplyProjectEmpty(t *testing.T) {
	// 取出默认的那一份配置或字段。
	raw, err := Marshal(Default(KindProject))
	// 没能取出默认的那一份配置或字段就停住本用例。
	if err != nil {
		// 没能取出默认的那一份配置或字段就停住本用例。
		t.Fatal(err)
	}
	// 按字段表套用正文。
	out, err := Apply(raw, nil)
	// 没能按字段表套用正文就停住本用例。
	if err != nil {
		// 没能按字段表套用正文就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if string(out) != "[]" {
		// 和预期不符就停住本用例。
		t.Fatalf("%s", out)
	}
}

// 没选中的种类要从结果里拿掉。
func TestApplyProjectDropsUnselectedKind(t *testing.T) {
	// 准备收集字符串结果。
	sch := Schema{Root: RootArray, Kinds: []string{ItemSingle}}
	// 把结构收成字节，再交给后面。
	raw, err := Marshal(sch)
	// 没能把结构收成字节就停住本用例。
	if err != nil {
		// 没能把结构收成字节就停住本用例。
		t.Fatal(err)
	}
	// 准备一份测试或签名用的字节。
	out, err := Apply(raw, []byte(`[{"kind":"multi","name":"多层"},{"kind":"single","name":"单"}]`))
	// 没能按字段表套用正文就停住本用例。
	if err != nil {
		// 没能按字段表套用正文就停住本用例。
		t.Fatal(err)
	}
	// 准备承接解出来的对象。
	var arr []map[string]any
	// 没能把字节还原成结构就停住本用例。
	if err := json.Unmarshal(out, &arr); err != nil {
		// 没能把字节还原成结构就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if len(arr) != 1 || arr[0]["name"] != "单" || arr[0]["kind"] != ItemSingle {
		// 和预期不符就停住本用例。
		t.Fatalf("%s", out)
	}
}

// 工程种类不合法时校验要拒绝。
func TestValidateProjectKinds(t *testing.T) {
	// 没有出错就按成功返回，不用再补救。
	if err := Validate(Schema{Root: RootArray, Kinds: []string{ItemExtra}}); err == nil {
		t.Fatal("extra-only")
	}
	// 没有出错就按成功返回，不用再补救。
	if err := Validate(Schema{Root: RootArray, Kinds: []string{ItemSingle, ItemSingle}}); err == nil {
		t.Fatal("dup")
	}
	// 没能取出默认的那一份配置或字段就停住本用例。
	if err := Validate(Default(KindProject)); err != nil {
		// 没能取出默认的那一份配置或字段就停住本用例。
		t.Fatal(err)
	}
	// 没有出错就按成功返回，不用再补救。
	if err := Validate(Schema{Root: RootArray, Templates: []ProjectTemplate{{ID: defaultTplSingle, Name: "", Kind: ItemSingle}}}); err == nil {
		t.Fatal("empty name")
	}
	// 没有出错就按成功返回，不用再补救。
	if err := Validate(Schema{Root: RootArray, Templates: []ProjectTemplate{{ID: defaultTplSingle, Name: "x", Kind: ItemExtra}}}); err == nil {
		t.Fatal("extra kind")
	}
	// 没能检查字段表能不能拿来套就停住本用例。
	if err := Validate(Schema{Root: RootArray}); err != nil {
		t.Fatal(err)
	}
}

// 选项和文本要按规则收，对不上用默认。
func TestApplyEnumAndText(t *testing.T) {
	// 先把这一步的结果放下，后面还要用。
	sch := Schema{Root: RootObject, Fields: []Field{
		{Key: "name", Label: "名称", Type: TypeString},
		{Key: "amp", Label: "振幅", Type: TypeString, Unit: "mm", Default: 4.0},
		{Key: "on", Label: "启用", Type: TypeString, Options: []string{"否", "是"}, Default: "否"},
	}}
	// 把结构收成字节，再交给后面。
	raw, err := Marshal(sch)
	// 没能把结构收成字节就停住本用例。
	if err != nil {
		// 没能把结构收成字节就停住本用例。
		t.Fatal(err)
	}
	// 准备一份测试或签名用的字节。
	out, err := Apply(raw, []byte(`{"name":"a","amp":"1.5","on":true,"extra":1}`))
	// 没能按字段表套用正文就停住本用例。
	if err != nil {
		// 没能按字段表套用正文就停住本用例。
		t.Fatal(err)
	}
	// 准备承接解出来的对象。
	var m map[string]any
	// 没能把字节还原成结构就停住本用例。
	if err := json.Unmarshal(out, &m); err != nil {
		// 没能把字节还原成结构就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if m["amp"] != 1.5 {
		// 和预期不符就停住本用例。
		t.Fatalf("amp %v", m["amp"])
	}
	// 结果和预期不符就进入失败。
	if m["on"] != "是" {
		// 和预期不符就停住本用例。
		t.Fatalf("on %v", m["on"])
	}
	// 结果和预期不符就进入失败。
	if _, ok := m["extra"]; ok {
		// 多余字段不该留下来。
		t.Fatalf("extra kept")
	}
	// 准备一份测试或签名用的字节。
	out, err = Apply(raw, []byte(`{"on":"也许"}`))
	// 没能按字段表套用正文就停住本用例。
	if err != nil {
		// 没能按字段表套用正文就停住本用例。
		t.Fatal(err)
	}
	// 没能把字节还原成结构就停住本用例。
	if err := json.Unmarshal(out, &m); err != nil {
		// 没能把字节还原成结构就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if m["on"] != "否" {
		// 选项没有按规则收。
		t.Fatalf("bad enum %v", m["on"])
	}
}

// 是否开关要收成布尔，旧写法也认。
func TestApplyBoolFlag(t *testing.T) {
	// 开关，再交给后面，再交给后面。
	sch := Schema{Root: RootObject, Fields: []Field{flag("on", "启用", false)}}
	// 把结构收成字节，再交给后面。
	raw, err := Marshal(sch)
	// 没能把结构收成字节就停住本用例。
	if err != nil {
		// 没能把结构收成字节就停住本用例。
		t.Fatal(err)
	}
	// 准备一份测试或签名用的字节。
	out, err := Apply(raw, []byte(`{"on":"是"}`))
	// 没能按字段表套用正文就停住本用例。
	if err != nil {
		// 没能按字段表套用正文就停住本用例。
		t.Fatal(err)
	}
	// 准备承接解出来的对象。
	var m map[string]any
	// 没能把字节还原成结构就停住本用例。
	if err := json.Unmarshal(out, &m); err != nil {
		// 没能把字节还原成结构就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if m["on"] != true {
		// 和预期不符就停住本用例。
		t.Fatalf("on %v", m["on"])
	}
}

// 正文里的空对象要原样留下。
func TestApplyKeepsNullObject(t *testing.T) {
	// 先把这一步的结果放下，后面还要用。
	sch := Schema{Root: RootObject, Fields: []Field{
		{Key: "ref", Label: "参考", Type: TypeObject, Fields: poseFields()},
	}}
	// 把结构收成字节，再交给后面。
	raw, err := Marshal(sch)
	// 没能把结构收成字节就停住本用例。
	if err != nil {
		// 没能把结构收成字节就停住本用例。
		t.Fatal(err)
	}
	// 准备一份测试或签名用的字节。
	out, err := Apply(raw, []byte(`{"ref":null}`))
	// 没能按字段表套用正文就停住本用例。
	if err != nil {
		// 没能按字段表套用正文就停住本用例。
		t.Fatal(err)
	}
	// 准备承接解出来的对象。
	var m map[string]any
	// 没能把字节还原成结构就停住本用例。
	if err := json.Unmarshal(out, &m); err != nil {
		// 没能把字节还原成结构就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if m["ref"] != nil {
		// 和预期不符就停住本用例。
		t.Fatalf("ref %v", m["ref"])
	}
	// 准备一份测试或签名用的字节。
	out, err = Apply(raw, []byte(`{}`))
	// 没能按字段表套用正文就停住本用例。
	if err != nil {
		// 没能按字段表套用正文就停住本用例。
		t.Fatal(err)
	}
	// 没能把字节还原成结构就停住本用例。
	if err := json.Unmarshal(out, &m); err != nil {
		// 没能把字节还原成结构就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if m["ref"] == nil {
		// 缺了该有的内容就停住。
		t.Fatal("missing ref should default")
	}
}

// 键不合法时校验要拒绝。
func TestValidateRejectsBadKey(t *testing.T) {
	// 定下这段文本，再交给后面。
	s := Schema{Root: RootObject, Fields: []Field{{Key: "a-b", Label: "坏", Type: TypeNumber}}}
	// 没有出错就按成功返回，不用再补救。
	if err := Validate(s); err == nil {
		// 非法输入竟能通过就停住。
		t.Fatal("want invalid")
	}
}

// 工艺类型槽不合规则时要拒绝。
func TestValidateProcessType(t *testing.T) {
	// 先当还没找到，找到再改成命中。
	ok := Schema{Root: RootObject, Fields: []Field{{Key: "p", Label: "工艺", Type: TypeProcess, Default: ""}}}
	// 没能检查字段表能不能拿来套就停住本用例。
	if err := Validate(ok); err != nil {
		// 没能检查字段表能不能拿来套就停住本用例。
		t.Fatal(err)
	}
	// 准备收集字符串结果。
	bad := Schema{Root: RootObject, Fields: []Field{{Key: "p", Label: "工艺", Type: TypeProcess, Options: []string{"a"}}}}
	// 没有出错就按成功返回，不用再补救。
	if err := Validate(bad); err == nil {
		// 非法输入竟能通过就停住。
		t.Fatal("want invalid options")
	}
}

// 工艺槽只留字符串身份，空表示未选。
func TestApplyProcessID(t *testing.T) {
	// 先把这一步的结果放下，后面还要用。
	sch := Schema{Root: RootObject, Fields: []Field{{Key: "p", Label: "工艺", Type: TypeProcess, Default: ""}}}
	// 把结构收成字节，再交给后面。
	raw, err := Marshal(sch)
	// 没能把结构收成字节就停住本用例。
	if err != nil {
		// 没能把结构收成字节就停住本用例。
		t.Fatal(err)
	}
	// 准备一份测试或签名用的字节。
	out, err := Apply(raw, []byte(`{"p":"  abc  ","extra":1}`))
	// 没能按字段表套用正文就停住本用例。
	if err != nil {
		// 没能按字段表套用正文就停住本用例。
		t.Fatal(err)
	}
	// 准备承接解出来的对象。
	var m map[string]any
	// 没能把字节还原成结构就停住本用例。
	if err := json.Unmarshal(out, &m); err != nil {
		// 没能把字节还原成结构就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if m["p"] != "abc" {
		// 和预期不符就停住本用例。
		t.Fatalf("p %v", m["p"])
	}
	// 准备一份测试或签名用的字节。
	out, err = Apply(raw, []byte(`{"p":1}`))
	// 没能按字段表套用正文就停住本用例。
	if err != nil {
		// 没能按字段表套用正文就停住本用例。
		t.Fatal(err)
	}
	// 没能把字节还原成结构就停住本用例。
	if err := json.Unmarshal(out, &m); err != nil {
		// 没能把字节还原成结构就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if m["p"] != "" {
		// 不是文本却被照单收下。
		t.Fatalf("non-string %v", m["p"])
	}
}

// 默认工程要带上工艺身份槽。
func TestDefaultProjectRefsProcessID(t *testing.T) {
	// 取出默认的那一份配置或字段。
	s := Default(KindProject)
	// 结果和预期不符就进入失败。
	if len(s.Templates) != 2 || s.Item != nil || len(s.Kinds) != 0 {
		// 种类和预期不符就停住。
		t.Fatalf("templates %+v item %v kinds %v", s.Templates, s.Item, s.Kinds)
	}
	// 准备收集字符串结果。
	var keys []string
	// 逐项处理，空的就不进入循环。
	for _, k := range []string{ItemSingle, ItemMulti, ItemTBar} {
		fields := ItemFields(k, true)
		item := Field{Type: TypeObject, Fields: fields}
		keys = append(keys, fieldKeys(&item)...)
	}
	// 定下有没有，再交给后面。
	has := map[string]int{}
	// 逐项处理，空的就不进入循环。
	for _, k := range keys {
		// 定下有没有，再交给后面。
		has[k]++
	}
	// 路径键和预期不符就进入失败。
	if has["processPath"] != 0 {
		// 路径键不该还留在结果里。
		t.Fatalf("processPath still in catalog: %v", keys)
	}
	// 结果和预期不符就进入失败。
	if has["process"] != 0 {
		// 和预期不符就停住本用例。
		t.Fatalf("nested process still in catalog: %v", keys)
	}
	// 工艺身份和预期不符就进入失败。
	if has["processId"] < 4 {
		// 工艺身份槽和预期不符。
		t.Fatalf("processId count %d want >=4 in %v", has["processId"], keys)
	}
	// 定下数量，再交给后面。
	n := 0
	// 逐项处理，空的就不进入循环。
	for _, k := range []string{ItemSingle, ItemMulti, ItemTBar} {
		item := Field{Type: TypeObject, Fields: ItemFields(k, true)}
		n += countProcessType(&item)
	}
	// 结果和预期不符就进入失败。
	if n < 5 {
		// 没有得到预期结果就停住。
		t.Fatalf("process type count %d want >=5", n)
	}
	// 结果和预期不符就进入失败。
	if has["cornerGroupParams"] != 0 {
		// 和预期不符就停住本用例。
		t.Fatalf("corner still in catalog: %v", keys)
	}
}

// 种子工程应是单层、多层和丁字排三份。
func TestSeedProjectItemsThree(t *testing.T) {
	// 取出单层、多层、丁字排三份种子。
	seed := SeedProjectItems()
	// 种子和预期不符就进入失败。
	if len(seed) != 3 {
		// 种子条数和预期不符。
		t.Fatalf("seed %d", len(seed))
	}
	// 旧记录工程条目，再交给后面。
	legacy := LegacyProjectItems()
	// 结果和预期不符就进入失败。
	if len(legacy) != 2 {
		// 旧库基线的结果不对。
		t.Fatalf("legacy %d", len(legacy))
	}
	// 先把这一步的结果放下，后面还要用。
	var tbar *ProjectItemSchema
	// 按下标扫过去，直到越界或提前结束。
	for i := range seed {
		// 对上了才走这一路，其余分开处理。
		if seed[i].ID == SeedTplTBar {
			// 把这个值定下来，后面的判断才有依据。
			tbar = &seed[i]
		}
	}
	// 丁字排和预期不符就进入失败。
	if tbar == nil || tbar.Name != "T排对接" {
		// 丁字排字段和预期不符。
		t.Fatalf("tbar %+v", seed)
	}
	// 列出这一字段下面有哪些键。
	keys := fieldKeys(&Field{Type: TypeObject, Fields: tbar.Fields})
	// 定下有没有，再交给后面。
	has := map[string]int{}
	// 逐项处理，空的就不进入循环。
	for _, k := range keys {
		// 定下有没有，再交给后面。
		has[k]++
	}
	// 结果和预期不符就进入失败。
	if has["gapBands"] == 0 || has["rootProcessId"] == 0 || has["capProcessId"] == 0 {
		// 字段和预期不符就停住。
		t.Fatalf("tbar fields %v", keys)
	}
	// 路径键和预期不符就进入失败。
	if has["processPath"] != 0 {
		// 路径键不该还留在结果里。
		t.Fatalf("processPath in tbar: %v", keys)
	}
	// 准备放下多层，再交给后面。
	var multi *ProjectItemSchema
	// 按下标扫过去，直到越界或提前结束。
	for i := range seed {
		// 对上了才走这一路，其余分开处理。
		if seed[i].ID == SeedTplMulti {
			// 定下多层，再交给后面。
			multi = &seed[i]
		}
	}
	// 结果和预期不符就进入失败。
	if multi == nil {
		// 缺了该有的内容就停住。
		t.Fatal("missing multi seed")
	}
	// 列出这一字段下面有哪些键。
	mkeys := fieldKeys(&Field{Type: TypeObject, Fields: multi.Fields})
	// 先把这一步的结果放下，后面还要用。
	mhas := map[string]int{}
	// 逐项处理，空的就不进入循环。
	for _, k := range mkeys {
		// 把这个值定下来，后面的判断才有依据。
		mhas[k]++
	}
	// 结果和预期不符就进入失败。
	if mhas["refPointXMiddle"] == 0 || mhas["refPointZMiddle"] == 0 || mhas["kind"] == 0 {
		// 缺了该有的内容就停住。
		t.Fatalf("multi missing app fields %v", mkeys)
	}
	// 定下加上，再交给后面。
	on := false
	// 逐项处理，空的就不进入循环。
	for _, f := range itemSingle(true) {
		// 对上了才走这一路，其余分开处理。
		if f.Key == "isEnabled" {
			// 定下加上，再交给后面。
			on = f.Type == TypeBool
		}
	}
	// 结果和预期不符就进入失败。
	if !on {
		// 启用开关没有收成布尔。
		t.Fatal("isEnabled not bool")
	}
	// 取出默认的那一份配置或字段。
	got := ExpandLegacyProject(Default(KindProject))
	// 结果和预期不符就进入失败。
	if len(got) != 2 {
		// 取出默认的那一份配置或字段和预期不符就停住。
		t.Fatalf("expand default %d", len(got))
	}
	// 结果和预期不符就进入失败。
	if InferItemKind(map[string]any{"gapBands": []any{}}) != ItemTBar {
		t.Fatalf("infer gapBands")
	}
	// 结果和预期不符就进入失败。
	if InferItemKind(map[string]any{"points": []any{map[string]any{"type": "GROOVE_A_LOWER"}}}) != ItemTBar {
		t.Fatalf("infer groove")
	}
	// 结果和预期不符就进入失败。
	if InferItemKind(map[string]any{"cornerGroupParams": map[string]any{}}) != ItemSingle {
		t.Fatalf("infer corner")
	}
	// 展开旧记录工程，再交给后面。
	dropped := ExpandLegacyProject(Schema{Root: RootArray, Templates: []ProjectTemplate{
		{ID: defaultTplSingle, Name: "单层焊道", Kind: ItemSingle, Extra: true},
		{ID: "33333333-3333-4333-8333-333333333333", Name: "包角", Kind: "corner", Extra: true},
	}})
	// 结果和预期不符就进入失败。
	if len(dropped) != 1 || dropped[0].ID != defaultTplSingle {
		// 和预期不符就停住本用例。
		t.Fatalf("drop corner %+v", dropped)
	}
}

// 把这一字段的键收成列表，方便断言。
func fieldKeys(f *Field) []string {
	// 没有这一字段就跳过，不当成还有下级。
	if f == nil {
		return nil
	}
	// 准备收集字符串结果。
	var out []string
	// 有内容才继续处理这一支。
	if f.Key != "" {
		// 把这一段接进结果，顺序要保持住。
		out = append(out, f.Key)
	}
	// 按下标扫过去，直到越界或提前结束。
	for i := range f.Fields {
		// 这个字符不是横线，放进库名。
		out = append(out, fieldKeys(&f.Fields[i])...)
	}
	// 已经带了元素模版就按它往下套。
	if f.Items != nil {
		// 把这一段接进结果，顺序要保持住。
		out = append(out, fieldKeys(f.Items)...)
	}
	return out
}

// countProcessType 统计工艺引用字段个数。
func countProcessType(f *Field) int {
	// 没有这一字段就跳过，不当成还有下级。
	if f == nil {
		return 0
	}
	// 定下数量，再交给后面。
	n := 0
	// 这个槽是工艺引用，要按身份来收。
	if f.Type == TypeProcess {
		// 定下数量，再交给后面。
		n++
	}
	// 按下标扫过去，直到越界或提前结束。
	for i := range f.Fields {
		// 计数工艺类型，再交给后面。
		n += countProcessType(&f.Fields[i])
	}
	// 已经带了元素模版就按它往下套。
	if f.Items != nil {
		// 计数工艺类型，再交给后面。
		n += countProcessType(f.Items)
	}
	return n
}
