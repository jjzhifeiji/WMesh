// 第 3 圈：厂库模型约束（无环、唯一登录名、角色作用域、停用保护、禁止物理删）。
package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/store"
)

func TestFactoryConstraints(t *testing.T) {
	ctx := context.Background()
	facDB, facID := testpg.Fresh(t)
	s := store.Open(facDB, facID)

	sa, err := s.CreatePerson(ctx, "sa", "初始超管", true)
	if err != nil {
		t.Fatalf("sa: %v", err)
	}
	if sa.Status != store.StatusPending {
		t.Fatalf("sa status %s", sa.Status)
	}
	if _, err := s.CreatePerson(ctx, "sa2", "第二初始", true); err != domain.ErrInitialSAExists {
		t.Fatalf("second initial: %v", err)
	}
	if _, err := s.CreatePerson(ctx, "sa", "重名", false); err != domain.ErrLoginNameTaken {
		t.Fatalf("dup login: %v", err)
	}

	p, err := s.CreatePerson(ctx, "op", "操作员", false)
	if err != nil {
		t.Fatalf("person: %v", err)
	}
	oldID := p.ID
	if err := s.RenamePerson(ctx, p.ID, "改名", "op-new"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if p.ID != oldID {
		t.Fatalf("id changed")
	}

	typ, err := s.CreateOrgType(ctx, "场地")
	if err != nil {
		t.Fatalf("type: %v", err)
	}
	site, err := s.CreateOrgUnit(ctx, typ.ID, "一号场地", nil)
	if err != nil {
		t.Fatalf("site: %v", err)
	}
	shop, err := s.CreateOrgUnit(ctx, typ.ID, "车间", &site.ID)
	if err != nil {
		t.Fatalf("shop: %v", err)
	}
	line, err := s.CreateOrgUnit(ctx, typ.ID, "产线", &shop.ID)
	if err != nil {
		t.Fatalf("line: %v", err)
	}
	team, err := s.CreateOrgUnit(ctx, typ.ID, "班组", &line.ID)
	if err != nil {
		t.Fatalf("team: %v", err)
	}

	if err := s.ReparentOrgUnit(ctx, site.ID, &site.ID); err != domain.ErrCycle {
		t.Fatalf("self parent: %v", err)
	}
	if err := s.ReparentOrgUnit(ctx, site.ID, &team.ID); err != domain.ErrCycle {
		t.Fatalf("descendant parent: %v", err)
	}
	if err := s.ReparentOrgUnit(ctx, team.ID, &shop.ID); err != nil {
		t.Fatalf("reparent ok: %v", err)
	}
	if err := s.RenameOrgUnit(ctx, team.ID, "班组改名"); err != nil {
		t.Fatalf("rename unit: %v", err)
	}

	if _, err := s.Assign(ctx, p.ID, site.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if _, err := s.Assign(ctx, p.ID, shop.ID); err != nil {
		t.Fatalf("second assign: %v", err)
	}
	if _, err := s.Assign(ctx, p.ID, site.ID); err != domain.ErrDuplicateAssignment {
		t.Fatalf("dup assign: %v", err)
	}
	if err := s.Unassign(ctx, p.ID, site.ID); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if _, err := s.Assign(ctx, p.ID, site.ID); err != nil {
		t.Fatalf("reassign after end: %v", err)
	}

	if _, err := s.GrantRole(ctx, sa.ID, store.RoleFactorySuperAdmin, store.ScopeFactory, nil); err != nil {
		t.Fatalf("sa role: %v", err)
	}
	if _, err := s.GrantRole(ctx, sa.ID, store.RoleFactorySuperAdmin, store.ScopeOrgUnit, &site.ID); err != domain.ErrInvalidRoleScope {
		t.Fatalf("sa org scope: %v", err)
	}
	if _, err := s.GrantRole(ctx, p.ID, store.RoleOrgAdmin, store.ScopeFactory, nil); err != domain.ErrInvalidRoleScope {
		t.Fatalf("org admin factory scope: %v", err)
	}
	grant, err := s.GrantRole(ctx, p.ID, store.RoleOperator, store.ScopeOrgUnit, &shop.ID)
	if err != nil {
		t.Fatalf("operator: %v", err)
	}
	if _, err := s.GrantRole(ctx, p.ID, store.RoleOperator, store.ScopeOrgUnit, &shop.ID); err != domain.ErrDuplicateRoleGrant {
		t.Fatalf("dup grant: %v", err)
	}
	if err := s.RevokeRole(ctx, grant.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if err := s.DisableOrgType(ctx, typ.ID); err != domain.ErrHasActiveUnits {
		t.Fatalf("disable type with units: %v", err)
	}
	if err := s.DisableOrgUnit(ctx, shop.ID); err != domain.ErrHasActiveChildren {
		t.Fatalf("disable with children: %v", err)
	}
	if err := s.DisableOrgUnit(ctx, team.ID); err != nil {
		t.Fatalf("disable leaf: %v", err)
	}
	if _, err := s.Assign(ctx, p.ID, team.ID); err != domain.ErrDisabledOrgUnit {
		t.Fatalf("assign disabled: %v", err)
	}
	if _, err := s.CreateOrgUnit(ctx, typ.ID, "新班组", &team.ID); err != domain.ErrDisabledOrgUnit {
		t.Fatalf("child of disabled: %v", err)
	}

	if _, err := s.CreateSession(ctx, p.ID, "sess", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("session: %v", err)
	}
	if _, err := s.CreateSession(ctx, p.ID, "sess", time.Now().Add(time.Hour)); err != domain.ErrDuplicateSession {
		t.Fatalf("dup session: %v", err)
	}

	if err := s.AppendAudit(ctx, audit.Event{Action: "assign", Target: site.ID.String(), Result: audit.Allow, ActorID: &sa.ID}); err != nil {
		t.Fatalf("audit: %v", err)
	}

	if err := facDB.Exec("DELETE FROM people WHERE id = ?", sa.ID).Error; !domain.IsForeignKeyViolation(err) {
		t.Fatalf("expected fk when deleting referenced person, got %v", err)
	}
}

func TestLoginNameUniquePerFactoryDB(t *testing.T) {
	ctx := context.Background()
	admin := testpg.Open(t)
	_, dsnA := testpg.CreateDB(t, admin, "wmesh_fac")
	_, dsnB := testpg.CreateDB(t, admin, "wmesh_fac")
	a := store.Open(testpg.OpenMigrated(t, dsnA), uuid.MustParse("00000000-0000-0000-0000-00000000000a"))
	b := store.Open(testpg.OpenMigrated(t, dsnB), uuid.MustParse("00000000-0000-0000-0000-00000000000b"))
	if _, err := a.CreatePerson(ctx, "same", "甲", false); err != nil {
		t.Fatalf("a: %v", err)
	}
	if _, err := b.CreatePerson(ctx, "same", "乙", false); err != nil {
		t.Fatalf("b: %v", err)
	}
}

func TestPhysicalDeleteOrgUnitBlockedWhenAssigned(t *testing.T) {
	ctx := context.Background()
	facDB, facID := testpg.Fresh(t)
	s := store.Open(facDB, facID)
	p, err := s.CreatePerson(ctx, "p", "人", false)
	if err != nil {
		t.Fatal(err)
	}
	typ, err := s.CreateOrgType(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateOrgUnit(ctx, typ.ID, "u", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Assign(ctx, p.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := facDB.Exec("DELETE FROM org_units WHERE id = ?", u.ID).Error; !domain.IsForeignKeyViolation(err) {
		t.Fatalf("expected fk, got %v", err)
	}
}
