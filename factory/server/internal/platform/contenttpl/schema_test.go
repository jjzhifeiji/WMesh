// 套用字段表：填缺省、丢掉未知键、非法 JSON 回退缺省。
package contenttpl

import (
	"encoding/json"
	"testing"
)

func TestApplyFillsAndStrips(t *testing.T) {
	raw, err := Marshal(Default(KindProcess))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Apply(raw, []byte(`{"name":"1K","current":250,"extra":1,"offsetX":"1.5"}`))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["extra"]; ok {
		t.Fatalf("extra kept: %s", out)
	}
	if m["name"] != "1K" {
		t.Fatalf("name %v", m["name"])
	}
	if m["current"] != 250.0 {
		t.Fatalf("current %v", m["current"])
	}
	if m["offsetX"] != 1.5 {
		t.Fatalf("offsetX %v", m["offsetX"])
	}
	if m["voltage"] != 24.0 {
		t.Fatalf("voltage default %v", m["voltage"])
	}
	osc, _ := m["oscillation"].(map[string]any)
	if osc["frequency"] != 2.0 {
		t.Fatalf("osc %v", osc)
	}
}

func TestApplyInvalidJSONUsesDefaults(t *testing.T) {
	raw, err := Marshal(Default(KindProcess))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Apply(raw, []byte("not-json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["current"] != 200.0 {
		t.Fatalf("%v", m["current"])
	}
}

func TestApplyProjectArray(t *testing.T) {
	raw, err := Marshal(Default(KindProject))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Apply(raw, []byte(`[{"name":"焊道 1","points":[{"type":"START","pose":{"x":1},"junk":true}],"secret":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(out, &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 || arr[0]["name"] != "焊道 1" || arr[0]["kind"] != ItemSingle {
		t.Fatalf("%s", out)
	}
	if _, ok := arr[0]["secret"]; ok {
		t.Fatalf("secret kept")
	}
	if _, ok := arr[0]["basePath"]; ok {
		t.Fatalf("stuffed multi")
	}
	if _, ok := arr[0]["cornerGroupParams"]; ok {
		t.Fatalf("stuffed corner")
	}
	if _, ok := arr[0]["passes"]; ok {
		t.Fatalf("stuffed passes")
	}
	if arr[0]["templateId"] != defaultTplSingle {
		t.Fatalf("templateId %v", arr[0]["templateId"])
	}
	pts, _ := arr[0]["points"].([]any)
	if len(pts) != 1 {
		t.Fatalf("points %v", pts)
	}
	p0 := pts[0].(map[string]any)
	if _, ok := p0["junk"]; ok {
		t.Fatalf("point junk kept")
	}
	pose := p0["pose"].(map[string]any)
	if pose["x"] != 1.0 || pose["y"] != 0.0 {
		t.Fatalf("pose %v", pose)
	}
}

func TestApplyProjectEmpty(t *testing.T) {
	raw, err := Marshal(Default(KindProject))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Apply(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "[]" {
		t.Fatalf("%s", out)
	}
}

func TestApplyProjectDropsUnselectedKind(t *testing.T) {
	sch := Schema{Root: RootArray, Kinds: []string{ItemSingle}}
	raw, err := Marshal(sch)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Apply(raw, []byte(`[{"kind":"multi","name":"多层"},{"kind":"single","name":"单"}]`))
	if err != nil {
		t.Fatal(err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(out, &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 || arr[0]["name"] != "单" || arr[0]["kind"] != ItemSingle {
		t.Fatalf("%s", out)
	}
}

func TestValidateProjectKinds(t *testing.T) {
	if err := Validate(Schema{Root: RootArray, Kinds: []string{ItemExtra}}); err == nil {
		t.Fatal("extra-only")
	}
	if err := Validate(Schema{Root: RootArray, Kinds: []string{ItemSingle, ItemSingle}}); err == nil {
		t.Fatal("dup")
	}
	if err := Validate(Default(KindProject)); err != nil {
		t.Fatal(err)
	}
	if err := Validate(Schema{Root: RootArray, Templates: []ProjectTemplate{{ID: defaultTplSingle, Name: "", Kind: ItemSingle}}}); err == nil {
		t.Fatal("empty name")
	}
	if err := Validate(Schema{Root: RootArray, Templates: []ProjectTemplate{{ID: defaultTplSingle, Name: "x", Kind: ItemExtra}}}); err == nil {
		t.Fatal("extra kind")
	}
	if err := Validate(Schema{Root: RootArray}); err != nil {
		t.Fatal(err)
	}
}

func TestApplyEnumAndText(t *testing.T) {
	sch := Schema{Root: RootObject, Fields: []Field{
		{Key: "name", Label: "名称", Type: TypeString},
		{Key: "amp", Label: "振幅", Type: TypeString, Unit: "mm", Default: 4.0},
		{Key: "on", Label: "启用", Type: TypeString, Options: []string{"否", "是"}, Default: "否"},
	}}
	raw, err := Marshal(sch)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Apply(raw, []byte(`{"name":"a","amp":"1.5","on":true,"extra":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["amp"] != 1.5 {
		t.Fatalf("amp %v", m["amp"])
	}
	if m["on"] != "是" {
		t.Fatalf("on %v", m["on"])
	}
	if _, ok := m["extra"]; ok {
		t.Fatalf("extra kept")
	}
	out, err = Apply(raw, []byte(`{"on":"也许"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["on"] != "否" {
		t.Fatalf("bad enum %v", m["on"])
	}
}

func TestValidateRejectsBadKey(t *testing.T) {
	s := Schema{Root: RootObject, Fields: []Field{{Key: "a-b", Label: "坏", Type: TypeNumber}}}
	if err := Validate(s); err == nil {
		t.Fatal("want invalid")
	}
}

func TestValidateProcessType(t *testing.T) {
	ok := Schema{Root: RootObject, Fields: []Field{{Key: "p", Label: "工艺", Type: TypeProcess, Default: ""}}}
	if err := Validate(ok); err != nil {
		t.Fatal(err)
	}
	bad := Schema{Root: RootObject, Fields: []Field{{Key: "p", Label: "工艺", Type: TypeProcess, Options: []string{"a"}}}}
	if err := Validate(bad); err == nil {
		t.Fatal("want invalid options")
	}
}

func TestApplyProcessID(t *testing.T) {
	sch := Schema{Root: RootObject, Fields: []Field{{Key: "p", Label: "工艺", Type: TypeProcess, Default: ""}}}
	raw, err := Marshal(sch)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Apply(raw, []byte(`{"p":"  abc  ","extra":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["p"] != "abc" {
		t.Fatalf("p %v", m["p"])
	}
	out, err = Apply(raw, []byte(`{"p":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["p"] != "" {
		t.Fatalf("non-string %v", m["p"])
	}
}

func TestDefaultProjectRefsProcessID(t *testing.T) {
	s := Default(KindProject)
	if len(s.Templates) != 3 || s.Item != nil || len(s.Kinds) != 0 {
		t.Fatalf("templates %+v item %v kinds %v", s.Templates, s.Item, s.Kinds)
	}
	var keys []string
	for _, k := range []string{ItemSingle, ItemMulti, ItemCorner} {
		fields := ItemFields(k, true)
		item := Field{Type: TypeObject, Fields: fields}
		keys = append(keys, fieldKeys(&item)...)
	}
	has := map[string]int{}
	for _, k := range keys {
		has[k]++
	}
	if has["processPath"] != 0 {
		t.Fatalf("processPath still in catalog: %v", keys)
	}
	if has["process"] != 0 {
		t.Fatalf("nested process still in catalog: %v", keys)
	}
	if has["processId"] < 5 {
		t.Fatalf("processId count %d want >=5 in %v", has["processId"], keys)
	}
	n := 0
	for _, k := range []string{ItemSingle, ItemMulti, ItemCorner} {
		item := Field{Type: TypeObject, Fields: ItemFields(k, true)}
		n += countProcessType(&item)
	}
	if n < 5 {
		t.Fatalf("process type count %d want >=5", n)
	}
	if has["cornerGroupParams"] != 1 {
		t.Fatalf("corner missing: %v", keys)
	}
}

func TestSeedProjectItemsFour(t *testing.T) {
	seed := SeedProjectItems()
	if len(seed) != 4 {
		t.Fatalf("seed %d", len(seed))
	}
	legacy := LegacyProjectItems()
	if len(legacy) != 3 {
		t.Fatalf("legacy %d", len(legacy))
	}
	var tbar *ProjectItemSchema
	for i := range seed {
		if seed[i].ID == SeedTplTBar {
			tbar = &seed[i]
		}
	}
	if tbar == nil || tbar.Name != "T排对接" {
		t.Fatalf("tbar %+v", seed)
	}
	keys := fieldKeys(&Field{Type: TypeObject, Fields: tbar.Fields})
	has := map[string]int{}
	for _, k := range keys {
		has[k]++
	}
	if has["gapBands"] == 0 || has["rootProcessId"] == 0 || has["capProcessId"] == 0 {
		t.Fatalf("tbar fields %v", keys)
	}
	if has["processPath"] != 0 {
		t.Fatalf("processPath in tbar: %v", keys)
	}
	got := ExpandLegacyProject(Default(KindProject))
	if len(got) != 3 {
		t.Fatalf("expand default %d", len(got))
	}
	if InferItemKind(map[string]any{"gapBands": []any{}}) != ItemTBar {
		t.Fatalf("infer gapBands")
	}
	if InferItemKind(map[string]any{"points": []any{map[string]any{"type": "GROOVE_A_LOWER"}}}) != ItemTBar {
		t.Fatalf("infer groove")
	}
}

func fieldKeys(f *Field) []string {
	if f == nil {
		return nil
	}
	var out []string
	if f.Key != "" {
		out = append(out, f.Key)
	}
	for i := range f.Fields {
		out = append(out, fieldKeys(&f.Fields[i])...)
	}
	if f.Items != nil {
		out = append(out, fieldKeys(f.Items)...)
	}
	return out
}

// countProcessType 统计工艺引用字段个数。
func countProcessType(f *Field) int {
	if f == nil {
		return 0
	}
	n := 0
	if f.Type == TypeProcess {
		n++
	}
	for i := range f.Fields {
		n += countProcessType(&f.Fields[i])
	}
	if f.Items != nil {
		n += countProcessType(f.Items)
	}
	return n
}
