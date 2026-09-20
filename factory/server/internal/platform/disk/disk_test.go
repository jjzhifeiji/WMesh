package disk

import "testing"

func TestOfRoot(t *testing.T) {
	got, err := Of("/")
	if err != nil {
		t.Fatal(err)
	}
	if got.Total <= 0 || got.Avail < 0 || got.Used < 0 {
		t.Fatalf("%+v", got)
	}
}
