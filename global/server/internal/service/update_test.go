// 软件更新服务层：WAN 发布与按厂下发（U1 / U5 / U11 / U13 / U15）。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/nodekey"
	global "wmesh/global/internal/service"
)

func TestMatrixSoftware(t *testing.T) {
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
	b, err := h.WAN.CreateFactory(ctx, tok, "厂B", "sa-b", "超管B")
	if err != nil {
		t.Fatal(err)
	}
	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := h.WAN.ConfirmEnroll(ctx, a.Factory.ID, pub); err != nil {
		t.Fatal(err)
	}

	t.Run("U1", func(t *testing.T) {
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.5.0", 5, []byte("svc-5")); err != nil {
			t.Fatal(err)
		}
		snap, err := h.WAN.DistributeSoftware(ctx, tok, global.SoftwareFactoryService, 5, a.Factory.ID)
		if err != nil || snap.Version != 5 || snap.TargetFactoryID != a.Factory.ID {
			t.Fatalf("%+v %v", snap, err)
		}
	})
	t.Run("U5", func(t *testing.T) {
		// WAN 无确认入口；下发不等于安装。
		if _, err := h.WAN.DistributeSoftware(ctx, tok, global.SoftwareFactoryService, 5, a.Factory.ID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("U13", func(t *testing.T) {
		s1, err := h.WAN.DistributeSoftware(ctx, tok, global.SoftwareFactoryService, 5, a.Factory.ID)
		if err != nil {
			t.Fatal(err)
		}
		s2, err := h.WAN.DistributeSoftware(ctx, tok, global.SoftwareFactoryService, 5, a.Factory.ID)
		if err != nil || s2.Version != s1.Version {
			t.Fatalf("%+v %v", s2, err)
		}
	})
	t.Run("U11", func(t *testing.T) {
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.4.0", 4, []byte("svc-4")); err != nil {
			t.Fatal(err)
		}
		if _, err := h.WAN.DistributeSoftware(ctx, tok, global.SoftwareFactoryService, 4, a.Factory.ID); !errors.Is(err, domain.ErrStaleRevision) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("U15", func(t *testing.T) {
		if _, err := h.WAN.DistributeSoftware(ctx, tok, global.SoftwareFactoryService, 5, b.Factory.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("unclaimed %v", err)
		}
		ghost := a.Factory.ID
		_ = ghost
		if _, err := h.WAN.PublishSoftware(ctx, tok, "process", "x", 1, []byte("no")); !errors.Is(err, domain.ErrInvalidName) {
			t.Fatalf("kind %v", err)
		}
	})

	snaps, err := h.WAN.Updates.SnapshotsForFactory(ctx, a.Factory.ID)
	if err != nil || len(snaps) != 1 || snaps[0].Version != 5 || len(snaps[0].Body) == 0 {
		t.Fatalf("replay snaps %+v %v", snaps, err)
	}

	rows, err := h.WAN.ListAudit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !audit.HasResult(rows, "distribute_software", audit.Allow) || !audit.HasResult(rows, "distribute_software", audit.Deny) {
		t.Fatal("missing distribute audit")
	}
}
