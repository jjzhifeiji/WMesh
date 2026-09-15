// 工厂停用、启用、删除：WAN 权威，厂端按修订落地。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/service"
)

func TestFactoryLifecycle(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	if err != nil {
		t.Fatal(err)
	}

	a, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	if a.Factory.Status != service.FactoryActive {
		t.Fatalf("new status %s", a.Factory.Status)
	}
	disabled, err := h.WAN.DisableFactory(ctx, tok, a.Factory.ID)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Status != service.FactoryDisabled || disabled.LifecycleRevision != 1 {
		t.Fatalf("disable %+v", disabled)
	}
	if _, err := h.WAN.OfferEnroll(ctx, a.EnrollmentToken); !errors.Is(err, domain.ErrFactoryDisabled) {
		t.Fatalf("enroll while disabled: %v", err)
	}
	enabled, err := h.WAN.EnableFactory(ctx, tok, a.Factory.ID)
	if err != nil {
		t.Fatal(err)
	}
	if enabled.Status != service.FactoryActive || enabled.LifecycleRevision != 2 {
		t.Fatalf("enable %+v", enabled)
	}
	offer, err := h.WAN.OfferEnroll(ctx, a.EnrollmentToken)
	if err != nil {
		t.Fatal(err)
	}
	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := h.WAN.ConfirmEnroll(ctx, offer.FactoryID, pub); err != nil {
		t.Fatal(err)
	}
	if err := h.WAN.RequireFactoryKey(ctx, a.Factory.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.WAN.DisableFactory(ctx, tok, a.Factory.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.WAN.RequireFactoryKey(ctx, a.Factory.ID); err != nil {
		t.Fatalf("key while disabled: %v", err)
	}
	retired, err := h.WAN.DeleteFactory(ctx, tok, a.Factory.ID)
	if err != nil || retired == nil || retired.Status != service.FactoryRetired {
		t.Fatalf("retire enrolled: %+v %v", retired, err)
	}
	if _, err := h.WAN.EnableFactory(ctx, tok, a.Factory.ID); !errors.Is(err, domain.ErrFactoryRetired) {
		t.Fatalf("enable retired: %v", err)
	}
	if err := h.WAN.RequireFactoryKey(ctx, a.Factory.ID); !errors.Is(err, domain.ErrFactoryRetired) {
		t.Fatalf("key retired: %v", err)
	}
	dir, err := h.WAN.Directory(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range dir.Factories {
		if f.ID == a.Factory.ID {
			found = true
			if f.Status != service.FactoryRetired {
				t.Fatalf("listed status %s", f.Status)
			}
		}
	}
	if !found {
		t.Fatal("retired factory missing from directory")
	}

	b, err := h.WAN.CreateFactory(ctx, tok, "厂B", "sa-b", "超管B")
	if err != nil {
		t.Fatal(err)
	}
	gone, err := h.WAN.DeleteFactory(ctx, tok, b.Factory.ID)
	if err != nil || gone != nil {
		t.Fatalf("delete unclaimed: %+v %v", gone, err)
	}
	dir, err = h.WAN.Directory(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range dir.Factories {
		if f.ID == b.Factory.ID {
			t.Fatal("unclaimed factory still listed")
		}
	}
	if err := h.WAN.RequireFactoryKey(ctx, b.Factory.ID); !errors.Is(err, domain.ErrFactoryRetired) {
		t.Fatalf("key after delete: %v", err)
	}
}
