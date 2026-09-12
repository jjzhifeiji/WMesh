// 阶段3第4圈：厂内侧身份、修订、状态、完整性（1.1～4.3、16.1～16.2）。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

func testAssetIdentity(t *testing.T, run func(string, func(*testing.T))) {
	t.Helper()
	ctx := context.Background()
	h := New(t)
	seed, facA, err := h.Provision(ctx, "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	if err := facA.Activate(ctx, "sa-a", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saA := mustLogin(t, ctx, facA, "sa-a", "sa-pass")
	pe := mustCreateRole(t, ctx, facA, saA, "pe-a", "pe-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil)
	pe2 := mustCreateRole(t, ctx, facA, saA, "pe-a2", "pe2-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil)
	direct := factory.WorkContext{Direct: true}
	body := []byte("weld-body-v1")
	next := []byte("weld-body-v2")

	var weld factory.Asset
	run("1.1", func(t *testing.T) {
		var err error
		weld, err = facA.CreateFactoryProcess(ctx, pe.tok, direct, "焊接", body)
		if err != nil {
			t.Fatal(err)
		}
		if weld.ID.String() == "" || weld.Revision != 1 || weld.Status != factory.AssetDraft ||
			weld.Level != factory.AssetLevelFactory || weld.Kind != factory.KindProcess ||
			!weld.Copyable || weld.Name != "焊接" || weld.FactoryID != seed.ID || weld.Content != nil {
			t.Fatalf("%+v", weld)
		}
	})
	run("1.2", func(t *testing.T) {
		got, err := facA.RenameAsset(ctx, pe.tok, weld.ID, weld.Revision, "焊接-2")
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != weld.ID || got.Revision != weld.Revision+1 || got.Name != "焊接-2" {
			t.Fatalf("%+v", got)
		}
		weld = got
	})

	run("2.1", func(t *testing.T) {
		got, err := facA.UpdateAssetContent(ctx, pe.tok, weld.ID, weld.Revision, next)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != weld.ID || got.Revision != weld.Revision+1 {
			t.Fatalf("%+v", got)
		}
		gotBody, err := facA.ReadAssetContent(ctx, pe.tok, weld.ID)
		if err != nil || !bytes.Equal(gotBody, next) {
			t.Fatalf("%q %v", gotBody, err)
		}
		weld = got
	})
	run("2.2", func(t *testing.T) {
		if _, err := facA.UpdateAssetContent(ctx, pe.tok, weld.ID, weld.Revision-1, []byte("stale")); !errors.Is(err, domain.ErrRevisionConflict) {
			t.Fatalf("got %v", err)
		}
		still, err := facA.GetAsset(ctx, pe.tok, weld.ID)
		if err != nil || still.Revision != weld.Revision || still.Name != weld.Name {
			t.Fatalf("%+v %v", still, err)
		}
		gotBody, err := facA.ReadAssetContent(ctx, pe.tok, weld.ID)
		if err != nil || !bytes.Equal(gotBody, next) {
			t.Fatalf("%q %v", gotBody, err)
		}
	})
	run("2.3", func(t *testing.T) {
		first, err := facA.UpdateAssetContent(ctx, pe.tok, weld.ID, weld.Revision, []byte("weld-body-v3"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := facA.UpdateAssetContent(ctx, pe2.tok, weld.ID, weld.Revision, []byte("lost")); !errors.Is(err, domain.ErrRevisionConflict) {
			t.Fatalf("got %v", err)
		}
		still, err := facA.GetAsset(ctx, pe.tok, weld.ID)
		if err != nil || still.Revision != first.Revision {
			t.Fatalf("%+v %v", still, err)
		}
		weld = first
	})

	run("3.1", func(t *testing.T) {
		draft, err := facA.CreateFactoryProcess(ctx, pe.tok, direct, "草稿工艺", body)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := facA.CreateFactoryProject(ctx, pe.tok, direct, "工程", body, []factory.AssetDep{{
			ID: draft.ID, Revision: draft.Revision, Digest: draft.Digest,
		}}); !errors.Is(err, domain.ErrAssetNotAvailable) {
			t.Fatalf("got %v", err)
		}
	})
	run("3.2", func(t *testing.T) {
		draft, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "个人草稿", body)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := facA.PromoteToFactory(ctx, pe.tok, draft.ID); !errors.Is(err, domain.ErrAssetNotAvailable) {
			t.Fatalf("got %v", err)
		}
		still, err := facA.GetAsset(ctx, pe.tok, draft.ID)
		if err != nil || still.Level != factory.AssetLevelPersonal || still.Revision != 1 {
			t.Fatalf("%+v %v", still, err)
		}
	})
	run("3.3", func(t *testing.T) {
		got, err := facA.PublishAsset(ctx, pe.tok, weld.ID, weld.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != factory.AssetAvailable || got.Revision != weld.Revision+1 {
			t.Fatalf("%+v", got)
		}
		weld = got
	})
	run("16.2", func(t *testing.T) {
		gotBody, err := facA.ReadAssetContent(ctx, pe.tok, weld.ID)
		if err != nil || !bytes.Equal(gotBody, []byte("weld-body-v3")) {
			t.Fatalf("%q %v", gotBody, err)
		}
		rows, err := facA.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if bad := audit.Incomplete(rows); len(bad) > 0 {
			t.Fatalf("incomplete %#v", bad[0])
		}
		if audit.ContainsAny(audit.Dump(rows), "weld-body-v1", "weld-body-v2", "weld-body-v3", "pe-pass", "sa-pass") {
			t.Fatalf("secret or body leaked")
		}
	})
	run("3.4", func(t *testing.T) {
		got, err := facA.DisableAsset(ctx, pe.tok, weld.ID, weld.Revision)
		if err != nil {
			t.Fatal(err)
		}
		weld = got
		if _, err := facA.UpdateAssetContent(ctx, pe.tok, weld.ID, weld.Revision, []byte("after-disable")); !errors.Is(err, domain.ErrAssetNotAvailable) {
			t.Fatalf("update: %v", err)
		}
		if _, err := facA.CreateFactoryProject(ctx, pe.tok, direct, "依赖停用", body, []factory.AssetDep{{
			ID: weld.ID, Revision: weld.Revision, Digest: weld.Digest,
		}}); !errors.Is(err, domain.ErrAssetNotAvailable) {
			t.Fatalf("dep: %v", err)
		}
		p, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "停用再升", body)
		if err != nil {
			t.Fatal(err)
		}
		p, err = facA.PublishAsset(ctx, pe.tok, p.ID, p.Revision)
		if err != nil {
			t.Fatal(err)
		}
		p, err = facA.DisableAsset(ctx, pe.tok, p.ID, p.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := facA.PromoteToFactory(ctx, pe.tok, p.ID); !errors.Is(err, domain.ErrAssetNotAvailable) {
			t.Fatalf("promote: %v", err)
		}
	})
	run("3.5", func(t *testing.T) {
		got, err := facA.ReenableAsset(ctx, pe.tok, weld.ID, weld.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != factory.AssetAvailable || got.Revision != weld.Revision+1 {
			t.Fatalf("%+v", got)
		}
		weld = got
	})
	run("3.6", func(t *testing.T) {
		if err := facA.DeleteAsset(ctx, pe.tok, weld.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := facA.GetAsset(ctx, pe.tok, weld.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v", err)
		}
	})
	run("3.7", func(t *testing.T) {
		p, err := facA.CreateFactoryProcess(ctx, pe.tok, direct, "被依赖", body)
		if err != nil {
			t.Fatal(err)
		}
		p, err = facA.PublishAsset(ctx, pe.tok, p.ID, p.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := facA.CreateFactoryProject(ctx, pe.tok, direct, "钉死依赖", body, []factory.AssetDep{{
			ID: p.ID, Revision: p.Revision, Digest: p.Digest,
		}}); err != nil {
			t.Fatal(err)
		}
		if err := facA.DeleteAsset(ctx, pe.tok, p.ID); !errors.Is(err, domain.ErrReferenced) {
			t.Fatalf("got %v", err)
		}
		if _, err := facA.GetAsset(ctx, pe.tok, p.ID); err != nil {
			t.Fatal(err)
		}
	})
	run("4.2", func(t *testing.T) {
		if err := facA.CreatePlatformProcess(ctx, pe.tok, "平台", body); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("pe: %v", err)
		}
		if err := facA.CreatePlatformProcess(ctx, saA, "平台", body); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("sa: %v", err)
		}
	})
	run("1.3", func(t *testing.T) {
		src, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "个人焊接", body)
		if err != nil {
			t.Fatal(err)
		}
		src, err = facA.PublishAsset(ctx, pe.tok, src.ID, src.Revision)
		if err != nil {
			t.Fatal(err)
		}
		promoted, err := facA.PromoteToFactory(ctx, pe.tok, src.ID)
		if err != nil {
			t.Fatal(err)
		}
		if promoted.ID == src.ID || promoted.Level != factory.AssetLevelFactory || promoted.Revision != 1 ||
			promoted.Status != factory.AssetAvailable || !promoted.Copyable || promoted.Content != nil ||
			promoted.SourceID == nil || *promoted.SourceID != src.ID ||
			promoted.SourceRevision == nil || *promoted.SourceRevision != src.Revision {
			t.Fatalf("%+v from %+v", promoted, src)
		}
		orig, err := facA.GetAsset(ctx, pe.tok, src.ID)
		if err != nil || orig.Level != factory.AssetLevelPersonal || orig.Revision != src.Revision {
			t.Fatalf("%+v %v", orig, err)
		}
	})
	run("16.1", func(t *testing.T) {
		a, err := facA.CreateFactoryProcess(ctx, pe.tok, direct, "脏数据", body)
		if err != nil {
			t.Fatal(err)
		}
		a, err = facA.PublishAsset(ctx, pe.tok, a.ID, a.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if err := facA.Store().TamperAssetContent(ctx, a.ID, []byte("tampered")); err != nil {
			t.Fatal(err)
		}
		if _, err := facA.GetAsset(ctx, pe.tok, a.ID); !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("get: %v", err)
		}
		if _, err := facA.ReadAssetContent(ctx, pe.tok, a.ID); !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("read: %v", err)
		}
	})
}
