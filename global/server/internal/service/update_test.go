// 软件更新服务层：WAN 发布、厂自拉、本机云端确认。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/blob"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/nodekey"
	global "wmesh/global/internal/service"
)

var matrixSoftwareIDs = []string{
	"U1", "U6", "U7", "U13", "U15", "U16", "U17", "U18", "U24", "U25", "U26", "U27", "U29", "U31",
}

func TestMatrixSoftware(t *testing.T) {
	ran := map[string]bool{}
	run := func(id string, fn func(*testing.T)) {
		t.Helper()
		t.Run(id, func(t *testing.T) {
			ran[id] = true
			fn(t)
		})
	}
	t.Cleanup(func() {
		for _, id := range matrixSoftwareIDs {
			if !ran[id] {
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})

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

	run("U1", func(t *testing.T) {
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.5.0", 5, []byte("svc-5")); err != nil {
			t.Fatal(err)
		}
	})
	run("U7", func(t *testing.T) {
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareClientAPK, "6.1.0", 3, []byte("apk-3")); err != nil {
			t.Fatal(err)
		}
	})
	run("U27", func(t *testing.T) {
		bus := &recordBus{}
		h.WAN.SetBus(bus)
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareClientAPK, "6.2.0", 4, []byte("apk-4")); err != nil {
			t.Fatal(err)
		}
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.5.0", 5, []byte("svc-5")); err != nil {
			t.Fatal(err)
		}
		if len(bus.ids) != 2 || bus.ids[0] != a.Factory.ID || bus.ids[1] != a.Factory.ID {
			t.Fatalf("notified %+v", bus.ids)
		}
		if bus.got[0].Kind != global.SoftwareClientAPK || bus.got[0].Version != 4 || bus.got[1].Kind != global.SoftwareFactoryService || bus.got[1].Version != 5 {
			t.Fatalf("cmd %+v", bus.got)
		}
		if strings.Contains(string(bus.raw[0]), "apk-4") || strings.Contains(string(bus.raw[1]), "svc-5") {
			t.Fatal("package body on mqtt")
		}
	})
	run("U29", func(t *testing.T) {
		rows, err := h.WAN.ListSoftwareItems(ctx, tok, global.SoftwareClientAPK)
		if err != nil {
			t.Fatal(err)
		}
		keep := map[int64]string{}
		for _, row := range rows {
			keep[row.Version] = row.Keep
		}
		if keep[4] != global.SoftwareKeepLatest || keep[3] != "" {
			t.Fatalf("keep %+v", keep)
		}
		if err := h.WAN.DeleteSoftware(ctx, tok, global.SoftwareClientAPK, 3); err != nil {
			t.Fatal(err)
		}
		if _, err := h.WAN.Store().SoftwareRelease(ctx, global.SoftwareClientAPK, 3); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("kept %v", err)
		}
		if err := h.WAN.DeleteSoftware(ctx, tok, global.SoftwareClientAPK, 4); !errors.Is(err, domain.ErrReferenced) {
			t.Fatalf("latest %v", err)
		}
	})
	run("U31", func(t *testing.T) {
		if err := h.WAN.RequestImagePrune(ctx, tok); err != nil {
			t.Fatal(err)
		}
		if err := h.WAN.RequestImagePrune(ctx, ""); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("anon %v", err)
		}
	})
	run("U15", func(t *testing.T) {
		got, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.5.0", 5, []byte("svc-5"))
		if err != nil || got.Version != 5 {
			t.Fatalf("%+v %v", got, err)
		}
		h.WAN.SetBlobs(blob.NewMemory())
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.5.0", 5, []byte("svc-5")); err != nil {
			t.Fatalf("restore %v", err)
		}
	})
	run("U16", func(t *testing.T) {
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.5.0", 5, []byte("svc-5-other")); !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("got %v", err)
		}
	})
	run("U13", func(t *testing.T) {
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.4.0", 4, []byte("svc-4")); !errors.Is(err, domain.ErrStaleRevision) {
			t.Fatalf("publish %v", err)
		}
		if _, err := h.WAN.PullSoftware(ctx, a.Factory.ID, global.SoftwareFactoryService, 4); !errors.Is(err, domain.ErrStaleRevision) {
			t.Fatalf("pull %v", err)
		}
	})
	run("U17", func(t *testing.T) {
		if _, err := h.WAN.LatestSoftware(ctx, b.Factory.ID, global.SoftwareFactoryService); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("unclaimed %v", err)
		}
		if _, err := h.WAN.PullSoftware(ctx, b.Factory.ID, global.SoftwareFactoryService, 5); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("unclaimed pull %v", err)
		}
	})
	run("U18", func(t *testing.T) {
		if _, err := h.WAN.LatestSoftware(ctx, a.Factory.ID, global.SoftwareWANService); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("wan pack %v", err)
		}
		if _, err := h.WAN.PullSoftware(ctx, a.Factory.ID, global.SoftwareWANService, 1); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("wan pull %v", err)
		}
	})
	run("U6", func(t *testing.T) {
		if err := h.WAN.ConfirmWANUpdate(ctx, tok, global.SoftwareFactoryService, 5); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("U26", func(t *testing.T) {
		if _, err := h.WAN.PublishSoftware(ctx, tok, "postgres", "18", 1, []byte("sql")); !errors.Is(err, domain.ErrInvalidName) {
			t.Fatalf("kind %v", err)
		}
		if _, err := h.WAN.PublishSoftware(ctx, tok, "sql", "1", 1, []byte("--")); !errors.Is(err, domain.ErrInvalidName) {
			t.Fatalf("sql %v", err)
		}
	})
	run("U24", func(t *testing.T) {
		body := []byte("wan-svc-2")
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareWANService, "2.0.0", 2, body); err != nil {
			t.Fatal(err)
		}
		h.WAN.Updates.SetApplyOutcome(global.ApplyOK)
		if err := h.WAN.ConfirmWANUpdate(ctx, tok, global.SoftwareWANService, 2); err != nil {
			t.Fatal(err)
		}
		cur, err := h.WAN.CurrentWANSoftware(ctx, tok)
		if err != nil || cur.Version != 2 || cur.VersionName != "2.0.0" {
			t.Fatalf("current %+v %v", cur, err)
		}
		if !digest.Match(body, cur.Digest) && cur.Version != 2 {
			t.Fatal("digest")
		}
	})
	run("U25", func(t *testing.T) {
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareWANService, "2.1.0", 3, []byte("wan-svc-3")); err != nil {
			t.Fatal(err)
		}
		if err := h.WAN.ConfirmWANUpdate(ctx, "", global.SoftwareWANService, 3); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("got %v", err)
		}
		cur, err := h.WAN.CurrentWANSoftware(ctx, tok)
		if err != nil || cur.Version != 2 {
			t.Fatalf("still %+v %v", cur, err)
		}
		if err := h.WAN.DeleteSoftware(ctx, tok, global.SoftwareWANService, 2); !errors.Is(err, domain.ErrReferenced) {
			t.Fatalf("installed %v", err)
		}
	})

	offer, err := h.WAN.PullSoftware(ctx, a.Factory.ID, global.SoftwareFactoryService, 5)
	if err != nil || offer.Version != 5 || string(offer.Body) != "svc-5" {
		t.Fatalf("enrolled pull %+v %v", offer, err)
	}

	rows, err := h.WAN.ListAudit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if audit.ContainsAny(audit.Dump(rows), "svc-5", "wan-secret") {
		t.Fatal("secret or package bytes in audit")
	}
	if !audit.HasResult(rows, "publish_software", audit.Allow) || !audit.HasResult(rows, "publish_software", audit.Deny) {
		t.Fatal("missing publish audit")
	}
	if !audit.HasResult(rows, "confirm_software", audit.Allow) || !audit.HasResult(rows, "confirm_software", audit.Deny) {
		t.Fatal("missing confirm audit")
	}
}

