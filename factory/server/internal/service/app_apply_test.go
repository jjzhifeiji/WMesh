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

func TestApplyAppContent(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa-a", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	sa := mustLogin(t, ctx, fac, "sa-a", "sa-pass")
	op := mustCreateRole(t, ctx, fac, sa, "op-a", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	other := mustCreateRole(t, ctx, fac, sa, "op-b", "op-b-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	admin := mustCreateRole(t, ctx, fac, sa, "oa-a", "oa-pass", factory.RoleOrgAdmin, factory.ScopeFactory, nil)
	aud := mustCreateRole(t, ctx, fac, sa, "aud-a", "aud-pass", factory.RoleAuditor, factory.ScopeFactory, nil)
	direct := factory.WorkContext{Direct: true}

	proc, err := fac.CreateFactoryProcess(ctx, op.tok, direct, "厂工艺", []byte(`{"n":1}`))
	if err != nil {
		t.Fatal(err)
	}
	proc, err = fac.PublishAsset(ctx, op.tok, proc.ID, proc.Revision)
	if err != nil {
		t.Fatal(err)
	}
	web, err := fac.UpdateAssetContent(ctx, sa, proc.ID, proc.Revision, []byte(`{"n":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.ApplyAppContent(ctx, op.tok, proc.ID, []byte(`{"n":9}`)); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("operator factory: %v", err)
	}
	if _, err := fac.ApplyAppContent(ctx, aud.tok, proc.ID, []byte(`{"n":9}`)); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("auditor factory: %v", err)
	}
	got, err := fac.ApplyAppContent(ctx, admin.tok, proc.ID, []byte(`{"n":9}`))
	if err != nil || got.Revision != web.Revision+1 {
		t.Fatalf("admin apply %+v %v", got, err)
	}
	body, err := fac.ReadAssetContent(ctx, sa, proc.ID)
	if err != nil || !bytes.Equal(body, []byte(`{"n":9}`)) {
		t.Fatalf("covered %q %v", body, err)
	}
	if _, err := fac.UpdateAssetContent(ctx, sa, proc.ID, web.Revision, []byte(`{"n":8}`)); !errors.Is(err, domain.ErrRevisionConflict) {
		t.Fatalf("web stale: %v", err)
	}

	mine, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "我的", []byte(`{"p":1}`), uuid.Nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	webMine, err := fac.UpdateAssetContent(ctx, sa, mine.ID, mine.Revision, []byte(`{"p":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.ApplyAppContent(ctx, sa, mine.ID, []byte(`{"p":3}`)); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("sa personal: %v", err)
	}
	if _, err := fac.ApplyAppContent(ctx, other.tok, mine.ID, []byte(`{"p":3}`)); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("other personal: %v", err)
	}
	mineGot, err := fac.ApplyAppContent(ctx, op.tok, mine.ID, []byte(`{"p":3}`))
	if err != nil || mineGot.Revision != webMine.Revision+1 {
		t.Fatalf("creator apply %+v %v", mineGot, err)
	}
	body, err = fac.ReadAssetContent(ctx, op.tok, mine.ID)
	if err != nil || !bytes.Equal(body, []byte(`{"p":3}`)) {
		t.Fatalf("personal covered %q %v", body, err)
	}

	off, err := fac.DisableAsset(ctx, sa, proc.ID, got.Revision)
	if err != nil {
		t.Fatal(err)
	}
	coveredOff, err := fac.ApplyAppContent(ctx, sa, proc.ID, []byte(`{"n":10}`))
	if err != nil || coveredOff.Status != factory.AssetDisabled || coveredOff.Revision != off.Revision+1 {
		t.Fatalf("disabled cover %+v %v", coveredOff, err)
	}
	if meta, err := fac.GetAsset(ctx, sa, proc.ID); err != nil || meta.Status != factory.AssetDisabled {
		t.Fatalf("still disabled %+v %v", meta, err)
	}
	if body, err := fac.ReadAssetContent(ctx, sa, proc.ID); err != nil || !bytes.Equal(body, []byte(`{"n":10}`)) {
		t.Fatalf("disabled body %q %v", body, err)
	}

	drop, err := fac.CreateFactoryProcess(ctx, sa, direct, "可删", []byte(`{"d":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.DeleteAsset(ctx, sa, drop.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.ApplyAppContent(ctx, sa, drop.ID, []byte(`{"d":2}`)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}

	platID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	plain := []byte(`{"plat":1}`)
	if _, err := fac.Store().InsertReplica(ctx, store.AssetReplica{
		ID: platID, Revision: 1, Kind: store.KindProcess, Level: store.AssetLevelPlatform,
		Name: "平台焊", Status: store.AssetAvailable, Copyable: false, Content: plain, Digest: digest.Sum(plain),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.ApplyAppContent(ctx, sa, platID, []byte(`{"plat":2}`)); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("platform: %v", err)
	}

	pinned, err := fac.CreateFactoryProcess(ctx, sa, direct, "被引用", []byte(`{"b":1}`))
	if err != nil {
		t.Fatal(err)
	}
	pinned, err = fac.PublishAsset(ctx, sa, pinned.ID, pinned.Revision)
	if err != nil {
		t.Fatal(err)
	}
	proj, err := fac.CreateFactoryProject(ctx, sa, direct, "厂工程", []byte(`[]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	proj, err = fac.PublishAsset(ctx, sa, proj.ID, proj.Revision)
	if err != nil {
		t.Fatal(err)
	}
	next := []byte(`[{"processId":"` + pinned.ID.String() + `"}]`)
	covered, err := fac.ApplyAppContent(ctx, sa, proj.ID, next)
	if err != nil || covered.Revision != proj.Revision+1 || len(covered.Deps) != 1 || covered.Deps[0].ID != pinned.ID {
		t.Fatalf("project %+v %v", covered, err)
	}
}
