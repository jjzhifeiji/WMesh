// 云端非可用不进厂列表，删除只撤回展示。
package service_test

import (
	"context"
	"testing"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

func TestPlatformReplicaSync(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	created, fac, err := h.Provision(ctx, "sa-a", "初始超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa-a", created.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	tok, err := fac.Login(ctx, "sa-a", "sa-pass")
	if err != nil {
		t.Fatal(err)
	}
	fid := created.ID
	body := []byte("plat-sync")
	m := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台工艺",
		Status: factory.AssetAvailable, Copyable: false, Revision: 1, Content: body, Digest: digest.Sum(body),
	}
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		t.Fatal(err)
	}
	listed, err := fac.ListAssets(ctx, tok, factory.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range listed {
		if a.ID == m.ID && a.Status == factory.AssetAvailable {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("available replica missing")
	}
	disabled := m
	disabled.Revision = 2
	disabled.Status = factory.AssetDisabled
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, disabled, nil, &fid, nil)); err != nil {
		t.Fatal(err)
	}
	listed, err = fac.ListAssets(ctx, tok, factory.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range listed {
		if a.ID == m.ID {
			t.Fatal("disabled replica still listed")
		}
	}
	if err := fac.RetractPlatformDelivery(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	listed, err = fac.ListAssets(ctx, tok, factory.KindProcess)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range listed {
		if a.ID == m.ID {
			t.Fatal("retracted replica still listed")
		}
	}
	got, err := fac.GetReplica(ctx, tok, m.ID, 1)
	if err != nil || got.ID != m.ID || got.Revision != 1 {
		t.Fatalf("pinned replica %+v %v", got, err)
	}
}
