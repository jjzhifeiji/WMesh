// 套用字段表：填缺省、丢掉未知键、非法 JSON 回退缺省。
package contenttpl

import (
	"encoding/json"
	"testing"
)

// 已知值留下，多余键删掉，缺的数字补默认。
func TestApplyFillsAndStrips(t *testing.T) {
	// 先把模版收成规范正文。
	raw, err := Marshal(Default(KindProcess))
	// 收不成规范正文就停。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 用这份正文去套模版。
	out, err := Apply(raw, []byte(`{"name":"1K","current":250,"extra":1,"offsetX":"1.5"}`))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 准备接套开之后的结构。
	var m map[string]any
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(out, &m); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 这个键不该留下来。
	if _, ok := m["extra"]; ok {
		// 多余键还在就中止。
		t.Fatalf("extra kept: %s", out)
	}
	// 结果和预期不一致就中止。
	if m["name"] != "1K" {
		// 对不上就中止并带上原值。
		t.Fatalf("name %v", m["name"])
	}
	// 结果和预期不一致就中止。
	if m["current"] != 250.0 {
		// 对不上就中止并带上原值。
		t.Fatalf("current %v", m["current"])
	}
	// 结果和预期不一致就中止。
	if m["offsetX"] != 1.5 {
		// 对不上就中止并带上原值。
		t.Fatalf("offsetX %v", m["offsetX"])
	}
	// 结果和预期不一致就中止。
	if m["voltage"] != 24.0 {
		// 对不上就中止并带上原值。
		t.Fatalf("voltage default %v", m["voltage"])
	}
	// 钻进这一层再核对。
	osc, _ := m["oscillation"].(map[string]any)
	// 结果和预期不一致就中止。
	if osc["frequency"] != 2.0 {
		// 对不上就中止并带上原值。
		t.Fatalf("osc %v", osc)
	}
}

// 非法正文应整份退回缺省，不报套用失败。
func TestApplyInvalidJSONUsesDefaults(t *testing.T) {
	// 先把模版收成规范正文。
	raw, err := Marshal(Default(KindProcess))
	// 收不成规范正文就停。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 用这份正文去套模版。
	out, err := Apply(raw, []byte("not-json"))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 准备接套开之后的结构。
	var m map[string]any
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(out, &m); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 结果和预期不一致就中止。
	if m["current"] != 200.0 {
		// 对不上就中止并带上原值。
		t.Fatalf("%v", m["current"])
	}
}

// 工程数组只留对上的份，点上的多余键删掉。
func TestApplyProjectArray(t *testing.T) {
	// 先把模版收成规范正文。
	raw, err := Marshal(Default(KindProject))
	// 收不成规范正文就停。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 用这份正文去套模版。
	out, err := Apply(raw, []byte(`[{"name":"焊道 1","points":[{"type":"START","pose":{"x":1},"junk":true}],"secret":1}]`))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 准备接套开之后的结构。
	var arr []map[string]any
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(out, &arr); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 结果和预期不一致就中止。
	if len(arr) != 1 || arr[0]["name"] != "焊道 1" || arr[0]["kind"] != ItemSingle {
		// 对不上就中止并带上原值。
		t.Fatalf("%s", out)
	}
	// 这个键不该留下来。
	if _, ok := arr[0]["secret"]; ok {
		// 多余键还在就中止。
		t.Fatalf("secret kept")
	}
	// 这个键不该留下来。
	if _, ok := arr[0]["basePath"]; ok {
		// 对不上就中止并带上原值。
		t.Fatalf("stuffed multi")
	}
	// 这个键不该留下来。
	if _, ok := arr[0]["cornerGroupParams"]; ok {
		// 不该出现的键还在就中止。
		t.Fatalf("stuffed corner")
	}
	// 这个键不该留下来。
	if _, ok := arr[0]["passes"]; ok {
		// 对不上就中止并带上原值。
		t.Fatalf("stuffed passes")
	}
	// 结果和预期不一致就中止。
	if arr[0]["templateId"] != defaultTplSingle {
		// 对不上就中止并带上原值。
		t.Fatalf("templateId %v", arr[0]["templateId"])
	}
	// 钻进这一层再核对。
	pts, _ := arr[0]["points"].([]any)
	// 结果和预期不一致就中止。
	if len(pts) != 1 {
		// 对不上就中止并带上原值。
		t.Fatalf("points %v", pts)
	}
	// 钻进这一层再核对。
	p0 := pts[0].(map[string]any)
	// 这个键不该留下来。
	if _, ok := p0["junk"]; ok {
		// 多余键还在就中止。
		t.Fatalf("point junk kept")
	}
	// 钻进这一层再核对。
	pose := p0["pose"].(map[string]any)
	// 结果和预期不一致就中止。
	if pose["x"] != 1.0 || pose["y"] != 0.0 {
		// 对不上就中止并带上原值。
		t.Fatalf("pose %v", pose)
	}
}

