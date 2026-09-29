// 云端工艺/工程另存为新草稿，不看可复制，原件不动。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/domain"
	global "wmesh/global/internal/service"
)

// 另存工艺成新草稿，原件修订和正文不动。
func TestCopyPlatformProcess(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 固定云端口令，登录和泄密检查都用它。
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		// 立云端超管失败就停，立不住后面没有人能登录。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", wanPass)
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 准备正文样本，后面入库和比对都用它。
	body := []byte("wan-copy-src")
	// 新建平台工艺「源工艺」，建不成后面没有工艺号可对。
	src, err := h.WAN.CreatePlatformProcess(ctx, tok, "源工艺", body)
	// 新建平台工艺「源工艺」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「源工艺」失败。
		t.Fatal(err)
	}
	// 原件这时不该可复制，可复制说明默认错了。
	if src.Copyable {
		// 不符即停：原件这时不该可复制。
		t.Fatal("default copyable")
	}
	// 另存工艺「新工艺」，另存失败就没有新草稿。
	got, err := h.WAN.CopyPlatformProcess(ctx, tok, src.ID, "新工艺")
	// 另存工艺「新工艺」失败就停，另存失败就没有新草稿。
	if err != nil {
		// 不符即停：另存工艺「新工艺」失败。
		t.Fatal(err)
	}
	// 返回值多项不符预期就停，说明没有按规则落下。
	if got.ID == src.ID || got.Revision != 1 || got.Status != global.AssetDraft || got.Copyable || got.Name != "新工艺" || got.Kind != global.KindProcess {
		// 不符即停：返回值多项不符预期。
		t.Fatalf("%+v", got)
	}
	// 读取资产，读不到就无法核对原件。
	still, err := h.WAN.GetPlatformAsset(ctx, tok, src.ID)
	// 读取资产失败或修订没有跟上就停，不能当通过。
	if err != nil || still.Revision != src.Revision || still.Name != src.Name || still.Copyable {
		// 不符即停：读取资产失败或修订没有跟上。
		t.Fatalf("src %+v %v", still, err)
	}
	// 读平台正文，读不到就无法比对。
	copied, err := h.WAN.ReadPlatformAssetContent(ctx, tok, got.ID)
	// 读平台正文失败或正文不一致就停，不能当通过。
	if err != nil || !bytes.Equal(copied, applyProcess(body)) {
		// 不符即停：读平台正文失败或正文不一致。
		t.Fatalf("%q %v", copied, err)
	}
	// 另存工艺「」应被拒为名称不合法，放行或错类都算没拦住。
	if _, err := h.WAN.CopyPlatformProcess(ctx, tok, src.ID, "  "); !errors.Is(err, domain.ErrInvalidName) {
		// 不符即停：另存工艺「」应被拒为名称不合法。
		t.Fatalf("empty name: %v", err)
	}
	// 发布资产，发不出去后面不能当可用。
	pub, err := h.WAN.PublishPlatformAsset(ctx, tok, src.ID, src.Revision)
	// 发布资产失败就停，发不出去后面不能当可用。
	if err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 停用资产，停用失败仍会被当成可用。
	off, err := h.WAN.DisablePlatformAsset(ctx, tok, pub.ID, pub.Revision)
	// 停用资产失败就停，停用失败仍会被当成可用。
	if err != nil {
		// 不符即停：停用资产失败。
		t.Fatal(err)
	}
	// 另存工艺「停用」应被拒为资产不可用，放行或错类都算没拦住。
	if _, err := h.WAN.CopyPlatformProcess(ctx, tok, off.ID, "停用"); !errors.Is(err, domain.ErrAssetNotAvailable) {
		// 不符即停：另存工艺「停用」应被拒为资产不可用。
		t.Fatalf("disabled: %v", err)
	}
}

