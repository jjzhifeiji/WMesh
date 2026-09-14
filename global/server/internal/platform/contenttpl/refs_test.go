// 工艺引用：按模版收集、改写身份，不扫正文里多出来的键。
package contenttpl

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

func projectSchema(t *testing.T) []byte {
	t.Helper()
	raw, err := Marshal(Default(KindProject))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCollectProcessIDs(t *testing.T) {
	sch := projectSchema(t)
	raw := []byte(`[
		{"kind":"single","processId":"A","extraProcesses":[{"id":"x","processId":"C"}]},
		{"kind":"multi","basePath":{"processId":"A"},"passes":[{"processId":"B"}]},
		{"kind":"corner","processId":"D","cornerGroupParams":{"processId":"D"}}
	]`)
	ids, err := CollectProcessIDs(sch, raw)
	if err != nil {
		t.Fatal(err)
	}
	got := append([]string(nil), ids...)
	slices.Sort(got)
	if !slices.Equal(got, []string{"A", "B", "C", "D"}) {
		t.Fatalf("%v", ids)
	}
	ids, err = CollectProcessIDs(sch, []byte("job"))
	if err != nil || len(ids) != 0 {
		t.Fatalf("%v %v", ids, err)
	}
	if _, err := CollectProcessIDs(sch, []byte(`[{"processId":1}]`)); !errors.Is(err, errBadProcessID) {
		t.Fatalf("got %v", err)
	}
}

func TestCollectSkipsUnselectedKind(t *testing.T) {
	sch, err := Marshal(Schema{Root: RootArray, Kinds: []string{ItemSingle}})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := CollectProcessIDs(sch, []byte(`[{"processId":"KEEP","extraProcesses":[{"processId":"SKIP"}],"basePath":{"processId":"SKIP2"}}]`))
	if err != nil || len(ids) != 1 || ids[0] != "KEEP" {
		t.Fatalf("%v %v", ids, err)
	}
}

func TestCollectFollowsSchema(t *testing.T) {
	sch, err := Marshal(Schema{Root: RootArray, Item: &Field{Type: TypeObject, Label: "焊缝", Fields: []Field{
		{Key: "alt", Label: "另", Type: TypeObject, Fields: []Field{str("processId", "工艺", "")}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := CollectProcessIDs(sch, []byte(`[{"processId":"SKIP","alt":{"processId":"KEEP"}}]`))
	if err != nil || len(ids) != 1 || ids[0] != "KEEP" {
		t.Fatalf("%v %v", ids, err)
	}
}

func TestCollectFollowsProcessType(t *testing.T) {
	sch, err := Marshal(Schema{Root: RootArray, Item: &Field{Type: TypeObject, Label: "焊缝", Fields: []Field{
		{Key: "foo", Label: "工艺", Type: TypeProcess, Default: ""},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := CollectProcessIDs(sch, []byte(`[{"foo":"KEEP","processId":"SKIP"}]`))
	if err != nil || len(ids) != 1 || ids[0] != "KEEP" {
		t.Fatalf("%v %v", ids, err)
	}
}

func TestRewriteFollowsSchema(t *testing.T) {
	sch, err := Marshal(Schema{Root: RootArray, Item: &Field{Type: TypeObject, Label: "焊缝", Fields: []Field{
		{Key: "alt", Label: "另", Type: TypeObject, Fields: []Field{str("processId", "工艺", "")}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`[{"processId":"SKIP","alt":{"processId":"KEEP"}}]`)
	out, err := RewriteProcessIDs(sch, raw, map[string]string{"KEEP": "NEXT"})
	if err != nil {
		t.Fatal(err)
	}
	var welds []map[string]any
	if err := json.Unmarshal(out, &welds); err != nil {
		t.Fatal(err)
	}
	if welds[0]["processId"] != "SKIP" {
		t.Fatalf("template-extra %s", out)
	}
	alt := welds[0]["alt"].(map[string]any)
	if alt["processId"] != "NEXT" {
		t.Fatalf("alt %v", alt)
	}
}

func TestRewriteProcessIDs(t *testing.T) {
	sch := projectSchema(t)
	raw := []byte(`[{"kind":"multi","name":"w","keep":true,"basePath":{"processId":"fac-1"},"passes":[{"processId":"fac-1"}]}]`)
	out, err := RewriteProcessIDs(sch, raw, map[string]string{"fac-1": "plat-1"})
	if err != nil {
		t.Fatal(err)
	}
	var welds []map[string]any
	if err := json.Unmarshal(out, &welds); err != nil {
		t.Fatal(err)
	}
	if welds[0]["keep"] != true {
		t.Fatalf("%s", out)
	}
	base, _ := welds[0]["basePath"].(map[string]any)
	if base["processId"] != "plat-1" {
		t.Fatalf("base %v", base)
	}
	passes, _ := welds[0]["passes"].([]any)
	p0 := passes[0].(map[string]any)
	if p0["processId"] != "plat-1" {
		t.Fatalf("pass %v", p0)
	}
	same, err := RewriteProcessIDs(sch, []byte("job"), map[string]string{"fac-1": "plat-1"})
	if err != nil || string(same) != "job" {
		t.Fatalf("%s %v", same, err)
	}
	if _, err := RewriteProcessIDs(sch, raw, map[string]string{"other": "plat-1"}); !errors.Is(err, errUnknownProcessID) {
		t.Fatalf("got %v", err)
	}
}
