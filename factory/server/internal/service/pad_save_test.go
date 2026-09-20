// 平板保存即为可用；管理后台新建仍是草稿。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

func TestPadSaveLandsAvailable(t *testing.T) {
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
	op := mustCreateRole(t, ctx, fac, sa, "op-a", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	direct := factory.WorkContext{Direct: true}
	body := []byte(`{"name":"mine","current":170}`)

	draft, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "后台草稿", body)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != factory.AssetDraft {
		t.Fatalf("admin create: %+v", draft)
	}

	localID := id.New()
	got, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "平板工艺", body, localID, "GY-C0008-000001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != localID || got.Status != factory.AssetAvailable || got.Level != factory.AssetLevelPersonal ||
		got.CreatorID != op.acc.ID || got.Code != "GY-C0008-000001" || !got.Copyable || got.Content != nil {
		t.Fatalf("pad create: %+v", got)
	}
	opened, err := fac.ReadAssetContent(ctx, op.tok, got.ID)
	if err != nil || !bytes.Equal(opened, body) {
		t.Fatalf("read %q %v", opened, err)
	}
	asSA, err := fac.ReadAssetContent(ctx, sa, got.ID)
	if err != nil || !bytes.Equal(asSA, body) {
		t.Fatalf("sa read %q %v", asSA, err)
	}
	nextBody := []byte(`{"name":"mine","current":180}`)
	edited, err := fac.UpdateAssetContent(ctx, sa, got.ID, got.Revision, nextBody)
	if err != nil {
		t.Fatal(err)
	}
	got = edited
	body = nextBody
	if _, err := fac.ReadAssetContent(ctx, pe.tok, got.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("pe read: %v", err)
	}

	if _, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "重号", body, id.New(), "GY-C0008-000001", nil); !errors.Is(err, domain.ErrAssetCodeConflict) {
		t.Fatalf("dup code: %v", err)
	}

	projBody := []byte(`[{"processId":"` + got.ID.String() + `"}]`)
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "厂工程", []byte(`[]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	proj, err = fac.PublishAsset(ctx, pe.tok, proj.ID, proj.Revision)
	if err != nil {
		t.Fatal(err)
	}
	dep := factory.AssetDep{ID: got.ID, Revision: got.Revision, Digest: got.Digest}
	pinned, err := fac.SetProjectDeps(ctx, op.tok, proj.ID, proj.Revision, []factory.AssetDep{dep})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := fac.UpdateAssetContent(ctx, op.tok, pinned.ID, pinned.Revision, projBody)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision <= pinned.Revision || !bytes.Equal(updated.Digest, digest.Sum(projBody)) {
		t.Fatalf("update %+v", updated)
	}
	if _, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProject, "坏工程", []byte(`{"processPath":""}`), uuid.Nil, "", nil); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("processPath: %v", err)
	}
	if _, err := fac.CreatePadPersonal(ctx, op.tok, "other", "坏", body, uuid.Nil, "", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("kind: %v", err)
	}
}