// 空正文的工程应套成空数组。
func TestApplyProjectEmpty(t *testing.T) {
	// 先把模版收成规范正文。
	raw, err := Marshal(Default(KindProject))
	// 收不成规范正文就停。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 用这份正文去套模版。
	out, err := Apply(raw, nil)
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 结果和预期不一致就中止。
	if string(out) != "[]" {
		// 对不上就中止并带上原值。
		t.Fatalf("%s", out)
	}
}

// 没选进种类表的份整段丢掉。
func TestApplyProjectDropsUnselectedKind(t *testing.T) {
	// 拼一份只覆盖这一处的模版。
	sch := Schema{Root: RootArray, Kinds: []string{ItemSingle}}
	// 先把模版收成规范正文。
	raw, err := Marshal(sch)
	// 收不成规范正文就停。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 用这份正文去套模版。
	out, err := Apply(raw, []byte(`[{"kind":"multi","name":"多层"},{"kind":"single","name":"单"}]`))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 准备接套开之后的结构。
	var arr []map[string]any
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(out, &arr); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 结果和预期不一致就中止。
	if len(arr) != 1 || arr[0]["name"] != "单" || arr[0]["kind"] != ItemSingle {
		// 对不上就中止并带上原值。
		t.Fatalf("%s", out)
	}
}

// 只有附加、重复种类、空名称都要拒绝。
func TestValidateProjectKinds(t *testing.T) {
	// 本该拒绝的模版却通过了。
	if err := Validate(Schema{Root: RootArray, Kinds: []string{ItemExtra}}); err == nil {
		t.Fatal("extra-only")
	}
	// 本该拒绝的模版却通过了。
	if err := Validate(Schema{Root: RootArray, Kinds: []string{ItemSingle, ItemSingle}}); err == nil {
		t.Fatal("dup")
	}
	// 本该通过的检查失败了。
	if err := Validate(Default(KindProject)); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 本该拒绝的模版却通过了。
	if err := Validate(Schema{Root: RootArray, Templates: []ProjectTemplate{{ID: defaultTplSingle, Name: "", Kind: ItemSingle}}}); err == nil {
		t.Fatal("empty name")
	}
	// 本该拒绝的模版却通过了。
	if err := Validate(Schema{Root: RootArray, Templates: []ProjectTemplate{{ID: defaultTplSingle, Name: "x", Kind: ItemExtra}}}); err == nil {
		t.Fatal("extra kind")
	}
	// 本该通过的检查失败了。
	if err := Validate(Schema{Root: RootArray}); err != nil {
		t.Fatal(err)
	}
}

// 数字串收成数字，布尔收成枚举，对不上用默认。
func TestApplyEnumAndText(t *testing.T) {
	// 拼一份只覆盖这一处的模版。
	sch := Schema{Root: RootObject, Fields: []Field{
		{Key: "name", Label: "名称", Type: TypeString},
		{Key: "amp", Label: "振幅", Type: TypeString, Unit: "mm", Default: 4.0},
		{Key: "on", Label: "启用", Type: TypeString, Options: []string{"否", "是"}, Default: "否"},
	}}
	// 先把模版收成规范正文。
	raw, err := Marshal(sch)
	// 收不成规范正文就停。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 用这份正文去套模版。
	out, err := Apply(raw, []byte(`{"name":"a","amp":"1.5","on":true,"extra":1}`))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 准备接套开之后的结构。
	var m map[string]any
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(out, &m); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 结果和预期不一致就中止。
	if m["amp"] != 1.5 {
		// 对不上就中止并带上原值。
		t.Fatalf("amp %v", m["amp"])
	}
	// 结果和预期不一致就中止。
	if m["on"] != "是" {
		// 对不上就中止并带上原值。
		t.Fatalf("on %v", m["on"])
	}
	// 这个键不该留下来。
	if _, ok := m["extra"]; ok {
		// 多余键还在就中止。
		t.Fatalf("extra kept")
	}
	// 用这份正文去套模版。
	out, err = Apply(raw, []byte(`{"on":"也许"}`))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(out, &m); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 结果和预期不一致就中止。
	if m["on"] != "否" {
		// 对不上就中止并带上原值。
		t.Fatalf("bad enum %v", m["on"])
	}
}

