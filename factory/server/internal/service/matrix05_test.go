// 阶段3第5圈：厂内侧厂级/个人级制作与个人保密（5.1～7.3）。
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

func testAssetAuthorship(t *testing.T, run func(string, func(*testing.T))) {
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
	pe := mustCreateRole(t, ctx, facA, saA, "pe-a", "pe-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil)
	pe2 := mustCreateRole(t, ctx, facA, saA, "pe-a2", "pe2-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil)
	op := mustCreateRole(t, ctx, facA, saA, "op-a", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	peB := mustCreateRole(t, ctx, facB, saB, "pe-b", "pe-b-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil)
	direct := factory.WorkContext{Direct: true}
	body := []byte("personal-secret-body")
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
	peShop := mustCreateRole(t, ctx, facA, saA, "pe-shop", "shop-pass", factory.RoleProcessEngineer, factory.ScopeOrgUnit, &shop.ID)
	if err := facA.Assign(ctx, saA, peShop.acc.ID, shop.ID); err != nil {
		t.Fatal(err)
	}

	var facProc factory.Asset
	run("5.1", func(t *testing.T) {
		var err error
		facProc, err = facA.CreateFactoryProcess(ctx, pe.tok, direct, "焊接", body)
		if err != nil {
			t.Fatal(err)
		}
		if facProc.Level != factory.AssetLevelFactory || !facProc.Copyable || facProc.FactoryID != seed.ID ||
			facProc.Status != factory.AssetDraft || facProc.Content != nil {
			t.Fatalf("%+v", facProc)
		}
	})
	run("5.2", func(t *testing.T) {
		if _, err := facA.CreateFactoryProcess(ctx, op.tok, direct, "焊接", body); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("op: %v", err)
		}
		if _, err := facA.CreateFactoryProcess(ctx, saA, direct, "焊接", body); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("sa: %v", err)
		}
		if _, err := facA.CreateFactoryProcess(ctx, peB.tok, direct, "焊接", body); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("pe-b: %v", err)
		}
	})
	run("5.3", func(t *testing.T) {
		got, err := facA.CreateFactoryProcess(ctx, peShop.tok, factory.WorkContext{OrgUnitID: &shop.ID}, "车间工艺", body)
		if err != nil {
			t.Fatal(err)
		}
		if got.OrgUnitID == nil || *got.OrgUnitID != shop.ID || len(got.OrgPath) == 0 || got.OrgPath[len(got.OrgPath)-1].ID != shop.ID {
			t.Fatalf("%+v", got)
		}
	})
	run("5.4", func(t *testing.T) {
		if _, err := facA.CreateFactoryProcess(ctx, peShop.tok, direct, "直属", body); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("direct: %v", err)
		}
		if _, err := facA.CreateFactoryProcess(ctx, peShop.tok, factory.WorkContext{OrgUnitID: &shopB.ID}, "他车间", body); !errors.Is(err, domain.ErrWorkContext) && !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("shopB: %v", err)
		}
	})

	var personal factory.Asset
	run("6.1", func(t *testing.T) {
		var err error
		personal, err = facA.CreatePersonalProcess(ctx, pe.tok, direct, "个人焊接", body)
		if err != nil {
			t.Fatal(err)
		}
		if personal.Level != factory.AssetLevelPersonal || personal.CreatorID != pe.acc.ID ||
			personal.FactoryID != seed.ID || !personal.Copyable || personal.Status != factory.AssetDraft ||
			personal.Content != nil || personal.OrgUnitID != nil {
			t.Fatalf("%+v", personal)
		}
	})
	run("6.2", func(t *testing.T) {
		if _, err := facA.CreatePersonalProcess(ctx, op.tok, direct, "操作员个人", body); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("6.3", func(t *testing.T) {
		if _, err := facA.CreatePersonalProcess(ctx, pe.tok, factory.WorkContext{}, "无上下文", body); !errors.Is(err, domain.ErrWorkContext) {
			t.Fatalf("got %v", err)
		}
	})
	run("7.1", func(t *testing.T) {
		got, err := facA.ReadAssetContent(ctx, pe.tok, personal.ID)
		if err != nil || !bytes.Equal(got, body) {
			t.Fatalf("%q %v", got, err)
		}
		rows, err := facA.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if audit.ContainsAny(audit.Dump(rows), string(body), "pe-pass") {
			t.Fatalf("body leaked")
		}
	})
	run("7.2", func(t *testing.T) {
		if _, err := facA.ReadAssetContent(ctx, saA, personal.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("sa: %v", err)
		}
		if _, err := facA.ReadAssetContent(ctx, pe2.tok, personal.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("pe2: %v", err)
		}
		if _, err := facA.ReadAssetContent(ctx, op.tok, personal.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("op: %v", err)
		}
		if _, err := facA.ReadAssetContent(ctx, peB.tok, personal.ID); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("pe-b: %v", err)
		}
	})
	run("7.3", func(t *testing.T) {
		got, err := facA.GetAsset(ctx, saA, personal.ID)
		if err != nil || got.Content != nil || got.Level != factory.AssetLevelPersonal ||
			got.CreatorID != pe.acc.ID || got.Status != factory.AssetDraft {
			t.Fatalf("%+v %v", got, err)
		}
		if _, err := facA.GetAsset(ctx, pe2.tok, personal.ID); err != nil {
			t.Fatal(err)
		}
	})
}
