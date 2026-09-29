// 阶段3第6圈：WAN 侧升平台、不持厂内原件、平台工程依赖（9.2、10.1、10.3、12.1～12.2、13.6、14.1、18.1～18.2）。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

// 验升档、不可复制拒绝，以及平台工程依赖。
func testWANAssetPromote(t *testing.T, run func(string, func(*testing.T))) {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
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
	// 登记工厂「厂A」，建不成后面没有厂可授权。
	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 准备正文样本，后面入库和比对都用它。
	body := []byte(`{"name":"焊接","current":180}`)
	// 先算正文摘要，升档和发布都要拿它核对。
	d := digest.Sum(body)
	// 新造一个身份，后面用来区分是不是同一份。
	procSrc := id.New()

	// 9.2：升档应被拒为不可复制，放行或错类都算没拦住。
	run("9.2", func(t *testing.T) {
		// 拼升档快照，身份或摘要错了会被拒或串档。
		snap := global.AssetSnapshot{
			SourceID: id.New(), SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProcess, Name: "不可复制", Content: body, Digest: d,
			Copyable: false, Status: global.AssetAvailable,
		}
		// 升档应被拒为不可复制，放行或错类都算没拦住。
		if _, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap); !errors.Is(err, domain.ErrAssetNotCopyable) {
			// 不符即停：升档应被拒为不可复制。
			t.Fatalf("got %v", err)
		}
	})

	// 先留出位置，循环里找到再填，找不到就失败。
	var plat global.Asset
	// 10.1：平台资产多项不符预期就停，说明没有按规则落下。
	run("10.1", func(t *testing.T) {
		// 拼升档快照，身份或摘要错了会被拒或串档。
		snap := global.AssetSnapshot{
			SourceID: procSrc, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProcess, Name: "焊接", Content: body, Digest: d,
			Copyable: true, Status: global.AssetAvailable,
		}
		// 预留错误位，后面这一步失败再停。
		var err error
		// 升档，升不上去档位就断了。
		plat, err = h.WAN.PromoteFromSnapshot(ctx, tok, snap)
		// 升档失败就停，升不上去档位就断了。
		if err != nil {
			// 不符即停：升档失败。
			t.Fatal(err)
		}
		// 平台资产多项不符预期就停，说明没有按规则落下。
		if plat.ID == procSrc || plat.Copyable || plat.Level != global.AssetLevelPlatform ||
			plat.Status != global.AssetDraft || plat.SourceID == nil || *plat.SourceID != procSrc || plat.Content != nil {
			// 不符即停：平台资产多项不符预期。
			t.Fatalf("%+v", plat)
		}
	})
	// 10.4：再次结果身份变了或修订没有跟上就停，不能当通过。
	run("10.4", func(t *testing.T) {
		// 拼升档快照，身份或摘要错了会被拒或串档。
		snap := global.AssetSnapshot{
			SourceID: procSrc, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProcess, Name: "焊接", Content: body, Digest: d,
			Copyable: true, Status: global.AssetAvailable,
		}
		// 升档，升不上去档位就断了。
		again, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap)
		// 升档失败就停，升不上去档位就断了。
		if err != nil {
			// 不符即停：升档失败。
			t.Fatal(err)
		}
		// 再次结果身份变了或修订没有跟上就停，不能当通过。
		if again.ID != plat.ID || again.Revision != plat.Revision {
			// 不符即停：再次结果身份变了或修订没有跟上。
			t.Fatalf("skip %+v want %+v", again, plat)
		}
	})
	// 10.5：返回值多项不符预期就停，说明没有按规则落下。
	run("10.5", func(t *testing.T) {
		// 准备正文样本，后面入库和比对都用它。
		next := []byte(`{"name":"焊接","current":220}`)
		// 拼升档快照，身份或摘要错了会被拒或串档。
		snap := global.AssetSnapshot{
			SourceID: procSrc, SourceRevision: 2, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProcess, Name: "焊接", Content: next, Digest: digest.Sum(next),
			Copyable: true, Status: global.AssetAvailable,
		}
		// 升档，升不上去档位就断了。
		got, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap)
		// 升档失败就停，升不上去档位就断了。
		if err != nil {
			// 不符即停：升档失败。
			t.Fatal(err)
		}
		// 返回值多项不符预期就停，说明没有按规则落下。
		if got.ID != plat.ID || got.Revision != plat.Revision+1 || got.Status != global.AssetDraft || !bytes.Equal(got.Digest, digest.Sum(next)) {
			// 不符即停：返回值多项不符预期。
			t.Fatalf("overwrite %+v", got)
		}
		// 留下这一步的结果，后面几格接着用它。
		plat = got
	})
	// 10.3：改厂内资产应被拒为越权，放行或错类都算没拦住。
	run("10.3", func(t *testing.T) {
		// 改厂内资产应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.UpdateFactoryAsset(ctx, tok, fac.Factory.ID, procSrc); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：改厂内资产应被拒为越权。
			t.Fatalf("got %v", err)
		}
	})
	// 12.1：读厂侧副本应被拒为越权，放行或错类都算没拦住。
	run("12.1", func(t *testing.T) {
		// 读厂侧副本应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.GetFactoryAsset(ctx, tok, fac.Factory.ID, procSrc); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：读厂侧副本应被拒为越权。
			t.Fatalf("get: %v", err)
		}
		// 读厂内正文应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.ReadFactoryAssetContent(ctx, tok, fac.Factory.ID, procSrc); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：读厂内正文应被拒为越权。
			t.Fatalf("read: %v", err)
		}
		// 读取资产应被拒为找不到，放行或错类都算没拦住。
		if _, err := h.WAN.GetPlatformAsset(ctx, tok, procSrc); !errors.Is(err, domain.ErrNotFound) {
			// 不符即停：读取资产应被拒为找不到。
			t.Fatalf("src as platform: %v", err)
		}
	})
	// 12.2：返回值多项不符预期就停，说明没有按规则落下。
	run("12.2", func(t *testing.T) {
		// 读取资产，读不到就无法核对原件。
		got, err := h.WAN.GetPlatformAsset(ctx, tok, plat.ID)
		// 返回值多项不符预期就停，说明没有按规则落下。
		if err != nil || got.ID != plat.ID || got.ID == procSrc || got.Content != nil {
			// 不符即停：返回值多项不符预期。
			t.Fatalf("%+v %v", got, err)
		}
	})
	// 13.6：新建平台工程「依赖厂级」应被拒为依赖不符，放行或错类都算没拦住。
	run("13.6", func(t *testing.T) {
		// 新建平台工程「依赖厂级」应被拒为依赖不符，放行或错类都算没拦住。
		if _, err := h.WAN.CreatePlatformProject(ctx, tok, "依赖厂级", body, []global.AssetDep{{
			ID: procSrc, Revision: 1, Digest: d,
		}}); !errors.Is(err, domain.ErrAssetDependency) && !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v", err)
		}
	})
	// 14.1：返回值多项不符预期就停，说明没有按规则落下。
	run("14.1", func(t *testing.T) {
		// 预留错误位，后面这一步失败再停。
		var err error
		// 发布资产，发不出去后面不能当可用。
		plat, err = h.WAN.PublishPlatformAsset(ctx, tok, plat.ID, plat.Revision)
		// 发布资产失败就停，发不出去后面不能当可用。
		if err != nil {
			// 不符即停：发布资产失败。
			t.Fatal(err)
		}
		// 新造一个身份，后面用来区分是不是同一份。
		projSrc := id.New()
		// 拼升档快照，身份或摘要错了会被拒或串档。
		snap := global.AssetSnapshot{
			SourceID: projSrc, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProject, Name: "工程", Content: []byte("job-params"), Digest: digest.Sum([]byte("job-params")),
			Copyable: true, Status: global.AssetAvailable,
			Deps: []global.AssetDep{{ID: procSrc, Revision: 1, Digest: d}},
		}
		// 升档，升不上去档位就断了。
		got, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap)
		// 升档失败就停，升不上去档位就断了。
		if err != nil {
			// 不符即停：升档失败。
			t.Fatal(err)
		}
		// 返回值多项不符预期就停，说明没有按规则落下。
		if got.Kind != global.KindProject || got.Status != global.AssetDraft || got.ID == projSrc || len(got.Deps) != 1 ||
			got.Deps[0].ID != plat.ID || got.Deps[0].ID == procSrc {
			// 不符即停：返回值多项不符预期。
			t.Fatalf("%+v", got)
		}
	})
	// 18.1：审计记录缺字段就停，以后对不了账。
	run("18.1", func(t *testing.T) {
		// 读审计，读不到就无法核对有没有记。
		rows, err := h.WAN.ListAudit(ctx)
		// 读审计失败就停，读不到就无法核对有没有记。
		if err != nil {
			// 不符即停：读审计失败。
			t.Fatal(err)
		}
		// 审计记录缺字段就停，以后对不了账。
		if bad := audit.Incomplete(rows); len(bad) > 0 {
			// 不符即停：审计记录缺字段。
			t.Fatalf("incomplete %#v", bad[0])
		}
		// 审计里不能出现口令或正文，出现就是泄密。
		if audit.ContainsAny(audit.Dump(rows), string(body), wanPass, tok) {
			// 不符即停：审计里不能出现口令或正文。
			t.Fatalf("secret leaked")
		}
	})
	// 18.2：把审计打成文本正文缺了应有内容就停，这一步不能算通过。
	run("18.2", func(t *testing.T) {
		// 读审计，读不到就无法核对有没有记。
		rows, err := h.WAN.ListAudit(ctx)
		// 读审计失败就停，读不到就无法核对有没有记。
		if err != nil {
			// 不符即停：读审计失败。
			t.Fatal(err)
		}
		// 把审计打成文本正文缺了应有内容就停，这一步不能算通过。
		if !bytes.Contains([]byte(audit.Dump(rows)), []byte("rev=")) {
			// 不符即停：把审计打成文本正文缺了应有内容。
			t.Fatalf("missing revision in audit")
		}
	})
}
