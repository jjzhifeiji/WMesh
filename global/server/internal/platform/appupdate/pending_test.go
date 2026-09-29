// 更新目录进度：空闲、更换中、成功。
package appupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// 空目录是空闲，落下包后是更换中，写上成功结果后是完成。
func TestProgressPhases(t *testing.T) {
	// 用一块空的临时目录。
	dir := Dir(t.TempDir())
	// 什么都没有时应是空闲。
	phase, _, _, _, err := dir.Progress()
	// 空闲对不上就中止。
	if err != nil || phase != "idle" {
		// 阶段不对就中止并带上原值。
		t.Fatalf("idle %s %v", phase, err)
	}
	// 落下待切换包，还没有结果。
	if err := dir.Stage("wan_service", 4, []byte("digest-not-checked"), []byte("tar")); err != nil {
		// 包没落下就中止本用例。
		t.Fatal(err)
	}
	// 此时应是正在换，种类和版本跟落下的一致。
	phase, kind, ver, _, err := dir.Progress()
	// 更换中对不上就中止。
	if err != nil || phase != "applying" || kind != "wan_service" || ver != 4 {
		// 阶段不对就中止并带上原值。
		t.Fatalf("applying %s %s %d %v", phase, kind, ver, err)
	}
	// 假装 updater 写了探活通过。
	raw, err := json.Marshal(statusMeta{OK: true})
	// 结果收不成则后面写不了。
	if err != nil {
		// 收不成结果就中止本用例。
		t.Fatal(err)
	}
	// 把成功结果写进目录。
	if err := os.WriteFile(filepath.Join(string(dir), statusFile), raw, 0o600); err != nil {
		// 写入失败就中止本用例。
		t.Fatal(err)
	}
	// 有成功结果后应是完成，且没有失败原因。
	phase, kind, ver, errMsg, err := dir.Progress()
	// 完成对不上就中止。
	if err != nil || phase != "ok" || kind != "wan_service" || ver != 4 || errMsg != "" {
		// 阶段不对就中止并带上原值。
		t.Fatalf("ok %s %s %d %q %v", phase, kind, ver, errMsg, err)
	}
}

// 还没有占用文件时应为空，写上之后原样读回。
func TestImagesJSON(t *testing.T) {
	// 用一块空的临时目录。
	dir := Dir(t.TempDir())
	// 文件不在应是空，不当错误。
	raw, err := dir.ImagesJSON()
	// 不该读出内容或错误。
	if err != nil || raw != nil {
		// 对不上就中止本用例。
		t.Fatalf("missing %q %v", raw, err)
	}
	// 准备一份镜像占用。
	body := []byte(`{"kind":"wan_service","used":10,"count":1,"items":[]}`)
	// 把占用正文写进更新目录。
	if err := os.WriteFile(filepath.Join(string(dir), imagesFile), body, 0o600); err != nil {
		// 写入失败就中止本用例。
		t.Fatal(err)
	}
	// 写上之后应原样读回。
	got, err := dir.ImagesJSON()
	// 内容和错误都要对。
	if err != nil || string(got) != string(body) {
		// 对不上就中止本用例。
		t.Fatalf("got %q %v", got, err)
	}
}

// 指定引用和空引用应分别写成那一份和全部。
func TestRequestPruneWritesRef(t *testing.T) {
	// 用一块空的临时目录。
	dir := Dir(t.TempDir())
	// 请只清这一份镜像。
	if err := dir.RequestPrune("app:old"); err != nil {
		// 请求写失败就中止。
		t.Fatal(err)
	}
	// 把清镜像请求再读回来。
	raw, err := os.ReadFile(filepath.Join(string(dir), pruneReq))
	// 读不到就不知道写了什么。
	if err != nil {
		// 读不到请求就中止本用例。
		t.Fatal(err)
	}
	// 正文必须就是这一份引用。
	if string(raw) != "app:old" {
		// 对不上就中止本用例。
		t.Fatalf("req %s", raw)
	}
	// 空引用表示清全部可清镜像。
	if err := dir.RequestPrune(""); err != nil {
		// 第二次请求失败就中止。
		t.Fatal(err)
	}
	// 再读回，应被换成全部。
	raw, err = os.ReadFile(filepath.Join(string(dir), pruneReq))
	// 必须是全部，且读得出来。
	if err != nil || string(raw) != "ALL" {
		// 对不上就中止本用例。
		t.Fatalf("all %s %v", raw, err)
	}
}
