// 作业类型按空库三份模版身份和焊缝种类判定。
package contenttpl

import "testing"

// 单层、多层、T 排和自定义份要按模版与种类分开。
func TestContentMatchesWeldKind(t *testing.T) {
	// 单层正文带上空库单层模版。
	single := []byte(`[{"templateId":"` + SeedTplSingle + `","kind":"single"}]`)
	// 多层正文带上空库多层模版。
	multi := []byte(`[{"templateId":"` + SeedTplMulti + `","kind":"multi"}]`)
	// T 排只写种类，不写模版。
	tbar := []byte(`[{"kind":"tbar"}]`)
	// 自定义份用认不出的模版和种类。
	custom := []byte(`[{"templateId":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","kind":"extra"}]`)
	// 单层应通过，多层不得冒充单层。
	if !ContentMatchesWeldKind(WeldSingle, single) || ContentMatchesWeldKind(WeldSingle, multi) {
		// 串了单层和多层就中止。
		t.Fatal("single vs multi")
	}
	// T 排应通过，单层不得冒充 T 排。
	if !ContentMatchesWeldKind(WeldTBar, tbar) || ContentMatchesWeldKind(WeldSingle, tbar) {
		// T 排串了种类就中止。
		t.Fatal("tbar kind")
	}
	// 认不出的自定义份应放过。
	if !ContentMatchesWeldKind(WeldSingle, custom) {
		// 自定义被拒就中止。
		t.Fatal("custom")
	}
	// 多层应推断成多层，空正文退回单层。
	if InferWeldKind(multi) != WeldMultilayer || InferWeldKind(nil) != WeldSingle {
		// 推断出的类型不对就中止。
		t.Fatal("infer")
	}
}
