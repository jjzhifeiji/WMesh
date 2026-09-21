// 工艺/工程作业类型：单层焊道、多层焊缝、T排对接；工程与工艺必须同类型。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

func TestFactoryWeldKind(t *testing.T) {
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
	pe := mustCreateRole(t, ctx, fac, sa, "pe-a", "pe-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil)
	direct := factory.WorkContext{Direct: true}

	def, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "默认工艺", []byte(`{"name":"p"}`))
	if err != nil {
		t.Fatal(err)
	}
	if def.WeldKind != store.WeldKindSingle {
		t.Fatalf("default %s", def.WeldKind)
	}
	if _, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "坏类型", []byte(`{"name":"p"}`), "nope"); !errors.Is(err, domain.ErrInvalidWeldKind) {
		t.Fatalf("invalid: %v", err)
	}
	multiP, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "多层工艺", []byte(`{"name":"m"}`), "multi")
	if err != nil {
		t.Fatal(err)
	}
	if multiP.WeldKind != store.WeldKindMultilayer {
		t.Fatalf("alias %s", multiP.WeldKind)
	}
	copied, err := fac.CopyProcess(ctx, pe.tok, multiP.ID, "多层副本")
	if err != nil || copied.WeldKind != store.WeldKindMultilayer {
		t.Fatalf("copy %+v %v", copied, err)
	}

	pub, err := fac.PublishAsset(ctx, pe.tok, multiP.ID, multiP.Revision)
	if err != nil {
		t.Fatal(err)
	}
	singleBody := []byte(`[{"templateId":"` + contenttpl.SeedTplSingle + `","name":"单"}]`)
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "单层工程钉多层", singleBody, []factory.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}}, store.WeldKindSingle); !errors.Is(err, domain.ErrWeldKindMismatch) {
		t.Fatalf("dep mismatch: %v", err)
	}
	multiBody := []byte(`[{"templateId":"` + contenttpl.SeedTplMulti + `","name":"多层"}]`)
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "单层工程用多层模版", multiBody, nil, store.WeldKindSingle); !errors.Is(err, domain.ErrWeldKindMismatch) {
		t.Fatalf("content mismatch: %v", err)
	}
	ok, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "多层工程", multiBody, []factory.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}}, store.WeldKindMultilayer)
	if err != nil {
		t.Fatal(err)
	}
	if ok.WeldKind != store.WeldKindMultilayer {
		t.Fatalf("project %s", ok.WeldKind)
	}
}
