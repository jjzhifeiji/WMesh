// 第 3 圈：WAN 库约束（单管理员、一厂一名初始超管、不见厂内表）。
package store_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/contentcrypt"
	"wmesh/global/internal/platform/digest"
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

	fac, err := s.RegisterFactory(ctx, id.New(), "厂A", id.New(), "sa-a", "超管A", "enroll-a")
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if err := s.BindInitialSuperAdmin(ctx, fac.ID, id.New(), "sa-a-2", "超管2"); err != domain.ErrInitialSAExists {
		t.Fatalf("second initial sa: %v", err)
	}
	// 同一身份重复注册会整体回滚，名录里不会多出一行。
	if _, err := s.RegisterFactory(ctx, fac.ID, "厂A重复", id.New(), "sa-a-3", "超管3", "enroll-dup"); err == nil {
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
	a, err := s.RegisterFactory(ctx, id.New(), "厂A", id.New(), "sa-a", "超管A", "enroll-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.RegisterFactory(ctx, id.New(), "厂B", id.New(), "sa-b", "超管B", "enroll-b")
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
	c, err := s.CreateClient(ctx, id.New(), "焊机-A", cPub)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if c.Name != "焊机-A" || c.FactoryID != nil || c.BindingRevision != 0 {
		t.Fatalf("unbound: %+v", c)
	}
	pending, err := s.CreateClient(ctx, id.New(), "待上线", nil)
	if err != nil || pending.Name != "待上线" || len(pending.PublicKey) != 0 {
		t.Fatalf("no pubkey: %+v %v", pending, err)
	}
	if _, err := s.CreateClient(ctx, id.New(), "  ", nil); err != domain.ErrInvalidName {
		t.Fatalf("empty name: %v", err)
	}
	if _, err := s.CreateClient(ctx, id.New(), "焊机-A2", cPub); err != domain.ErrClientKeyTaken {
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
	u, err := s.CreateClient(ctx, id.New(), "焊机-U", unboundPub)
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

func TestWANPlatformAssets(t *testing.T) {
	ctx := context.Background()
	db := testpg.Fresh(t)
	s := store.Open(db)
	admin, err := s.CreateAdmin(ctx, "wan", "hash")
	if err != nil {
		t.Fatal(err)
	}
	fac, err := s.RegisterFactory(ctx, id.New(), "厂A", id.New(), "sa-a", "超管A", "enroll-a")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"voltage":40}`)
	sum := digest.Sum(body)
	a, err := s.InsertAsset(ctx, store.Asset{
		Kind: store.KindProcess, Name: "平台工艺", Status: store.AssetDraft,
		Copyable: true, Content: body, Digest: sum, CreatorID: admin.ID,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if a.Level != store.AssetLevelPlatform || !a.Copyable || a.Revision != 1 || contentcrypt.IsEnvelope(a.Content) {
		t.Fatalf("platform copyable: %+v", a)
	}
	src := id.New()
	rev := int64(3)
	promoted, err := s.InsertAsset(ctx, store.Asset{
		Kind: store.KindProcess, Name: "升档来的", Status: store.AssetAvailable,
		Content: body, Digest: sum, CreatorID: admin.ID,
		SourceID: &src, SourceRevision: &rev, SourceFactoryID: &fac.ID,
	})
	if err != nil || promoted.SourceID == nil || *promoted.SourceID != src || promoted.Copyable {
		t.Fatalf("promoted: %+v %v", promoted, err)
	}
	if _, err := s.UpdateAsset(ctx, a.ID, 1, store.AssetWrite{
		Name: "平台工艺-2", Content: body, Digest: sum, Status: store.AssetAvailable,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := s.UpdateAsset(ctx, a.ID, 1, store.AssetWrite{
		Name: "旧", Content: body, Digest: sum, Status: store.AssetDraft,
	}); err != domain.ErrRevisionConflict {
		t.Fatalf("conflict: %v", err)
	}
	if err := db.Exec("UPDATE assets SET content = ? WHERE id = ?", []byte("dirty"), a.ID).Error; err != nil {
		t.Fatal(err)
	}
	dirty, err := s.AssetByID(ctx, a.ID)
	if err != nil || digest.Match(dirty.Content, dirty.Digest) {
		t.Fatalf("tamper: match=%v err=%v", digest.Match(dirty.Content, dirty.Digest), err)
	}
	if err := s.DeleteAsset(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(
		`INSERT INTO assets (id, kind, level, name, code, status, copyable, revision, content, digest, creator_id, deps)
		 VALUES (?, 'process', 'platform', '可复制草稿', 'GY-W-000099', 'draft', true, 1, decode('00','hex'), ?, ?, '[]'::jsonb)`,
		id.New(), sum, admin.ID,
	).Error; err != nil {
		t.Fatalf("copyable true: %v", err)
	}
	ok, err := s.HasTable(ctx, "people")
	if err != nil || ok {
		t.Fatalf("wan must not have people: %v %v", ok, err)
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
