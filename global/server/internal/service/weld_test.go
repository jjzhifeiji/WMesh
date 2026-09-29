// 工艺/工程作业类型：单层焊道、多层焊缝、T排对接；工程与工艺必须同类型。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/domain"
	global "wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 验作业类型，工程必须和所引用工艺同类型。
func TestPlatformWeldKind(t *testing.T) {
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

	// 新建平台工艺「默认工艺」，建不成后面没有工艺号可对。
	def, err := h.WAN.CreatePlatformProcess(ctx, tok, "默认工艺", []byte(`{"name":"p"}`))
	// 新建平台工艺「默认工艺」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「默认工艺」失败。
		t.Fatal(err)
	}
	// 默认工艺作业类型不是单层就停，这一步不能算通过。
	if def.WeldKind != store.WeldKindSingle {
		// 不符即停：默认工艺作业类型不是单层。
		t.Fatalf("default %s", def.WeldKind)
	}
	// 新建平台工艺「坏类型」应被拒为类型非法，放行或错类都算没拦住。
	if _, err := h.WAN.CreatePlatformProcess(ctx, tok, "坏类型", []byte(`{"name":"p"}`), "nope"); !errors.Is(err, domain.ErrInvalidWeldKind) {
		// 不符即停：新建平台工艺「坏类型」应被拒为类型非法。
		t.Fatalf("invalid: %v", err)
	}
	// 新建平台工艺「多层工艺」，建不成后面没有工艺号可对。
	multiP, err := h.WAN.CreatePlatformProcess(ctx, tok, "多层工艺", []byte(`{"name":"m"}`), "multi")
	// 新建平台工艺「多层工艺」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「多层工艺」失败。
		t.Fatal(err)
	}
	// 多层工艺作业类型不是多层就停，这一步不能算通过。
	if multiP.WeldKind != store.WeldKindMultilayer {
		// 不符即停：多层工艺作业类型不是多层。
		t.Fatalf("alias %s", multiP.WeldKind)
	}
	// 另存工艺「多层副本」，另存失败就没有新草稿。
	copied, err := h.WAN.CopyPlatformProcess(ctx, tok, multiP.ID, "多层副本")
	// 另存工艺「多层副本」失败或作业类型不是多层就停，不能当通过。
	if err != nil || copied.WeldKind != store.WeldKindMultilayer {
		// 不符即停：另存工艺「多层副本」失败或作业类型不是多层。
		t.Fatalf("copy %+v %v", copied, err)
	}

	// 发布资产，发不出去后面不能当可用。
	pub, err := h.WAN.PublishPlatformAsset(ctx, tok, multiP.ID, multiP.Revision)
	// 发布资产失败就停，发不出去后面不能当可用。
	if err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 准备正文样本，后面入库和比对都用它。
	singleBody := []byte(`[{"templateId":"` + contenttpl.SeedTplSingle + `","name":"单"}]`)
	// 新建平台工程「单层工程钉多层」应被拒为类型不一致，放行或错类都算没拦住。
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "单层工程钉多层", singleBody, []global.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}}, store.WeldKindSingle); !errors.Is(err, domain.ErrWeldKindMismatch) {
		t.Fatalf("dep mismatch: %v", err)
	}
	// 准备正文样本，后面入库和比对都用它。
	multiBody := []byte(`[{"templateId":"` + contenttpl.SeedTplMulti + `","name":"多层"}]`)
	// 新建平台工程「单层工程用多层模版」应被拒为类型不一致，放行或错类都算没拦住。
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "单层工程用多层模版", multiBody, nil, store.WeldKindSingle); !errors.Is(err, domain.ErrWeldKindMismatch) {
		// 不符即停：新建平台工程「单层工程用多层模版」应被拒为类型不一致。
		t.Fatalf("content mismatch: %v", err)
	}
	// 新建平台工程「多层工程」，建不成后面没有工程可引用。
	ok, err := h.WAN.CreatePlatformProject(ctx, tok, "多层工程", multiBody, []global.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}}, store.WeldKindMultilayer)
	// 新建平台工程「多层工程」失败就停，建不成后面没有工程可引用。
	if err != nil {
		// 不符即停：新建平台工程「多层工程」失败。
		t.Fatal(err)
	}
	// 这份工程作业类型不是多层就停，这一步不能算通过。
	if ok.WeldKind != store.WeldKindMultilayer {
		// 不符即停：这份工程作业类型不是多层。
		t.Fatalf("project %s", ok.WeldKind)
	}

	// 改作业类型，类型改不了工程会对不上。
	changed, err := h.WAN.SetPlatformWeldKind(ctx, tok, def.ID, def.Revision, store.WeldKindTBar)
	// 改作业类型失败或作业类型不是T排就停，不能当通过。
	if err != nil || changed.WeldKind != store.WeldKindTBar {
		// 不符即停：改作业类型失败或作业类型不是T排。
		t.Fatalf("set kind %+v %v", changed, err)
	}
	// 改作业类型，类型改不了工程会对不上。
	switched, err := h.WAN.SetPlatformWeldKind(ctx, tok, ok.ID, ok.Revision, store.WeldKindSingle)
	// 改作业类型失败或作业类型不是单层就停，不能当通过。
	if err != nil || switched.WeldKind != store.WeldKindSingle {
		// 不符即停：改作业类型失败或作业类型不是单层。
		t.Fatalf("project kind %+v %v", switched, err)
	}
	// 读平台正文，读不到就无法比对。
	cleared, err := h.WAN.ReadPlatformAssetContent(ctx, tok, switched.ID)
	// 读平台正文失败或正文没有清空就停，不能当通过。
	if err != nil || string(cleared) != "[]" {
		// 不符即停：读平台正文失败或正文没有清空。
		t.Fatalf("project body %s %v", cleared, err)
	}
	// 改作业类型应被拒为类型非法，放行或错类都算没拦住。
	if _, err := h.WAN.SetPlatformWeldKind(ctx, tok, changed.ID, changed.Revision, "nope"); !errors.Is(err, domain.ErrInvalidWeldKind) {
		// 不符即停：改作业类型应被拒为类型非法。
		t.Fatalf("bad kind: %v", err)
	}
}
