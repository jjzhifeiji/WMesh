// 作业类型按空库三份模版身份和焊缝种类判定。
package contenttpl

import "testing"

func TestContentMatchesWeldKind(t *testing.T) {
	single := []byte(`[{"templateId":"` + SeedTplSingle + `","kind":"single"}]`)
	multi := []byte(`[{"templateId":"` + SeedTplMulti + `","kind":"multi"}]`)
	tbar := []byte(`[{"kind":"tbar"}]`)
	custom := []byte(`[{"templateId":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","kind":"extra"}]`)
	if !ContentMatchesWeldKind(WeldSingle, single) || ContentMatchesWeldKind(WeldSingle, multi) {
		t.Fatal("single vs multi")
	}
	if !ContentMatchesWeldKind(WeldTBar, tbar) || ContentMatchesWeldKind(WeldSingle, tbar) {
		t.Fatal("tbar kind")
	}
	if !ContentMatchesWeldKind(WeldSingle, custom) {
		t.Fatal("custom")
	}
	if InferWeldKind(multi) != WeldMultilayer || InferWeldKind(nil) != WeldSingle {
		t.Fatal("infer")
	}
}
