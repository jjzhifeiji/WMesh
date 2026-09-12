// 可复制工艺另存为新草稿，原件不动。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

func TestCopyProcess(t *testing.T) {
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
	other := mustCreateRole(t, ctx, fac, sa, "pe-b", "pe-b-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil)
	direct := factory.WorkContext{Direct: true}
	body := []byte("copy-src-body")

	src, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "源工艺", body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fac.CopyProcess(ctx, pe.tok, src.ID, "新工艺")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == src.ID || got.Revision != 1 || got.Status != factory.AssetDraft ||
		got.Level != factory.AssetLevelFactory || got.Name != "新工艺" || !got.Copyable || got.SourceID != nil {
		t.Fatalf("%+v", got)
	}
	still, err := fac.GetAsset(ctx, pe.tok, src.ID)
	if err != nil || still.Revision != src.Revision || still.Name != src.Name {
		t.Fatalf("src %+v %v", still, err)
	}
	copied, err := fac.ReadAssetContent(ctx, pe.tok, got.ID)
	if err != nil || !bytes.Equal(copied, body) {
		t.Fatalf("%q %v", copied, err)
	}
	if _, err := fac.CopyProcess(ctx, pe.tok, src.ID, "  "); !errors.Is(err, domain.ErrInvalidName) {
		t.Fatalf("empty name: %v", err)
	}

	tight, err := fac.SetAssetCopyable(ctx, pe.tok, src.ID, src.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.CopyProcess(ctx, pe.tok, src.ID, "应拒绝"); !errors.Is(err, domain.ErrAssetNotCopyable) {
		t.Fatalf("not copyable: %v", err)
	}

	pub, err := fac.PublishAsset(ctx, pe.tok, tight.ID, tight.Revision)
	if err != nil {
		t.Fatal(err)
	}
	off, err := fac.DisableAsset(ctx, pe.tok, pub.ID, pub.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.CopyProcess(ctx, pe.tok, off.ID, "停用"); !errors.Is(err, domain.ErrAssetNotAvailable) {
		t.Fatalf("disabled: %v", err)
	}

	mine, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "个人源", body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.CopyProcess(ctx, other.tok, mine.ID, "偷复制"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("other personal: %v", err)
	}
	pers, err := fac.CopyProcess(ctx, pe.tok, mine.ID, "个人副本")
	if err != nil || pers.Level != factory.AssetLevelPersonal || pers.ID == mine.ID {
		t.Fatalf("%+v %v", pers, err)
	}

	platBody := []byte("plat-copy")
	m := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台可复制",
		Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: platBody, Digest: digest.Sum(platBody),
	}
	fid := seed.ID
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		t.Fatal(err)
	}
	fromPlat, err := fac.CopyProcess(ctx, pe.tok, m.ID, "从平台复制")
	if err != nil || fromPlat.Level != factory.AssetLevelFactory || fromPlat.Status != factory.AssetDraft {
		t.Fatalf("%+v %v", fromPlat, err)
	}
	gotPlat, err := fac.ReadAssetContent(ctx, pe.tok, fromPlat.ID)
	if err != nil || !bytes.Equal(gotPlat, platBody) {
		t.Fatalf("%q %v", gotPlat, err)
	}
}
