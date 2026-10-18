// 云端非可用不进厂列表，删除只撤回展示。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
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

// 收过同一修订的闭包判断为已有；没收过、撤回了或缺依赖修订都判断为没有。
func TestHasPlatformDelivery(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂，失败则没有厂可测。
	created, fac, err := h.Provision(ctx, "sa-a", "初始超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 留下工厂编号，闭包要指到这家厂。
	fid := created.ID
	// 准备依赖工艺的正文，摘要按它算。
	procBody := []byte("has-proc")
	// 拼工程依赖的平台工艺。
	proc := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "依赖工艺",
		Status: factory.AssetAvailable, Revision: 1, Content: procBody, Digest: digest.Sum(procBody),
	}
	// 准备工程正文，摘要按它算。
	projBody := []byte("has-proj")
	// 拼工程根成员，钉住那条工艺的第一修订。
	proj := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProject, Level: factory.AssetLevelPlatform, Name: "判断工程",
		Status: factory.AssetAvailable, Revision: 2, Content: projBody, Digest: digest.Sum(projBody),
		Deps: []factory.AssetDep{{ID: proc.ID, Revision: 1, Digest: proc.Digest}},
	}
	// 还没收过就该判断为没有。
	if ok, err := fac.HasPlatformDelivery(ctx, proj.ID, 2); err != nil || ok {
		// 没收过却说已有就停。
		t.Fatalf("before accept %v %v", ok, err)
	}
	// 收下工程闭包，失败就没有可判断的副本。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProject, proj, []factory.ClosureMember{proc}, &fid, nil)); err != nil {
		// 把收闭包的错误打出来并停住。
		t.Fatal(err)
	}
	// 收过同一修订就该判断为已有。
	if ok, err := fac.HasPlatformDelivery(ctx, proj.ID, 2); err != nil || !ok {
		// 收过却说没有就停。
		t.Fatalf("after accept %v %v", ok, err)
	}
	// 没收过的更高修订仍判断为没有。
	if ok, err := fac.HasPlatformDelivery(ctx, proj.ID, 3); err != nil || ok {
		// 新修订被当成已有就停。
		t.Fatalf("newer revision %v %v", ok, err)
	}
	// 准备一份缺依赖的工程正文。
	lonelyBody := []byte("lonely-proj")
	// 直接写一条工程副本，依赖指向没收过的工艺修订。
	lonely := store.AssetReplica{
		ID: id.New(), Revision: 1, Kind: factory.KindProject, Level: factory.AssetLevelPlatform,
		Name: "缺依赖工程", Status: factory.AssetAvailable, Content: lonelyBody, Digest: digest.Sum(lonelyBody),
		Deps: []factory.AssetDep{{ID: proc.ID, Revision: 9, Digest: proc.Digest}},
	}
	// 写入根副本失败就停，后面无从判断。
	if _, err := fac.Store().InsertReplica(ctx, lonely); err != nil {
		// 把写副本的错误打出来并停住。
		t.Fatal(err)
	}
	// 依赖修订缺了就该判断为没有，好让通道整包重收。
	if ok, err := fac.HasPlatformDelivery(ctx, lonely.ID, 1); err != nil || ok {
		// 缺依赖却说已有就停。
		t.Fatalf("missing dep %v %v", ok, err)
	}
	// 撤回工程，之后重送要重新收。
	if err := fac.RetractPlatformDelivery(ctx, proj.ID); err != nil {
		// 把撤回的错误打出来并停住。
		t.Fatal(err)
	}
	// 撤回后同一修订判断为没有。
	if ok, err := fac.HasPlatformDelivery(ctx, proj.ID, 2); err != nil || ok {
		// 撤回了还说已有就停。
		t.Fatalf("after retract %v %v", ok, err)
	}
}

