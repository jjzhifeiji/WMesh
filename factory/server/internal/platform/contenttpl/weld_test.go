// 作业类型按空库三份模版身份和焊缝种类判定。
package contenttpl

import "testing"

// 正文和焊缝种类一致才算对上。
func TestContentMatchesWeldKind(t *testing.T) {
	// 准备一份测试或签名用的字节。
	single := []byte(`[{"templateId":"` + SeedTplSingle + `","kind":"single"}]`)
	// 准备一份测试或签名用的字节。
	multi := []byte(`[{"templateId":"` + SeedTplMulti + `","kind":"multi"}]`)
	// 准备一份测试或签名用的字节。
	tbar := []byte(`[{"kind":"tbar"}]`)
	// 准备一份测试或签名用的字节。
	custom := []byte(`[{"templateId":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","kind":"extra"}]`)
	// 结果和预期不符就进入失败。
	if !ContentMatchesWeldKind(WeldSingle, single) || ContentMatchesWeldKind(WeldSingle, multi) {
		// 核对正文和焊缝种类是否一致和预期不符就停住。
		t.Fatal("single vs multi")
	}
	// 丁字排和预期不符就进入失败。
	if !ContentMatchesWeldKind(WeldTBar, tbar) || ContentMatchesWeldKind(WeldSingle, tbar) {
		// 种类和预期不符就停住。
		t.Fatal("tbar kind")
	}
	// 结果和预期不符就进入失败。
	if !ContentMatchesWeldKind(WeldSingle, custom) {
		// 核对正文和焊缝种类是否一致和预期不符就停住。
		t.Fatal("custom")
	}
	// 结果和预期不符就进入失败。
	if InferWeldKind(multi) != WeldMultilayer || InferWeldKind(nil) != WeldSingle {
		// 从正文推断焊缝种类和预期不符就停住。
		t.Fatal("infer")
	}
}
