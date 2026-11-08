package contenttpl

import (
	"strings"
	"testing"
)

// 网页送来什么都以现存几何为准；对不上身份的焊道不动；没有包角的正文字节不动。
func TestRestoreCornerGeometry(t *testing.T) {
	// 现存正文，层参数用好认的数。
	stored := []byte(`[{"id":"w2","points":[{"x":987.25}],"cornerGroupParams":{"groupId":"g","layerCount":3,"passes":[{"length":777.5}],"processId":"p"}}]`)
	// 遮过的正文不带几何。
	masked := string(MaskCornerGeometry(stored))
	// 几何不能出现。
	if strings.Contains(masked, "777.5") || strings.Contains(masked, "987.25") || strings.Contains(masked, "layerCount") {
		// 把结果打出来。
		t.Fatalf("masked %s", masked)
	}
	// 盖回遮过的正文。
	back := string(RestoreCornerGeometry(stored, []byte(masked)))
	// 几何回来了。
	if !strings.Contains(back, "777.5") || !strings.Contains(back, "987.25") || !strings.Contains(back, `"layerCount":3`) {
		// 把结果打出来。
		t.Fatalf("restored %s", back)
	}
	// 网页伪造的几何和换掉的工艺引用。
	forged := []byte(`[{"id":"w2","points":[{"x":1}],"cornerGroupParams":{"groupId":"g","layerCount":9,"passes":[],"processId":"q"}}]`)
	// 盖回。
	out := string(RestoreCornerGeometry(stored, forged))
	// 几何以现存为准，工艺引用用送来的。
	if strings.Contains(out, `"layerCount":9`) || !strings.Contains(out, "777.5") || !strings.Contains(out, `"processId":"q"`) {
		// 把结果打出来。
		t.Fatalf("forged %s", out)
	}
	// 没有包角的正文字节不动。
	plain := []byte(`[ {"id":"w1"} ]`)
	// 现存没有包角，送来的原样。
	if got := RestoreCornerGeometry(plain, plain); string(got) != string(plain) {
		// 把结果打出来。
		t.Fatalf("plain %s", got)
	}
	// 只有工艺引用的包角组（多层、T 排）不算几何，不遮。
	refOnly := []byte(`[{"id":"w3","points":[{"x":2}],"cornerGroupParams":{"processId":"p"}}]`)
	// 字节不动。
	if got := MaskCornerGeometry(refOnly); string(got) != string(refOnly) {
		// 把结果打出来。
		t.Fatalf("ref only %s", got)
	}
}
