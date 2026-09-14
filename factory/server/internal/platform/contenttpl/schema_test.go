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
	if len(arr) != 1 || arr[0]["name"] != "焊道 1" {
		t.Fatalf("%s", out)
	}
	if _, ok := arr[0]["secret"]; ok {
		t.Fatalf("secret kept")
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

func TestDefaultProjectRefsProcessID(t *testing.T) {
	keys := fieldKeys(Default(KindProject).Item)
	has := map[string]int{}
	for _, k := range keys {
		has[k]++
	}
	if has["processPath"] != 0 {
		t.Fatalf("processPath still in default: %v", keys)
	}
	if has["process"] != 0 {
		t.Fatalf("nested process still in default: %v", keys)
	}
	if has["processId"] < 5 {
		t.Fatalf("processId count %d want >=5 in %v", has["processId"], keys)
	}
	if has["cornerGroupParams"] != 1 {
		t.Fatalf("corner missing: %v", keys)
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
