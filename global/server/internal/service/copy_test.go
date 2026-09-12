// 可复制平台级工艺另存为新草稿，原件不动。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/domain"
	global "wmesh/global/internal/service"
)

func TestCopyPlatformProcess(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", wanPass)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("wan-copy-src")
	src, err := h.WAN.CreatePlatformProcess(ctx, tok, "源工艺", body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WAN.CopyPlatformProcess(ctx, tok, src.ID, "应拒绝"); !errors.Is(err, domain.ErrAssetNotCopyable) {
		t.Fatalf("default not copyable: %v", err)
	}
	on, err := h.WAN.SetPlatformCopyable(ctx, tok, src.ID, src.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.WAN.CopyPlatformProcess(ctx, tok, on.ID, "新工艺")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == on.ID || got.Revision != 1 || got.Status != global.AssetDraft || got.Copyable || got.Name != "新工艺" {
		t.Fatalf("%+v", got)
	}
	still, err := h.WAN.GetPlatformAsset(ctx, tok, on.ID)
	if err != nil || still.Revision != on.Revision || still.Name != on.Name {
		t.Fatalf("src %+v %v", still, err)
	}
	copied, err := h.WAN.ReadPlatformAssetContent(ctx, tok, got.ID)
	if err != nil || !bytes.Equal(copied, applyProcess(body)) {
		t.Fatalf("%q %v", copied, err)
	}
	if _, err := h.WAN.CopyPlatformProcess(ctx, tok, on.ID, "  "); !errors.Is(err, domain.ErrInvalidName) {
		t.Fatalf("empty name: %v", err)
	}
	pub, err := h.WAN.PublishPlatformAsset(ctx, tok, on.ID, on.Revision)
	if err != nil {
		t.Fatal(err)
	}
	off, err := h.WAN.DisablePlatformAsset(ctx, tok, pub.ID, pub.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WAN.CopyPlatformProcess(ctx, tok, off.ID, "停用"); !errors.Is(err, domain.ErrAssetNotAvailable) {
		t.Fatalf("disabled: %v", err)
	}
}