// 收过同一修订的模版判断为已有；工程模版缺名称时判断为没有，补上名称后才算已有。
func TestHasTemplateDelivery(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂，失败则没有厂可测。
	created, fac, err := h.Provision(ctx, "sa-a", "初始超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 留下工厂编号，模版要指到这家厂。
	fid := created.ID
	// 编出默认工艺字段表，作模版正文。
	schema, err := contenttpl.Marshal(contenttpl.Default(contenttpl.KindProcess))
	// 编不出字段表就停，后面没有模版可收。
	if err != nil {
		// 把编字段表的错误打出来并停住。
		t.Fatal(err)
	}
	// 拼一份工艺模版第一修订，工艺模版不带名称。
	snap := factory.TemplateSnapshot{
		ID: id.New(), Kind: factory.KindProcess, Revision: 1,
		Schema: json.RawMessage(schema), Digest: digest.Sum(schema), TargetFactoryID: &fid,
	}
	// 还没收过就该判断为没有。
	if ok, err := fac.HasTemplateDelivery(ctx, snap.ID, 1); err != nil || ok {
		// 没收过却说已有就停。
		t.Fatalf("before accept %v %v", ok, err)
	}
	// 收下工艺模版，失败就没有可判断的副本。
	if err := fac.AcceptTemplateDelivery(ctx, snap); err != nil {
		// 把收模版的错误打出来并停住。
		t.Fatal(err)
	}
	// 收过同一修订就该判断为已有，工艺模版不看名称。
	if ok, err := fac.HasTemplateDelivery(ctx, snap.ID, 1); err != nil || !ok {
		// 收过却说没有就停。
		t.Fatalf("after accept %v %v", ok, err)
	}
	// 没收过的更高修订仍判断为没有。
	if ok, err := fac.HasTemplateDelivery(ctx, snap.ID, 2); err != nil || ok {
		// 新修订被当成已有就停。
		t.Fatalf("newer revision %v %v", ok, err)
	}
	// 拼一条旧工程模版行，当年落库时没带名称。
	noName := store.ContentTemplate{ID: id.New(), Kind: factory.KindProject, Revision: 1, Schema: schema, Digest: digest.Sum(schema)}
	// 写入无名工程模版失败就停。
	if _, err := fac.Store().InsertTemplateReplica(ctx, noName); err != nil {
		// 把写模版的错误打出来并停住。
		t.Fatal(err)
	}
	// 工程模版缺名称就该判断为没有，好让通道重收补名。
	if ok, err := fac.HasTemplateDelivery(ctx, noName.ID, 1); err != nil || ok {
		// 缺名称却说已有就停。
		t.Fatalf("project without name %v %v", ok, err)
	}
	// 同修订带上名称再收一次，补上名称。
	noName.Name = "工程明细"
	// 补名失败就停。
	if _, err := fac.Store().InsertTemplateReplica(ctx, noName); err != nil {
		// 把补名的错误打出来并停住。
		t.Fatal(err)
	}
	// 补上名称后就该判断为已有。
	if ok, err := fac.HasTemplateDelivery(ctx, noName.ID, 1); err != nil || !ok {
		// 补了名称还说没有就停。
		t.Fatalf("project with name %v %v", ok, err)
	}
}

// 重复撤回只记一条撤回审计，平台目录里的文件节点照样删掉。
func TestRetractPlatformDeliveryOnce(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂，失败则没有厂可测。
	created, fac, err := h.Provision(ctx, "sa-a", "初始超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 留下工厂编号，闭包要指到这家厂。
	fid := created.ID
	// 准备工艺正文，摘要按它算。
	body := []byte("retract-once")
	// 拼一条平台工艺成员。
	m := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "撤回工艺",
		Status: factory.AssetAvailable, Revision: 1, Content: body, Digest: digest.Sum(body),
	}
	// 封成闭包，后面收两次用同一份。
	snap := sealSnap(factory.KindProcess, m, nil, &fid, nil)
	// 收下闭包，失败就没有可撤回的副本。
	if err := fac.AcceptPlatformDelivery(ctx, snap); err != nil {
		// 把收闭包的错误打出来并停住。
		t.Fatal(err)
	}
	// 第一次撤回，应记一条审计并删掉文件节点。
	if err := fac.RetractPlatformDelivery(ctx, m.ID); err != nil {
		// 把撤回的错误打出来并停住。
		t.Fatal(err)
	}
	// 文件节点应已删掉。
	if _, err := fac.Store().FSFileByAsset(ctx, m.ID); !errors.Is(err, domain.ErrNotFound) {
		// 撤回后文件节点还在就停。
		t.Fatalf("file after first retract: %v", err)
	}
	// 同修订再送一次，副本已在，文件节点会被补回来。
	if err := fac.AcceptPlatformDelivery(ctx, snap); err != nil {
		// 把重送的错误打出来并停住。
		t.Fatal(err)
	}
	// 补回的文件节点应能读到，否则下面验不出删除。
	if _, err := fac.Store().FSFileByAsset(ctx, m.ID); err != nil {
		// 文件节点没补回就停。
		t.Fatalf("file after resend: %v", err)
	}
	// 第二次撤回：行已撤回改不到，文件节点照样要删。
	if err := fac.RetractPlatformDelivery(ctx, m.ID); err != nil {
		// 把撤回的错误打出来并停住。
		t.Fatal(err)
	}
	// 文件节点应再次删掉。
	if _, err := fac.Store().FSFileByAsset(ctx, m.ID); !errors.Is(err, domain.ErrNotFound) {
		// 重复撤回没删文件节点就停。
		t.Fatalf("file after second retract: %v", err)
	}
	// 读出本厂审计，数撤回成功几条。
	rows, err := fac.ListAudit(ctx)
	// 审计读不出就停。
	if err != nil {
		// 把读审计的错误打出来并停住。
		t.Fatal(err)
	}
	// 数这份资产的撤回成功审计。
	n := 0
	// 逐条看审计。
	for _, r := range rows {
		// 是这份资产的撤回成功才算。
		if r.Action == "retract_closure" && r.Target == m.ID.String() && r.Result == audit.Allow {
			// 记一条。
			n++
		}
	}
	// 重复撤回只该记一条。
	if n != 1 {
		// 审计条数不对就停。
		t.Fatalf("retract audits %d", n)
	}
}
