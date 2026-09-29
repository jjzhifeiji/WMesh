// 第 6 圈：工作上下文、路径快照、历史不漂移、个人资产不改归属。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

// 工作上下文和路径快照不随改派漂移，个人归属不变。
func TestAttrCircle(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	created, fac, err := h.Provision(ctx, "sa-a", "超管A")
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
	saTok, err := fac.Login(ctx, "sa-a", "sa-pass")
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把登录的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 新建组织节点，失败说明名称或上级不合法。
	site, err := fac.CreateOrgUnit(ctx, saTok, "场地", nil)
	// 建组织失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建组织节点，失败说明名称或上级不合法。
	shopA, err := fac.CreateOrgUnit(ctx, saTok, "车间A", &site.ID)
	// 建组织失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建组织节点，失败说明名称或上级不合法。
	shopB, err := fac.CreateOrgUnit(ctx, saTok, "车间B", &site.ID)
	// 建组织失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建组织节点，失败说明名称或上级不合法。
	team, err := fac.CreateOrgUnit(ctx, saTok, "班组", &shopA.ID)
	// 建组织失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 新建厂内人员，失败说明登录名冲突或越权。
	none, err := fac.CreatePerson(ctx, saTok, "none", "只分配")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建人员的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 挂组织失败就停，否则后面没有可靠结果。
	if err := fac.Assign(ctx, saTok, none.ID, shopA.ID); err != nil {
		// 把挂组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把默认口令改成测试口令，失败说明改密被拒。
	noneTok := mustAdoptPassword(t, ctx, fac, "none", "none-pass")
	// 写事实应因越权被拒绝，放行说明没拦住。
	if _, err := fac.CreateFact(ctx, noneTok, factory.WorkContext{OrgUnitID: &shopA.ID}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("7.1: %v", err)
	}

	// 新建厂内人员，失败说明登录名冲突或越权。
	oa, err := fac.CreatePerson(ctx, saTok, "oa", "组织管理员")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建人员的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 授角色失败就停，否则后面没有可靠结果。
	if _, err := fac.GrantRole(ctx, saTok, oa.ID, factory.RoleOrgAdmin, factory.ScopeOrgUnit, &shopA.ID); err != nil {
		// 把授角色的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把默认口令改成测试口令，失败说明改密被拒。
	oaTok := mustAdoptPassword(t, ctx, fac, "oa", "oa-pass")
	// 写事实应因上下文非法被拒绝，放行说明没拦住。
	if _, err := fac.CreateFact(ctx, oaTok, factory.WorkContext{OrgUnitID: &shopA.ID}); !errors.Is(err, domain.ErrWorkContext) {
		t.Fatalf("8.3: %v", err)
	}

	// 新建厂内人员，失败说明登录名冲突或越权。
	eng, err := fac.CreatePerson(ctx, saTok, "eng", "工程师")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建人员的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 授角色失败就停，否则后面没有可靠结果。
	if _, err := fac.GrantRole(ctx, saTok, eng.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shopA.ID); err != nil {
		// 把授角色的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把默认口令改成测试口令，失败说明改密被拒。
	engTok := mustAdoptPassword(t, ctx, fac, "eng", "eng-pass")
	// 写事实应因上下文非法被拒绝，放行说明没拦住。
	if _, err := fac.CreateFact(ctx, engTok, factory.WorkContext{OrgUnitID: &shopA.ID}); !errors.Is(err, domain.ErrWorkContext) {
		t.Fatalf("8.4: %v", err)
	}

	// 新建厂内人员，失败说明登录名冲突或越权。
	q, err := fac.CreatePerson(ctx, saTok, "q", "厂级操作员")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建人员的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 授角色失败就停，否则后面没有可靠结果。
	if _, err := fac.GrantRole(ctx, saTok, q.ID, factory.RoleOperator, factory.ScopeFactory, nil); err != nil {
		// 把授角色的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把默认口令改成测试口令，失败说明改密被拒。
	qTok := mustAdoptPassword(t, ctx, fac, "q", "q-pass")
	// 写一条工作事实，失败说明上下文或权限不够。
	direct, err := fac.CreateFact(ctx, qTok, factory.WorkContext{Direct: true})
	// 写事实失败就停，否则后面没有可靠结果。
	if err != nil {
		// 条目 16.1 没对上，这一条矩阵行为偏了。
		t.Fatalf("16.1: %v", err)
	}
	// 路径上的节点和名称应与当时组织一致，漂移即失败。
	if direct.OrgUnitID != nil || len(direct.OrgPath) != 0 {
		// 条目 16.1 没对上，这一条矩阵行为偏了。
		t.Fatalf("16.1 not direct: %+v", direct)
	}
	// 写事实应因上下文非法被拒绝，放行说明没拦住。
	if _, err := fac.CreateFact(ctx, qTok, factory.WorkContext{OrgUnitID: &shopA.ID}); !errors.Is(err, domain.ErrWorkContext) {
		t.Fatalf("16.3: %v", err)
	}

	// 新建厂内人员，失败说明登录名冲突或越权。
	orgOp, err := fac.CreatePerson(ctx, saTok, "orgop", "节点操作员")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建人员的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 授角色失败就停，否则后面没有可靠结果。
	if _, err := fac.GrantRole(ctx, saTok, orgOp.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shopA.ID); err != nil {
		// 把授角色的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把默认口令改成测试口令，失败说明改密被拒。
	orgOpTok := mustAdoptPassword(t, ctx, fac, "orgop", "orgop-pass")
	// 写事实应因越权被拒绝，放行说明没拦住。
	if _, err := fac.CreateFact(ctx, orgOpTok, factory.WorkContext{Direct: true}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("16.2: %v", err)
	}

	// 挂组织失败就停，否则后面没有可靠结果。
	if err := fac.Assign(ctx, saTok, q.ID, shopA.ID); err != nil {
		// 把挂组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 写一条工作事实，失败说明上下文或权限不够。
	factA, err := fac.CreateFact(ctx, qTok, factory.WorkContext{OrgUnitID: &shopA.ID})
	// 写事实失败就停，否则后面没有可靠结果。
	if err != nil {
		// 条目 12.1 没对上，这一条矩阵行为偏了。
		t.Fatalf("12.1: %v", err)
	}
	// 事实应落在预期组织上，串了车间说明上下文偏了。
	if factA.OrgUnitID == nil || *factA.OrgUnitID != shopA.ID {
		// 条目 12.1 没对上，这一条矩阵行为偏了。
		t.Fatalf("12.1 unit: %+v", factA)
	}
	// 路径上的节点和名称应与当时组织一致，漂移即失败。
	if !pathHas(factA.OrgPath, site.ID) || !pathHas(factA.OrgPath, shopA.ID) || pathHas(factA.OrgPath, shopB.ID) {
		// 条目 12.1 没对上，这一条矩阵行为偏了。
		t.Fatalf("12.1 path: %+v", factA.OrgPath)
	}
	// 路径上的节点和名称应与当时组织一致，漂移即失败。
	if pathName(factA.OrgPath, shopA.ID) != "车间A" {
		// 条目 12.1 没对上，这一条矩阵行为偏了。
		t.Fatalf("12.1 name: %+v", factA.OrgPath)
	}

	// 准备个人资产正文，审计里出现它就算泄密。
	const payload = "personal-secret"
	// 新建一份个人资产，失败说明归属或上下文不对。
	asset, err := fac.CreatePersonalAsset(ctx, qTok, factory.WorkContext{OrgUnitID: &shopA.ID}, payload)
	// 建个人资产失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建个人资产的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 摘组织失败就停，否则后面没有可靠结果。
	if err := fac.Unassign(ctx, saTok, q.ID, shopA.ID); err != nil {
		// 把摘组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 挂组织失败就停，否则后面没有可靠结果。
	if err := fac.Assign(ctx, saTok, q.ID, shopB.ID); err != nil {
		// 把挂组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 按编号读工作事实，失败说明无权或已不存在。
	old, err := fac.GetFact(ctx, qTok, factA.ID)
	// 读事实失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把读事实的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 路径上的节点和名称应与当时组织一致，漂移即失败。
	if *old.OrgUnitID != shopA.ID || pathName(old.OrgPath, shopA.ID) != "车间A" || pathHas(old.OrgPath, shopB.ID) {
		// 条目 13.1 没对上，这一条矩阵行为偏了。
		t.Fatalf("13.1 drifted: %+v", old)
	}
	// 写一条工作事实，失败说明上下文或权限不够。
	factB, err := fac.CreateFact(ctx, qTok, factory.WorkContext{OrgUnitID: &shopB.ID})
	// 写事实失败就停，否则后面没有可靠结果。
	if err != nil {
		// 条目 13.2 没对上，这一条矩阵行为偏了。
		t.Fatalf("13.2: %v", err)
	}
	// 路径上的节点和名称应与当时组织一致，漂移即失败。
	if *factB.OrgUnitID != shopB.ID || pathHas(factB.OrgPath, shopA.ID) {
		// 条目 13.2 没对上，这一条矩阵行为偏了。
		t.Fatalf("13.2: %+v", factB)
	}
	// 写事实应因上下文非法被拒绝，放行说明没拦住。
	if _, err := fac.CreateFact(ctx, qTok, factory.WorkContext{OrgUnitID: &shopA.ID}); !errors.Is(err, domain.ErrWorkContext) {
		t.Fatalf("13 after unassign A: %v", err)
	}

	// 摘组织失败就停，否则后面没有可靠结果。
	if err := fac.Unassign(ctx, saTok, q.ID, shopB.ID); err != nil {
		// 把摘组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 挂组织失败就停，否则后面没有可靠结果。
	if err := fac.Assign(ctx, saTok, q.ID, shopA.ID); err != nil {
		// 把挂组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 改组织名失败就停，否则后面没有可靠结果。
	if err := fac.RenameOrgUnit(ctx, saTok, shopA.ID, "车间A改名"); err != nil {
		// 把改组织名的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 改上级失败就停，否则后面没有可靠结果。
	if err := fac.ReparentOrgUnit(ctx, saTok, shopA.ID, &shopB.ID); err != nil {
		// 把改上级的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 按编号读工作事实，失败说明无权或已不存在。
	still, err := fac.GetFact(ctx, saTok, factA.ID)
	// 读事实失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把读事实的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 路径上的节点和名称应与当时组织一致，漂移即失败。
	if pathName(still.OrgPath, shopA.ID) != "车间A" || pathHas(still.OrgPath, shopB.ID) {
		// 条目 14.1 没对上，这一条矩阵行为偏了。
		t.Fatalf("14.1 drifted: %+v", still)
	}
	// 写一条工作事实，失败说明上下文或权限不够。
	fresh, err := fac.CreateFact(ctx, qTok, factory.WorkContext{OrgUnitID: &shopA.ID})
	// 写事实失败就停，否则后面没有可靠结果。
	if err != nil {
		// 条目 14.2 没对上，这一条矩阵行为偏了。
		t.Fatalf("14.2: %v", err)
	}
	// 路径上的节点和名称应与当时组织一致，漂移即失败。
	if pathName(fresh.OrgPath, shopA.ID) != "车间A改名" || !pathHas(fresh.OrgPath, shopB.ID) {
		// 条目 14.2 没对上，这一条矩阵行为偏了。
		t.Fatalf("14.2 new path: %+v", fresh.OrgPath)
	}
	// 改写路径应因越权被拒绝，放行说明没拦住。
	if err := fac.RewriteFactPath(ctx, saTok, factA.ID); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("14 rewrite: %v", err)
	}

	// 读取个人资产，失败说明无权或已经没有。
	gotAsset, err := fac.GetPersonalAsset(ctx, qTok, asset.ID)
	// 读个人稿失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把读个人稿的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 路径上的节点和名称应与当时组织一致，漂移即失败。
	if gotAsset.CreatorID != q.ID || pathName(gotAsset.OrgPath, shopA.ID) != "车间A" {
		// 条目 15.1 没对上，这一条矩阵行为偏了。
		t.Fatalf("15.1: %+v", gotAsset)
	}
	// 转个人资产应因越权被拒绝，放行说明没拦住。
	if err := fac.TransferPersonalAsset(ctx, saTok, asset.ID, created.SuperAdminID); !errors.Is(err, domain.ErrForbidden) {
		// 条目 15.2 没对上，这一条矩阵行为偏了。
		t.Fatalf("15.2: %v", err)
	}
	// 读个人正文应因越权被拒绝，放行说明没拦住。
	if _, err := fac.ReadPersonalAssetContent(ctx, saTok, asset.ID); !errors.Is(err, domain.ErrForbidden) {
		// 条目 15.3 没对上，这一条矩阵行为偏了。
		t.Fatalf("15.3: %v", err)
	}
	// 读回个人正文，失败说明无权或稿已不可读。
	body, err := fac.ReadPersonalAssetContent(ctx, qTok, asset.ID)
	// 读个人正文失败或结果不符就停，说明没达预期。
	if err != nil || body != payload {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("15 creator read: %v %q", err, body)
	}

	// 摘组织失败就停，否则后面没有可靠结果。
	if err := fac.Unassign(ctx, saTok, q.ID, shopA.ID); err != nil {
		// 把摘组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 挂组织失败就停，否则后面没有可靠结果。
	if err := fac.Assign(ctx, saTok, q.ID, team.ID); err != nil {
		// 把挂组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 停用组织失败就停，否则后面没有可靠结果。
	if err := fac.DisableOrgUnit(ctx, saTok, team.ID); err != nil {
		// 把停用组织的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 写事实应因组织已停用被拒绝，放行说明没拦住。
	if _, err := fac.CreateFact(ctx, qTok, factory.WorkContext{OrgUnitID: &team.ID}); !errors.Is(err, domain.ErrDisabledOrgUnit) {
		t.Fatalf("11.5: %v", err)
	}

	// 停用账号失败就停，否则后面没有可靠结果。
	if err := fac.DisableAccount(ctx, saTok, q.ID); err != nil {
		// 把停用账号的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 按作者列出事实，失败说明无权查看名单。
	listed, err := fac.ListFactsByCreator(ctx, saTok, q.ID)
	// 列事实失败或条数不对就停，说明没达预期。
	if err != nil || len(listed) == 0 {
		// 条目 10.8 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.8 list: %v %d", err, len(listed))
	}
	// 读事实失败就停，否则后面没有可靠结果。
	if _, err := fac.GetFact(ctx, saTok, factA.ID); err != nil {
		// 条目 10.8 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.8 get: %v", err)
	}
	// 读个人正文应因越权被拒绝，放行说明没拦住。
	if _, err := fac.ReadPersonalAssetContent(ctx, saTok, asset.ID); !errors.Is(err, domain.ErrForbidden) {
		// 条目 15.3 没对上，这一条矩阵行为偏了。
		t.Fatalf("15.3 after disable: %v", err)
	}

	// 拉出审计流水，失败则无法核对有没有记账。
	rows, err := fac.ListAudit(ctx)
	// 拉审计失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把拉审计的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把审计打成可搜文本，打不出则查不了泄密。
	dump := audit.Dump(rows)
	// 审计里不应出现口令或正文，出现了就算泄密。
	if audit.ContainsAny(dump, payload, "q-pass", "sa-pass") {
		// 结果里出现了不该有的敏感词，说明已经泄密。
		t.Fatalf("17 secret/content leaked")
	}
	// 审计应留下允许或拒绝，缺了说明这步没记账。
	if !audit.HasResult(rows, "create_fact", audit.Allow) || !audit.HasResult(rows, "create_fact", audit.Deny) {
		// 条目 17.1 没对上，这一条矩阵行为偏了。
		t.Fatalf("17.1 fact audit missing")
	}
}

// 看路径里有没有这个节点，没有说明快照漏记。
func pathHas(path []factory.PathNode, id interface{ String() string }) bool { // 要能把编号打成文本
	want := id.String()
	for _, n := range path {
		// 节点对上就返回，对不上则继续看路径剩下的。
		if n.ID.String() == want {
			return true
		}
	}
	return false
}

// 取出路径上的名称，对不上说明改名没进快照。
func pathName(path []factory.PathNode, id interface{ String() string }) string { // 要能把编号打成文本
	want := id.String()
	for _, n := range path {
		// 节点对上就返回，对不上则继续看路径剩下的。
		if n.ID.String() == want {
			return n.Name
		}
	}
	return ""
}
