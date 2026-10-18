package assetcode

import "testing"

// 云端、厂端、本机三种编号都要拼对。
func TestFormat(t *testing.T) {
	// 按种类和短码把编号拼出来。
	got, err := Format("process", OriginWAN, 1)
	// 出错或结果对不上就停住本用例。
	if err != nil || got != "GY-W-000001" {
		// 云端工艺编号不对就停住。
		t.Fatalf("wan process: %s %v", got, err)
	}
	// 按种类和短码把编号拼出来。
	got, err = Format("project", "F01", 45)
	// 出错或结果对不上就停住本用例。
	if err != nil || got != "GC-F01-000045" {
		// 厂端工程编号不对就停住。
		t.Fatalf("factory project: %s %v", got, err)
	}
	// 按种类和短码把编号拼出来。
	got, err = Format("process", "C0008", 12)
	// 出错或结果对不上就停住本用例。
	if err != nil || got != "GY-C0008-000012" {
		// 按种类和短码把编号拼出来和预期不符就停住。
		t.Fatalf("client: %s %v", got, err)
	}
	// 没有出错就按成功返回，不用再补救。
	if _, err := Format("process", OriginWAN, 0); err == nil {
		// 序号为零还能编出来就停住。
		t.Fatal("seq 0")
	}
	// 结果和预期不符就进入失败。
	if !MatchKind("process", "GY-W-000001") || MatchKind("project", "GY-W-000001") {
		// 种类和预期不符就停住。
		t.Fatal("kind")
	}
	// 把厂序号拼成两位短码。
	f, err := FormatFactory(1)
	// 出错或结果对不上就停住本用例。
	if err != nil || f != "F01" || !ValidFactoryOrigin(f) || ValidFactoryOrigin("F00") {
		// 厂短码不对就停住。
		t.Fatalf("factory origin: %s %v", f, err)
	}
	// 把本机序号拼成四位短码。
	c, err := FormatClient(8)
	// 出错或结果对不上就停住本用例。
	if err != nil || c != "C0008" || !ValidClientOrigin(c) {
		// 本机短码不对就停住。
		t.Fatalf("client origin: %s %v", c, err)
	}
}

// 三种短码都要原样取出，坏串和空串只回空。
func TestOrigin(t *testing.T) {
	// 逐条列出编号和应取出的短码。
	cases := map[string]string{
		"GY-C0008-000012": "C0008",
		"GC-F01-000001":   "F01",
		"GY-W-000001":     "W",
		"GY-X01-000001":   "",
		"GY-C0008-12":     "",
		"":                "",
	}
	// 逐条核对取出的短码。
	for code, want := range cases {
		// 取出的短码和预期不同就报错。
		if got := Origin(code); got != want {
			// 带上编号和两边结果，便于看出偏在哪。
			t.Errorf("Origin(%q) = %q, want %q", code, got, want)
		}
	}
}
