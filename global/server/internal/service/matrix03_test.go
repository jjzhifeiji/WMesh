// 阶段3第4圈：WAN 侧平台级制作、升档快照、完整性（4.1、4.3、4.4、1.3、16.1～16.2）。
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

// 验平台工艺的草稿、发布和正文完整性。
func testWANAssetIdentity(t *testing.T, run func(string, func(*testing.T))) {
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
	// 准备正文样本，后面入库和比对都用它。
	body := []byte("platform-weld-secret")

	// 先留出位置，循环里找到再填，找不到就失败。
	var proc global.Asset
	// 4.1：这份工艺多项不符预期就停，说明没有按规则落下。
	run("4.1", func(t *testing.T) {
		// 预留错误位，后面这一步失败再停。
		var err error
		// 新建平台工艺「焊接」，建不成后面没有工艺号可对。
		proc, err = h.WAN.CreatePlatformProcess(ctx, tok, "焊接", body)
		// 新建平台工艺「焊接」失败就停，建不成后面没有工艺号可对。
		if err != nil {
			// 不符即停：新建平台工艺「焊接」失败。
			t.Fatal(err)
		}
		// 这份工艺多项不符预期就停，说明没有按规则落下。
		if proc.ID.String() == "" || proc.Revision != 1 || proc.Status != global.AssetDraft ||
			proc.Level != global.AssetLevelPlatform || proc.Copyable || proc.Name != "焊接" || proc.Content != nil {
			// 不符即停：这份工艺多项不符预期。
			t.Fatalf("%+v", proc)
		}
	})
	// 发布资产，发不出去后面不能当可用。
	proc, err = h.WAN.PublishPlatformAsset(ctx, tok, proc.ID, proc.Revision)
	// 发布资产失败就停，发不出去后面不能当可用。
	if err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 4.3：改可复制失败或不可复制就停，不能当通过。
	run("4.3", func(t *testing.T) {
		// 改可复制，可复制改不了升档会被挡。
		got, err := h.WAN.SetPlatformCopyable(ctx, tok, proc.ID, proc.Revision, true)
		// 改可复制失败或不可复制就停，不能当通过。
		if err != nil || !got.Copyable {
			// 不符即停：改可复制失败或不可复制。
			t.Fatalf("%+v %v", got, err)
		}
		// 留下这一步的结果，后面几格接着用它。
		proc = got
	})
	// 4.4：改可复制失败或不可复制就停，不能当通过。
	run("4.4", func(t *testing.T) {
		// 新建平台工艺「可复制草稿」，建不成后面没有工艺号可对。
		draft, err := h.WAN.CreatePlatformProcess(ctx, tok, "可复制草稿", []byte("draft-copy"))
		// 新建平台工艺「可复制草稿」失败就停，建不成后面没有工艺号可对。
		if err != nil {
			// 不符即停：新建平台工艺「可复制草稿」失败。
			t.Fatal(err)
		}
		// 改可复制，可复制改不了升档会被挡。
		got, err := h.WAN.SetPlatformCopyable(ctx, tok, draft.ID, draft.Revision, true)
		// 改可复制失败或不可复制就停，不能当通过。
		if err != nil || !got.Copyable {
			// 不符即停：改可复制失败或不可复制。
			t.Fatalf("%+v %v", got, err)
		}
		// 发布资产，发不出去后面不能当可用。
		pub, err := h.WAN.PublishPlatformAsset(ctx, tok, got.ID, got.Revision)
		// 发布资产失败就停，发不出去后面不能当可用。
		if err != nil {
			// 不符即停：发布资产失败。
			t.Fatal(err)
		}
		// 改可复制，可复制改不了升档会被挡。
		tight, err := h.WAN.SetPlatformCopyable(ctx, tok, pub.ID, pub.Revision, false)
		// 改可复制失败或竟可复制就停，不能当通过。
		if err != nil || tight.Copyable {
			// 不符即停：改可复制失败或竟可复制。
			t.Fatalf("%+v %v", tight, err)
		}
		// 改可复制，可复制改不了升档会被挡。
		wide, err := h.WAN.SetPlatformCopyable(ctx, tok, tight.ID, tight.Revision, true)
		// 改可复制失败或不可复制就停，不能当通过。
		if err != nil || !wide.Copyable {
			// 不符即停：改可复制失败或不可复制。
			t.Fatalf("%+v %v", wide, err)
		}
	})
	// 16.2：读平台正文失败或正文不一致就停，不能当通过。
	run("16.2", func(t *testing.T) {
		// 读平台正文，读不到就无法比对。
		gotBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proc.ID)
		// 读平台正文失败或正文不一致就停，不能当通过。
		if err != nil || !bytes.Equal(gotBody, applyProcess(body)) {
			// 不符即停：读平台正文失败或正文不一致。
			t.Fatalf("%q %v", gotBody, err)
		}
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
			t.Fatalf("secret or body leaked")
		}
	})
	// 1.3：升档结果多项不符预期就停，说明没有按规则落下。
	run("1.3", func(t *testing.T) {
		// 登记工厂「厂A」，建不成后面没有厂可授权。
		fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
		// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
		if err != nil {
			// 不符即停：登记工厂「厂A」失败。
			t.Fatal(err)
		}
		// 新造一个身份，后面用来区分是不是同一份。
		srcID := id.New()
		// 拼升档快照，身份或摘要错了会被拒或串档。
		snap := global.AssetSnapshot{
			SourceID: srcID, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProcess, Name: "焊接", Content: body, Digest: digest.Sum(body),
			Copyable: true, Status: global.AssetAvailable,
		}
		// 升档，升不上去档位就断了。
		promoted, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap)
		// 升档失败就停，升不上去档位就断了。
		if err != nil {
			// 不符即停：升档失败。
			t.Fatal(err)
		}
		// 升档结果多项不符预期就停，说明没有按规则落下。
		if promoted.ID == srcID || promoted.Level != global.AssetLevelPlatform || promoted.Revision != 1 ||
			promoted.Copyable || promoted.Status != global.AssetDraft || promoted.Content != nil ||
			promoted.SourceID == nil || *promoted.SourceID != srcID ||
			promoted.SourceRevision == nil || *promoted.SourceRevision != 1 ||
			promoted.SourceFactoryID == nil || *promoted.SourceFactoryID != fac.Factory.ID {
			// 不符即停：升档结果多项不符预期。
			t.Fatalf("%+v", promoted)
		}
		// 列平台资产，列表读不到就对不了编号。
		listed, err := h.WAN.ListPlatformAssets(ctx, tok, global.KindProcess)
		// 列平台资产失败就停，列表读不到就对不了编号。
		if err != nil {
			// 不符即停：列平台资产失败。
			t.Fatal(err)
		}
		// 先留出位置，循环里找到再填，找不到就失败。
		var listedName string
		// 逐条查看结果，漏看一条会把结论判错。
		for _, a := range listed {
			// 条件成立才做这一支，漏进来会算错样本。
			if a.ID == promoted.ID {
				// 先占一个候选，循环里再改成真正要的那条。
				listedName = a.SourceFactoryName
			}
		}
		// 这里不是厂A就停，这一步不能算通过。
		if listedName != "厂A" {
			// 不符即停：这里不是厂A。
			t.Fatalf("source factory display %q", listedName)
		}
		// 读厂侧副本应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.GetFactoryAsset(ctx, tok, fac.Factory.ID, srcID); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：读厂侧副本应被拒为越权。
			t.Fatalf("got %v", err)
		}
	})
	// 16.1：篡改正文且不升修订应被拒为摘要不符，放行或错类都算没拦住。
	run("16.1", func(t *testing.T) {
		// 篡改正文且不升修订失败就停，篡改失败完整性就验不成。
		if err := h.WAN.Store().TamperAssetContent(ctx, proc.ID, []byte("tampered")); err != nil {
			// 不符即停：篡改正文且不升修订失败。
			t.Fatal(err)
		}
		// 篡改正文且不升修订应被拒为摘要不符，放行或错类都算没拦住。
		if _, err := h.WAN.GetPlatformAsset(ctx, tok, proc.ID); !errors.Is(err, domain.ErrIntegrity) {
			// 不符即停：篡改正文且不升修订应被拒为摘要不符。
			t.Fatalf("get: %v", err)
		}
		// 读平台正文应被拒为摘要不符，放行或错类都算没拦住。
		if _, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proc.ID); !errors.Is(err, domain.ErrIntegrity) {
			// 不符即停：读平台正文应被拒为摘要不符。
			t.Fatalf("read: %v", err)
		}
	})
}