// 旧写法的是，应套成布尔真。
func TestApplyBoolFlag(t *testing.T) {
	// 拼一份只覆盖这一处的模版。
	sch := Schema{Root: RootObject, Fields: []Field{flag("on", "启用", false)}}
	// 先把模版收成规范正文。
	raw, err := Marshal(sch)
	// 收不成规范正文就停。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 用这份正文去套模版。
	out, err := Apply(raw, []byte(`{"on":"是"}`))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 准备接套开之后的结构。
	var m map[string]any
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(out, &m); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 结果和预期不一致就中止。
	if m["on"] != true {
		// 对不上就中止并带上原值。
		t.Fatalf("on %v", m["on"])
	}
}

// 显式空对象要留下，缺键才补缺省结构。
func TestApplyKeepsNullObject(t *testing.T) {
	// 拼一份只覆盖这一处的模版。
	sch := Schema{Root: RootObject, Fields: []Field{
		{Key: "ref", Label: "参考", Type: TypeObject, Fields: poseFields()},
	}}
	// 先把模版收成规范正文。
	raw, err := Marshal(sch)
	// 收不成规范正文就停。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 用这份正文去套模版。
	out, err := Apply(raw, []byte(`{"ref":null}`))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 准备接套开之后的结构。
	var m map[string]any
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(out, &m); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 结果和预期不一致就中止。
	if m["ref"] != nil {
		// 对不上就中止并带上原值。
		t.Fatalf("ref %v", m["ref"])
	}
	// 用这份正文去套模版。
	out, err = Apply(raw, []byte(`{}`))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(out, &m); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 结果和预期不一致就中止。
	if m["ref"] == nil {
		// 缺了该有的结构就中止。
		t.Fatal("missing ref should default")
	}
}

// 键里有横线就不能当字段表。
func TestValidateRejectsBadKey(t *testing.T) {
	// 拼一份只覆盖这一处的模版。
	s := Schema{Root: RootObject, Fields: []Field{{Key: "a-b", Label: "坏", Type: TypeNumber}}}
	// 本该拒绝的模版却通过了。
	if err := Validate(s); err == nil {
		// 本该拒绝时却通过了，就中止。
		t.Fatal("want invalid")
	}
}

// 工艺引用可以没有选项，带上选项就要拒绝。
func TestValidateProcessType(t *testing.T) {
	// 拼一份只覆盖这一处的模版。
	ok := Schema{Root: RootObject, Fields: []Field{{Key: "p", Label: "工艺", Type: TypeProcess, Default: ""}}}
	// 本该通过的检查失败了。
	if err := Validate(ok); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 拼一份只覆盖这一处的模版。
	bad := Schema{Root: RootObject, Fields: []Field{{Key: "p", Label: "工艺", Type: TypeProcess, Options: []string{"a"}}}}
	// 本该拒绝的模版却通过了。
	if err := Validate(bad); err == nil {
		// 本该拒绝时却通过了，就中止。
		t.Fatal("want invalid options")
	}
}

// 工艺身份去掉空白；不是字符串就当未选。
func TestApplyProcessID(t *testing.T) {
	// 拼一份只覆盖这一处的模版。
	sch := Schema{Root: RootObject, Fields: []Field{{Key: "p", Label: "工艺", Type: TypeProcess, Default: ""}}}
	// 先把模版收成规范正文。
	raw, err := Marshal(sch)
	// 收不成规范正文就停。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 用这份正文去套模版。
	out, err := Apply(raw, []byte(`{"p":"  abc  ","extra":1}`))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 准备接套开之后的结构。
	var m map[string]any
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(out, &m); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 结果和预期不一致就中止。
	if m["p"] != "abc" {
		// 对不上就中止并带上原值。
		t.Fatalf("p %v", m["p"])
	}
	// 用这份正文去套模版。
	out, err = Apply(raw, []byte(`{"p":1}`))
	// 套用失败则后面无从核对。
	if err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 拆不开就不是套用后的正文。
	if err := json.Unmarshal(out, &m); err != nil {
		// 失败就中止本用例。
		t.Fatal(err)
	}
	// 结果和预期不一致就中止。
	if m["p"] != "" {
		// 对不上就中止并带上原值。
		t.Fatalf("non-string %v", m["p"])
	}
}

