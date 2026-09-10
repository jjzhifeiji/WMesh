// 第 3 圈：WAN 库约束（单管理员、一厂一名初始超管、不见厂内表）。
package store_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
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

	for _, table := range []string{"people", "org_types", "org_units", "assignments", "role_grants", "person_offline_grants", "signing_keys"} {
		ok, err := s.HasTable(ctx, table)
		if err != nil {
			t.Fatalf("has %s: %v", table, err)
		}
		if ok {
			t.Fatalf("wan must not have table %s", table)
		}
	}
}

func TestWANClientBinding(t *testing.T) {
	ctx := context.Background()
	s := store.Open(testpg.Fresh(t))

	if _, err := s.CreateAdmin(ctx, "wan", "hash"); err != nil {
		t.Fatal(err)
	}
	a, err := s.RegisterFactory(ctx, id.New(), "厂A", id.New(), "sa-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.RegisterFactory(ctx, id.New(), "厂B", id.New(), "sa-b")
	if err != nil {
		t.Fatal(err)
	}

	pubA, _ := mustEd25519(t)
	if err := s.PutFactoryPublicKey(ctx, a.ID, pubA); err != nil {
		t.Fatalf("factory key: %v", err)
	}
	if err := s.PutFactoryPublicKey(ctx, a.ID, pubA); err != domain.ErrFactoryKeyExists {
		t.Fatalf("dup factory key: %v", err)
	}
	if err := s.PutFactoryPublicKey(ctx, a.ID, []byte("short")); err != domain.ErrInvalidKey {
		t.Fatalf("short factory key: %v", err)
	}

	cPub, _ := mustEd25519(t)
	c, err := s.CreateClient(ctx, id.New(), cPub)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if c.FactoryID != nil || c.BindingRevision != 0 {
		t.Fatalf("unbound: %+v", c)
	}
	if _, err := s.CreateClient(ctx, id.New(), cPub); err != domain.ErrClientKeyTaken {
		t.Fatalf("dup pubkey: %v", err)
	}

	bound, err := s.BindClient(ctx, c.ID, a.ID)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if bound.FactoryID == nil || *bound.FactoryID != a.ID || bound.BindingRevision != 1 {
		t.Fatalf("bound: %+v", bound)
	}
	if _, err := s.BindClient(ctx, c.ID, b.ID); err != domain.ErrClientBound {
		t.Fatalf("second bind: %v", err)
	}

	reb, err := s.RebindClient(ctx, c.ID, b.ID)
	if err != nil {
		t.Fatalf("rebind: %v", err)
	}
	if reb.FactoryID == nil || *reb.FactoryID != b.ID || reb.BindingRevision != 2 {
		t.Fatalf("rebind: %+v", reb)
	}
	if _, err := s.RebindClient(ctx, c.ID, b.ID); err != domain.ErrClientBound {
		t.Fatalf("rebind same: %v", err)
	}

	unboundPub, _ := mustEd25519(t)
	u, err := s.CreateClient(ctx, id.New(), unboundPub)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RebindClient(ctx, u.ID, a.ID); err != domain.ErrUnbound {
		t.Fatalf("rebind unbound: %v", err)
	}

	ok, err := s.HasColumn(ctx, "clients", "private_key")
	if err != nil || ok {
		t.Fatalf("wan clients must not have private_key: ok=%v err=%v", ok, err)
	}

	admin, err := s.AdminByLogin(ctx, "wan")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendAudit(ctx, audit.Event{
		Action: "bind_client", Target: c.ID.String(), Result: audit.Allow,
		ActorID: &admin.ID, FactoryID: &b.ID, TimeSource: audit.Local,
	}); err != nil {
		t.Fatalf("local audit: %v", err)
	}
}

func mustEd25519(t *testing.T) ([]byte, []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}
