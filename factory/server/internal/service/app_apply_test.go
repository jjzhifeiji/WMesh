// 示教器盖厂库：不比对版本号；平台级、停用、已删、越权都拒绝。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 示教器盖写厂库，越权、停用、已删和平台稿都要拒绝。
func TestApplyAppContent(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa-a", "超管A")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa-a", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa-a", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	op := mustCreateRole(t, ctx, fac, sa, "op-a", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 建人并授好角色，失败说明后面没有账号可用。
	other := mustCreateRole(t, ctx, fac, sa, "op-b", "op-b-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 建人并授好角色，失败说明后面没有账号可用。
	admin := mustCreateRole(t, ctx, fac, sa, "oa-a", "oa-pass", factory.RoleOrgAdmin, factory.ScopeFactory, nil)
	// 建人并授好角色，失败说明后面没有账号可用。
	aud := mustCreateRole(t, ctx, fac, sa, "aud-a", "aud-pass", factory.RoleAuditor, factory.ScopeFactory, nil)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}

	// 新建一份厂级工艺，失败说明起草入口坏了。
	proc, err := fac.CreateFactoryProcess(ctx, op.tok, direct, "厂工艺", []byte(`{"n":1}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	proc, err = fac.PublishAsset(ctx, op.tok, proc.ID, proc.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 改写资产正文，失败说明修订冲突或越权。
	web, err := fac.UpdateAssetContent(ctx, sa, proc.ID, proc.Revision, []byte(`{"n":2}`))
	// 改正文失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 提交正文应因越权被拒绝，放行说明没拦住。
	if _, err := fac.ApplyAppContent(ctx, op.tok, proc.ID, []byte(`{"n":9}`)); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("operator factory: %v", err)
	}
	// 提交正文应因越权被拒绝，放行说明没拦住。
	if _, err := fac.ApplyAppContent(ctx, aud.tok, proc.ID, []byte(`{"n":9}`)); !errors.Is(err, domain.ErrForbidden) {
		// 审计里缺了这条记录，说明这一步没有记账。
		t.Fatalf("auditor factory: %v", err)
	}
	// 用应用提交正文，失败说明作者或状态不允许。
	got, err := fac.ApplyAppContent(ctx, admin.tok, proc.ID, []byte(`{"n":9}`))
	// 提交正文失败或修订不对就停，说明没达预期。
	if err != nil || got.Revision != web.Revision+1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("admin apply %+v %v", got, err)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	body, err := fac.ReadAssetContent(ctx, sa, proc.ID)
	// 读正文失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(body, []byte(`{"n":9}`)) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("covered %q %v", body, err)
	}
	// 改正文应因修订冲突被拒绝，放行说明没拦住。
	if _, err := fac.UpdateAssetContent(ctx, sa, proc.ID, web.Revision, []byte(`{"n":8}`)); !errors.Is(err, domain.ErrRevisionConflict) {
		// 旧修订盖住了新稿，说明先后比较写反了。
		t.Fatalf("web stale: %v", err)
	}

	// 在平板上新建个人稿，失败说明会话或上下文不对。
	mine, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "我的", []byte(`{"p":1}`), uuid.Nil, "", nil)
	// 平板建稿失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把平板建稿的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 改写资产正文，失败说明修订冲突或越权。
	webMine, err := fac.UpdateAssetContent(ctx, sa, mine.ID, mine.Revision, []byte(`{"p":2}`))
	// 改正文失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 提交正文应因越权被拒绝，放行说明没拦住。
	if _, err := fac.ApplyAppContent(ctx, sa, mine.ID, []byte(`{"p":3}`)); !errors.Is(err, domain.ErrForbidden) {
		// 个人稿串到了别人名下，说明归属没保住。
		t.Fatalf("sa personal: %v", err)
	}
	// 提交正文应因越权被拒绝，放行说明没拦住。
	if _, err := fac.ApplyAppContent(ctx, other.tok, mine.ID, []byte(`{"p":3}`)); !errors.Is(err, domain.ErrForbidden) {
		// 个人稿串到了别人名下，说明归属没保住。
		t.Fatalf("other personal: %v", err)
	}
	// 用应用提交正文，失败说明作者或状态不允许。
	mineGot, err := fac.ApplyAppContent(ctx, op.tok, mine.ID, []byte(`{"p":3}`))
	// 提交正文失败或修订不对就停，说明没达预期。
	if err != nil || mineGot.Revision != webMine.Revision+1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("creator apply %+v %v", mineGot, err)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	body, err = fac.ReadAssetContent(ctx, op.tok, mine.ID)
	// 读正文失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(body, []byte(`{"p":3}`)) {
		// 个人稿串到了别人名下，说明归属没保住。
		t.Fatalf("personal covered %q %v", body, err)
	}

	// 停用这份资产，失败说明仍被引用或越权。
	off, err := fac.DisableAsset(ctx, sa, proc.ID, got.Revision)
	// 停用资产失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把停用资产的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 用应用提交正文，失败说明作者或状态不允许。
	coveredOff, err := fac.ApplyAppContent(ctx, sa, proc.ID, []byte(`{"n":10}`))
	// 提交正文失败或修订不对就停，说明没达预期。
	if err != nil || coveredOff.Status != factory.AssetDisabled || coveredOff.Revision != off.Revision+1 {
		// 停用之后仍能操作，说明停用没有生效。
		t.Fatalf("disabled cover %+v %v", coveredOff, err)
	}
	// 读台账失败或状态不对就停，说明没达预期。
	if meta, err := fac.GetAsset(ctx, sa, proc.ID); err != nil || meta.Status != factory.AssetDisabled {
		// 停用之后仍能操作，说明停用没有生效。
		t.Fatalf("still disabled %+v %v", meta, err)
	}
	// 读正文失败或正文不同就停，说明没达预期。
	if body, err := fac.ReadAssetContent(ctx, sa, proc.ID); err != nil || !bytes.Equal(body, []byte(`{"n":10}`)) {
		// 停用之后仍能操作，说明停用没有生效。
		t.Fatalf("disabled body %q %v", body, err)
	}

	// 新建一份厂级工艺，失败说明起草入口坏了。
	drop, err := fac.CreateFactoryProcess(ctx, sa, direct, "可删", []byte(`{"d":1}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 删除资产失败就停，否则后面没有可靠结果。
	if err := fac.DeleteAsset(ctx, sa, drop.ID); err != nil {
		// 把删除资产的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 提交正文应因找不到被拒绝，放行说明没拦住。
	if _, err := fac.ApplyAppContent(ctx, sa, drop.ID, []byte(`{"d":2}`)); !errors.Is(err, domain.ErrNotFound) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("deleted: %v", err)
	}

	// 解析固定编号，失败说明样例编号写坏了。
	platID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	// 准备这段正文字节，读回对不上说明没写进去。
	plain := []byte(`{"plat":1}`)
	// 写副本失败就停，否则后面没有可靠结果。
	if _, err := fac.Store().InsertReplica(ctx, store.AssetReplica{
		// 填上这条记录的身份和级别，填错会把归属判反。
		ID: platID, Revision: 1, Kind: store.KindProcess, Level: store.AssetLevelPlatform,
		Name: "平台焊", Status: store.AssetAvailable, Copyable: false, Content: plain, Digest: digest.Sum(plain),
	}); err != nil {
		t.Fatal(err)
	}
	// 提交正文应因越权被拒绝，放行说明没拦住。
	if _, err := fac.ApplyAppContent(ctx, sa, platID, []byte(`{"plat":2}`)); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("platform: %v", err)
	}

	// 新建一份厂级工艺，失败说明起草入口坏了。
	pinned, err := fac.CreateFactoryProcess(ctx, sa, direct, "被引用", []byte(`{"b":1}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	pinned, err = fac.PublishAsset(ctx, sa, pinned.ID, pinned.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份厂级工程，失败说明起草入口坏了。
	proj, err := fac.CreateFactoryProject(ctx, sa, direct, "厂工程", []byte(`[]`), nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	proj, err = fac.PublishAsset(ctx, sa, proj.ID, proj.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把编号打成文本，方便和路径或正文比对。
	next := []byte(`[{"processId":"` + pinned.ID.String() + `"}]`)
	// 用应用提交正文，失败说明作者或状态不允许。
	covered, err := fac.ApplyAppContent(ctx, sa, proj.ID, next)
	// 提交正文失败或修订不对就停，说明没达预期。
	if err != nil || covered.Revision != proj.Revision+1 || len(covered.Deps) != 1 || covered.Deps[0].ID != pinned.ID {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("project %+v %v", covered, err)
	}
}