// 旧工程登记簿不含路径键，工艺引用要在。
func TestDefaultProjectRefsProcessID(t *testing.T) {
	// 取出旧工程登记簿。
	s := Default(KindProject)
	// 结果和预期不一致就中止。
	if len(s.Templates) != 2 || s.Item != nil || len(s.Kinds) != 0 {
		// 对不上就中止并带上原值。
		t.Fatalf("templates %+v item %v kinds %v", s.Templates, s.Item, s.Kinds)
	}
	// 把三种根项的键摊平。
	var keys []string
	// 三种根项都要看一遍。
	for _, k := range []string{ItemSingle, ItemMulti, ItemTBar} {
		fields := ItemFields(k, true)
		item := Field{Type: TypeObject, Fields: fields}
		keys = append(keys, fieldKeys(&item)...)
	}
	// 按键计数，看缺了还是多了。
	has := map[string]int{}
	// 每个键都计入次数。
	for _, k := range keys {
		// 这个键出现一次就加一。
		has[k]++
	}
	// 这份结构对不上就中止。
	if has["processPath"] != 0 {
		// 不该出现的键还在就中止。
		t.Fatalf("processPath still in catalog: %v", keys)
	}
	// 这份结构对不上就中止。
	if has["process"] != 0 {
		// 不该出现的键还在就中止。
		t.Fatalf("nested process still in catalog: %v", keys)
	}
	// 这份结构对不上就中止。
	if has["processId"] < 4 {
		// 对不上就中止并带上原值。
		t.Fatalf("processId count %d want >=4 in %v", has["processId"], keys)
	}
	// 先当没有，后面见到再改。
	n := 0
	// 三种根项都要看一遍。
	for _, k := range []string{ItemSingle, ItemMulti, ItemTBar} {
		item := Field{Type: TypeObject, Fields: ItemFields(k, true)}
		n += countProcessType(&item)
	}
	// 这份结构对不上就中止。
	if n < 5 {
		// 对不上就中止并带上原值。
		t.Fatalf("process type count %d want >=5", n)
	}
	// 这份结构对不上就中止。
	if has["cornerGroupParams"] != 0 {
		// 不该出现的键还在就中止。
		t.Fatalf("corner still in catalog: %v", keys)
	}
}

