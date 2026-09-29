// 云端非可用不进厂列表，删除只撤回展示。
package service_test

import (
	"context"
	"testing"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// 不可用的云端稿不进厂列表，删除只撤回展示。
func TestPlatformReplicaSync(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	created, fac, err := h.Provision(ctx, "sa-a", "初始超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa-a", created.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录换取会话令牌，失败说明口令或状态不对。
	tok, err := fac.Login(ctx, "sa-a", "sa-pass")
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把登录的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 留下工厂编号，后面用来核对资产没有串厂。
	fid := created.ID
	// 准备这段正文字节，读回对不上说明没写进去。
	body := []byte("plat-sync")
	// 组装一条组包成员，字段错了后面会对不上。
	m := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台工艺",
		Status: factory.AssetAvailable, Copyable: false, Revision: 1, Content: body, Digest: digest.Sum(body),
	}
	// 收平台稿失败就停，否则后面没有可靠结果。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 列出可见资产，失败说明会话或范围不对。
	listed, err := fac.ListAssets(ctx, tok, factory.KindProcess)
	// 列资产失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把列资产的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 先当成没找到，扫到再改，避免沿用旧结果。
	found := false
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, a := range listed {
		// 状态应符合这一步的预期，停在旧状态说明没流转。
		if a.ID == m.ID && a.Status == factory.AssetAvailable {
			// 记成已经找到，最后仍是假说明列表里没有。
			found = true
			break
		}
	}
	// 扫完应该能找到，还没有说明结果里漏了这条。
	if !found {
		// 应存在的记录没有出现，说明这一步没落下。
		t.Fatal("available replica missing")
	}
	// 复制成员准备改名，用来验证新修订能盖过旧名。
	renamed := m
	// 把修订拨到更新，旧修订不应再盖住它。
	renamed.Revision = 2
	// 改成新名字，同步后仍是旧名说明没覆盖。
	renamed.Name = "改名工艺"
	// 收平台稿失败就停，否则后面没有可靠结果。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, renamed, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 列出可见资产，失败说明会话或范围不对。
	listed, err = fac.ListAssets(ctx, tok, factory.KindProcess)
	// 列资产失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把列资产的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 重新当成没找到，避免上一轮的命中还留着。
	found = false
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, a := range listed {
		// 修订号应按这次保存前进，没变说明写没落库。
		if a.ID == m.ID && a.Name == "改名工艺" && a.Revision == 2 {
			// 记成已经找到，最后仍是假说明列表里没有。
			found = true
			break
		}
	}
	// 扫完应该能找到，还没有说明结果里漏了这条。
	if !found {
		// 应存在的记录没有出现，说明这一步没落下。
		t.Fatal("renamed replica missing")
	}
	// 复制成员准备停用，用来验证停用状态会同步。
	disabled := m
	// 停用修订应更新，旧修订盖住说明比较写反。
	disabled.Revision = 3
	// 标成停用，同步后仍可用说明状态没传过去。
	disabled.Status = factory.AssetDisabled
	// 收平台稿失败就停，否则后面没有可靠结果。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, disabled, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 列出可见资产，失败说明会话或范围不对。
	listed, err = fac.ListAssets(ctx, tok, factory.KindProcess)
	// 列资产失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把列资产的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, a := range listed {
		// 应是另一条记录而不是同一条，撞号说明没另立。
		if a.ID == m.ID {
			// 停用之后仍能操作，说明停用没有生效。
			t.Fatal("disabled replica still listed")
		}
	}
	// 撤回下发失败就停，否则后面没有可靠结果。
	if err := fac.RetractPlatformDelivery(ctx, m.ID); err != nil {
		// 把撤回下发的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 列出可见资产，失败说明会话或范围不对。
	listed, err = fac.ListAssets(ctx, tok, factory.KindProcess)
	// 列资产失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把列资产的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, a := range listed {
		// 应是另一条记录而不是同一条，撞号说明没另立。
		if a.ID == m.ID {
			// 副本没有按预期同步，说明下发没有写进厂。
			t.Fatal("retracted replica still listed")
		}
	}
	// 读回这份副本，失败说明同步没写进来。
	got, err := fac.GetReplica(ctx, tok, m.ID, 1)
	// 读副本失败或修订不对就停，说明没达预期。
	if err != nil || got.ID != m.ID || got.Revision != 1 {
		// 副本没有按预期同步，说明下发没有写进厂。
		t.Fatalf("pinned replica %+v %v", got, err)
	}
}
