package contenttpl

import "testing"

// 工艺所属模版只从正文算：包角标记说了算，templateId 只认自建模版的身份，两个键矛盾时不能绕过种类约束。
func TestProcessTemplateOf(t *testing.T) {
	// 一个自建模版的身份。
	const custom = "0193a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	// 各种正文和期望的归属。
	cases := []struct {
		body string // 工艺正文
		want string // 期望的所属模版
	}{
		{`{"current":180}`, SeedTplWeld},
		{`{"kind":"cornerGeometry","passes":[]}`, SeedTplCorner},
		{`{"templateId":"` + custom + `","amp":3}`, custom},
		{`{"templateId":"0193A1B2-C3D4-7E5F-8A9B-0C1D2E3F4A5B"}`, custom},
		{`{"templateId":"` + SeedTplWeld + `","kind":"cornerGeometry"}`, SeedTplCorner},
		{`{"templateId":"` + custom + `","kind":"cornerGeometry"}`, SeedTplCorner},
		{`{"templateId":"` + SeedTplCorner + `","current":180}`, SeedTplWeld},
		{`{"templateId":"` + SeedTplWeld + `"}`, SeedTplWeld},
		{`{"templateId":"not-a-uuid"}`, SeedTplWeld},
		{`{"templateId":7,"kind":1}`, SeedTplWeld},
		{`not json`, SeedTplWeld},
		{`[{"kind":"cornerGeometry"}]`, SeedTplWeld},
	}
	// 逐条核对。
	for _, c := range cases {
		// 和期望不符就报。
		if got := ProcessTemplateOf([]byte(c.body)); got != c.want {
			// 报出正文和实得结果。
			t.Errorf("%q => %q want %q", c.body, got, c.want)
		}
		// 是不是焊接参数要和归属一致。
		if got := IsWeldProcess([]byte(c.body)); got != (c.want == SeedTplWeld) {
			// 报出正文和实得结果。
			t.Errorf("%q weld => %v", c.body, got)
		}
	}
}
