// 阶段3第6圈：厂内侧升档、跨厂隔离、工程依赖与归属（8.1～15.2、17.1～18.2）。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

func testAssetPromote(t *testing.T, run func(string, func(*testing.T))) {
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
	seedB, facB, err := h.Provision(ctx, "sa-b", "超管B")
	if err != nil {
		t.Fatal(err)
	}
	if err := facB.Activate(ctx, "sa-b", seedB.ActivationToken, "sa-b-pass"); err != nil {
		t.Fatal(err)
	}
	saB := mustLogin(t, ctx, facB, "sa-b", "sa-b-pass")
	pe := mustCreateRole(t, ctx, facA, saA, "pe-a", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	pe2 := mustCreateRole(t, ctx, facA, saA, "pe-a2", "pe2-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	op := mustCreateRole(t, ctx, facA, saA, "op-a", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	peB := mustCreateRole(t, ctx, facB, saB, "pe-b", "pe-b-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	direct := factory.WorkContext{Direct: true}
	body := []byte("circle6-body-secret")
	site, err := facA.CreateOrgUnit(ctx, saA, "场地", nil)
	if err != nil {
		t.Fatal(err)
	}
	shop, err := facA.CreateOrgUnit(ctx, saA, "车间A", &site.ID)
	if err != nil {
		t.Fatal(err)
	}
	shopB, err := facA.CreateOrgUnit(ctx, saA, "车间B", &site.ID)
	if err != nil {
		t.Fatal(err)
	}
	peShop := mustCreateRole(t, ctx, facA, saA, "pe-shop", "shop-pass", factory.RoleOperator, factory.ScopeOrgUnit, &shop.ID)
	if err := facA.Assign(ctx, saA, peShop.acc.ID, shop.ID); err != nil {
		t.Fatal(err)
	}

	var personal, promoted, facProc, pinned factory.Asset
	run("8.1", func(t *testing.T) {
		src, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "个人焊接", body)
		if err != nil {
			t.Fatal(err)
		}
		src, err = facA.PublishAsset(ctx, pe.tok, src.ID, src.Revision)
		if err != nil {
			t.Fatal(err)
		}
		personal = src
		promoted, err = facA.PromoteToFactory(ctx, pe.tok, src.ID)
		if err != nil {
			t.Fatal(err)
		}
		if promoted.ID == src.ID || promoted.Level != factory.AssetLevelFactory || promoted.Content != nil ||
			promoted.SourceID == nil || *promoted.SourceID != src.ID {
			t.Fatalf("%+v", promoted)
		}
		orig, err := facA.GetAsset(ctx, pe.tok, src.ID)
		if err != nil || orig.Level != factory.AssetLevelPersonal || orig.Revision != src.Revision {
			t.Fatalf("%+v %v", orig, err)
		}
		got, err := facA.ReadAssetContent(ctx, pe.tok, src.ID)
		if err != nil || !bytes.Equal(got, body) {
			t.Fatalf("%q %v", got, err)
		}
	})
	run("8.4", func(t *testing.T) {
		again, err := facA.PromoteToFactory(ctx, pe.tok, personal.ID)
		if err != nil {
			t.Fatal(err)
		}
		if again.ID != promoted.ID || again.Revision != promoted.Revision {
			t.Fatalf("skip %+v want %+v", again, promoted)
		}
	})
	run("8.5", func(t *testing.T) {
		changed, err := facA.UpdateAssetContent(ctx, pe.tok, personal.ID, personal.Revision, []byte("personal-v2"))
		if err != nil {
			t.Fatal(err)
		}
		personal = changed
		got, err := facA.PromoteToFactory(ctx, pe.tok, personal.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != promoted.ID || got.Revision != promoted.Revision+1 || !bytes.Equal(got.Digest, personal.Digest) {
			t.Fatalf("overwrite %+v from %+v", got, personal)
		}
		promoted = got
	})
	run("8.2", func(t *testing.T) {
		if err := facA.Assign(ctx, saA, pe.acc.ID, shopB.ID); err != nil {
			t.Fatal(err)
		}
		src, err := facA.CreatePersonalProcess(ctx, pe.tok, factory.WorkContext{OrgUnitID: &shopB.ID}, "他车间个人", body)
		if err != nil {
			t.Fatal(err)
		}
		src, err = facA.PublishAsset(ctx, pe.tok, src.ID, src.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := facA.PromoteToFactory(ctx, peShop.tok, src.ID); err != nil {
			t.Fatalf("got %v", err)
		}
		still, err := facA.GetAsset(ctx, pe.tok, src.ID)
		if err != nil || still.Level != factory.AssetLevelPersonal {
			t.Fatalf("%+v %v", still, err)
		}
	})
	run("8.3", func(t *testing.T) {
		if _, err := facA.PromoteToFactory(ctx, saA, personal.ID); err != nil {
			t.Fatalf("got %v", err)
		}
	})
	run("9.1", func(t *testing.T) {
		p, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "不可复制", body)
		if err != nil {
			t.Fatal(err)
		}
		p, err = facA.SetAssetCopyable(ctx, pe.tok, p.ID, p.Revision, false)
		if err != nil {
			t.Fatal(err)
		}
		p, err = facA.PublishAsset(ctx, pe.tok, p.ID, p.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := facA.PromoteToFactory(ctx, pe.tok, p.ID); !errors.Is(err, domain.ErrAssetNotCopyable) {
			t.Fatalf("got %v", err)
		}
	})
	run("9.2", func(t *testing.T) {
		p, err := facA.CreateFactoryProcess(ctx, pe.tok, direct, "不可升平台", body)
		if err != nil {
			t.Fatal(err)
		}
		p, err = facA.PublishAsset(ctx, pe.tok, p.ID, p.Revision)
		if err != nil {
			t.Fatal(err)
		}
		p, err = facA.SetAssetCopyable(ctx, pe.tok, p.ID, p.Revision, false)
		if err != nil {
			t.Fatal(err)
		}
		snap, err := facA.ExportAssetSnapshot(ctx, pe.tok, p.ID)
		if err != nil || snap.Copyable || snap.SourceID != p.ID {
			t.Fatalf("%+v %v", snap, err)
		}
	})
	run("9.3", func(t *testing.T) {
		p, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "脏升档", body)
		if err != nil {
			t.Fatal(err)
		}
		p, err = facA.PublishAsset(ctx, pe.tok, p.ID, p.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if err := facA.Store().TamperAssetContent(ctx, p.ID, []byte("tampered")); err != nil {
			t.Fatal(err)
		}
		if _, err := facA.PromoteToFactory(ctx, pe.tok, p.ID); !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("got %v", err)
		}
	})
	run("10.2", func(t *testing.T) {
		if err := facA.CreatePlatformProcess(ctx, pe.tok, "平台", body); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})

	facProc, err = facA.CreateFactoryProcess(ctx, pe.tok, direct, "厂级工艺", body)
	if err != nil {
		t.Fatal(err)
	}
	facProc, err = facA.PublishAsset(ctx, pe.tok, facProc.ID, facProc.Revision)
	if err != nil {
		t.Fatal(err)
	}

	run("11.1", func(t *testing.T) {
		if _, err := facA.GetAsset(ctx, peB.tok, facProc.ID); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("get: %v", err)
		}
		if _, err := facA.UpdateAssetContent(ctx, peB.tok, facProc.ID, facProc.Revision, body); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("update: %v", err)
		}
		if _, err := facA.PromoteToFactory(ctx, peB.tok, personal.ID); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("promote: %v", err)
		}
		if _, err := facB.GetAsset(ctx, peB.tok, facProc.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("facB: %v", err)
		}
	})
	run("11.2", func(t *testing.T) {
		if _, err := facB.CreateFactoryProject(ctx, peB.tok, direct, "跨厂工程", body, []factory.AssetDep{{
			ID: facProc.ID, Revision: facProc.Revision, Digest: facProc.Digest,
		}}); !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrAssetDependency) {
			t.Fatalf("got %v", err)
		}
	})
	run("11.3", func(t *testing.T) {
		got, err := facB.CreateFactoryProcess(ctx, peB.tok, direct, "焊接", body)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID == facProc.ID || got.FactoryID != seedB.ID {
			t.Fatalf("%+v", got)
		}
	})
	run("13.1", func(t *testing.T) {
		var err error
		pinned, err = facA.CreateFactoryProject(ctx, pe.tok, direct, "工程", body, []factory.AssetDep{{
			ID: facProc.ID, Revision: facProc.Revision, Digest: facProc.Digest,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if len(pinned.Deps) != 1 || pinned.Deps[0].ID != facProc.ID || pinned.Deps[0].Revision != facProc.Revision {
			t.Fatalf("%+v", pinned)
		}
	})
	run("13.2", func(t *testing.T) {
		if _, err := facA.CreateFactoryProject(ctx, pe.tok, direct, "缺", body, []factory.AssetDep{{
			ID: uuid.New(), Revision: 1, Digest: facProc.Digest,
		}}); !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrAssetDependency) {
			t.Fatalf("missing: %v", err)
		}
		draft, err := facA.CreateFactoryProcess(ctx, pe.tok, direct, "草稿", body)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := facA.CreateFactoryProject(ctx, pe.tok, direct, "草稿依赖", body, []factory.AssetDep{{
			ID: draft.ID, Revision: draft.Revision, Digest: draft.Digest,
		}}); !errors.Is(err, domain.ErrAssetNotAvailable) {
			t.Fatalf("draft: %v", err)
		}
		off, err := facA.CreateFactoryProcess(ctx, pe.tok, direct, "将停用", body)
		if err != nil {
			t.Fatal(err)
		}
		off, err = facA.PublishAsset(ctx, pe.tok, off.ID, off.Revision)
		if err != nil {
			t.Fatal(err)
		}
		off, err = facA.DisableAsset(ctx, pe.tok, off.ID, off.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := facA.CreateFactoryProject(ctx, pe.tok, direct, "停用依赖", body, []factory.AssetDep{{
			ID: off.ID, Revision: off.Revision, Digest: off.Digest,
		}}); !errors.Is(err, domain.ErrAssetNotAvailable) {
			t.Fatalf("disabled: %v", err)
		}
		if _, err := facA.CreateFactoryProject(ctx, pe.tok, direct, "错修订", body, []factory.AssetDep{{
			ID: facProc.ID, Revision: facProc.Revision + 9, Digest: facProc.Digest,
		}}); !errors.Is(err, domain.ErrAssetDependency) {
			t.Fatalf("rev: %v", err)
		}
	})
	run("13.3", func(t *testing.T) {
		oldRev := pinned.Deps[0].Revision
		if _, err := facA.UpdateAssetContent(ctx, pe.tok, facProc.ID, facProc.Revision, []byte("weld-v2")); err != nil {
			t.Fatal(err)
		}
		got, err := facA.GetAsset(ctx, pe.tok, pinned.ID)
		if err != nil || len(got.Deps) != 1 || got.Deps[0].Revision != oldRev || got.Deps[0].ID != facProc.ID {
			t.Fatalf("%+v %v", got, err)
		}
	})
	run("13.4", func(t *testing.T) {
		other, err := facA.CreatePersonalProcess(ctx, pe2.tok, direct, "他人个人", body)
		if err != nil {
			t.Fatal(err)
		}
		other, err = facA.PublishAsset(ctx, pe2.tok, other.ID, other.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := facA.CreatePersonalProject(ctx, pe.tok, direct, "借他人", body, []factory.AssetDep{{
			ID: other.ID, Revision: other.Revision, Digest: other.Digest,
		}}); !errors.Is(err, domain.ErrAssetDependency) {
			t.Fatalf("got %v", err)
		}
	})
	run("13.5", func(t *testing.T) {
		own, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "自己个人", body)
		if err != nil {
			t.Fatal(err)
		}
		own, err = facA.PublishAsset(ctx, pe.tok, own.ID, own.Revision)
		if err != nil {
			t.Fatal(err)
		}
		facNow, err := facA.GetAsset(ctx, pe.tok, facProc.ID)
		if err != nil {
			t.Fatal(err)
		}
		got, err := facA.CreatePersonalProject(ctx, pe.tok, direct, "个人工程", body, []factory.AssetDep{
			{ID: own.ID, Revision: own.Revision, Digest: own.Digest},
			{ID: facNow.ID, Revision: facNow.Revision, Digest: facNow.Digest},
		})
		if err != nil || len(got.Deps) != 2 {
			t.Fatalf("%+v %v", got, err)
		}
	})
	run("14.2", func(t *testing.T) {
		own, err := facA.CreatePersonalProcess(ctx, pe.tok, direct, "未先升工艺", body)
		if err != nil {
			t.Fatal(err)
		}
		own, err = facA.PublishAsset(ctx, pe.tok, own.ID, own.Revision)
		if err != nil {
			t.Fatal(err)
		}
		proj, err := facA.CreatePersonalProject(ctx, pe.tok, direct, "个人工程待升", body, []factory.AssetDep{{
			ID: own.ID, Revision: own.Revision, Digest: own.Digest,
		}})
		if err != nil {
			t.Fatal(err)
		}
		proj, err = facA.PublishAsset(ctx, pe.tok, proj.ID, proj.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := facA.PromoteToFactory(ctx, pe.tok, proj.ID); !errors.Is(err, domain.ErrAssetDependency) {
			t.Fatalf("got %v", err)
		}
	})
	run("14.3", func(t *testing.T) {
		if err := facA.CreateProcessFromProject(ctx, pe.tok, pinned.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("15.1", func(t *testing.T) {
		if _, err := facA.ReadAssetContent(ctx, op.tok, personal.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("read: %v", err)
		}
		got, err := facA.CreateFactoryProject(ctx, op.tok, direct, "当厂级依赖", body, []factory.AssetDep{{
			ID: personal.ID, Revision: personal.Revision, Digest: personal.Digest,
		}})
		if err != nil {
			t.Fatalf("dep: %v", err)
		}
		if len(got.Deps) != 1 || got.Deps[0].ID != personal.ID {
			t.Fatalf("deps %+v", got.Deps)
		}
		still, err := facA.GetAsset(ctx, pe.tok, personal.ID)
		if err != nil || still.Level != factory.AssetLevelPersonal {
			t.Fatalf("%+v %v", still, err)
		}
	})
	run("15.2", func(t *testing.T) {
		if _, err := facA.ReadAssetContent(ctx, pe2.tok, personal.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("orig: %v", err)
		}
		if _, err := facA.GetAsset(ctx, pe.tok, promoted.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := facA.ReadAssetContent(ctx, pe.tok, promoted.ID); err != nil {
			t.Fatal(err)
		}
	})
	run("18.1", func(t *testing.T) {
		rows, err := facA.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if bad := audit.Incomplete(rows); len(bad) > 0 {
			t.Fatalf("incomplete %#v", bad[0])
		}
		if audit.ContainsAny(audit.Dump(rows), string(body), "pe-pass", "sa-pass", "op-pass") {
			t.Fatalf("secret leaked")
		}
	})
	run("18.2", func(t *testing.T) {
		rows, err := facA.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		dump := audit.Dump(rows)
		if !bytes.Contains([]byte(dump), []byte("rev=")) {
			t.Fatalf("missing revision in audit")
		}
		still, err := facA.GetAsset(ctx, pe.tok, personal.ID)
		if err != nil || still.Level != factory.AssetLevelPersonal {
			t.Fatalf("%+v %v", still, err)
		}
	})
	run("17.1", func(t *testing.T) {
		if err := facA.Unassign(ctx, saA, pe.acc.ID, shopB.ID); err != nil {
			t.Fatal(err)
		}
		if err := facA.Assign(ctx, saA, pe.acc.ID, shop.ID); err != nil {
			t.Fatal(err)
		}
		p, err := facA.CreatePersonalProcess(ctx, pe.tok, factory.WorkContext{OrgUnitID: &shop.ID}, "路径个人", body)
		if err != nil {
			t.Fatal(err)
		}
		if err := facA.Unassign(ctx, saA, pe.acc.ID, shop.ID); err != nil {
			t.Fatal(err)
		}
		got, err := facA.GetAsset(ctx, saA, p.ID)
		if err != nil || got.CreatorID != pe.acc.ID || got.OrgUnitID == nil || *got.OrgUnitID != shop.ID ||
			len(got.OrgPath) == 0 || got.OrgPath[len(got.OrgPath)-1].ID != shop.ID {
			t.Fatalf("%+v %v", got, err)
		}
	})
	run("17.2", func(t *testing.T) {
		if err := facA.RehomeAsset(ctx, saA, personal.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		got, err := facA.GetAsset(ctx, saA, personal.ID)
		if err != nil || got.CreatorID != pe.acc.ID {
			t.Fatalf("%+v %v", got, err)
		}
	})
	run("17.3", func(t *testing.T) {
		before, err := facA.GetAsset(ctx, saA, personal.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := facA.DisableAccount(ctx, saA, pe.acc.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := facA.UpdateAssetContent(ctx, pe.tok, personal.ID, personal.Revision, []byte("after-disable")); !errors.Is(err, domain.ErrAccountDisabled) {
			t.Fatalf("got %v", err)
		}
		still, err := facA.GetAsset(ctx, saA, personal.ID)
		if err != nil || still.Revision != before.Revision {
			t.Fatalf("%+v %v", still, err)
		}
	})
}