// 空库三份，旧库两份，包角拆行时丢掉。
func TestSeedProjectItemsThree(t *testing.T) {
	// 取出空库准备好的三份。
	seed := SeedProjectItems()
	// 结果和预期不一致就中止。
	if len(seed) != 3 {
		// 对不上就中止并带上原值。
		t.Fatalf("seed %d", len(seed))
	}
	// 取出旧库拆行用的两份。
	legacy := LegacyProjectItems()
	// 结果和预期不一致就中止。
	if len(legacy) != 2 {
		// 对不上就中止并带上原值。
		t.Fatalf("legacy %d", len(legacy))
	}
	// 准备接要核对的那一份。
	var tbar *ProjectItemSchema
	// 在空库三份里找这一份。
	for i := range seed {
		// 这一份是 T 排才记下。
		if seed[i].ID == SeedTplTBar {
			// 记下这一份，后面核对字段。
			tbar = &seed[i]
		}
	}
	// 这份结构对不上就中止。
	if tbar == nil || tbar.Name != "T排对接" {
		// 对不上就中止并带上原值。
		t.Fatalf("tbar %+v", seed)
	}
	// 摊平这一份的全部键。
	keys := fieldKeys(&Field{Type: TypeObject, Fields: tbar.Fields})
	// 按键计数，看缺了还是多了。
	has := map[string]int{}
	// 每个键都计入次数。
	for _, k := range keys {
		// 这个键出现一次就加一。
		has[k]++
	}
	// 这份结构对不上就中止。
	if has["gapBands"] == 0 || has["rootProcessId"] == 0 || has["capProcessId"] == 0 {
		// 对不上就中止并带上原值。
		t.Fatalf("tbar fields %v", keys)
	}
	// 这份结构对不上就中止。
	if has["processPath"] != 0 {
		// 不该出现的键还在就中止。
		t.Fatalf("processPath in tbar: %v", keys)
	}
	// 准备接要核对的那一份。
	var multi *ProjectItemSchema
	// 在空库三份里找这一份。
	for i := range seed {
		// 这一份是多层才记下。
		if seed[i].ID == SeedTplMulti {
			// 记下这一份，后面核对字段。
			multi = &seed[i]
		}
	}
	// 这份结构对不上就中止。
	if multi == nil {
		// 缺了该有的结构就中止。
		t.Fatal("missing multi seed")
	}
	// 摊平这一份的全部键。
	mkeys := fieldKeys(&Field{Type: TypeObject, Fields: multi.Fields})
	// 按键计数，看缺了还是多了。
	mhas := map[string]int{}
	// 每个键都计入次数。
	for _, k := range mkeys {
		// 这个键出现一次就加一。
		mhas[k]++
	}
	// 这份结构对不上就中止。
	if mhas["refPointXMiddle"] == 0 || mhas["refPointZMiddle"] == 0 || mhas["kind"] == 0 {
		// 缺了该有的结构就中止。
		t.Fatalf("multi missing app fields %v", mkeys)
	}
	// 先当没有，后面见到再改。
	on := false
	// 在单层字段里找启用开关。
	for _, f := range itemSingle(true) {
		// 找到启用开关再看它的类型。
		if f.Key == "isEnabled" {
			// 启用开关必须是布尔。
			on = f.Type == TypeBool
		}
	}
	// 这份结构对不上就中止。
	if !on {
		// 对不上就中止并带上原值。
		t.Fatal("isEnabled not bool")
	}
	// 用旧登记簿试拆行。
	got := ExpandLegacyProject(Default(KindProject))
	// 结果和预期不一致就中止。
	if len(got) != 2 {
		// 对不上就中止并带上原值。
		t.Fatalf("expand default %d", len(got))
	}
	// 形状对不上推断就中止。
	if InferItemKind(map[string]any{"gapBands": []any{}}) != ItemTBar {
		t.Fatalf("infer gapBands")
	}
	// 形状对不上推断就中止。
	if InferItemKind(map[string]any{"points": []any{map[string]any{"type": "GROOVE_A_LOWER"}}}) != ItemTBar {
		t.Fatalf("infer groove")
	}
	// 形状对不上推断就中止。
	if InferItemKind(map[string]any{"cornerGroupParams": map[string]any{}}) != ItemSingle {
		t.Fatalf("infer corner")
	}
	// 带上包角，看拆行会不会丢掉。
	dropped := ExpandLegacyProject(Schema{Root: RootArray, Templates: []ProjectTemplate{
		{ID: defaultTplSingle, Name: "单层焊道", Kind: ItemSingle, Extra: true},
		{ID: "33333333-3333-4333-8333-333333333333", Name: "包角", Kind: "corner", Extra: true},
	}})
	// 结果和预期不一致就中止。
	if len(dropped) != 1 || dropped[0].ID != defaultTplSingle {
		// 不该出现的键还在就中止。
		t.Fatalf("drop corner %+v", dropped)
	}
}

// 把这份字段和子字段的键摊平，给断言数个数。
func fieldKeys(f *Field) []string {
	// 空字段没有键也没有引用。
	if f == nil {
		return nil
	}
	// 准备接摊平之后的键。
	var out []string
	// 有键才计入这一份。
	if f.Key != "" {
		// 把这个键收进结果里。
		out = append(out, f.Key)
	}
	// 子字段继续往下摊平。
	for i := range f.Fields {
		// 子字段的键接在后面。
		out = append(out, fieldKeys(&f.Fields[i])...)
	}
	// 数组元素的键也要算。
	if f.Items != nil {
		// 元素里的键接在后面。
		out = append(out, fieldKeys(f.Items)...)
	}
	return out
}

// countProcessType 统计工艺引用字段个数。
func countProcessType(f *Field) int {
	// 空字段没有键也没有引用。
	if f == nil {
		return 0
	}
	// 先当没有，后面见到再改。
	n := 0
	// 这一处就是工艺引用。
	if f.Type == TypeProcess {
		// 这一处引用计入个数。
		n++
	}
	// 子字段继续往下摊平。
	for i := range f.Fields {
		// 把这一份的工艺引用个数累上。
		n += countProcessType(&f.Fields[i])
	}
	// 数组元素的键也要算。
	if f.Items != nil {
		// 把这一份的工艺引用个数累上。
		n += countProcessType(f.Items)
	}
	return n
}
