// 更新目录进度：空闲、更换中、失败。
package appupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// 空目录是空闲，落下包是更换中，失败要能读出。
func TestProgressPhases(t *testing.T) {
	// 向测试框架要一块临时目录。
	dir := Dir(t.TempDir())
	// 读出当前换包进行到哪一步。
	phase, _, _, _, err := dir.Progress()
	// 出错或阶段不对就停住本用例。
	if err != nil || phase != "idle" {
		// 空闲阶段不对就停住。
		t.Fatalf("idle %s %v", phase, err)
	}
	// 没能把确认过的包暂存进更新目录就停住本用例。
	if err := dir.Stage("factory_service", 5, []byte("digest-not-checked"), []byte("tar")); err != nil {
		// 没能把确认过的包暂存进更新目录就停住本用例。
		t.Fatal(err)
	}
	// 读出当前换包进行到哪一步。
	phase, kind, ver, _, err := dir.Progress()
	// 出错或阶段不对就停住本用例。
	if err != nil || phase != "applying" || kind != "factory_service" || ver != 5 {
		// 更换阶段不对就停住。
		t.Fatalf("applying %s %s %d %v", phase, kind, ver, err)
	}
	// 把结构收成字节，再交给后面。
	raw, err := json.Marshal(statusMeta{OK: false, Error: "health check failed"})
	// 没能把结构收成字节就停住本用例。
	if err != nil {
		// 没能把结构收成字节就停住本用例。
		t.Fatal(err)
	}
	// 没能拼出同一目录下的路径就停住本用例。
	if err := os.WriteFile(filepath.Join(string(dir), statusFile), raw, 0o600); err != nil {
		// 没能拼出同一目录下的路径就停住本用例。
		t.Fatal(err)
	}
	// 读出当前换包进行到哪一步。
	phase, kind, ver, errMsg, err := dir.Progress()
	// 出错或阶段不对就停住本用例。
	if err != nil || phase != "fail" || kind != "factory_service" || ver != 5 || errMsg != "health check failed" {
		// 失败阶段不对就停住。
		t.Fatalf("fail %s %s %d %q %v", phase, kind, ver, errMsg, err)
	}
}

// 没有占用文件就是空的，有文件就原样读回。
func TestImagesJSON(t *testing.T) {
	// 向测试框架要一块临时目录。
	dir := Dir(t.TempDir())
	// 读出本机镜像占用的原文。
	raw, err := dir.ImagesJSON()
	// 出错或结果对不上就停住本用例。
	if err != nil || raw != nil {
		// 缺了该有的内容就停住。
		t.Fatalf("missing %q %v", raw, err)
	}
	// 准备一份测试或签名用的字节。
	body := []byte(`{"kind":"factory_service","used":10,"count":1,"items":[]}`)
	// 没能拼出同一目录下的路径就停住本用例。
	if err := os.WriteFile(filepath.Join(string(dir), imagesFile), body, 0o600); err != nil {
		// 没能拼出同一目录下的路径就停住本用例。
		t.Fatal(err)
	}
	// 读出本机镜像占用的原文。
	got, err := dir.ImagesJSON()
	// 出错或结果对不上就停住本用例。
	if err != nil || string(got) != string(body) {
		// 结果和预期不符就停住。
		t.Fatalf("got %q %v", got, err)
	}
}

// 点名和清空两种请求都要写进文件。
func TestRequestPruneWritesRef(t *testing.T) {
	// 向测试框架要一块临时目录。
	dir := Dir(t.TempDir())
	// 没能写下让更新器清理镜像的请求就停住本用例。
	if err := dir.RequestPrune("app:old"); err != nil {
		// 没能写下让更新器清理镜像的请求就停住本用例。
		t.Fatal(err)
	}
	// 拼出同一目录下的路径。
	raw, err := os.ReadFile(filepath.Join(string(dir), pruneReq))
	// 没能拼出同一目录下的路径就停住本用例。
	if err != nil {
		// 没能拼出同一目录下的路径就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if string(raw) != "app:old" {
		// 和预期不符就停住本用例。
		t.Fatalf("req %s", raw)
	}
	// 没能写下让更新器清理镜像的请求就停住本用例。
	if err := dir.RequestPrune(""); err != nil {
		// 没能写下让更新器清理镜像的请求就停住本用例。
		t.Fatal(err)
	}
	// 拼出同一目录下的路径。
	raw, err = os.ReadFile(filepath.Join(string(dir), pruneReq))
	// 出错或结果对不上就停住本用例。
	if err != nil || string(raw) != "ALL" {
		// 拼出同一目录下的路径和预期不符就停住。
		t.Fatalf("all %s %v", raw, err)
	}
}
