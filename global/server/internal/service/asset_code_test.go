// 工艺工程只读编号：云端发号、另存/升档换新号、改名不改号。
package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"wmesh/global/internal/platform/assetcode"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

// 验云端发号、另存换号、改名不改号。
func TestPlatformAssetCodes(t *testing.T) {
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

	// 新建平台工艺「焊1」，建不成后面没有工艺号可对。
	p1, err := h.WAN.CreatePlatformProcess(ctx, tok, "焊1", []byte(`{"name":"p","current":180}`))
	// 新建平台工艺「焊1」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「焊1」失败。
		t.Fatal(err)
	}
	// 第一份工艺编号不是GY-W-000001或编号格式不对就停，不能当通过。
	if p1.Code != "GY-W-000001" || !assetcode.Valid(p1.Code) {
		// 不符即停：第一份工艺编号不是GY-W-000001或编号格式不对。
		t.Fatalf("process code %s", p1.Code)
	}
	// 绕过服务直接入库，入库失败就没法证明号被忽略。
	forced, err := h.WAN.Store().InsertAsset(ctx, global.Asset{
		Kind: global.KindProcess, Name: "指定号", Status: global.AssetDraft,
		Content: []byte("x"), Digest: digest.Sum([]byte("x")), CreatorID: p1.CreatorID, Code: "GY-W-009999",
	})
	// 绕过服务直接入库失败就停，入库失败就没法证明号被忽略。
	if err != nil {
		// 不符即停：绕过服务直接入库失败。
		t.Fatal(err)
	}
	// 入库结果编号不是GY-W-000002，对不上说明发号或沿用错了。
	if forced.Code != "GY-W-000002" {
		// 不符即停：入库结果编号不是GY-W-000002。
		t.Fatalf("caller code ignored: %s", forced.Code)
	}
	// 新建平台工程「工程1」，建不成后面没有工程可引用。
	j1, err := h.WAN.CreatePlatformProject(ctx, tok, "工程1", []byte(`[{"name":"焊道"}]`), nil)
	// 新建平台工程「工程1」失败就停，建不成后面没有工程可引用。
	if err != nil {
		// 不符即停：新建平台工程「工程1」失败。
		t.Fatal(err)
	}
	// 这份工程编号不是GC-W-000001，对不上说明发号或沿用错了。
	if j1.Code != "GC-W-000001" {
		// 不符即停：这份工程编号不是GC-W-000001。
		t.Fatalf("project code %s", j1.Code)
	}
	// 新建平台工艺「焊2」，建不成后面没有工艺号可对。
	p2, err := h.WAN.CreatePlatformProcess(ctx, tok, "焊2", []byte(`{"name":"p2","current":181}`))
	// 新建平台工艺「焊2」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「焊2」失败。
		t.Fatal(err)
	}
	// 后续工艺编号不是GY-W-000003，对不上说明发号或沿用错了。
	if p2.Code != "GY-W-000003" {
		// 不符即停：后续工艺编号不是GY-W-000003。
		t.Fatalf("third process %s", p2.Code)
	}

	// 改名「改名」，改名失败就证明不了号还在。
	renamed, err := h.WAN.RenamePlatformAsset(ctx, tok, p1.ID, p1.Revision, "改名")
	// 改名「改名」失败就停，改名失败就证明不了号还在。
	if err != nil {
		// 不符即停：改名「改名」失败。
		t.Fatal(err)
	}
	// 改名结果编号变了或修订没有跟上就停，不能当通过。
	if renamed.Code != p1.Code || renamed.Revision != p1.Revision+1 {
		// 不符即停：改名结果编号变了或修订没有跟上。
		t.Fatalf("rename changed code: %+v", renamed)
	}

	// 另存工艺「副本」，另存失败就没有新草稿。
	copied, err := h.WAN.CopyPlatformProcess(ctx, tok, p1.ID, "副本")
	// 另存工艺「副本」失败就停，另存失败就没有新草稿。
	if err != nil {
		// 不符即停：另存工艺「副本」失败。
		t.Fatal(err)
	}
	// 另存结果多项不符预期就停，说明没有按规则落下。
	if copied.Code == p1.Code || copied.ID == p1.ID || !strings.HasPrefix(copied.Code, "GY-W-") {
		// 不符即停：另存结果多项不符预期。
		t.Fatalf("copy code %+v vs %s", copied, p1.Code)
	}
	// 读取资产，读不到就无法核对原件。
	still, err := h.WAN.GetPlatformAsset(ctx, tok, p1.ID)
	// 读取资产失败或编号变了就停，不能当通过。
	if err != nil || still.Code != p1.Code {
		// 不符即停：读取资产失败或编号变了。
		t.Fatalf("src after copy %+v %v", still, err)
	}

	// 登记工厂「厂A」，建不成后面没有厂可授权。
	a, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 登记工厂「厂B」，建不成后面没有厂可授权。
	b, err := h.WAN.CreateFactory(ctx, tok, "厂B", "sa-b", "超管B")
	// 登记工厂「厂B」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂B」失败。
		t.Fatal(err)
	}
	// 厂A短码不对就停，这一步不能算通过。
	if a.Factory.ShortCode != "F01" || b.Factory.ShortCode != "F02" {
		// 不符即停：厂A短码不对。
		t.Fatalf("factory codes %s %s", a.Factory.ShortCode, b.Factory.ShortCode)
	}
	// 登记焊机，焊机登记失败就没有设备。
	c, err := h.WAN.RegisterClient(ctx, tok, "焊机", id.New(), a.Factory.ID, nil, "ARM-1")
	// 登记焊机失败就停，焊机登记失败就没有设备。
	if err != nil {
		// 不符即停：登记焊机失败。
		t.Fatal(err)
	}
	// 焊机短码不对或编号格式不对就停，不能当通过。
	if c.ShortCode != "C0001" || !assetcode.ValidClientOrigin(c.ShortCode) {
		// 不符即停：焊机短码不对或编号格式不对。
		t.Fatalf("client short %s", c.ShortCode)
	}

	// 新造一个身份，后面用来区分是不是同一份。
	srcID := id.New()
	// 按工艺字段表套正文，套错了摘要会对不上。
	plain := applyProcess([]byte(`{"name":"升","current":190}`))
	// 先算正文摘要，升档和发布都要拿它核对。
	sum := digest.Sum(plain)
	// 拼升档快照，身份或摘要错了会被拒或串档。
	snap := global.AssetSnapshot{
		SourceID: srcID, SourceRevision: 1, SourceFactoryID: a.Factory.ID,
		Kind: global.KindProcess, Name: "厂级升", Content: plain, Digest: sum,
		Copyable: true, Status: global.AssetAvailable,
	}
	// 升档，升不上去档位就断了。
	promoted, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap)
	// 升档失败就停，升不上去档位就断了。
	if err != nil {
		// 不符即停：升档失败。
		t.Fatal(err)
	}
	// 升档结果身份没有分开或编号格式不对就停，不能当通过。
	if promoted.ID == srcID || !strings.HasPrefix(promoted.Code, "GY-W-") {
		// 不符即停：升档结果身份没有分开或编号格式不对。
		t.Fatalf("promote %+v", promoted)
	}
	// 升档，升不上去档位就断了。
	again, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap)
	// 升档失败或身份变了就停，不能当通过。
	if err != nil || again.ID != promoted.ID || again.Code != promoted.Code {
		// 不符即停：升档失败或身份变了。
		t.Fatalf("re-promote %+v %v", again, err)
	}

	// 按编号查资产，按号查不到就证明不了唯一。
	got, err := h.WAN.Store().AssetByCode(ctx, p1.Code)
	// 按编号查资产失败或身份变了就停，不能当通过。
	if err != nil || got.ID != p1.ID {
		// 不符即停：按编号查资产失败或身份变了。
		t.Fatalf("lookup %v %v", got, err)
	}
	// 列平台资产，列表读不到就对不了编号。
	list, err := h.WAN.ListPlatformAssets(ctx, tok, "process")
	// 列平台资产失败就停，列表读不到就对不了编号。
	if err != nil {
		// 不符即停：列平台资产失败。
		t.Fatal(err)
	}
	// 从零开始数，最后条数不对就失败。
	n := 0
	// 逐条查看结果，漏看一条会把结论判错。
	for _, row := range list {
		// 条件成立才做这一支，漏进来会算错样本。
		if row.Code == p1.Code {
			// 命中一条就加一，漏计会把列表判重或判丢。
			n++
		}
	}
	// 同一编号必须只出现一次，重复或缺失都算错。
	if n != 1 {
		// 不符即停：同一编号必须只出现一次。
		t.Fatalf("list by code %d", n)
	}
	// 按编号查资产应被拒为找不到，放行或错类都算没拦住。
	if _, err := h.WAN.Store().AssetByCode(ctx, "GY-W-999999"); !errors.Is(err, domain.ErrNotFound) {
		// 不符即停：按编号查资产应被拒为找不到。
		t.Fatalf("missing: %v", err)
	}

	// 发布资产，发不出去后面不能当可用。
	pub, err := h.WAN.PublishPlatformAsset(ctx, tok, p1.ID, still.Revision)
	// 发布资产失败就停，发不出去后面不能当可用。
	if err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 钉住依赖的身份修订和摘要，错了工程建不成。
	dep := global.AssetDep{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}
	// 新建平台工程「编号当引用」应被拒为依赖不符，放行或错类都算没拦住。
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "编号当引用", []byte(`[{"templateId":"`+seedSingleID+`","processId":"`+p1.Code+`"}]`), []global.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("code as processId: %v", err)
	}
}
