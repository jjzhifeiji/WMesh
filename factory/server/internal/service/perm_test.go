// 第 5 圈：六种角色、作用域、默认拒绝、最后管理员保护。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

func TestPermCircle(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	a, facA, err := h.Provision(ctx, "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	b, facB, err := h.Provision(ctx, "sa-b", "超管B")
	if err != nil {
		t.Fatal(err)
	}
	if err := facA.Activate(ctx, "sa-a", a.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	if err := facB.Activate(ctx, "sa-b", b.ActivationToken, "sb-pass"); err != nil {
		t.Fatal(err)
	}
	saA, err := facA.Login(ctx, "sa-a", "sa-pass")
	if err != nil {
		t.Fatal(err)
	}
	saB, err := facB.Login(ctx, "sa-b", "sb-pass")
	if err != nil {
		t.Fatal(err)
	}

	site, err := facA.CreateOrgUnit(ctx, saA, "场地", nil)
	if err != nil {
		t.Fatal(err)
	}
	shopA, err := facA.CreateOrgUnit(ctx, saA, "车间A", &site.ID)
	if err != nil {
		t.Fatal(err)
	}
	shopB, err := facA.CreateOrgUnit(ctx, saA, "车间B", &site.ID)
	if err != nil {
		t.Fatal(err)
	}
	line, err := facA.CreateOrgUnit(ctx, saA, "产线", &shopA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := facA.CreateOrgUnit(ctx, saA, "班组", &line.ID); err != nil {
		t.Fatalf("5.1: %v", err)
	}

	p, err := facA.CreatePerson(ctx, saA, "p", "无角色")
	if err != nil {
		t.Fatalf("4.1 person: %v", err)
	}
	pTok := mustAdoptPassword(t, ctx, facA, "p", "p-pass")
	if err := facA.Assign(ctx, saA, p.ID, shopA.ID); err != nil {
		t.Fatalf("6.2: %v", err)
	}
	if err := facA.Assign(ctx, saA, p.ID, shopB.ID); !errors.Is(err, domain.ErrDuplicateAssignment) {
		t.Fatalf("6.3: %v", err)
	}
	if err := facA.Unassign(ctx, saA, p.ID, shopA.ID); err != nil {
		t.Fatal(err)
	}
	if err := facA.Assign(ctx, saA, p.ID, shopB.ID); err != nil {
		t.Fatal(err)
	}
	if err := facA.Operate(ctx, pTok, shopA.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("7.2: %v", err)
	}

	oa, err := facA.CreatePerson(ctx, saA, "oa", "组织管理员")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := facA.GrantRole(ctx, saA, oa.ID, factory.RoleOrgAdmin, factory.ScopeOrgUnit, &shopA.ID); err != nil {
		t.Fatalf("4.1 grant: %v", err)
	}
	oaTok := mustAdoptPassword(t, ctx, facA, "oa", "oa-pass")
	if _, err := facA.CreateOrgUnit(ctx, oaTok, "线2", &shopA.ID); err != nil {
		t.Fatalf("8.1/9.5: %v", err)
	}
	if _, err := facA.CreateOrgUnit(ctx, oaTok, "越界", &shopB.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("9.1 sibling: %v", err)
	}
	if _, err := facA.CreateOrgUnit(ctx, oaTok, "根", nil); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("9.1 parent/root: %v", err)
	}
	if err := facA.ReparentOrgUnit(ctx, oaTok, shopA.ID, &shopB.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("9.1 reparent out: %v", err)
	}
	if _, err := facA.GrantRole(ctx, oaTok, p.ID, factory.RoleFactorySuperAdmin, factory.ScopeFactory, nil); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("9.6 oa grant sa: %v", err)
	}
	if _, err := facA.GrantRole(ctx, oaTok, p.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shopB.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("9.6 outside tree: %v", err)
	}

	lead, err := facA.CreatePerson(ctx, saA, "lead", "负责人")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := facA.GrantRole(ctx, saA, lead.ID, factory.RoleOrgLead, factory.ScopeOrgUnit, &shopA.ID); err != nil {
		t.Fatal(err)
	}
	leadTok := mustAdoptPassword(t, ctx, facA, "lead", "lead-pass")
	if err := facA.ViewOrg(ctx, leadTok, shopA.ID); err != nil {
		t.Fatalf("8.2 lead: %v", err)
	}
	if _, err := facA.CreateOrgUnit(ctx, leadTok, "x", &shopA.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("9.2 lead org: %v", err)
	}
	if _, err := facA.CreatePerson(ctx, leadTok, "x", "x"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("9.2 lead account: %v", err)
	}

	op, err := facA.CreatePerson(ctx, saA, "op", "操作员")
	if err != nil {
		t.Fatal(err)
	}
	opGrant, err := facA.GrantRole(ctx, saA, op.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shopA.ID)
	if err != nil {
		t.Fatal(err)
	}
	opTok := mustAdoptPassword(t, ctx, facA, "op", "op-pass")
	if err := facA.Operate(ctx, opTok, shopA.ID); err != nil {
		t.Fatalf("operator operate: %v", err)
	}
	if _, err := facA.CreatePerson(ctx, opTok, "y", "y"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("9.3 create: %v", err)
	}
	if err := facA.DisableAccount(ctx, opTok, p.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("9.3 disable: %v", err)
	}
	if _, err := facA.ResetPassword(ctx, opTok, p.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("9.3 reset: %v", err)
	}
	if _, err := facA.GrantRole(ctx, opTok, p.ID, factory.RoleOperator, factory.ScopeOrgUnit, &shopA.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("9.3 grant: %v", err)
	}

	aud, err := facA.CreatePerson(ctx, saA, "aud", "审计员")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := facA.GrantRole(ctx, saA, aud.ID, factory.RoleAuditor, factory.ScopeOrgUnit, &shopA.ID); err != nil {
		t.Fatal(err)
	}
	audTok := mustAdoptPassword(t, ctx, facA, "aud", "aud-pass")
	if err := facA.ViewOrg(ctx, audTok, shopA.ID); err != nil {
		t.Fatalf("8.2 aud: %v", err)
	}
	if err := facA.Operate(ctx, audTok, shopA.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("9.4 operate: %v", err)
	}
	if _, err := facA.CreateOrgUnit(ctx, audTok, "z", &shopA.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("9.4 org: %v", err)
	}

	if err := facA.ReparentOrgUnit(ctx, saA, shopA.ID, &shopA.ID); !errors.Is(err, domain.ErrCycle) {
		t.Fatalf("5.4 self: %v", err)
	}
	if err := facA.ReparentOrgUnit(ctx, saA, site.ID, &line.ID); !errors.Is(err, domain.ErrCycle) {
		t.Fatalf("5.4 desc: %v", err)
	}
	if err := facA.AddParent(ctx, saA, shopA.ID, shopB.ID); !errors.Is(err, domain.ErrMultiParent) {
		t.Fatalf("5.3: %v", err)
	}
	if err := facA.ReparentOrgUnit(ctx, saA, shopA.ID, ptr(uuid.MustParse("00000000-0000-0000-0000-0000000000bb"))); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("5.2: %v", err)
	}
	if err := facA.Assign(ctx, saA, p.ID, uuid.MustParse("00000000-0000-0000-0000-0000000000bb")); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("6.4: %v", err)
	}

	same, err := facB.CreatePerson(ctx, saB, "p", "厂B同名")
	if err != nil {
		t.Fatalf("6.6: %v", err)
	}
	if same.ID == p.ID {
		t.Fatalf("6.6 same id")
	}
	mustAdoptPassword(t, ctx, facB, "p", "pb-pass")

	if err := facA.RevokeRole(ctx, saA, opGrant.ID); err != nil {
		t.Fatal(err)
	}
	if err := facA.Operate(ctx, opTok, shopA.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("10.4 revoke: %v", err)
	}

	sa2, err := facA.CreatePerson(ctx, saA, "sa2", "第二超管")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := facA.GrantRole(ctx, saA, sa2.ID, factory.RoleFactorySuperAdmin, factory.ScopeFactory, nil); err != nil {
		t.Fatal(err)
	}
	mustAdoptPassword(t, ctx, facA, "sa2", "sa2-pass")
	if err := facA.DisableAccount(ctx, saA, sa2.ID); err != nil {
		t.Fatalf("10.5 disable extra sa: %v", err)
	}
	sa3, err := facA.CreatePerson(ctx, saA, "sa3", "第三超管")
	if err != nil {
		t.Fatal(err)
	}
	g3, err := facA.GrantRole(ctx, saA, sa3.ID, factory.RoleFactorySuperAdmin, factory.ScopeFactory, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustAdoptPassword(t, ctx, facA, "sa3", "sa3-pass")
	if err := facA.RevokeRole(ctx, saA, g3.ID); err != nil {
		t.Fatalf("10.5 revoke extra sa: %v", err)
	}
	if err := facA.DisableAccount(ctx, saA, a.SuperAdminID); !errors.Is(err, domain.ErrLastAdmin) {
		t.Fatalf("10.6 disable last: %v", err)
	}
	saGrant, err := onlyFactorySAGrant(ctx, facA, a.SuperAdminID)
	if err != nil {
		t.Fatal(err)
	}
	if err := facA.RevokeRole(ctx, saA, saGrant); !errors.Is(err, domain.ErrLastAdmin) {
		t.Fatalf("10.6 revoke last: %v", err)
	}

	oldID := p.ID
	if err := facA.Rename(ctx, pTok, "改名", "p"); err != nil {
		t.Fatalf("4.2: %v", err)
	}
	got, err := facA.RequireActive(ctx, pTok)
	if err != nil || got.ID != oldID {
		t.Fatalf("4.2 id drifted: %v %+v", err, got)
	}
}

func ptr(id uuid.UUID) *uuid.UUID { return &id }

func onlyFactorySAGrant(ctx context.Context, fac *factory.Service, personID uuid.UUID) (uuid.UUID, error) {
	grants, err := fac.Store().ActiveGrants(ctx, personID)
	if err != nil {
		return uuid.Nil, err
	}
	for _, g := range grants {
		if g.Role == factory.RoleFactorySuperAdmin {
			return g.ID, nil
		}
	}
	return uuid.Nil, domain.ErrNotFound
}
