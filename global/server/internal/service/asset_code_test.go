// 工艺工程只读编号：云端发号、另存/升档换新号、改名不改号。
package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"wmesh/global/internal/platform/assetcode"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

func TestPlatformAssetCodes(t *testing.T) {
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

	p1, err := h.WAN.CreatePlatformProcess(ctx, tok, "焊1", []byte(`{"name":"p","current":180}`))
	if err != nil {
		t.Fatal(err)
	}
	if p1.Code != "GY-W-000001" || !assetcode.Valid(p1.Code) {
		t.Fatalf("process code %s", p1.Code)
	}
	forced, err := h.WAN.Store().InsertAsset(ctx, global.Asset{
		Kind: global.KindProcess, Name: "指定号", Status: global.AssetDraft,
		Content: []byte("x"), Digest: digest.Sum([]byte("x")), CreatorID: p1.CreatorID, Code: "GY-W-009999",
	})
	if err != nil {
		t.Fatal(err)
	}
	if forced.Code != "GY-W-000002" {
		t.Fatalf("caller code ignored: %s", forced.Code)
	}
	j1, err := h.WAN.CreatePlatformProject(ctx, tok, "工程1", []byte(`[{"name":"焊道"}]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if j1.Code != "GC-W-000001" {
		t.Fatalf("project code %s", j1.Code)
	}
	p2, err := h.WAN.CreatePlatformProcess(ctx, tok, "焊2", []byte(`{"name":"p2","current":181}`))
	if err != nil {
		t.Fatal(err)
	}
	if p2.Code != "GY-W-000003" {
		t.Fatalf("third process %s", p2.Code)
	}

	renamed, err := h.WAN.RenamePlatformAsset(ctx, tok, p1.ID, p1.Revision, "改名")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Code != p1.Code || renamed.Revision != p1.Revision+1 {
		t.Fatalf("rename changed code: %+v", renamed)
	}

	on, err := h.WAN.SetPlatformCopyable(ctx, tok, p1.ID, renamed.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	copied, err := h.WAN.CopyPlatformProcess(ctx, tok, p1.ID, "副本")
	if err != nil {
		t.Fatal(err)
	}
	if copied.Code == p1.Code || copied.ID == p1.ID || !strings.HasPrefix(copied.Code, "GY-W-") {
		t.Fatalf("copy code %+v vs %s", copied, p1.Code)
	}
	still, err := h.WAN.GetPlatformAsset(ctx, tok, p1.ID)
	if err != nil || still.Code != p1.Code {
		t.Fatalf("src after copy %+v %v", still, err)
	}
	_ = on

	a, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.WAN.CreateFactory(ctx, tok, "厂B", "sa-b", "超管B")
	if err != nil {
		t.Fatal(err)
	}
	if a.Factory.ShortCode != "F01" || b.Factory.ShortCode != "F02" {
		t.Fatalf("factory codes %s %s", a.Factory.ShortCode, b.Factory.ShortCode)
	}
	c, err := h.WAN.RegisterClient(ctx, tok, "焊机", id.New(), a.Factory.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.ShortCode != "C0001" || !assetcode.ValidClientOrigin(c.ShortCode) {
		t.Fatalf("client short %s", c.ShortCode)
	}

	srcID := id.New()
	plain := applyProcess([]byte(`{"name":"升","current":190}`))
	sum := digest.Sum(plain)
	snap := global.AssetSnapshot{
		SourceID: srcID, SourceRevision: 1, SourceFactoryID: a.Factory.ID,
		Kind: global.KindProcess, Name: "厂级升", Content: plain, Digest: sum,
		Copyable: true, Status: global.AssetAvailable,
	}
	promoted, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap)
	if err != nil {
		t.Fatal(err)
	}
	if promoted.ID == srcID || !strings.HasPrefix(promoted.Code, "GY-W-") {
		t.Fatalf("promote %+v", promoted)
	}
	again, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap)
	if err != nil || again.ID != promoted.ID || again.Code != promoted.Code {
		t.Fatalf("re-promote %+v %v", again, err)
	}

	got, err := h.WAN.Store().AssetByCode(ctx, p1.Code)
	if err != nil || got.ID != p1.ID {
		t.Fatalf("lookup %v %v", got, err)
	}
	list, err := h.WAN.ListPlatformAssets(ctx, tok, "process")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, row := range list {
		if row.Code == p1.Code {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("list by code %d", n)
	}
	if _, err := h.WAN.Store().AssetByCode(ctx, "GY-W-999999"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}

	pub, err := h.WAN.PublishPlatformAsset(ctx, tok, p1.ID, still.Revision)
	if err != nil {
		t.Fatal(err)
	}
	dep := global.AssetDep{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "编号当引用", []byte(`[{"templateId":"`+seedSingleID+`","processId":"`+p1.Code+`"}]`), []global.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("code as processId: %v", err)
	}
}
