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

func TestAttrCircle(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	created, fac, err := h.Provision(ctx, "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa-a", created.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saTok, err := fac.Login(ctx, "sa-a", "sa-pass")
	if err != nil {
		t.Fatal(err)
	}

	site, err := fac.CreateOrgUnit(ctx, saTok, "场地", nil)
	if err != nil {
		t.Fatal(err)
	}
	shopA, err := fac.CreateOrgUnit(ctx, saTok, "车间A", &site.ID)
	if err != nil {
		t.Fatal(err)
	}
	shopB, err := fac.CreateOrgUnit(ctx, saTok, "车间B", &site.ID)
	if err != nil {
		t.Fatal(err)
	}
	team, err := fac.CreateOrgUnit(ctx, saTok, "班组", &shopA.ID)
	if err != nil {
		t.Fatal(err)
	}

	none, err := fac.CreatePerson(ctx, saTok, "none", "只分配")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Assign(ctx, saTok, none.ID, shopA.ID); err != nil {
		t.Fatal(err)
	}
	noneTok := mustAdoptPassword(t, ctx, fac, "none", "none-pass")
	if _, err := fac.CreateFact(ctx, noneTok, factory.WorkContext{OrgUnitID: &shopA.ID}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("7.1: %v", err)
	}

	oa, err := fac.CreatePerson(ctx, saTok, "oa", "组织管理员")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.GrantRole(ctx, saTok, oa.ID, factory.RoleOrgAdmin, factory.ScopeOrgUnit, &shopA.ID); err != nil {
		t.Fatal(err)
	}
	oaTok := mustAdoptPassword(t, ctx, fac, "oa", "oa-pass")
	if _, err := fac.CreateFact(ctx, oaTok, factory.WorkContext{OrgUnitID: &shopA.ID}); !errors.Is(err, domain.ErrWorkContext) {
		t.Fatalf("8.3: %v", err)
	}

	eng, err := fac.CreatePerson(ctx, saTok, "eng", "工程师")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.GrantRole(ctx, saTok, eng.ID, factory.RoleProcessEngineer, factory.ScopeOrgUnit, &shopA.ID); err != nil {
		t.Fatal(err)
	}
	engTok := mustAdoptPassword(t, ctx, fac, "eng", "eng-pass")
	if _, err := fac.CreateFact(ctx, engTok, factory.WorkContext{OrgUnitID: &shopA.ID}); !errors.Is(err, domain.ErrWorkContext) {
		t.Fatalf("8.4: %v", err)
	}

	q, err := fac.CreatePerson(ctx, saTok, "q", "厂级操作员")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.GrantRole(ctx, saTok, q.ID, factory.RoleOperator, factory.ScopeFactory, nil); err != nil {
		t.Fatal(err)
	}
	qTok := mustAdoptPassword(t, ctx, fac, "q", "q-pass")
	direct, err := fac.CreateFact(ctx, qTok, factory.WorkContext{Direct: true})
	if err != nil {
		t.Fatalf("16.1: %v", err)
	}
	if direct.OrgUnitID != nil || len(direct.OrgPath) != 0 {
		t.Fatalf("16.1 not direct: %+v", direct)
	}
	if _, err := fac.CreateFact(ctx, qTok, factory.WorkContext{OrgUnitID: &shopA.ID}); !errors.Is(err, domain.ErrWorkContext) {
		t.Fatalf("16.3: %v", err)
	}

	orgOp, err := fac.CreatePerson(ctx, saTok, "orgop", "节点操作员")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.GrantRole(ctx, saTok, orgOp.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shopA.ID); err != nil {
		t.Fatal(err)
	}
	orgOpTok := mustAdoptPassword(t, ctx, fac, "orgop", "orgop-pass")
	if _, err := fac.CreateFact(ctx, orgOpTok, factory.WorkContext{Direct: true}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("16.2: %v", err)
	}

	if err := fac.Assign(ctx, saTok, q.ID, shopA.ID); err != nil {
		t.Fatal(err)
	}
	factA, err := fac.CreateFact(ctx, qTok, factory.WorkContext{OrgUnitID: &shopA.ID})
	if err != nil {
		t.Fatalf("12.1: %v", err)
	}
	if factA.OrgUnitID == nil || *factA.OrgUnitID != shopA.ID {
		t.Fatalf("12.1 unit: %+v", factA)
	}
	if !pathHas(factA.OrgPath, site.ID) || !pathHas(factA.OrgPath, shopA.ID) || pathHas(factA.OrgPath, shopB.ID) {
		t.Fatalf("12.1 path: %+v", factA.OrgPath)
	}
	if pathName(factA.OrgPath, shopA.ID) != "车间A" {
		t.Fatalf("12.1 name: %+v", factA.OrgPath)
	}

	const payload = "personal-secret"
	asset, err := fac.CreatePersonalAsset(ctx, qTok, factory.WorkContext{OrgUnitID: &shopA.ID}, payload)
	if err != nil {
		t.Fatal(err)
	}

	if err := fac.Unassign(ctx, saTok, q.ID, shopA.ID); err != nil {
		t.Fatal(err)
	}
	if err := fac.Assign(ctx, saTok, q.ID, shopB.ID); err != nil {
		t.Fatal(err)
	}
	old, err := fac.GetFact(ctx, qTok, factA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *old.OrgUnitID != shopA.ID || pathName(old.OrgPath, shopA.ID) != "车间A" || pathHas(old.OrgPath, shopB.ID) {
		t.Fatalf("13.1 drifted: %+v", old)
	}
	factB, err := fac.CreateFact(ctx, qTok, factory.WorkContext{OrgUnitID: &shopB.ID})
	if err != nil {
		t.Fatalf("13.2: %v", err)
	}
	if *factB.OrgUnitID != shopB.ID || pathHas(factB.OrgPath, shopA.ID) {
		t.Fatalf("13.2: %+v", factB)
	}
	if _, err := fac.CreateFact(ctx, qTok, factory.WorkContext{OrgUnitID: &shopA.ID}); !errors.Is(err, domain.ErrWorkContext) {
		t.Fatalf("13 after unassign A: %v", err)
	}

	if err := fac.Unassign(ctx, saTok, q.ID, shopB.ID); err != nil {
		t.Fatal(err)
	}
	if err := fac.Assign(ctx, saTok, q.ID, shopA.ID); err != nil {
		t.Fatal(err)
	}
	if err := fac.RenameOrgUnit(ctx, saTok, shopA.ID, "车间A改名"); err != nil {
		t.Fatal(err)
	}
	if err := fac.ReparentOrgUnit(ctx, saTok, shopA.ID, &shopB.ID); err != nil {
		t.Fatal(err)
	}
	still, err := fac.GetFact(ctx, saTok, factA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pathName(still.OrgPath, shopA.ID) != "车间A" || pathHas(still.OrgPath, shopB.ID) {
		t.Fatalf("14.1 drifted: %+v", still)
	}
	fresh, err := fac.CreateFact(ctx, qTok, factory.WorkContext{OrgUnitID: &shopA.ID})
	if err != nil {
		t.Fatalf("14.2: %v", err)
	}
	if pathName(fresh.OrgPath, shopA.ID) != "车间A改名" || !pathHas(fresh.OrgPath, shopB.ID) {
		t.Fatalf("14.2 new path: %+v", fresh.OrgPath)
	}
	if err := fac.RewriteFactPath(ctx, saTok, factA.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("14 rewrite: %v", err)
	}

	gotAsset, err := fac.GetPersonalAsset(ctx, qTok, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotAsset.CreatorID != q.ID || pathName(gotAsset.OrgPath, shopA.ID) != "车间A" {
		t.Fatalf("15.1: %+v", gotAsset)
	}
	if err := fac.TransferPersonalAsset(ctx, saTok, asset.ID, created.SuperAdminID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("15.2: %v", err)
	}
	if _, err := fac.ReadPersonalAssetContent(ctx, saTok, asset.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("15.3: %v", err)
	}
	body, err := fac.ReadPersonalAssetContent(ctx, qTok, asset.ID)
	if err != nil || body != payload {
		t.Fatalf("15 creator read: %v %q", err, body)
	}

	if err := fac.Unassign(ctx, saTok, q.ID, shopA.ID); err != nil {
		t.Fatal(err)
	}
	if err := fac.Assign(ctx, saTok, q.ID, team.ID); err != nil {
		t.Fatal(err)
	}
	if err := fac.DisableOrgUnit(ctx, saTok, team.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.CreateFact(ctx, qTok, factory.WorkContext{OrgUnitID: &team.ID}); !errors.Is(err, domain.ErrDisabledOrgUnit) {
		t.Fatalf("11.5: %v", err)
	}

	if err := fac.DisableAccount(ctx, saTok, q.ID); err != nil {
		t.Fatal(err)
	}
	listed, err := fac.ListFactsByCreator(ctx, saTok, q.ID)
	if err != nil || len(listed) == 0 {
		t.Fatalf("10.8 list: %v %d", err, len(listed))
	}
	if _, err := fac.GetFact(ctx, saTok, factA.ID); err != nil {
		t.Fatalf("10.8 get: %v", err)
	}
	if _, err := fac.ReadPersonalAssetContent(ctx, saTok, asset.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("15.3 after disable: %v", err)
	}

	rows, err := fac.ListAudit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dump := audit.Dump(rows)
	if audit.ContainsAny(dump, payload, "q-pass", "sa-pass") {
		t.Fatalf("17 secret/content leaked")
	}
	if !audit.HasResult(rows, "create_fact", audit.Allow) || !audit.HasResult(rows, "create_fact", audit.Deny) {
		t.Fatalf("17.1 fact audit missing")
	}
}

func pathHas(path []factory.PathNode, id interface{ String() string }) bool {
	want := id.String()
	for _, n := range path {
		if n.ID.String() == want {
			return true
		}
	}
	return false
}

func pathName(path []factory.PathNode, id interface{ String() string }) string {
	want := id.String()
	for _, n := range path {
		if n.ID.String() == want {
			return n.Name
		}
	}
	return ""
}
