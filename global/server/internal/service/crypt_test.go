// WAN 库平台级仍明文；同一把租约钥续期；过站信封到站解开后明文入库。
package service_test

import (
	"bytes"
	"context"
	"testing"

	"wmesh/global/internal/platform/contentcrypt"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

func TestContentLeaseAndWANPlaintext(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("platform-weld-secret")
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "焊接", body)
	if err != nil {
		t.Fatal(err)
	}
	row, err := h.WAN.Store().AssetByID(ctx, proc.ID)
	if err != nil || contentcrypt.IsEnvelope(row.Content) {
		t.Fatalf("wan must store plaintext: %v", err)
	}

	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := h.WAN.IssueContentLease(ctx, fac.Factory.ID)
	if err != nil || len(lease.Key) != contentcrypt.KeySize {
		t.Fatalf("lease %v", err)
	}
	again, err := h.WAN.IssueContentLease(ctx, fac.Factory.ID)
	if err != nil || !bytes.Equal(lease.Key, again.Key) {
		t.Fatalf("lease rotated")
	}

	srcID := id.New()
	plain := []byte("promote-plain")
	env, err := contentcrypt.Seal(lease.Key, plain, contentcrypt.TransitAAD(fac.Factory.ID, srcID, 1))
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := h.WAN.PromoteFromSnapshot(ctx, tok, global.AssetSnapshot{
		SourceID: srcID, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
		Kind: global.KindProcess, Name: "升档焊", Content: env, Digest: digest.Sum(plain),
		Copyable: true, Status: global.AssetAvailable,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.WAN.ReadPlatformAssetContent(ctx, tok, promoted.ID)
	if err != nil || !bytes.Equal(got, plain) || contentcrypt.IsEnvelope(got) {
		t.Fatalf("promoted %q %v", got, err)
	}
}
