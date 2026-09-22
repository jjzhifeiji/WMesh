// 第 7 圈：厂内侧矩阵编号。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

var matrixIDs = []string{
	"2.1", "2.3", "2.4",
	"4.1", "4.2",
	"5.1", "5.2", "5.3", "5.4",
	"6.1", "6.2", "6.3", "6.4", "6.5", "6.6",
	"7.1", "7.2",
	"8.1", "8.2", "8.3", "8.4",
	"9.1", "9.2", "9.3", "9.4", "9.5", "9.6",
	"10.1", "10.2", "10.3", "10.4", "10.5", "10.6", "10.7", "10.8", "10.9", "10.10",
	"11.2", "11.3", "11.4", "11.5",
	"12.1",
	"13.1", "13.2", "13.3",
	"14.1", "14.2", "14.3",
	"15.1", "15.2", "15.3",
	"16.1", "16.2", "16.3",
	"17.1", "17.2", "17.3",
}

func TestMatrix(t *testing.T) {
	ctx := context.Background()
	ran := map[string]bool{}
	run := func(id string, fn func(*testing.T)) {
		t.Helper()
		t.Run(id, func(t *testing.T) {
			ran[id] = true
			fn(t)
		})
	}
	t.Cleanup(func() {
		for _, id := range matrixIDs {
			if !ran[id] {
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})

	h := New(t)
	a, facA, err := h.Provision(ctx, "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	run("2.1", func(t *testing.T) {
		if n, _ := facA.Store().PersonCount(ctx); n != 1 {
			t.Fatalf("sa count %d", n)
		}
	})
	run("10.7", func(t *testing.T) {
		p, err := facA.Store().PersonByID(ctx, a.SuperAdminID)
		if err != nil || p.Status != factory.StatusPending {
			t.Fatalf("pending sa: %+v %v", p, err)
		}
	})
	run("10.1", func(t *testing.T) {
		if _, err := facA.Login(ctx, "sa-a", "no"); !errors.Is(err, domain.ErrAccountPending) {
			t.Fatalf("login: %v", err)
		}
		if _, err := facA.RequireActive(ctx, "none"); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("protect: %v", err)
		}
	})
	var saA string
	run("2.4", func(t *testing.T) {
		if err := facA.Activate(ctx, "sa-a", a.ActivationToken, "sa-pass"); err != nil {
			t.Fatal(err)
		}
		tok, err := facA.Login(ctx, "sa-a", "sa-pass")
		if err != nil {
			t.Fatal(err)
		}
		saA = tok
	})
	run("17.3", func(t *testing.T) {
		if err := facA.ChangePassword(ctx, saA, "sa-pass-2"); err != nil {
			t.Fatal(err)
		}
		saA = mustLogin(t, ctx, facA, "sa-a", "sa-pass-2")
	})

	b, facB, err := h.Provision(ctx, "sa-b", "超管B")
	if err != nil {
		t.Fatal(err)
	}
	if err := facB.Activate(ctx, "sa-b", b.ActivationToken, "sb-pass"); err != nil {
		t.Fatal(err)
	}
	saB := mustLogin(t, ctx, facB, "sa-b", "sb-pass")

	run("2.3", func(t *testing.T) {
		if _, err := facB.Login(ctx, "sa-a", "sa-pass-2"); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("login B: %v", err)
		}
		if _, err := facB.RequireActive(ctx, saA); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("manage B: %v", err)
		}
		if _, err := facB.CreateFact(ctx, saA, factory.WorkContext{Direct: true}); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("fact B: %v", err)
		}
	})

	var site, shop, shopB, line, team, spare factory.OrgUnit
	run("4.1", func(t *testing.T) {
		var err error
		site, err = facA.CreateOrgUnit(ctx, saA, "场地", nil)
		if err != nil {
			t.Fatal(err)
		}
	})
	run("5.1", func(t *testing.T) {
		var err error
		shop, err = facA.CreateOrgUnit(ctx, saA, "车间", &site.ID)
		if err != nil {
			t.Fatal(err)
		}
		shopB, err = facA.CreateOrgUnit(ctx, saA, "车间B", &site.ID)
		if err != nil {
			t.Fatal(err)
		}
		line, err = facA.CreateOrgUnit(ctx, saA, "产线", &shop.ID)
		if err != nil {
			t.Fatal(err)
		}
		team, err = facA.CreateOrgUnit(ctx, saA, "班组", &line.ID)
		if err != nil {
			t.Fatal(err)
		}
		spare, err = facA.CreateOrgUnit(ctx, saA, "备用叶", &shopB.ID)
		if err != nil {
			t.Fatal(err)
		}
	})
	run("4.2", func(t *testing.T) {
		old, err := facA.RequireActive(ctx, saA)
		if err != nil {
			t.Fatal(err)
		}
		if err := facA.Rename(ctx, saA, "超管A改名", "sa-a"); err != nil {
			t.Fatal(err)
		}
		got, err := facA.RequireActive(ctx, saA)
		if err != nil || got.ID != old.ID {
			t.Fatalf("id drifted %+v %v", got, err)
		}
	})
	run("5.4", func(t *testing.T) {
		if err := facA.ReparentOrgUnit(ctx, saA, site.ID, &site.ID); !errors.Is(err, domain.ErrCycle) {
			t.Fatalf("self: %v", err)
		}
		if err := facA.ReparentOrgUnit(ctx, saA, site.ID, &team.ID); !errors.Is(err, domain.ErrCycle) {
			t.Fatalf("desc: %v", err)
		}
	})
	run("5.3", func(t *testing.T) {
		if err := facA.AddParent(ctx, saA, shop.ID, shopB.ID); !errors.Is(err, domain.ErrMultiParent) {
			t.Fatalf("got %v", err)
		}
	})
	bUnit, err := facB.CreateOrgUnit(ctx, saB, "厂B节点", nil)
	if err != nil {
		t.Fatal(err)
	}
	run("5.2", func(t *testing.T) {
		if err := facA.ReparentOrgUnit(ctx, saA, shop.ID, &bUnit.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v", err)
		}
	})

	lone, err := facA.CreatePerson(ctx, saA, "lone", "未分配")
	if err != nil {
		t.Fatal(err)
	}
	run("6.1", func(t *testing.T) {
		n, err := facA.Store().AssignmentCount(ctx, lone.ID)
		if err != nil || n != 0 {
			t.Fatalf("assign %d %v", n, err)
		}
	})
	p, err := facA.CreatePerson(ctx, saA, "p", "人员P")
	if err != nil {
		t.Fatal(err)
	}
	pTok := mustAdoptPassword(t, ctx, facA, "p", "p-pass")
	run("6.2", func(t *testing.T) {
		if err := facA.Assign(ctx, saA, p.ID, shop.ID); err != nil {
			t.Fatal(err)
		}
	})
	run("6.3", func(t *testing.T) {
		if err := facA.Assign(ctx, saA, p.ID, shopB.ID); !errors.Is(err, domain.ErrDuplicateAssignment) {
			t.Fatalf("got %v", err)
		}
	})
	run("6.4", func(t *testing.T) {
		if err := facA.Assign(ctx, saA, p.ID, bUnit.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v", err)
		}
	})
	run("6.5", func(t *testing.T) {
		if _, err := facB.Login(ctx, "p", "p-pass"); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("got %v", err)
		}
	})
	run("6.6", func(t *testing.T) {
		same, err := facB.CreatePerson(ctx, saB, "p", "厂B同名")
		if err != nil || same.ID == p.ID {
			t.Fatalf("%v %+v", err, same)
		}
	})
	run("7.1", func(t *testing.T) {
		if _, err := facA.CreateFact(ctx, pTok, factory.WorkContext{OrgUnitID: &shop.ID}); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("fact: %v", err)
		}
		if _, err := facA.CreatePersonalAsset(ctx, pTok, factory.WorkContext{OrgUnitID: &shop.ID}, "x"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("asset: %v", err)
		}
	})
	run("7.2", func(t *testing.T) {
		if err := facA.Operate(ctx, pTok, shop.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})

	oa := mustCreateRole(t, ctx, facA, saA, "oa", "oa-pass", factory.RoleOrgAdmin, factory.ScopeOrgUnit, &shop.ID)
	run("8.1", func(t *testing.T) {
		if _, err := facA.CreateOrgUnit(ctx, oa.tok, "线2", &shop.ID); err != nil {
			t.Fatal(err)
		}
	})
	run("9.5", func(t *testing.T) {
		if err := facA.Unassign(ctx, oa.tok, p.ID, shop.ID); err != nil {
			t.Fatal(err)
		}
		if err := facA.Assign(ctx, oa.tok, p.ID, line.ID); err != nil {
			t.Fatal(err)
		}
	})
	run("8.3", func(t *testing.T) {
		if _, err := facA.CreateFact(ctx, oa.tok, factory.WorkContext{OrgUnitID: &shop.ID}); !errors.Is(err, domain.ErrWorkContext) {
			t.Fatalf("got %v", err)
		}
	})
	lead := mustCreateRole(t, ctx, facA, saA, "lead", "lead-pass", factory.RoleOrgLead, factory.ScopeOrgUnit, &shop.ID)
	aud := mustCreateRole(t, ctx, facA, saA, "aud", "aud-pass", factory.RoleAuditor, factory.ScopeOrgUnit, &shop.ID)
	run("8.2", func(t *testing.T) {
		if err := facA.ViewOrg(ctx, lead.tok, shop.ID); err != nil {
			t.Fatal(err)
		}
		if err := facA.ViewOrg(ctx, aud.tok, shop.ID); err != nil {
			t.Fatal(err)
		}
	})
	eng := mustCreateRole(t, ctx, facA, saA, "eng", "eng-pass", factory.RoleOperator, factory.ScopeOrgUnit, &shop.ID)
	run("8.4", func(t *testing.T) {
		if _, err := facA.CreateFact(ctx, eng.tok, factory.WorkContext{OrgUnitID: &shop.ID}); !errors.Is(err, domain.ErrWorkContext) {
			t.Fatalf("got %v", err)
		}
	})
	run("9.1", func(t *testing.T) {
		if _, err := facA.CreateOrgUnit(ctx, oa.tok, "越界", &shopB.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("9.2", func(t *testing.T) {
		if _, err := facA.CreateOrgUnit(ctx, lead.tok, "x", &shop.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		if _, err := facA.CreatePerson(ctx, lead.tok, "x", "x"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	op := mustCreateRole(t, ctx, facA, saA, "op", "op-pass", factory.RoleOperator, factory.ScopeOrgUnit, &shop.ID)
	run("9.3", func(t *testing.T) {
		if _, err := facA.CreatePerson(ctx, op.tok, "y", "y"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		if _, err := facA.GrantRole(ctx, op.tok, p.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shop.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		if _, err := facA.ResetPassword(ctx, op.tok, p.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("reset: %v", err)
		}
	})
	run("9.4", func(t *testing.T) {
		if _, err := facA.CreateFact(ctx, aud.tok, factory.WorkContext{OrgUnitID: &shop.ID}); !errors.Is(err, domain.ErrWorkContext) && !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		if _, err := facA.CreateOrgUnit(ctx, aud.tok, "z", &shop.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("9.6", func(t *testing.T) {
		if _, err := facA.GrantRole(ctx, oa.tok, p.ID, factory.RoleFactorySuperAdmin, factory.ScopeFactory, nil); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		if _, err := facA.GrantRole(ctx, oa.tok, p.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shopB.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})

	run("11.2", func(t *testing.T) {
		if err := facA.DisableOrgUnit(ctx, saA, shop.ID); !errors.Is(err, domain.ErrHasActiveChildren) {
			t.Fatalf("got %v", err)
		}
	})
	run("11.3", func(t *testing.T) {
		if err := facA.DisableOrgUnit(ctx, saA, spare.ID); err != nil {
			t.Fatal(err)
		}
		u, err := facA.Store().Unit(ctx, spare.ID)
		if err != nil || u.Status != factory.StatusDisabled {
			t.Fatalf("not kept %+v %v", u, err)
		}
	})
	run("11.5", func(t *testing.T) {
		if err := facA.Assign(ctx, saA, p.ID, spare.ID); !errors.Is(err, domain.ErrDisabledOrgUnit) {
			t.Fatalf("assign: %v", err)
		}
		if _, err := facA.CreateFact(ctx, pTok, factory.WorkContext{OrgUnitID: &spare.ID}); !errors.Is(err, domain.ErrDisabledOrgUnit) && !errors.Is(err, domain.ErrForbidden) && !errors.Is(err, domain.ErrWorkContext) {
			t.Fatalf("ctx: %v", err)
		}
	})

	q := mustCreateRole(t, ctx, facA, saA, "q", "q-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	if err := facA.Assign(ctx, saA, q.acc.ID, shop.ID); err != nil {
		t.Fatal(err)
	}
	var factA factory.FactStub
	run("16.1", func(t *testing.T) {
		direct, err := facA.CreateFact(ctx, q.tok, factory.WorkContext{Direct: true})
		if err != nil || direct.OrgUnitID != nil || len(direct.OrgPath) != 0 {
			t.Fatalf("%v %+v", err, direct)
		}
	})
	run("16.2", func(t *testing.T) {
		if _, err := facA.CreateFact(ctx, op.tok, factory.WorkContext{Direct: true}); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("16.3", func(t *testing.T) {
		if _, err := facA.CreateFact(ctx, oa.tok, factory.WorkContext{OrgUnitID: &shop.ID}); !errors.Is(err, domain.ErrWorkContext) {
			t.Fatalf("got %v", err)
		}
	})
	run("12.1", func(t *testing.T) {
		var err error
		factA, err = facA.CreateFact(ctx, q.tok, factory.WorkContext{OrgUnitID: &shop.ID})
		if err != nil {
			t.Fatal(err)
		}
		if factA.OrgUnitID == nil || *factA.OrgUnitID != shop.ID || !pathHas(factA.OrgPath, site.ID) || pathHas(factA.OrgPath, shopB.ID) {
			t.Fatalf("path %+v", factA)
		}
	})
	const payload = "personal-secret"
	asset, err := facA.CreatePersonalAsset(ctx, q.tok, factory.WorkContext{OrgUnitID: &shop.ID}, payload)
	if err != nil {
		t.Fatal(err)
	}
	run("13.3", func(t *testing.T) {
		if err := facA.Unassign(ctx, saA, q.acc.ID, shop.ID); err != nil {
			t.Fatal(err)
		}
		if err := facA.Assign(ctx, saA, q.acc.ID, shopB.ID); err != nil {
			t.Fatal(err)
		}
	})
	run("13.1", func(t *testing.T) {
		old, err := facA.GetFact(ctx, q.tok, factA.ID)
		if err != nil || *old.OrgUnitID != shop.ID || pathHas(old.OrgPath, shopB.ID) {
			t.Fatalf("%v %+v", err, old)
		}
	})
	run("13.2", func(t *testing.T) {
		nb, err := facA.CreateFact(ctx, q.tok, factory.WorkContext{OrgUnitID: &shopB.ID})
		if err != nil || *nb.OrgUnitID != shopB.ID {
			t.Fatalf("%v %+v", err, nb)
		}
	})
	if err := facA.Unassign(ctx, saA, q.acc.ID, shopB.ID); err != nil {
		t.Fatal(err)
	}
	if err := facA.Assign(ctx, saA, q.acc.ID, shop.ID); err != nil {
		t.Fatal(err)
	}
	run("14.3", func(t *testing.T) {
		if err := facA.RenameOrgUnit(ctx, saA, shop.ID, "车间改名"); err != nil {
			t.Fatal(err)
		}
		if err := facA.ReparentOrgUnit(ctx, saA, shop.ID, &shopB.ID); err != nil {
			t.Fatal(err)
		}
	})
	run("14.1", func(t *testing.T) {
		old, err := facA.GetFact(ctx, saA, factA.ID)
		if err != nil || pathName(old.OrgPath, shop.ID) != "车间" || pathHas(old.OrgPath, shopB.ID) {
			t.Fatalf("%v %+v", err, old)
		}
	})
	run("14.2", func(t *testing.T) {
		fresh, err := facA.CreateFact(ctx, q.tok, factory.WorkContext{OrgUnitID: &shop.ID})
		if err != nil || pathName(fresh.OrgPath, shop.ID) != "车间改名" || !pathHas(fresh.OrgPath, shopB.ID) {
			t.Fatalf("%v %+v", err, fresh)
		}
	})
	run("15.1", func(t *testing.T) {
		got, err := facA.GetPersonalAsset(ctx, q.tok, asset.ID)
		if err != nil || got.CreatorID != q.acc.ID || pathName(got.OrgPath, shop.ID) != "车间" {
			t.Fatalf("%v %+v", err, got)
		}
	})
	run("15.2", func(t *testing.T) {
		if err := facA.TransferPersonalAsset(ctx, saA, asset.ID, a.SuperAdminID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		if err := facA.RewriteFactPath(ctx, saA, factA.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("15.3", func(t *testing.T) {
		if _, err := facA.ReadPersonalAssetContent(ctx, saA, asset.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		body, err := facA.ReadPersonalAssetContent(ctx, q.tok, asset.ID)
		if err != nil || body != payload {
			t.Fatalf("%v %q", err, body)
		}
	})
	run("11.4", func(t *testing.T) {
		if err := facA.Store().TryDeleteOrgUnit(ctx, shop.ID); !domain.IsForeignKeyViolation(err) {
			t.Fatalf("got %v", err)
		}
	})

	run("10.3", func(t *testing.T) {
		if err := facA.Logout(ctx, pTok); err != nil {
			t.Fatal(err)
		}
		if _, err := facA.RequireActive(ctx, pTok); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("got %v", err)
		}
		pTok = mustLogin(t, ctx, facA, "p", "p-pass")
	})
	run("10.4", func(t *testing.T) {
		if err := facA.RevokeRole(ctx, saA, op.grant); err != nil {
			t.Fatal(err)
		}
		if err := facA.Operate(ctx, op.tok, shop.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		if err := facA.DisableAccount(ctx, saA, p.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := facA.RequireActive(ctx, pTok); !errors.Is(err, domain.ErrAccountDisabled) {
			t.Fatalf("got %v", err)
		}
	})
	run("10.2", func(t *testing.T) {
		if _, err := facA.Login(ctx, "p", "p-pass"); !errors.Is(err, domain.ErrAccountDisabled) {
			t.Fatalf("got %v", err)
		}
	})
	run("10.8", func(t *testing.T) {
		if err := facA.DisableAccount(ctx, saA, q.acc.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := facA.GetFact(ctx, saA, factA.ID); err != nil {
			t.Fatal(err)
		}
		listed, err := facA.ListFactsByCreator(ctx, saA, q.acc.ID)
		if err != nil || len(listed) == 0 {
			t.Fatalf("%v %d", err, len(listed))
		}
	})
	run("10.5", func(t *testing.T) {
		sa2 := mustCreateRole(t, ctx, facA, saA, "sa2", "sa2-pass", factory.RoleFactorySuperAdmin, factory.ScopeFactory, nil)
		if err := facA.DisableAccount(ctx, saA, sa2.acc.ID); err != nil {
			t.Fatal(err)
		}
		sa3 := mustCreateRole(t, ctx, facA, saA, "sa3", "sa3-pass", factory.RoleFactorySuperAdmin, factory.ScopeFactory, nil)
		if err := facA.RevokeRole(ctx, saA, sa3.grant); err != nil {
			t.Fatal(err)
		}
	})
	run("10.6", func(t *testing.T) {
		if err := facA.DisableAccount(ctx, saA, a.SuperAdminID); !errors.Is(err, domain.ErrLastAdmin) {
			t.Fatalf("disable: %v", err)
		}
		g, err := onlyFactorySAGrant(ctx, facA, a.SuperAdminID)
		if err != nil {
			t.Fatal(err)
		}
		if err := facA.RevokeRole(ctx, saA, g); !errors.Is(err, domain.ErrLastAdmin) {
			t.Fatalf("revoke: %v", err)
		}
	})
	run("10.9", func(t *testing.T) {
		if err := facA.EnableAccount(ctx, saA, q.acc.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := facA.Login(ctx, "q", "q-pass"); err != nil {
			t.Fatal(err)
		}
	})
	run("10.10", func(t *testing.T) {
		lost, err := facA.CreatePerson(ctx, saA, "lost", "忘密码")
		if err != nil {
			t.Fatal(err)
		}
		mustAdoptPassword(t, ctx, facA, "lost", "lost-pass")
		if _, err := facA.ResetPassword(ctx, saA, a.SuperAdminID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("self: %v", err)
		}
		out, err := facA.ResetPassword(ctx, saA, lost.ID)
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != factory.StatusActive {
			t.Fatalf("status %s", out.Status)
		}
		if _, err := facA.Login(ctx, "lost", "lost-pass"); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("old pass: %v", err)
		}
		if _, err := facA.Login(ctx, "lost", personPass("lost")); err != nil {
			t.Fatalf("default pass: %v", err)
		}
	})

	run("17.2", func(t *testing.T) {
		if _, err := facA.Login(ctx, "ghost", "bad"); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatal(err)
		}
		rows, err := facA.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		row, ok := audit.LastLoginDeny(rows)
		if !ok || row.ActorID != nil || row.ClaimedLogin == nil || *row.ClaimedLogin != "ghost" {
			t.Fatalf("claimed %+v", row)
		}
	})
	run("17.1", func(t *testing.T) {
		aRows, err := facA.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		bRows, err := facB.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		all := append(aRows, bRows...)
		if bad := audit.Incomplete(all); len(bad) > 0 {
			t.Fatalf("incomplete audit %#v", bad[0])
		}
		dump := audit.Dump(all)
		if audit.ContainsAny(dump, "sa-pass", "sa-pass-2", "p-pass", "q-pass", "lost-pass", personPass("lost"), payload, a.ActivationToken, saA) {
			t.Fatalf("secret leaked")
		}
	})
}

type namedAcc struct {
	acc   factory.Account
	tok   string
	grant uuid.UUID
}

func mustLogin(t *testing.T, ctx context.Context, fac *factory.Service, login, pass string) string {
	t.Helper()
	tok, err := fac.Login(ctx, login, pass)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func mustCreateRole(t *testing.T, ctx context.Context, fac *factory.Service, saTok, login, pass, role, scope string, unit *uuid.UUID) namedAcc {
	t.Helper()
	acc, err := fac.CreatePerson(ctx, saTok, login, login)
	if err != nil {
		t.Fatal(err)
	}
	g, err := fac.GrantRole(ctx, saTok, acc.ID, role, scope, unit)
	if err != nil {
		t.Fatal(err)
	}
	return namedAcc{acc: acc, tok: mustAdoptPassword(t, ctx, fac, login, pass), grant: g.ID}
}
