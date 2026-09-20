// 更新目录进度：空闲、更换中、失败。
package appupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProgressPhases(t *testing.T) {
	dir := Dir(t.TempDir())
	phase, _, _, _, err := dir.Progress()
	if err != nil || phase != "idle" {
		t.Fatalf("idle %s %v", phase, err)
	}
	if err := dir.Stage("factory_service", 5, []byte("digest-not-checked"), []byte("tar")); err != nil {
		t.Fatal(err)
	}
	phase, kind, ver, _, err := dir.Progress()
	if err != nil || phase != "applying" || kind != "factory_service" || ver != 5 {
		t.Fatalf("applying %s %s %d %v", phase, kind, ver, err)
	}
	raw, err := json.Marshal(statusMeta{OK: false, Error: "health check failed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(string(dir), statusFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	phase, kind, ver, errMsg, err := dir.Progress()
	if err != nil || phase != "fail" || kind != "factory_service" || ver != 5 || errMsg != "health check failed" {
		t.Fatalf("fail %s %s %d %q %v", phase, kind, ver, errMsg, err)
	}
}

func TestImagesJSON(t *testing.T) {
	dir := Dir(t.TempDir())
	raw, err := dir.ImagesJSON()
	if err != nil || raw != nil {
		t.Fatalf("missing %q %v", raw, err)
	}
	body := []byte(`{"kind":"factory_service","used":10,"count":1,"items":[]}`)
	if err := os.WriteFile(filepath.Join(string(dir), imagesFile), body, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := dir.ImagesJSON()
	if err != nil || string(got) != string(body) {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestRequestPruneWritesRef(t *testing.T) {
	dir := Dir(t.TempDir())
	if err := dir.RequestPrune("app:old"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(string(dir), pruneReq))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "app:old" {
		t.Fatalf("req %s", raw)
	}
	if err := dir.RequestPrune(""); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(filepath.Join(string(dir), pruneReq))
	if err != nil || string(raw) != "ALL" {
		t.Fatalf("all %s %v", raw, err)
	}
}
