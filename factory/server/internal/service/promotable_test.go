// 通道升档：只出可用且可复制的厂级，不含个人级正文。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/domain"
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
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil)
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

	rows, err := fac.ListPromotable(ctx, factory.KindProcess)
	if err != nil || len(rows) != 1 || rows[0].ID != avail.ID || rows[0].Name != "可升" || len(rows[0].Digest) != 32 {
		t.Fatalf("%+v %v", rows, err)
	}

	if _, err := fac.SnapshotForChannel(ctx, draft.ID); !errors.Is(err, domain.ErrAssetNotAvailable) {
		t.Fatalf("draft: %v", err)
	}
	if _, err := fac.SnapshotForChannel(ctx, personal.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("personal: %v", err)
	}
	snap, err := fac.SnapshotForChannel(ctx, avail.ID)
	if err != nil || snap.SourceID != avail.ID || !bytes.Equal(snap.Content, body) {
		t.Fatalf("%+v %v", snap, err)
	}
}
