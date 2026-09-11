// 阶段4第7圈：WAN 侧下发授权与平台级闭包 5.1～5.4、15.2、17.1～17.2。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	global "wmesh/global/internal/service"
)

var matrix04IDs = []string{
	"5.1", "5.2", "5.3", "5.4",
	"15.2",
	"17.1", "17.2",
}

func TestMatrix04(t *testing.T) {
	ran := map[string]bool{}
	run := func(id string, fn func(*testing.T)) {
		t.Helper()
		t.Run(id, func(t *testing.T) {
			ran[id] = true
			fn(t)
		})
	}
	t.Cleanup(func() {
		for _, id := range matrix04IDs {
			if !ran[id] {
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})
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
	facA, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	facB, err := h.WAN.CreateFactory(ctx, tok, "厂B", "sa-b", "超管B")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("wan-closure-secret")
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "平台工艺", body)
	if err != nil {
		t.Fatal(err)
	}
	proc, err = h.WAN.PublishPlatformAsset(ctx, tok, proc.ID, proc.Revision)
	if err != nil {
		t.Fatal(err)
	}
	proc, err = h.WAN.GetPlatformAsset(ctx, tok, proc.ID)
	if err != nil {
		t.Fatal(err)
	}
	proj, err := h.WAN.CreatePlatformProject(ctx, tok, "平台工程", []byte("proj"), []global.AssetDep{{ID: proc.ID, Revision: proc.Revision, Digest: proc.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	proj, err = h.WAN.PublishPlatformAsset(ctx, tok, proj.ID, proj.Revision)
	if err != nil {
		t.Fatal(err)
	}

	run("5.2", func(t *testing.T) {
		if _, err := h.WAN.DistributeToFactory(ctx, tok, proj.ID, facA.Factory.ID); !errors.Is(err, domain.ErrForbidden) && !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v", err)
		}
	})
	run("5.4", func(t *testing.T) {
		if err := h.WAN.GrantFactoryAsset(ctx, tok, proc.ID, facA.Factory.ID); err != nil {
			t.Fatal(err)
		}
		snap, err := h.WAN.DistributeToFactory(ctx, tok, proc.ID, facA.Factory.ID)
		if err != nil || snap.Kind != global.KindProcess || snap.AssetID != proc.ID || len(snap.Members) != 1 {
			t.Fatalf("%+v %v", snap, err)
		}
	})
	run("5.1", func(t *testing.T) {
		if err := h.WAN.GrantFactoryAsset(ctx, tok, proj.ID, facA.Factory.ID); err != nil {
			t.Fatal(err)
		}
		snap, err := h.WAN.DistributeToFactory(ctx, tok, proj.ID, facA.Factory.ID)
		if err != nil || snap.Kind != global.KindProject || snap.AssetID != proj.ID || len(snap.Members) != 2 || snap.Copyable {
			t.Fatalf("%+v %v", snap, err)
		}
		if snap.TargetFactoryID == nil || *snap.TargetFactoryID != facA.Factory.ID {
			t.Fatalf("target %+v", snap.TargetFactoryID)
		}
	})
	run("5.3", func(t *testing.T) {
		if err := h.WAN.GrantFactoryAsset(ctx, tok, proj.ID, facB.Factory.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := h.WAN.DistributeToFactory(ctx, tok, proj.ID, facB.Factory.ID); err != nil {
			t.Fatal(err)
		}
		okA, err := h.WAN.HasDistributedTo(ctx, proj.ID, facA.Factory.ID)
		if err != nil || !okA {
			t.Fatalf("A %v %v", okA, err)
		}
		okB, err := h.WAN.HasDistributedTo(ctx, proj.ID, facB.Factory.ID)
		if err != nil || !okB {
			t.Fatalf("B %v %v", okB, err)
		}
	})
	run("15.2", func(t *testing.T) {
		snap, err := h.WAN.DistributeToFactory(ctx, tok, proj.ID, facA.Factory.ID)
		if err != nil || snap.Revision != proj.Revision {
			t.Fatalf("%+v %v", snap, err)
		}
	})
	run("17.1", func(t *testing.T) {
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
	run("17.2", func(t *testing.T) {
		rows, err := h.WAN.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !audit.HasResult(rows, "distribute_closure", audit.Deny) {
			t.Fatal("need unauthorized deny")
		}
	})
}