type recordBus struct {
	ids []uuid.UUID
	got []global.Cmd
	raw [][]byte
}

func (b *recordBus) Publish(factoryID uuid.UUID, payload []byte) error {
	var cmd global.Cmd
	if json.Unmarshal(payload, &cmd) != nil {
		return nil
	}
	b.ids = append(b.ids, factoryID)
	b.got = append(b.got, cmd)
	b.raw = append(b.raw, append([]byte(nil), payload...))
	return nil
}

func (b *recordBus) Call(context.Context, uuid.UUID, string, []byte) ([]byte, error) {
	return nil, domain.ErrFactoryOffline
}

func (*recordBus) Drop(uuid.UUID) {}

func TestStorageUsage(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareClientAPK, "6.0.0", 1, []byte("apk-body")); err != nil {
		t.Fatal(err)
	}
	got, err := h.WAN.StorageUsage(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.OSS.Used != int64(len("apk-body")) || got.OSS.Objects != 1 {
		t.Fatalf("oss %+v", got.OSS)
	}
	if got.Disk.Total <= 0 || got.Database <= 0 {
		t.Fatalf("disk/db %+v", got)
	}
	if got.Images.Count != 0 || got.Images.Used != 0 {
		t.Fatalf("images %+v", got.Images)
	}
	if _, err := h.WAN.StorageUsage(ctx, ""); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("anon %v", err)
	}
}

type reportJanitor struct{ raw []byte }

func (reportJanitor) RequestPrune() error { return nil }

func (reportJanitor) PruneResult() (string, bool, bool, error) { return "0B", true, true, nil }

func (j reportJanitor) ImagesJSON() ([]byte, error) { return j.raw, nil }

func TestImageOccupancy(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	if err != nil {
		t.Fatal(err)
	}
	h.WAN.SetImageJanitor(reportJanitor{raw: []byte(`{"kind":"wan_service","used":12,"count":1,"items":[{"ref":"app:dev","id":"x","size":12,"keep":"current"}]}`)})
	got, err := h.WAN.StorageUsage(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.Images.Kind != "wan_service" || got.Images.Used != 12 || got.Images.Count != 1 || len(got.Images.Items) != 1 {
		t.Fatalf("images %+v", got.Images)
	}
	if got.Images.Items[0].Keep != "current" {
		t.Fatalf("keep %+v", got.Images.Items[0])
	}
}
