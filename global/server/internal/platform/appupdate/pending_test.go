// 更新目录进度：空闲、更换中、成功。
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
	if err := dir.Stage("wan_service", 4, []byte("digest-not-checked"), []byte("tar")); err != nil {
		t.Fatal(err)
	}
	phase, kind, ver, _, err := dir.Progress()
	if err != nil || phase != "applying" || kind != "wan_service" || ver != 4 {
		t.Fatalf("applying %s %s %d %v", phase, kind, ver, err)
	}
	raw, err := json.Marshal(statusMeta{OK: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(string(dir), statusFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	phase, kind, ver, errMsg, err := dir.Progress()
	if err != nil || phase != "ok" || kind != "wan_service" || ver != 4 || errMsg != "" {
		t.Fatalf("ok %s %s %d %q %v", phase, kind, ver, errMsg, err)
	}
}

func TestImagesJSON(t *testing.T) {
	dir := Dir(t.TempDir())
	raw, err := dir.ImagesJSON()
	if err != nil || raw != nil {
		t.Fatalf("missing %q %v", raw, err)
	}
	body := []byte(`{"kind":"wan_service","used":10,"count":1,"items":[]}`)
	if err := os.WriteFile(filepath.Join(string(dir), imagesFile), body, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := dir.ImagesJSON()
	if err != nil || string(got) != string(body) {
		t.Fatalf("got %q %v", got, err)
	}
}
