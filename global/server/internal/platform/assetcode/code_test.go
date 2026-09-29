package assetcode

import "testing"

// 三种短码都应拼对，越界和错种类要拒绝。
func TestFormat(t *testing.T) {
	// 云端工艺从 1 编起。
	got, err := Format("process", OriginWAN, 1)
	// 必须是工艺、云端、六位 1。
	if err != nil || got != "GY-W-000001" {
		// 对不上就中止并带上结果。
		t.Fatalf("wan process: %s %v", got, err)
	}
	// 厂工程带两位厂短码。
	got, err = Format("project", "F01", 45)
	// 必须是工程、该厂、六位序号。
	if err != nil || got != "GC-F01-000045" {
		// 对不上就中止并带上结果。
		t.Fatalf("factory project: %s %v", got, err)
	}
	// 设备工艺带四位设备短码。
	got, err = Format("process", "C0008", 12)
	// 必须是工艺、该设备、六位序号。
	if err != nil || got != "GY-C0008-000012" {
		// 对不上就中止并带上结果。
		t.Fatalf("client: %s %v", got, err)
	}
	// 序号为零不能发号。
	if _, err := Format("process", OriginWAN, 0); err == nil {
		// 不该放行时就中止本用例。
		t.Fatal("seq 0")
	}
	// 前缀必须跟种类一致，不能串。
	if !MatchKind("process", "GY-W-000001") || MatchKind("project", "GY-W-000001") {
		// 种类串了就中止本用例。
		t.Fatal("kind")
	}
	// 厂序号 1 应收成两位。
	f, err := FormatFactory(1)
	// 全零不得当有效厂码。
	if err != nil || f != "F01" || !ValidFactoryOrigin(f) || ValidFactoryOrigin("F00") {
		// 厂短码不对就中止并带上结果。
		t.Fatalf("factory origin: %s %v", f, err)
	}
	// 设备序号 8 应收成四位。
	c, err := FormatClient(8)
	// 形态必须是设备短码。
	if err != nil || c != "C0008" || !ValidClientOrigin(c) {
		// 设备短码不对就中止并带上结果。
		t.Fatalf("client origin: %s %v", c, err)
	}
}
