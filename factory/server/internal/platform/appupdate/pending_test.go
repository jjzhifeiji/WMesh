// 更新目录进度：空闲、更换中、失败。
package appupdate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"wmesh/factory/internal/platform/domain"
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

// 假装 updater 写下一份换包结果，正文按它真写的格式给。
func writeStatus(t *testing.T, dir Dir, raw string) {
	// 出错时报在调用处，便于看是哪一步。
	t.Helper()
	// 写进更新目录，Progress 和 Stage 都会读到。
	if err := os.WriteFile(filepath.Join(string(dir), statusFile), []byte(raw), 0o600); err != nil {
		// 写入失败就中止本用例。
		t.Fatal(err)
	}
}

// 看更新目录里某个文件还在不在。
func fileThere(t *testing.T, dir Dir, name string) bool {
	// 出错时报在调用处，便于看是哪一步。
	t.Helper()
	// 只取文件信息，不读正文。
	_, err := os.Stat(filepath.Join(string(dir), name))
	// 除了不在之外的错误都说明环境不对。
	if err != nil && !os.IsNotExist(err) {
		// 查不了就中止本用例。
		t.Fatal(err)
	}
	return err == nil
}

// 上一包排队或正在换时再确认要拒绝，出了结果才放行。
func TestStageRejectsWhileApplying(t *testing.T) {
	// 用一块空的临时目录。
	dir := Dir(t.TempDir())
	// 先落下 v5，还没有结果。
	if err := dir.Stage("factory_service", 5, []byte("d5"), []byte("tar5")); err != nil {
		// 第一包没落下就中止本用例。
		t.Fatal(err)
	}
	// 没出结果时再落 v6 应被拒。
	err := dir.Stage("factory_service", 6, []byte("d6"), []byte("tar6"))
	// 必须是正在换的业务错误，HTTP 才回 409。
	if !errors.Is(err, domain.ErrSoftwareApplying) {
		// 错误不对就中止并带上原值。
		t.Fatalf("busy stage %v", err)
	}
	// 被拒后待切换的仍是 v5，包也没被换。
	phase, _, ver, _, err := dir.Progress()
	// 阶段或版本被动过就是覆盖了正在换的包。
	if err != nil || phase != "applying" || ver != 5 {
		// 对不上就中止并带上原值。
		t.Fatalf("after reject %s %d %v", phase, ver, err)
	}
	// 读回包正文，确认没被新包盖掉。
	body, err := os.ReadFile(filepath.Join(string(dir), tarFile))
	// 包读不到或已被换就中止。
	if err != nil || string(body) != "tar5" {
		// 对不上就中止并带上原值。
		t.Fatalf("tar %q %v", body, err)
	}
	// 假装 updater 装完 v5 写了结果。
	writeStatus(t, dir, `{"ok":true,"kind":"factory_service","version":5,"digest":""}`)
	// 出了结果再落 v6 应放行。
	if err := dir.Stage("factory_service", 6, []byte("d6"), []byte("tar6")); err != nil {
		// 出了结果仍被拒就中止本用例。
		t.Fatal(err)
	}
	// 放行后应是正在换 v6，旧结果已清。
	phase, _, ver, _, err = dir.Progress()
	// 阶段或版本不对就中止。
	if err != nil || phase != "applying" || ver != 6 {
		// 对不上就中止并带上原值。
		t.Fatalf("after ok %s %d %v", phase, ver, err)
	}
}

// 写包失败时，旧说明和旧结果应已先删掉，updater 不会重装旧包。
func TestStageRemovesOldMetaBeforeTar(t *testing.T) {
	// 用一块空的临时目录。
	dir := Dir(t.TempDir())
	// 先落下 v4。
	if err := dir.Stage("factory_service", 4, []byte("d4"), []byte("tar4")); err != nil {
		// 第一包没落下就中止本用例。
		t.Fatal(err)
	}
	// 假装 v4 已装好。
	writeStatus(t, dir, `{"ok":true,"kind":"factory_service","version":4,"digest":""}`)
	// 把包的临时文件位置占成目录，让写包失败。
	if err := os.Mkdir(filepath.Join(string(dir), tarFile+".tmp"), 0o700); err != nil {
		// 占不上位置就没法造失败。
		t.Fatal(err)
	}
	// 写包这一步应失败，但不是正在换。
	err := dir.Stage("factory_service", 5, []byte("d5"), []byte("tar5"))
	// 没失败或错成正在换都说明顺序不对。
	if err == nil || errors.Is(err, domain.ErrSoftwareApplying) {
		// 对不上就中止并带上原值。
		t.Fatalf("stage %v", err)
	}
	// 旧说明和旧结果都应已删掉。
	if fileThere(t, dir, pendingFile) || fileThere(t, dir, statusFile) {
		// 还留着就会让 updater 拿旧说明重装。
		t.Fatal("old pending or status left")
	}
	// 没有说明，进度应回到空闲。
	phase, _, _, _, err := dir.Progress()
	// 不是空闲就说明还会有人去装。
	if err != nil || phase != "idle" {
		// 对不上就中止并带上原值。
		t.Fatalf("phase %s %v", phase, err)
	}
}

// 结果写明 v4 而说明已是 v5 时，按结果报 v4，失败原因也带上。
func TestProgressPrefersStatusVersion(t *testing.T) {
	// 用一块空的临时目录。
	dir := Dir(t.TempDir())
	// 落下 v5 当作待切换说明。
	if err := dir.Stage("factory_service", 5, []byte("d5"), []byte("tar5")); err != nil {
		// 包没落下就中止本用例。
		t.Fatal(err)
	}
	// 结果是 updater 换 v4 时写的。
	writeStatus(t, dir, `{"ok":true,"kind":"factory_service","version":4,"digest":"ZDQ="}`)
	// 读出当前进度。
	phase, kind, ver, _, err := dir.Progress()
	// 应报 v4 成功，而不是说明里的 v5。
	if err != nil || phase != "ok" || kind != "factory_service" || ver != 4 {
		// 对不上就中止并带上原值。
		t.Fatalf("ok %s %s %d %v", phase, kind, ver, err)
	}
	// 换成 v4 失败的结果。
	writeStatus(t, dir, `{"ok":false,"kind":"factory_service","version":4,"digest":"ZDQ=","error":"health check failed"}`)
	// 再读一次进度。
	phase, kind, ver, errMsg, err := dir.Progress()
	// 应报 v4 失败并带原因。
	if err != nil || phase != "fail" || kind != "factory_service" || ver != 4 || errMsg != "health check failed" {
		// 对不上就中止并带上原值。
		t.Fatalf("fail %s %s %d %q %v", phase, kind, ver, errMsg, err)
	}
}

// 旧 updater 只写成败，进度退回读待切换说明的版本。
func TestProgressLegacyStatusUsesPending(t *testing.T) {
	// 用一块空的临时目录。
	dir := Dir(t.TempDir())
	// 落下 v5 当作待切换说明。
	if err := dir.Stage("factory_service", 5, []byte("d5"), []byte("tar5")); err != nil {
		// 包没落下就中止本用例。
		t.Fatal(err)
	}
	// 旧格式结果不带种类和版本。
	writeStatus(t, dir, `{"ok":true}`)
	// 读出当前进度。
	phase, kind, ver, _, err := dir.Progress()
	// 应按说明报 v5 成功。
	if err != nil || phase != "ok" || kind != "factory_service" || ver != 5 {
		// 对不上就中止并带上原值。
		t.Fatalf("legacy %s %s %d %v", phase, kind, ver, err)
	}
}
