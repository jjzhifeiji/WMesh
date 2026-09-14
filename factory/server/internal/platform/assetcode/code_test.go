package assetcode

import "testing"

func TestFormat(t *testing.T) {
	got, err := Format("process", OriginWAN, 1)
	if err != nil || got != "GY-W-000001" {
		t.Fatalf("wan process: %s %v", got, err)
	}
	got, err = Format("project", "F01", 45)
	if err != nil || got != "GC-F01-000045" {
		t.Fatalf("factory project: %s %v", got, err)
	}
	got, err = Format("process", "C0008", 12)
	if err != nil || got != "GY-C0008-000012" {
		t.Fatalf("client: %s %v", got, err)
	}
	if _, err := Format("process", OriginWAN, 0); err == nil {
		t.Fatal("seq 0")
	}
	if !MatchKind("process", "GY-W-000001") || MatchKind("project", "GY-W-000001") {
		t.Fatal("kind")
	}
	f, err := FormatFactory(1)
	if err != nil || f != "F01" || !ValidFactoryOrigin(f) || ValidFactoryOrigin("F00") {
		t.Fatalf("factory origin: %s %v", f, err)
	}
	c, err := FormatClient(8)
	if err != nil || c != "C0008" || !ValidClientOrigin(c) {
		t.Fatalf("client origin: %s %v", c, err)
	}
}