// 另存工程成新草稿，依赖钉住且原件不动。
func TestCopyPlatformProject(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 固定云端口令，登录和泄密检查都用它。
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		// 立云端超管失败就停，立不住后面没有人能登录。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", wanPass)
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 新建平台工艺「钉工艺」，建不成后面没有工艺号可对。
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "钉工艺", []byte(`{"name":"p"}`))
	// 新建平台工艺「钉工艺」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「钉工艺」失败。
		t.Fatal(err)
	}
	// 发布资产，发不出去后面不能当可用。
	pub, err := h.WAN.PublishPlatformAsset(ctx, tok, proc.ID, proc.Revision)
	// 发布资产失败就停，发不出去后面不能当可用。
	if err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 新建平台工程「源工程」，建不成后面没有工程可引用。
	src, err := h.WAN.CreatePlatformProject(ctx, tok, "源工程", []byte(`[]`), []global.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}})
	// 新建平台工程「源工程」失败就停，建不成后面没有工程可引用。
	if err != nil {
		// 不符即停：新建平台工程「源工程」失败。
		t.Fatal(err)
	}
	// 原件多项不符预期就停，说明没有按规则落下。
	if !src.Copyable || src.Kind != global.KindProject || len(src.Deps) != 1 {
		// 不符即停：原件多项不符预期。
		t.Fatalf("%+v", src)
	}
	// 另存工艺「新工程」，另存失败就没有新草稿。
	got, err := h.WAN.CopyPlatformProcess(ctx, tok, src.ID, "新工程")
	// 另存工艺「新工程」失败就停，另存失败就没有新草稿。
	if err != nil {
		// 不符即停：另存工艺「新工程」失败。
		t.Fatal(err)
	}
	// 返回值多项不符预期就停，说明没有按规则落下。
	if got.ID == src.ID || got.Kind != global.KindProject || got.Status != global.AssetDraft || !got.Copyable || got.Name != "新工程" {
		// 不符即停：返回值多项不符预期。
		t.Fatalf("%+v", got)
	}
	// 返回值多项不符预期就停，说明没有按规则落下。
	if len(got.Deps) != 1 || got.Deps[0].ID != pub.ID || got.Deps[0].Revision != pub.Revision {
		// 不符即停：返回值多项不符预期。
		t.Fatalf("deps %+v", got.Deps)
	}
	// 读平台正文，读不到就无法比对。
	want, err := h.WAN.ReadPlatformAssetContent(ctx, tok, src.ID)
	// 读平台正文失败就停，读不到就无法比对。
	if err != nil {
		// 不符即停：读平台正文失败。
		t.Fatal(err)
	}
	// 读平台正文，读不到就无法比对。
	copied, err := h.WAN.ReadPlatformAssetContent(ctx, tok, got.ID)
	// 读平台正文失败或正文不一致就停，不能当通过。
	if err != nil || !bytes.Equal(copied, want) {
		// 不符即停：读平台正文失败或正文不一致。
		t.Fatalf("%q vs %q %v", copied, want, err)
	}
	// 读取资产，读不到就无法核对原件。
	still, err := h.WAN.GetPlatformAsset(ctx, tok, src.ID)
	// 读取资产失败或修订没有跟上就停，不能当通过。
	if err != nil || still.Revision != src.Revision || still.Name != src.Name {
		// 不符即停：读取资产失败或修订没有跟上。
		t.Fatalf("src %+v %v", still, err)
	}
}

// 新建时可指定可复制，缺省则草稿不可复制。
func TestCreatePlatformProcessCopyable(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 固定云端口令，登录和泄密检查都用它。
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		// 立云端超管失败就停，立不住后面没有人能登录。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", wanPass)
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 准备正文样本，后面入库和比对都用它。
	body := []byte("wan-create-copyable")
	// 按可复制新建工艺「开焊」，指定可复制失败默认验不成。
	wide, err := h.WAN.CreatePlatformProcessWith(ctx, tok, "开焊", body, true)
	// 可复制工艺多项不符预期就停，说明没有按规则落下。
	if err != nil || !wide.Copyable || wide.Revision != 1 || wide.Status != global.AssetDraft {
		// 不符即停：可复制工艺多项不符预期。
		t.Fatalf("%+v %v", wide, err)
	}
	// 新建平台工艺「密焊」，建不成后面没有工艺号可对。
	tight, err := h.WAN.CreatePlatformProcess(ctx, tok, "密焊", body)
	// 新建平台工艺「密焊」失败或竟可复制就停，不能当通过。
	if err != nil || tight.Copyable || tight.Revision != 1 {
		// 不符即停：新建平台工艺「密焊」失败或竟可复制。
		t.Fatalf("%+v %v", tight, err)
	}
}
