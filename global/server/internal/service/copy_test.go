// 云端工艺/工程另存为新草稿，不看可复制，原件不动。
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
	if src.Copyable {
		t.Fatal("default copyable")
	}
	got, err := h.WAN.CopyPlatformProcess(ctx, tok, src.ID, "新工艺")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == src.ID || got.Revision != 1 || got.Status != global.AssetDraft || got.Copyable || got.Name != "新工艺" || got.Kind != global.KindProcess {
		t.Fatalf("%+v", got)
	}
	still, err := h.WAN.GetPlatformAsset(ctx, tok, src.ID)
	if err != nil || still.Revision != src.Revision || still.Name != src.Name || still.Copyable {
		t.Fatalf("src %+v %v", still, err)
	}
	copied, err := h.WAN.ReadPlatformAssetContent(ctx, tok, got.ID)
	if err != nil || !bytes.Equal(copied, applyProcess(body)) {
		t.Fatalf("%q %v", copied, err)
	}
	if _, err := h.WAN.CopyPlatformProcess(ctx, tok, src.ID, "  "); !errors.Is(err, domain.ErrInvalidName) {
		t.Fatalf("empty name: %v", err)
	}
	pub, err := h.WAN.PublishPlatformAsset(ctx, tok, src.ID, src.Revision)
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

func TestCopyPlatformProject(t *testing.T) {
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
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "钉工艺", []byte(`{"name":"p"}`))
	if err != nil {
		t.Fatal(err)
	}
	pub, err := h.WAN.PublishPlatformAsset(ctx, tok, proc.ID, proc.Revision)
	if err != nil {
		t.Fatal(err)
	}
	src, err := h.WAN.CreatePlatformProject(ctx, tok, "源工程", []byte(`[]`), []global.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	if src.Copyable || src.Kind != global.KindProject || len(src.Deps) != 1 {
		t.Fatalf("%+v", src)
	}
	got, err := h.WAN.CopyPlatformProcess(ctx, tok, src.ID, "新工程")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == src.ID || got.Kind != global.KindProject || got.Status != global.AssetDraft || got.Copyable || got.Name != "新工程" {
		t.Fatalf("%+v", got)
	}
	if len(got.Deps) != 1 || got.Deps[0].ID != pub.ID || got.Deps[0].Revision != pub.Revision {
		t.Fatalf("deps %+v", got.Deps)
	}
	want, err := h.WAN.ReadPlatformAssetContent(ctx, tok, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	copied, err := h.WAN.ReadPlatformAssetContent(ctx, tok, got.ID)
	if err != nil || !bytes.Equal(copied, want) {
		t.Fatalf("%q vs %q %v", copied, want, err)
	}
	still, err := h.WAN.GetPlatformAsset(ctx, tok, src.ID)
	if err != nil || still.Revision != src.Revision || still.Name != src.Name {
		t.Fatalf("src %+v %v", still, err)
	}
}

func TestCreatePlatformProcessCopyable(t *testing.T) {
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
	body := []byte("wan-create-copyable")
	wide, err := h.WAN.CreatePlatformProcessWith(ctx, tok, "开焊", body, true)
	if err != nil || !wide.Copyable || wide.Revision != 1 || wide.Status != global.AssetDraft {
		t.Fatalf("%+v %v", wide, err)
	}
	tight, err := h.WAN.CreatePlatformProcess(ctx, tok, "密焊", body)
	if err != nil || tight.Copyable || tight.Revision != 1 {
		t.Fatalf("%+v %v", tight, err)
	}
}
