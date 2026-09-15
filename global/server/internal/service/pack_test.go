// 修订升高只组这一条，不把该厂当前全部可用条再组一遍。
package service_test

import (
	"context"
	"testing"
)

func TestPackAssetForFactoryOnlyThatAsset(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	if err != nil {
		t.Fatal(err)
	}
	created, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	a, err := h.WAN.CreatePlatformProcess(ctx, tok, "焊A", []byte("body-a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.WAN.CreatePlatformProcess(ctx, tok, "焊B", []byte("body-b"))
	if err != nil {
		t.Fatal(err)
	}
	if a, err = h.WAN.PublishPlatformAsset(ctx, tok, a.ID, a.Revision); err != nil {
		t.Fatal(err)
	}
	if b, err = h.WAN.PublishPlatformAsset(ctx, tok, b.ID, b.Revision); err != nil {
		t.Fatal(err)
	}
	all, err := h.WAN.PackAvailableForFactory(ctx, created.Factory.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("pack all %d", len(all))
	}
	snap, err := h.WAN.PackAssetForFactory(ctx, a.ID, created.Factory.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.AssetID != a.ID || snap.Revision != a.Revision {
		t.Fatalf("pack one: %+v", snap)
	}
	renamed, err := h.WAN.RenamePlatformAsset(ctx, tok, a.ID, a.Revision, "焊A2")
	if err != nil {
		t.Fatal(err)
	}
	snap, err = h.WAN.PackAssetForFactory(ctx, a.ID, created.Factory.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.AssetID != a.ID || snap.Revision != renamed.Revision || snap.Members[0].Name != "焊A2" {
		t.Fatalf("renamed pack: %+v", snap)
	}
	hasB, err := h.WAN.HasDistributedTo(ctx, b.ID, created.Factory.ID)
	if err != nil || !hasB {
		t.Fatalf("other still granted: %v", err)
	}
}
