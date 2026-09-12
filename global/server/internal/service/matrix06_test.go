// 阶段3第6圈：WAN 侧升平台、不持厂内原件、平台工程依赖（9.2、10.1、10.3、12.1～12.2、13.6、14.1、18.1～18.2）。
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

func testWANAssetPromote(t *testing.T, run func(string, func(*testing.T))) {
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
	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"name":"焊接","current":180}`)
	d := digest.Sum(body)
	procSrc := id.New()

	run("9.2", func(t *testing.T) {
		snap := global.AssetSnapshot{
			SourceID: id.New(), SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProcess, Name: "不可复制", Content: body, Digest: d,
			Copyable: false, Status: global.AssetAvailable,
		}
		if _, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap); !errors.Is(err, domain.ErrAssetNotCopyable) {
			t.Fatalf("got %v", err)
		}
	})

	var plat global.Asset
	run("10.1", func(t *testing.T) {
		snap := global.AssetSnapshot{
			SourceID: procSrc, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProcess, Name: "焊接", Content: body, Digest: d,
			Copyable: true, Status: global.AssetAvailable,
		}
		var err error
		plat, err = h.WAN.PromoteFromSnapshot(ctx, tok, snap)
		if err != nil {
			t.Fatal(err)
		}
		if plat.ID == procSrc || plat.Copyable || plat.Level != global.AssetLevelPlatform ||
			plat.Status != global.AssetDraft || plat.SourceID == nil || *plat.SourceID != procSrc || plat.Content != nil {
			t.Fatalf("%+v", plat)
		}
	})
	run("10.4", func(t *testing.T) {
		snap := global.AssetSnapshot{
			SourceID: procSrc, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProcess, Name: "焊接", Content: body, Digest: d,
			Copyable: true, Status: global.AssetAvailable,
		}
		again, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap)
		if err != nil {
			t.Fatal(err)
		}
		if again.ID != plat.ID || again.Revision != plat.Revision {
			t.Fatalf("skip %+v want %+v", again, plat)
		}
	})
	run("10.5", func(t *testing.T) {
		next := []byte(`{"name":"焊接","current":220}`)
		snap := global.AssetSnapshot{
			SourceID: procSrc, SourceRevision: 2, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProcess, Name: "焊接", Content: next, Digest: digest.Sum(next),
			Copyable: true, Status: global.AssetAvailable,
		}
		got, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != plat.ID || got.Revision != plat.Revision+1 || got.Status != global.AssetDraft || !bytes.Equal(got.Digest, digest.Sum(next)) {
			t.Fatalf("overwrite %+v", got)
		}
		plat = got
	})
	run("10.3", func(t *testing.T) {
		if err := h.WAN.UpdateFactoryAsset(ctx, tok, fac.Factory.ID, procSrc); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("12.1", func(t *testing.T) {
		if err := h.WAN.GetFactoryAsset(ctx, tok, fac.Factory.ID, procSrc); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("get: %v", err)
		}
		if err := h.WAN.ReadFactoryAssetContent(ctx, tok, fac.Factory.ID, procSrc); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("read: %v", err)
		}
		if _, err := h.WAN.GetPlatformAsset(ctx, tok, procSrc); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("src as platform: %v", err)
		}
	})
	run("12.2", func(t *testing.T) {
		got, err := h.WAN.GetPlatformAsset(ctx, tok, plat.ID)
		if err != nil || got.ID != plat.ID || got.ID == procSrc || got.Content != nil {
			t.Fatalf("%+v %v", got, err)
		}
	})
	run("13.6", func(t *testing.T) {
		if _, err := h.WAN.CreatePlatformProject(ctx, tok, "依赖厂级", body, []global.AssetDep{{
			ID: procSrc, Revision: 1, Digest: d,
		}}); !errors.Is(err, domain.ErrAssetDependency) && !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v", err)
		}
	})
	run("14.1", func(t *testing.T) {
		var err error
		plat, err = h.WAN.PublishPlatformAsset(ctx, tok, plat.ID, plat.Revision)
		if err != nil {
			t.Fatal(err)
		}
		projSrc := id.New()
		snap := global.AssetSnapshot{
			SourceID: projSrc, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProject, Name: "工程", Content: []byte("job-params"), Digest: digest.Sum([]byte("job-params")),
			Copyable: true, Status: global.AssetAvailable,
			Deps: []global.AssetDep{{ID: procSrc, Revision: 1, Digest: d}},
		}
		got, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap)
		if err != nil {
			t.Fatal(err)
		}
		if got.Kind != global.KindProject || got.Status != global.AssetDraft || got.ID == projSrc || len(got.Deps) != 1 ||
			got.Deps[0].ID != plat.ID || got.Deps[0].ID == procSrc {
			t.Fatalf("%+v", got)
		}
	})
	run("18.1", func(t *testing.T) {
		rows, err := h.WAN.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if bad := audit.Incomplete(rows); len(bad) > 0 {
			t.Fatalf("incomplete %#v", bad[0])
		}
		if audit.ContainsAny(audit.Dump(rows), string(body), wanPass, tok) {
			t.Fatalf("secret leaked")
		}
	})
	run("18.2", func(t *testing.T) {
		rows, err := h.WAN.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains([]byte(audit.Dump(rows)), []byte("rev=")) {
			t.Fatalf("missing revision in audit")
		}
	})
}
