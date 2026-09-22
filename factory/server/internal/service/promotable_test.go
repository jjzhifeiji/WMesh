// 通道升档：列出本厂全部级别，不分状态。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

func TestPromotableForChannel(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	direct := factory.WorkContext{Direct: true}
	body := []byte("factory-body")

	draft, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "草稿", body)
	if err != nil {
		t.Fatal(err)
	}
	avail, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "可升", body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.PublishAsset(ctx, pe.tok, avail.ID, avail.Revision); err != nil {
		t.Fatal(err)
	}
	personal, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "个人", body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.PublishAsset(ctx, pe.tok, personal.ID, personal.Revision); err != nil {
		t.Fatal(err)
	}
	platBody := []byte("plat-copy")
	plat := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台副本",
		Status: factory.AssetAvailable, Copyable: false, Revision: 5, Content: platBody, Digest: digest.Sum(platBody),
	}
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, plat, nil, &seed.ID, nil)); err != nil {
		t.Fatal(err)
	}

	rows, err := fac.ListPromotable(ctx, factory.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	levels := map[string]string{}
	for _, r := range rows {
		seen[r.Name] = r.Status
		levels[r.Name] = r.Level
	}
	if seen["草稿"] != factory.AssetDraft || seen["可升"] != factory.AssetAvailable || seen["个人"] != factory.AssetAvailable || seen["平台副本"] != factory.AssetAvailable {
		t.Fatalf("%+v", rows)
	}
	if levels["草稿"] != factory.AssetLevelFactory || levels["个人"] != factory.AssetLevelPersonal || levels["平台副本"] != factory.AssetLevelPlatform {
		t.Fatalf("levels %+v", levels)
	}

	if snap, err := fac.SnapshotForChannel(ctx, draft.ID); err != nil || snap.Status != factory.AssetDraft || !contentcrypt.IsEnvelope(snap.Content) {
		t.Fatalf("draft: %+v %v", snap, err)
	}
	if snap, err := fac.SnapshotForChannel(ctx, personal.ID); err != nil || snap.SourceID != personal.ID || !contentcrypt.IsEnvelope(snap.Content) {
		t.Fatalf("personal: %+v %v", snap, err)
	}
	if _, err := fac.SnapshotForChannel(ctx, plat.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("platform replica: %v", err)
	}
	snap, err := fac.SnapshotForChannel(ctx, avail.ID)
	if err != nil || snap.SourceID != avail.ID || !contentcrypt.IsEnvelope(snap.Content) {
		t.Fatalf("%+v %v", snap, err)
	}
}

func TestPromotableProjectsAllStatuses(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	direct := factory.WorkContext{Direct: true}
	body := []byte("job")

	draft, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "草稿工程", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	avail, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "可用工程", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	avail, err = fac.PublishAsset(ctx, pe.tok, avail.ID, avail.Revision)
	if err != nil {
		t.Fatal(err)
	}
	off, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "停用工程", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	off, err = fac.PublishAsset(ctx, pe.tok, off.ID, off.Revision)
	if err != nil {
		t.Fatal(err)
	}
	off, err = fac.DisableAsset(ctx, pe.tok, off.ID, off.Revision)
	if err != nil {
		t.Fatal(err)
	}
	personal, err := fac.CreatePersonalProject(ctx, pe.tok, direct, "个人工程", body, nil)
	if err != nil {
		t.Fatal(err)
	}

	rows, err := fac.ListPromotable(ctx, factory.KindProject)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, r := range rows {
		seen[r.Name] = r.Status
	}
	if seen["草稿工程"] != factory.AssetDraft || seen["可用工程"] != factory.AssetAvailable || seen["停用工程"] != factory.AssetDisabled || seen["个人工程"] != factory.AssetDraft {
		t.Fatalf("%+v", rows)
	}

	if snap, err := fac.SnapshotForChannel(ctx, personal.ID); err != nil || snap.SourceID != personal.ID {
		t.Fatalf("personal: %+v %v", snap, err)
	}
	if snap, err := fac.SnapshotForChannel(ctx, draft.ID); err != nil || snap.Status != factory.AssetDraft || !contentcrypt.IsEnvelope(snap.Content) {
		t.Fatalf("draft snap %+v %v", snap, err)
	}
	if snap, err := fac.SnapshotForChannel(ctx, off.ID); err != nil || snap.Status != factory.AssetDisabled {
		t.Fatalf("disabled snap %+v %v", snap, err)
	}
}
