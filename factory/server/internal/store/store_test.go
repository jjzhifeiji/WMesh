// 第 3 圈：厂库模型约束（无环、唯一登录名、角色作用域、停用保护、禁止物理删）。
package store_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
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

	site, err := s.CreateOrgUnit(ctx, "一号场地", nil)
	if err != nil {
		t.Fatalf("site: %v", err)
	}
	shop, err := s.CreateOrgUnit(ctx, "车间", &site.ID)
	if err != nil {
		t.Fatalf("shop: %v", err)
	}
	line, err := s.CreateOrgUnit(ctx, "产线", &shop.ID)
	if err != nil {
		t.Fatalf("line: %v", err)
	}
	team, err := s.CreateOrgUnit(ctx, "班组", &line.ID)
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
	if _, err := s.Assign(ctx, p.ID, shop.ID); err != domain.ErrDuplicateAssignment {
		t.Fatalf("second unit: %v", err)
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

	if err := s.DisableOrgUnit(ctx, shop.ID); err != domain.ErrHasActiveChildren {
		t.Fatalf("disable with children: %v", err)
	}
	if err := s.DisableOrgUnit(ctx, team.ID); err != nil {
		t.Fatalf("disable leaf: %v", err)
	}
	if _, err := s.Assign(ctx, p.ID, team.ID); err != domain.ErrDisabledOrgUnit {
		t.Fatalf("assign disabled: %v", err)
	}
	if _, err := s.CreateOrgUnit(ctx, "新班组", &team.ID); err != domain.ErrDisabledOrgUnit {
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
	u, err := s.CreateOrgUnit(ctx, "u", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Assign(ctx, p.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := facDB.Exec("DELETE FROM org_units WHERE id = ?", u.ID).Error; !domain.IsForeignKeyViolation(err) {
		t.Fatalf("expected fk, got %v", err)
	}
	if err := s.DeleteOrgUnit(ctx, u.ID); err != domain.ErrReferenced {
		t.Fatalf("delete assigned: %v", err)
	}
}

func TestDeleteOrgWhenUnreferenced(t *testing.T) {
	ctx := context.Background()
	facDB, facID := testpg.Fresh(t)
	s := store.Open(facDB, facID)
	u, err := s.CreateOrgUnit(ctx, "u", nil)
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateOrgUnit(ctx, "child", &u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteOrgUnit(ctx, u.ID); err != domain.ErrReferenced {
		t.Fatalf("delete parent: %v", err)
	}
	if err := s.DeleteOrgUnit(ctx, child.ID); err != nil {
		t.Fatalf("delete unused child: %v", err)
	}
	if err := s.DeleteOrgUnit(ctx, u.ID); err != nil {
		t.Fatalf("delete emptied parent: %v", err)
	}
}

func TestDeleteOrgAfterUnassignAndRevoke(t *testing.T) {
	ctx := context.Background()
	facDB, facID := testpg.Fresh(t)
	s := store.Open(facDB, facID)
	p, err := s.CreatePerson(ctx, "p", "人", false)
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateOrgUnit(ctx, "u", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Assign(ctx, p.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	g, err := s.GrantRole(ctx, p.ID, store.RoleOperator, store.ScopeOrgUnit, &u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteOrgUnit(ctx, u.ID); err != domain.ErrReferenced {
		t.Fatalf("active refs: %v", err)
	}
	if err := s.Unassign(ctx, p.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeRole(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteOrgUnit(ctx, u.ID); err != nil {
		t.Fatalf("after end/revoke: %v", err)
	}
}

func TestEnableOrgAfterDisable(t *testing.T) {
	ctx := context.Background()
	facDB, facID := testpg.Fresh(t)
	s := store.Open(facDB, facID)
	u, err := s.CreateOrgUnit(ctx, "u", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DisableOrgUnit(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrgUnit(ctx, "child", &u.ID); err != domain.ErrDisabledOrgUnit {
		t.Fatalf("create under disabled: %v", err)
	}
	if err := s.EnableOrgUnit(ctx, u.ID); err != nil {
		t.Fatalf("enable unit: %v", err)
	}
	if _, err := s.CreateOrgUnit(ctx, "child", &u.ID); err != nil {
		t.Fatalf("create after enable: %v", err)
	}
}

func TestClientBindingAndGrants(t *testing.T) {
	ctx := context.Background()
	facDB, facID := testpg.Fresh(t)
	s := store.Open(facDB, facID)

	pub, priv := mustEd25519(t)
	if _, err := s.PutSigningKey(ctx, pub, priv); err != nil {
		t.Fatalf("signing key: %v", err)
	}
	if _, err := s.PutSigningKey(ctx, pub, priv); err != domain.ErrSigningKeyExists {
		t.Fatalf("dup signing key: %v", err)
	}

	cPub, _ := mustEd25519(t)
	cid := id.New()
	c, err := s.AcceptBinding(ctx, cid, "焊机-1", cPub, 1)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if c.Status != store.ClientStatusBound || c.BindingRevision != 1 || c.Name != "焊机-1" {
		t.Fatalf("client: %+v", c)
	}
	who, err := s.CreatePerson(ctx, "op-a", "操作员A", false)
	if err != nil {
		t.Fatalf("person: %v", err)
	}
	if err := s.SetClientOperator(ctx, cid, who.ID); err != nil {
		t.Fatalf("set operator: %v", err)
	}
	listed, err := s.ListClients(ctx)
	if err != nil || len(listed) != 1 || listed[0].OperatorID == nil || *listed[0].OperatorID != who.ID {
		t.Fatalf("list operator: %+v %v", listed, err)
	}
	ren, err := s.RenameClient(ctx, cid, "一线焊机")
	if err != nil || ren.Name != "一线焊机" {
		t.Fatalf("rename: %+v %v", ren, err)
	}
	if _, err := s.AcceptBinding(ctx, cid, "焊机-1", cPub, 1); err != nil {
		t.Fatalf("stale bind: %v", err)
	}
	other, _ := mustEd25519(t)
	if _, err := s.AcceptBinding(ctx, cid, "焊机-1", other, 2); err != domain.ErrClientKeyMismatch {
		t.Fatalf("key mismatch: %v", err)
	}

	now := time.Now().UTC()
	later := now.Add(24 * time.Hour)
	sig := make([]byte, 64)
	g, err := s.InsertRuntimeGrant(ctx, store.RuntimeGrant{
		ClientID: cid, Revision: 1, CanRun: true,
		NotBefore: now, NotAfter: later, Payload: []byte("run-1"), Signature: sig,
	})
	if err != nil {
		t.Fatalf("runtime grant: %v", err)
	}
	if _, err := s.InsertRuntimeGrant(ctx, store.RuntimeGrant{
		ClientID: cid, Revision: 1, CanRun: false,
		NotBefore: now, NotAfter: later, Payload: []byte("run-1b"), Signature: sig,
	}); err != domain.ErrStaleRevision {
		t.Fatalf("stale runtime: %v", err)
	}
	got, err := s.LatestRuntimeGrant(ctx, cid)
	if err != nil || got.ID != g.ID || !got.CanRun {
		t.Fatalf("latest runtime: %+v %v", got, err)
	}

	if err := s.VoidBinding(ctx, cid); err != nil {
		t.Fatal(err)
	}
	voided, err := s.ClientByID(ctx, cid)
	if err != nil || voided.OperatorID != nil {
		t.Fatalf("void clears operator: %+v %v", voided, err)
	}
	if _, err := s.InsertRuntimeGrant(ctx, store.RuntimeGrant{
		ClientID: cid, Revision: 2, CanRun: false,
		NotBefore: now, NotAfter: later, Payload: []byte("run-2"), Signature: sig,
	}); err != domain.ErrBindingVoid {
		t.Fatalf("void runtime: %v", err)
	}
	reb, err := s.AcceptBinding(ctx, cid, "焊机-1", cPub, 3)
	if err != nil || reb.Status != store.ClientStatusBound || reb.BindingRevision != 3 {
		t.Fatalf("re-accept: %+v %v", reb, err)
	}

	var hasPriv bool
	if err := facDB.Raw("SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='clients' AND column_name='private_key')").Scan(&hasPriv).Error; err != nil || hasPriv {
		t.Fatalf("clients must not have private_key: %v %v", hasPriv, err)
	}
	if err := s.AppendAudit(ctx, audit.Event{Action: "issue", Target: cid.String(), Result: audit.Allow, TimeSource: audit.Local}); err != nil {
		t.Fatalf("local audit: %v", err)
	}
}

func TestFactoryAssets(t *testing.T) {
	ctx := context.Background()
	facDB, facID := testpg.Fresh(t)
	s := store.Open(facDB, facID)
	// 夹具自签租约，才能把正文封进厂库。
	if err := s.GrantLocalLease(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreatePerson(ctx, "pe", "工艺师", false)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := s.CreateOrgUnit(ctx, "车间", nil)
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.PathSnapshot(ctx, unit.ID)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"current":200}`)
	sum := digest.Sum(body)
	a, err := s.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProcess, Level: store.AssetLevelFactory, Name: "焊接",
		Status: store.AssetDraft, Copyable: true, Content: body, Digest: sum,
		CreatorID: p.ID, OrgUnitID: &unit.ID, OrgPath: path,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if a.ID == uuid.Nil || a.Revision != 1 || a.FactoryID != facID || a.Name != "焊接" || !bytes.Equal(a.Content, body) {
		t.Fatalf("asset: %+v", a)
	}
	raw, err := s.RawGovernedContent(ctx, a.ID)
	if err != nil || !contentcrypt.IsEnvelope(raw) || bytes.Contains(raw, body) {
		t.Fatalf("want WM2 got %q %v", raw, err)
	}
	oldID := a.ID
	renamed, err := s.UpdateGovernedAsset(ctx, a.ID, 1, store.AssetWrite{
		Name: "焊接-2", Content: body, Digest: sum, Copyable: true, Status: store.AssetDraft,
	})
	if err != nil || renamed.ID != oldID || renamed.Revision != 2 || renamed.Name != "焊接-2" {
		t.Fatalf("rename: %+v %v", renamed, err)
	}
	raw2, err := s.RawGovernedContent(ctx, a.ID)
	if err != nil || !contentcrypt.IsEnvelope(raw2) || bytes.Contains(raw2, body) {
		t.Fatalf("rename must stay WM2 got %q %v", raw2, err)
	}
	if _, err := s.UpdateGovernedAsset(ctx, a.ID, 1, store.AssetWrite{
		Name: "旧修订", Content: body, Digest: sum, Copyable: true, Status: store.AssetDraft,
	}); err != domain.ErrRevisionConflict {
		t.Fatalf("conflict: %v", err)
	}
	got, err := s.GovernedAssetByID(ctx, a.ID)
	if err != nil || got.Name != "焊接-2" || got.Revision != 2 {
		t.Fatalf("unchanged after conflict: %+v %v", got, err)
	}

	if _, err := s.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProcess, Level: store.AssetLevelFactory, Name: "坏依赖",
		Status: store.AssetDraft, Copyable: true, Content: body, Digest: sum,
		CreatorID: p.ID, Deps: []store.AssetDep{{ID: a.ID, Revision: 1, Digest: sum}},
	}); err != domain.ErrAssetDependency {
		t.Fatalf("process deps: %v", err)
	}
	if _, err := s.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProcess, Level: store.AssetLevelFactory, Name: "短摘要",
		Status: store.AssetDraft, Copyable: true, Content: body, Digest: []byte("short"),
		CreatorID: p.ID,
	}); err != domain.ErrIntegrity {
		t.Fatalf("short digest: %v", err)
	}

	proj, err := s.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProject, Level: store.AssetLevelFactory, Name: "作业",
		Status: store.AssetAvailable, Copyable: true, Content: []byte(`{"beads":1}`),
		Digest: digest.Sum([]byte(`{"beads":1}`)), CreatorID: p.ID,
		Deps: []store.AssetDep{{ID: a.ID, Revision: 2, Digest: sum}},
	})
	if err != nil || len(proj.Deps) != 1 || proj.Deps[0].ID != a.ID {
		t.Fatalf("project: %+v %v", proj, err)
	}
	snap, err := s.ExportAssetSnapshot(ctx, a.ID)
	if err != nil || snap.SourceID != a.ID || snap.SourceFactoryID != facID || snap.SourceRevision != 2 {
		t.Fatalf("snapshot: %+v %v", snap, err)
	}

	if err := facDB.Exec("UPDATE assets SET content = ? WHERE id = ?", []byte("dirty"), a.ID).Error; err != nil {
		t.Fatal(err)
	}
	dirty, err := s.GovernedAssetByID(ctx, a.ID)
	if err != nil || digest.Match(dirty.Content, dirty.Digest) {
		t.Fatalf("tamper should break digest: match=%v err=%v", digest.Match(dirty.Content, dirty.Digest), err)
	}
	used, err := s.AssetIsReferenced(ctx, a.ID)
	if err != nil || !used {
		t.Fatalf("referenced: %v %v", used, err)
	}
	if err := s.DeleteGovernedAsset(ctx, proj.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGovernedAsset(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	var stubExists bool
	if err := facDB.Raw("SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='personal_asset_stubs')").Scan(&stubExists).Error; err != nil || !stubExists {
		t.Fatalf("stubs must remain: %v %v", stubExists, err)
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

func TestContentMasterRestore(t *testing.T) {
	ctx := context.Background()
	facDB, facID := testpg.Fresh(t)
	s := store.Open(facDB, facID)
	l, err := contentcrypt.RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyContentLease(ctx, l, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreatePerson(ctx, "pe", "工艺师", false)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"current":200}`)
	sum := digest.Sum(body)
	a, err := s.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProcess, Level: store.AssetLevelFactory, Name: "焊接",
		Status: store.AssetDraft, Copyable: true, Content: body, Digest: sum, CreatorID: p.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.ClearContentLease()
	if _, err := s.GovernedAssetMetaByID(ctx, a.ID); err != nil {
		t.Fatalf("meta %v", err)
	}
	if _, err := s.GovernedAssetByID(ctx, a.ID); !errors.Is(err, domain.ErrContentLeaseExpired) {
		t.Fatalf("open without lease %v", err)
	}
	if err := s.ApplyContentLease(ctx, l, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := s.GovernedAssetByID(ctx, a.ID)
	if err != nil || !bytes.Equal(got.Content, body) {
		t.Fatalf("restore %q %v", got.Content, err)
	}
	s.SetContentChannelOnline(true)
	if err := s.ApplyContentLease(ctx, l, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GovernedAssetByID(ctx, a.ID); err != nil {
		t.Fatalf("online past notAfter %v", err)
	}
	s.SetContentChannelOnline(false)
	if _, err := s.GovernedAssetByID(ctx, a.ID); !errors.Is(err, domain.ErrContentLeaseExpired) {
		t.Fatalf("offline past notAfter %v", err)
	}
}
