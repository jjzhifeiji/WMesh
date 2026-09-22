// 工艺/工程作业类型：单层焊道、多层焊缝、T排对接；工程与工艺必须同类型。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/domain"
	global "wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

func TestPlatformWeldKind(t *testing.T) {
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

	def, err := h.WAN.CreatePlatformProcess(ctx, tok, "默认工艺", []byte(`{"name":"p"}`))
	if err != nil {
		t.Fatal(err)
	}
	if def.WeldKind != store.WeldKindSingle {
		t.Fatalf("default %s", def.WeldKind)
	}
	if _, err := h.WAN.CreatePlatformProcess(ctx, tok, "坏类型", []byte(`{"name":"p"}`), "nope"); !errors.Is(err, domain.ErrInvalidWeldKind) {
		t.Fatalf("invalid: %v", err)
	}
	multiP, err := h.WAN.CreatePlatformProcess(ctx, tok, "多层工艺", []byte(`{"name":"m"}`), "multi")
	if err != nil {
		t.Fatal(err)
	}
	if multiP.WeldKind != store.WeldKindMultilayer {
		t.Fatalf("alias %s", multiP.WeldKind)
	}
	copied, err := h.WAN.CopyPlatformProcess(ctx, tok, multiP.ID, "多层副本")
	if err != nil || copied.WeldKind != store.WeldKindMultilayer {
		t.Fatalf("copy %+v %v", copied, err)
	}

	pub, err := h.WAN.PublishPlatformAsset(ctx, tok, multiP.ID, multiP.Revision)
	if err != nil {
		t.Fatal(err)
	}
	singleBody := []byte(`[{"templateId":"` + contenttpl.SeedTplSingle + `","name":"单"}]`)
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "单层工程钉多层", singleBody, []global.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}}, store.WeldKindSingle); !errors.Is(err, domain.ErrWeldKindMismatch) {
		t.Fatalf("dep mismatch: %v", err)
	}
	multiBody := []byte(`[{"templateId":"` + contenttpl.SeedTplMulti + `","name":"多层"}]`)
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "单层工程用多层模版", multiBody, nil, store.WeldKindSingle); !errors.Is(err, domain.ErrWeldKindMismatch) {
		t.Fatalf("content mismatch: %v", err)
	}
	ok, err := h.WAN.CreatePlatformProject(ctx, tok, "多层工程", multiBody, []global.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}}, store.WeldKindMultilayer)
	if err != nil {
		t.Fatal(err)
	}
	if ok.WeldKind != store.WeldKindMultilayer {
		t.Fatalf("project %s", ok.WeldKind)
	}

	changed, err := h.WAN.SetPlatformWeldKind(ctx, tok, def.ID, def.Revision, store.WeldKindTBar)
	if err != nil || changed.WeldKind != store.WeldKindTBar {
		t.Fatalf("set kind %+v %v", changed, err)
	}
	switched, err := h.WAN.SetPlatformWeldKind(ctx, tok, ok.ID, ok.Revision, store.WeldKindSingle)
	if err != nil || switched.WeldKind != store.WeldKindSingle {
		t.Fatalf("project kind %+v %v", switched, err)
	}
	cleared, err := h.WAN.ReadPlatformAssetContent(ctx, tok, switched.ID)
	if err != nil || string(cleared) != "[]" {
		t.Fatalf("project body %s %v", cleared, err)
	}
	if _, err := h.WAN.SetPlatformWeldKind(ctx, tok, changed.ID, changed.Revision, "nope"); !errors.Is(err, domain.ErrInvalidWeldKind) {
		t.Fatalf("bad kind: %v", err)
	}
}
