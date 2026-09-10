// 第 3 圈：WAN 库约束（单管理员、一厂一名初始超管、不见厂内表）。
package store_test

import (
	"context"
	"testing"
	"time"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/store"
)

func TestWANConstraints(t *testing.T) {
	ctx := context.Background()
	s := store.Open(testpg.Fresh(t))

	if n, err := s.AdminCount(ctx); err != nil || n != 0 {
		t.Fatalf("empty wan admins: n=%d err=%v", n, err)
	}
	admin, err := s.CreateAdmin(ctx, "wan", "hash-1")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if _, err := s.CreateAdmin(ctx, "other", "hash-2"); err != domain.ErrWANAdminExists {
		t.Fatalf("second admin: %v", err)
	}
	if n, err := s.AdminCount(ctx); err != nil || n != 1 {
		t.Fatalf("singleton admin: n=%d err=%v", n, err)
	}

	fac, err := s.RegisterFactory(ctx, id.New(), "厂A", id.New(), "sa-a")
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if err := s.BindInitialSuperAdmin(ctx, fac.ID, id.New(), "sa-a-2"); err != domain.ErrInitialSAExists {
		t.Fatalf("second initial sa: %v", err)
	}
	// 同一身份重复注册会整体回滚，名录里不会多出一行。
	if _, err := s.RegisterFactory(ctx, fac.ID, "厂A重复", id.New(), "sa-a-3"); err == nil {
		t.Fatalf("duplicate factory id must fail")
	}
	if facs, err := s.ListFactories(ctx); err != nil || len(facs) != 1 {
		t.Fatalf("factories after rollback: n=%d err=%v", len(facs), err)
	}

	if _, err := s.CreateSession(ctx, admin.ID, "tok-hash", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("session: %v", err)
	}
	if _, err := s.CreateSession(ctx, admin.ID, "tok-hash", time.Now().Add(time.Hour)); err != domain.ErrDuplicateSession {
		t.Fatalf("dup session: %v", err)
	}

	if err := s.AppendAudit(ctx, audit.Event{Action: "create_factory", Target: fac.ID.String(), Result: audit.Allow, ActorID: &admin.ID, FactoryID: &fac.ID}); err != nil {
		t.Fatalf("audit: %v", err)
	}

	for _, table := range []string{"people", "org_types", "org_units", "assignments", "role_grants"} {
		ok, err := s.HasTable(ctx, table)
		if err != nil {
			t.Fatalf("has %s: %v", table, err)
		}
		if ok {
			t.Fatalf("wan must not have table %s", table)
		}
	}
}
