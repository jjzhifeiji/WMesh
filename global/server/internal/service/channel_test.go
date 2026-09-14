// 建厂码认领、厂钥验签、通道在线与离线。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/nodekey"
)

func TestFactoryEnrollment(t *testing.T) {
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
	if created.EnrollmentToken == "" || created.SuperAdminID.String() == "" {
		t.Fatalf("missing enrollment: %+v", created)
	}
	if _, err := h.WAN.OfferEnroll(ctx, "not-the-code"); !errors.Is(err, domain.ErrInvalidEnrollment) {
		t.Fatalf("bad code: %v", err)
	}
	offer, err := h.WAN.OfferEnroll(ctx, created.EnrollmentToken)
	if err != nil {
		t.Fatal(err)
	}
	if offer.FactoryID != created.Factory.ID || offer.SAPersonID != created.SuperAdminID || offer.SALogin != "sa-a" {
		t.Fatalf("offer mismatch: %+v", offer)
	}
	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := h.WAN.ConfirmEnroll(ctx, offer.FactoryID, pub); err != nil {
		t.Fatal(err)
	}
	if _, err := h.WAN.OfferEnroll(ctx, created.EnrollmentToken); !errors.Is(err, domain.ErrInvalidEnrollment) {
		t.Fatalf("code reused: %v", err)
	}
	if err := h.WAN.ConfirmEnroll(ctx, offer.FactoryID, pub); err != nil {
		t.Fatalf("idempotent confirm: %v", err)
	}
	other, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(pub, other) {
		t.Fatal("keys collided")
	}
	if err := h.WAN.ConfirmEnroll(ctx, offer.FactoryID, other); !errors.Is(err, domain.ErrFactoryKeyExists) {
		t.Fatalf("other key: %v", err)
	}
}

func TestChannelPresence(t *testing.T) {
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
	fid := created.Factory.ID
	if err := h.WAN.RequireFactoryKey(ctx, fid); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("before enroll: %v", err)
	}
	pub, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := h.WAN.ConfirmEnroll(ctx, fid, pub); err != nil {
		t.Fatal(err)
	}
	if err := h.WAN.RequireFactoryKey(ctx, fid); err != nil {
		t.Fatal(err)
	}
	nonce := []byte("0123456789abcdef")
	if err := h.WAN.AcceptHello(ctx, fid, nonce, []byte("bad")); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("bad sig: %v", err)
	}
	sig := nodekey.Sign(priv, helloBytes(fid, nonce))
	if err := h.WAN.AcceptHello(ctx, fid, nonce, sig); err != nil {
		t.Fatal(err)
	}
	if err := h.WAN.MarkChannelOnline(ctx, fid); err != nil {
		t.Fatal(err)
	}
	dir, err := h.WAN.Directory(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if len(dir.Factories) != 1 || !dir.Factories[0].ChannelOnline || dir.Factories[0].ChannelConnectedAt == nil {
		t.Fatalf("online: %+v", dir.Factories)
	}
	if err := h.WAN.TouchChannel(ctx, fid); err != nil {
		t.Fatal(err)
	}
	if err := h.WAN.MarkChannelOffline(ctx, fid); err != nil {
		t.Fatal(err)
	}
	dir, err = h.WAN.Directory(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if dir.Factories[0].ChannelOnline || dir.Factories[0].ChannelDisconnectedAt == nil {
		t.Fatalf("offline: %+v", dir.Factories[0])
	}
	if err := h.WAN.MarkChannelOnline(ctx, fid); err != nil {
		t.Fatal(err)
	}
	if err := h.WAN.ResetChannelPresence(ctx); err != nil {
		t.Fatal(err)
	}
	dir, err = h.WAN.Directory(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if dir.Factories[0].ChannelOnline {
		t.Fatalf("reset left online: %+v", dir.Factories[0])
	}
}

func helloBytes(factoryID [16]byte, nonce []byte) []byte {
	b := make([]byte, 0, 18+16+len(nonce))
	b = append(b, "wmesh-wan-hello-v1"...)
	b = append(b, factoryID[:]...)
	b = append(b, nonce...)
	return b
}
