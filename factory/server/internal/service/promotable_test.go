// 通道升档：列出本厂全部级别，不分状态。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// 升档名单要含本厂全部级别，漏级别说明没列全。
func TestPromotableForChannel(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}
	// 准备这段正文字节，读回对不上说明没写进去。
	body := []byte("factory-body")

	// 新建一份厂级工艺，失败说明起草入口坏了。
	draft, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "草稿", body)
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份厂级工艺，失败说明起草入口坏了。
	avail, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "可升", body)
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布失败就停，否则后面没有可靠结果。
	if _, err := fac.PublishAsset(ctx, pe.tok, avail.ID, avail.Revision); err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份个人工艺，失败说明个人入口被拒。
	personal, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "个人", body)
	// 建个人工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建个人工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布失败就停，否则后面没有可靠结果。
	if _, err := fac.PublishAsset(ctx, pe.tok, personal.ID, personal.Revision); err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 准备这段正文字节，读回对不上说明没写进去。
	platBody := []byte("plat-copy")
	// 装一条平台级成员，用来验证升档名单的边界。
	plat := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台副本",
		Status: factory.AssetAvailable, Copyable: false, Revision: 5, Content: platBody, Digest: digest.Sum(platBody),
	}
	// 收平台稿失败就停，否则后面没有可靠结果。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, plat, nil, &seed.ID, nil)); err != nil {
		// 把收平台稿的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 列出可升档资产，失败说明通道或权限不对。
	rows, err := fac.ListPromotable(ctx, factory.KindProcess)
	// 列可升档失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把列可升档的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 按名称记下状态或级别，漏记会把名单核对错。
	seen := map[string]string{}
	// 按名称记下级别，漏记会把升档结果看错。
	levels := map[string]string{}
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, r := range rows {
		// 记下这条的状态，和预期不一致就说明名单偏了。
		seen[r.Name] = r.Status
		// 记下这条的级别，升错档会在这里对不上。
		levels[r.Name] = r.Level
	}
	// 结果应和这一步的预期一致，偏离说明行为写偏了。
	if seen["草稿"] != factory.AssetDraft || seen["可升"] != factory.AssetAvailable || seen["个人"] != factory.AssetAvailable || seen["平台副本"] != factory.AssetAvailable {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%+v", rows)
	}
	// 级别应是预期那一档，升错或没升都算失败。
	if levels["草稿"] != factory.AssetLevelFactory || levels["个人"] != factory.AssetLevelPersonal || levels["平台副本"] != factory.AssetLevelPlatform {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("levels %+v", levels)
	}

	// 做快照失败或状态不对就停，说明没达预期。
	if snap, err := fac.SnapshotForChannel(ctx, draft.ID); err != nil || snap.Status != factory.AssetDraft || !contentcrypt.IsEnvelope(snap.Content) {
		// 状态还停在草稿，说明保存没有落成可用。
		t.Fatalf("draft: %+v %v", snap, err)
	}
	// 做快照失败或结果不符就停，说明没达预期。
	if snap, err := fac.SnapshotForChannel(ctx, personal.ID); err != nil || snap.SourceID != personal.ID || !contentcrypt.IsEnvelope(snap.Content) {
		// 个人稿串到了别人名下，说明归属没保住。
		t.Fatalf("personal: %+v %v", snap, err)
	}
	// 做快照应因越权被拒绝，放行说明没拦住。
	if _, err := fac.SnapshotForChannel(ctx, plat.ID); !errors.Is(err, domain.ErrForbidden) {
		// 副本没有按预期同步，说明下发没有写进厂。
		t.Fatalf("platform replica: %v", err)
	}
	// 按通道做下发快照，失败说明没有可发的稿。
	snap, err := fac.SnapshotForChannel(ctx, avail.ID)
	// 认信封失败或结果不符就停，说明没达预期。
	if err != nil || snap.SourceID != avail.ID || !contentcrypt.IsEnvelope(snap.Content) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%+v %v", snap, err)
	}
}

// 升档名单不分状态，把草稿或停用漏掉即失败。
func TestPromotableProjectsAllStatuses(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}
	// 准备这段正文字节，读回对不上说明没写进去。
	body := []byte("job")

	// 新建一份厂级工程，失败说明起草入口坏了。
	draft, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "草稿工程", body, nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份厂级工程，失败说明起草入口坏了。
	avail, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "可用工程", body, nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	avail, err = fac.PublishAsset(ctx, pe.tok, avail.ID, avail.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份厂级工程，失败说明起草入口坏了。
	off, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "停用工程", body, nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	off, err = fac.PublishAsset(ctx, pe.tok, off.ID, off.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 停用这份资产，失败说明仍被引用或越权。
	off, err = fac.DisableAsset(ctx, pe.tok, off.ID, off.Revision)
	// 停用资产失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把停用资产的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份个人工程，失败说明个人入口被拒。
	personal, err := fac.CreatePersonalProject(ctx, pe.tok, direct, "个人工程", body, nil)
	// 建个人工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建个人工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 列出可升档资产，失败说明通道或权限不对。
	rows, err := fac.ListPromotable(ctx, factory.KindProject)
	// 列可升档失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把列可升档的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 按名称记下状态或级别，漏记会把名单核对错。
	seen := map[string]string{}
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, r := range rows {
		// 记下这条的状态，和预期不一致就说明名单偏了。
		seen[r.Name] = r.Status
	}
	// 结果应和这一步的预期一致，偏离说明行为写偏了。
	if seen["草稿工程"] != factory.AssetDraft || seen["可用工程"] != factory.AssetAvailable || seen["停用工程"] != factory.AssetDisabled || seen["个人工程"] != factory.AssetDraft {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%+v", rows)
	}

	// 做快照失败或结果不符就停，说明没达预期。
	if snap, err := fac.SnapshotForChannel(ctx, personal.ID); err != nil || snap.SourceID != personal.ID {
		// 个人稿串到了别人名下，说明归属没保住。
		t.Fatalf("personal: %+v %v", snap, err)
	}
	// 做快照失败或状态不对就停，说明没达预期。
	if snap, err := fac.SnapshotForChannel(ctx, draft.ID); err != nil || snap.Status != factory.AssetDraft || !contentcrypt.IsEnvelope(snap.Content) {
		// 状态还停在草稿，说明保存没有落成可用。
		t.Fatalf("draft snap %+v %v", snap, err)
	}
	// 做快照失败或状态不对就停，说明没达预期。
	if snap, err := fac.SnapshotForChannel(ctx, off.ID); err != nil || snap.Status != factory.AssetDisabled {
		// 停用之后仍能操作，说明停用没有生效。
		t.Fatalf("disabled snap %+v %v", snap, err)
	}
}
