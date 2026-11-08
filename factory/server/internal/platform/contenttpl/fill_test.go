// 示教器新建只补键不删键：模版缺的补上，示教器写了的原样留着。
package contenttpl

import (
	"encoding/json"
	"testing"
)

// 工艺只写了名字和电流：模版里的其它键补默认，电流不被改，示教器自有的键也留着。
func TestFillMissingKeepsOriginal(t *testing.T) {
	// 取出默认工艺模版。
	raw, err := Marshal(Default(KindProcess))
	// 取不出就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 示教器传来的正文：只有和默认不同的值，外加一个模版里没有的键。
	orig := []byte(`{"name":"ciqmb","current":177,"padOnly":"x"}`)
	// 先按模版套一遍。
	applied, err := Apply(raw, orig)
	// 套不上就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 再以原正文为准补缺。
	out, err := FillMissing(orig, applied)
	// 补不了就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 准备读出结果。
	var m map[string]any
	// 读不出就停住。
	if err := json.Unmarshal(out, &m); err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 原值和示教器自有键都要留着。
	if m["name"] != "ciqmb" || m["current"] != float64(177) || m["padOnly"] != "x" {
		// 原值被改或被删就停住。
		t.Fatalf("original lost: %s", out)
	}
	// 模版里的其它键要补上。
	if _, ok := m["offsetX"]; !ok {
		// 没补就停住。
		t.Fatalf("offsetX not filled: %s", out)
	}
}

// 工程数组等长逐项补；长度对不上（模版丢过项）就整段留原样；原正文不是 JSON 也原样交回。
func TestFillMissingArrays(t *testing.T) {
	// 等长：每项补上模版身份。
	out, err := FillMissing([]byte(`[{"id":"a","points":[]}]`), []byte(`[{"templateId":"t1","points":[1]}]`))
	// 补不了就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 模版身份补上，示教器写的点位不换。
	if string(out) != `[{"id":"a","points":[],"templateId":"t1"}]` {
		// 结果不对就停住。
		t.Fatalf("array fill = %s", out)
	}
	// 长度对不上就留原样。
	out, _ = FillMissing([]byte(`[{"id":"a"},2]`), []byte(`[{"templateId":"t1"}]`))
	// 被改就停住。
	if string(out) != `[{"id":"a"},2]` {
		// 结果不对就停住。
		t.Fatalf("length mismatch = %s", out)
	}
	// 一个键都没补就交回原字节，键序和空白都不动。
	out, _ = FillMissing([]byte(`{"name": "mine","current":170}`), []byte(`{"current":1,"name":"x"}`))
	// 被重排就停住。
	if string(out) != `{"name": "mine","current":170}` {
		// 结果不对就停住。
		t.Fatalf("nothing added = %s", out)
	}
	// 原正文不是 JSON 就原样交回。
	out, _ = FillMissing([]byte(`mine`), []byte(`{}`))
	// 被改就停住。
	if string(out) != "mine" {
		// 结果不对就停住。
		t.Fatalf("non-json = %s", out)
	}
}

// 补键时原正文里的数字按原文留着：177.0 不写成 177，大整数不丢精度。
func TestFillMissingKeepsNumberText(t *testing.T) {
	// 原正文带小数写法和超过浮点精度的整数，缺 extra 键。
	out, err := FillMissing([]byte(`{"current":177.0,"big":9007199254740993}`), []byte(`{"current":177,"big":1,"extra":2.5}`))
	// 补不了就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	// 数字原文和补上的键都要在。
	if want := `{"big":9007199254740993,"current":177.0,"extra":2.5}`; string(out) != want {
		// 原文被改就停住。
		t.Fatalf("got %s want %s", out, want)
	}
}
