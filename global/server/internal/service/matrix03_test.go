// 阶段3第4圈：WAN 侧平台级制作、升档快照、完整性（4.1、4.3、4.4、1.3、16.1～16.2）。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

func testWANAssetIdentity(t *testing.T, run func(string, func(*testing.T))) {
	t.Helper()
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
	body := []byte("platform-weld-secret")

	var proc global.Asset
	run("4.1", func(t *testing.T) {
		var err error
		proc, err = h.WAN.CreatePlatformProcess(ctx, tok, "焊接", body)
		if err != nil {
			t.Fatal(err)
		}
		if proc.ID.String() == "" || proc.Revision != 1 || proc.Status != global.AssetDraft ||
			proc.Level != global.AssetLevelPlatform || proc.Copyable || proc.Name != "焊接" || proc.Content != nil {
			t.Fatalf("%+v", proc)
		}
	})
	proc, err = h.WAN.PublishPlatformAsset(ctx, tok, proc.ID, proc.Revision)
	if err != nil {
		t.Fatal(err)
	}
	run("4.3", func(t *testing.T) {
		got, err := h.WAN.SetPlatformCopyable(ctx, tok, proc.ID, proc.Revision, true)
		if err != nil || !got.Copyable {
			t.Fatalf("%+v %v", got, err)
		}
		proc = got
	})
	run("4.4", func(t *testing.T) {
		draft, err := h.WAN.CreatePlatformProcess(ctx, tok, "可复制草稿", []byte("draft-copy"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := h.WAN.SetPlatformCopyable(ctx, tok, draft.ID, draft.Revision, true)
		if err != nil || !got.Copyable {
			t.Fatalf("%+v %v", got, err)
		}
		pub, err := h.WAN.PublishPlatformAsset(ctx, tok, got.ID, got.Revision)
		if err != nil {
			t.Fatal(err)
		}
		tight, err := h.WAN.SetPlatformCopyable(ctx, tok, pub.ID, pub.Revision, false)
		if err != nil || tight.Copyable {
			t.Fatalf("%+v %v", tight, err)
		}
		wide, err := h.WAN.SetPlatformCopyable(ctx, tok, tight.ID, tight.Revision, true)
		if err != nil || !wide.Copyable {
			t.Fatalf("%+v %v", wide, err)
		}
	})
	run("16.2", func(t *testing.T) {
		gotBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proc.ID)
		if err != nil || !bytes.Equal(gotBody, applyProcess(body)) {
			t.Fatalf("%q %v", gotBody, err)
		}
		rows, err := h.WAN.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if bad := audit.Incomplete(rows); len(bad) > 0 {
			t.Fatalf("incomplete %#v", bad[0])
		}
		if audit.ContainsAny(audit.Dump(rows), string(body), wanPass, tok) {
			t.Fatalf("secret or body leaked")
		}
	})
	run("1.3", func(t *testing.T) {
		fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
		if err != nil {
			t.Fatal(err)
		}
		srcID := id.New()
		snap := global.AssetSnapshot{
			SourceID: srcID, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProcess, Name: "焊接", Content: body, Digest: digest.Sum(body),
			Copyable: true, Status: global.AssetAvailable,
		}
		promoted, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap)
		if err != nil {
			t.Fatal(err)
		}
		if promoted.ID == srcID || promoted.Level != global.AssetLevelPlatform || promoted.Revision != 1 ||
			promoted.Copyable || promoted.Status != global.AssetDraft || promoted.Content != nil ||
			promoted.SourceID == nil || *promoted.SourceID != srcID ||
			promoted.SourceRevision == nil || *promoted.SourceRevision != 1 ||
			promoted.SourceFactoryID == nil || *promoted.SourceFactoryID != fac.Factory.ID {
			t.Fatalf("%+v", promoted)
		}
		listed, err := h.WAN.ListPlatformAssets(ctx, tok, global.KindProcess)
		if err != nil {
			t.Fatal(err)
		}
		var listedName string
		for _, a := range listed {
			if a.ID == promoted.ID {
				listedName = a.SourceFactoryName
			}
		}
		if listedName != "厂A" {
			t.Fatalf("source factory display %q", listedName)
		}
		if err := h.WAN.GetFactoryAsset(ctx, tok, fac.Factory.ID, srcID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("16.1", func(t *testing.T) {
		if err := h.WAN.Store().TamperAssetContent(ctx, proc.ID, []byte("tampered")); err != nil {
			t.Fatal(err)
		}
		if _, err := h.WAN.GetPlatformAsset(ctx, tok, proc.ID); !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("get: %v", err)
		}
		if _, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proc.ID); !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("read: %v", err)
		}
	})
}
