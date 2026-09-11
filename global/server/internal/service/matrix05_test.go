// 阶段3第5圈：WAN 不得代建厂级、不得读厂内正文（5.2、7.2）。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
)

func testWANAssetAuthorship(t *testing.T, run func(string, func(*testing.T))) {
	t.Helper()
	ctx := context.Background()
	h := New(t)
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	if err != nil {
		t.Fatal(err)
	}
	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	run("5.2", func(t *testing.T) {
		if err := h.WAN.CreateFactoryProcess(ctx, tok, fac.Factory.ID, "焊接", []byte("x")); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("7.2", func(t *testing.T) {
		if err := h.WAN.ReadFactoryAssetContent(ctx, tok, fac.Factory.ID, id.New()); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
}
