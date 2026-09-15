// 软件更新服务层矩阵 U1～U20（厂内侧送达、确认、本机袋）。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	"wmesh/factory/internal/platform/softwaresign"
	factory "wmesh/factory/internal/service"
)

var matrixSoftwareIDs = []string{
	"U1", "U2", "U3", "U4", "U5", "U6", "U7", "U8", "U9", "U10",
	"U11", "U12", "U13", "U14", "U15", "U16", "U17", "U18", "U19", "U20",
}

type failInstaller struct{}

func (failInstaller) Apply(context.Context, int64, []byte) error {
	return errors.New("boom")
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
	seedA, facA, err := h.Provision(ctx, "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	if err := facA.Activate(ctx, "sa-a", seedA.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saA := mustLogin(t, ctx, facA, "sa-a", "sa-pass")
	seedB, facB, err := h.Provision(ctx, "sa-b", "超管B")
	if err != nil {
		t.Fatal(err)
	}
	if err := facB.Activate(ctx, "sa-b", seedB.ActivationToken, "sa-b-pass"); err != nil {
		t.Fatal(err)
	}
	op := mustCreateRole(t, ctx, facA, saA, "op-a", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	pe := mustCreateRole(t, ctx, facA, saA, "pe-a", "pe-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil)
	now := time.Now().UTC()
	clocks := factory.Clocks{Server: now, Local: now}
	nb, na := now.Add(-time.Hour), now.Add(24*time.Hour)

	wanPub, wanPriv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := facA.SetWANPublicKey(ctx, wanPub); err != nil {
		t.Fatal(err)
	}
	if err := facB.SetWANPublicKey(ctx, wanPub); err != nil {
		t.Fatal(err)
	}

	sign := func(kind string, version int64, name string, body []byte, facID uuid.UUID) factory.SoftwareSnapshot {
		t.Helper()
		sum := digest.Sum(body)
		return factory.SoftwareSnapshot{
			Kind: kind, Version: version, VersionName: name, Digest: sum,
			Signature:    nodekey.Sign(wanPriv, softwaresign.Message(kind, version, sum, facID)),
			WANPublicKey: wanPub, TargetFactoryID: facID, Body: body,
		}
	}

	body5 := []byte("factory-svc-5")
	snap5 := sign(factory.SoftwareFactoryService, 5, "1.5.0", body5, seedA.ID)

	run("U1", func(t *testing.T) {
		if err := facA.AcceptSoftwareDelivery(ctx, snap5); err != nil {
			t.Fatal(err)
		}
	})
	run("U2", func(t *testing.T) {
		got, err := facA.Store().InstalledSoftware(ctx, factory.SoftwareFactoryService)
		if err != nil || got != 0 {
			t.Fatalf("installed %d %v", got, err)
		}
		p, err := facA.PendingFactorySoftware(ctx, saA)
		if err != nil || p == nil || p.Version != 5 {
			t.Fatalf("pending %+v %v", p, err)
		}
	})
	run("U4", func(t *testing.T) {
		if err := facA.ConfirmFactoryUpdate(ctx, op.tok, factory.SoftwareFactoryService, 5); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("U5", func(t *testing.T) {
		if err := facA.ConfirmFactoryUpdate(ctx, "", factory.SoftwareFactoryService, 5); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("got %v", err)
		}
	})
	var weldingBag factory.Bag
	run("U3", func(t *testing.T) {
		cid, bag := boundBag(t, ctx, facA, saA, seedA.ID, op.acc.ID, nb, na)
		_ = cid
		bag.Welding = true
		weldingBag = bag
		if err := facA.ConfirmFactoryUpdate(ctx, saA, factory.SoftwareFactoryService, 5); err != nil {
			t.Fatal(err)
		}
		got, err := facA.Store().InstalledSoftware(ctx, factory.SoftwareFactoryService)
		if err != nil || got != 5 {
			t.Fatalf("installed %d %v", got, err)
		}
		cur, err := facA.CurrentFactorySoftware(ctx, saA)
		if err != nil || cur.Version != 5 || cur.VersionName != "1.5.0" {
			t.Fatalf("current %+v %v", cur, err)
		}
	})
	run("U19", func(t *testing.T) {
		if !weldingBag.Welding {
			t.Fatal("welding cleared")
		}
		ev := factory.EvaluateRuntime(weldingBag, clocks, factory.NodeContinue)
		if ev.Decision != factory.NodeContinueWeld && ev.Decision != factory.NodeAllow {
			t.Fatalf("weld decision %v", ev.Decision)
		}
	})
	run("U6", func(t *testing.T) {
		apk := sign(factory.SoftwareClientAPK, 3, "6.1.0", []byte("apk-3"), seedA.ID)
		if err := facA.AcceptSoftwareDelivery(ctx, apk); err != nil {
			t.Fatal(err)
		}
	})
	cid1, bag1 := boundBag(t, ctx, facA, saA, seedA.ID, op.acc.ID, nb, na)
	cid2, bag2 := boundBag(t, ctx, facA, saA, seedA.ID, pe.acc.ID, nb, na)
	run("U7", func(t *testing.T) {
		ready, err := facA.ReadyClientUpdate(ctx, cid1)
		if err != nil {
			t.Fatal(err)
		}
		if err := facA.ConfirmClientUpdate(ctx, &bag1, clocks, ready); err != nil {
			t.Fatal(err)
		}
		if bag1.SoftwareVersion != 3 {
			t.Fatalf("ver %d", bag1.SoftwareVersion)
		}
	})
	run("U8", func(t *testing.T) {
		ready, err := facA.ReadyClientUpdate(ctx, cid2)
		if err != nil {
			t.Fatal(err)
		}
		bag2.Welding = true
		if err := facA.ConfirmClientUpdate(ctx, &bag2, clocks, ready); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		if bag2.SoftwareVersion != 0 {
			t.Fatal("installed while welding")
		}
		bag2.Welding = false
	})
	run("U9", func(t *testing.T) {
		ready, err := facA.ReadyClientUpdate(ctx, cid2)
		if err != nil {
			t.Fatal(err)
		}
		loggedOut := bag2
		loggedOut.OperatorID = nil
		if err := facA.ConfirmClientUpdate(ctx, &loggedOut, clocks, ready); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("U10", func(t *testing.T) {
		p, err := facA.PendingFactorySoftware(ctx, saA)
		if err != nil || p != nil {
			t.Fatalf("factory pending after install %+v %v", p, err)
		}
		apk4 := sign(factory.SoftwareClientAPK, 4, "6.2.0", []byte("apk-4"), seedA.ID)
		if err := facA.AcceptSoftwareDelivery(ctx, apk4); err != nil {
			t.Fatal(err)
		}
		if bag2.SoftwareVersion != 0 {
			t.Fatal("unconfirmed tablet changed")
		}
	})
	run("U20", func(t *testing.T) {
		if bag1.SoftwareVersion != 3 || bag2.SoftwareVersion != 0 {
			t.Fatalf("mixed %d %d", bag1.SoftwareVersion, bag2.SoftwareVersion)
		}
	})
	run("U12", func(t *testing.T) {
		apk6 := sign(factory.SoftwareClientAPK, 6, "6.3.0", []byte("apk-6"), seedA.ID)
		if err := facA.AcceptSoftwareDelivery(ctx, apk6); err != nil {
			t.Fatal(err)
		}
		ready, err := facA.ReadyClientUpdate(ctx, cid2)
		if err != nil || ready.Version != 6 {
			t.Fatalf("%+v %v", ready, err)
		}
		ready.Version = 4
		if err := facA.ConfirmClientUpdate(ctx, &bag2, clocks, ready); !errors.Is(err, domain.ErrStaleRevision) && !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("got %v", err)
		}
	})
	run("U11", func(t *testing.T) {
		old := sign(factory.SoftwareFactoryService, 4, "1.4.0", []byte("factory-svc-4"), seedA.ID)
		if err := facA.AcceptSoftwareDelivery(ctx, old); !errors.Is(err, domain.ErrStaleRevision) {
			t.Fatalf("got %v", err)
		}
	})
	run("U13", func(t *testing.T) {
		if err := facA.AcceptSoftwareDelivery(ctx, snap5); err != nil {
			t.Fatal(err)
		}
	})
	run("U14", func(t *testing.T) {
		bad := snap5
		bad.Body = []byte("tampered")
		if err := facA.AcceptSoftwareDelivery(ctx, bad); !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("got %v", err)
		}
		bad = snap5
		bad.Signature = bytes.Repeat([]byte{1}, 64)
		if err := facA.AcceptSoftwareDelivery(ctx, bad); !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("got %v", err)
		}
	})
	run("U15", func(t *testing.T) {
		if err := facB.AcceptSoftwareDelivery(ctx, snap5); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("U16", func(t *testing.T) {
		ready, err := facA.ReadyClientUpdate(ctx, cid2)
		if err != nil {
			t.Fatal(err)
		}
		ready.Signature = nodekey.Sign(wanPriv, softwaresign.Message(ready.Kind, ready.Version, ready.Digest, cid2))
		if err := facA.ConfirmClientUpdate(ctx, &bag2, clocks, ready); !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("got %v", err)
		}
	})
	run("U17", func(t *testing.T) {
		before, err := facA.ListAssets(ctx, saA, factory.KindProcess)
		if err != nil {
			t.Fatal(err)
		}
		n := len(before)
		if _, err := facA.Store().SoftwareReplica(ctx, factory.SoftwareFactoryService, 5); err != nil {
			t.Fatal(err)
		}
		after, err := facA.ListAssets(ctx, saA, factory.KindProcess)
		if err != nil || len(after) != n {
			t.Fatalf("assets leaked into software %d %d %v", n, len(after), err)
		}
	})
	run("U18", func(t *testing.T) {
		next := sign(factory.SoftwareFactoryService, 7, "1.7.0", []byte("factory-svc-7"), seedA.ID)
		if err := facA.AcceptSoftwareDelivery(ctx, next); err != nil {
			t.Fatal(err)
		}
		facA.SetFactoryInstaller(failInstaller{})
		if err := facA.ConfirmFactoryUpdate(ctx, saA, factory.SoftwareFactoryService, 7); !errors.Is(err, domain.ErrSoftwareInstallFailed) {
			t.Fatalf("got %v", err)
		}
		got, err := facA.Store().InstalledSoftware(ctx, factory.SoftwareFactoryService)
		if err != nil || got != 5 {
			t.Fatalf("rolled %d %v", got, err)
		}
		facA.SetFactoryInstaller(nil)
	})

	rows, err := facA.ListAudit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dump := audit.Dump(rows)
	if audit.ContainsAny(dump, string(wanPriv), string(body5)) {
		t.Fatal("secret or package bytes in audit")
	}
	if !audit.HasResult(rows, "confirm_software", audit.Allow) || !audit.HasResult(rows, "confirm_software", audit.Deny) {
		t.Fatal("missing confirm audit")
	}
}

func boundBag(t *testing.T, ctx context.Context, fac *factory.Service, sa string, factoryID, personID uuid.UUID, nb, na time.Time) (uuid.UUID, factory.Bag) {
	t.Helper()
	cid := id.New()
	pub, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.AcceptBinding(ctx, cid, "焊机", pub, 1); err != nil {
		t.Fatal(err)
	}
	facPub, err := fac.SigningPublicKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := fac.IssueRuntimeGrant(ctx, sa, cid, nb, na)
	if err != nil {
		t.Fatal(err)
	}
	bag := bagOf(factoryID, cid, pub, priv, facPub)
	bag.Connected = true
	bag.ApplyRuntime(rt)
	oid := personID
	bag.OperatorID = &oid
	return cid, bag
}

func TestAcceptSoftwareFirstTrust(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa-t", "超管")
	if err != nil {
		t.Fatal(err)
	}
	wanPub, wanPriv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("svc-tofu")
	sum := digest.Sum(body)
	snap := factory.SoftwareSnapshot{
		Kind: factory.SoftwareFactoryService, Version: 1, VersionName: "1.0.0", Digest: sum,
		Signature:    nodekey.Sign(wanPriv, softwaresign.Message(factory.SoftwareFactoryService, 1, sum, seed.ID)),
		WANPublicKey: wanPub, TargetFactoryID: seed.ID, Body: body,
	}
	if err := fac.AcceptSoftwareDelivery(ctx, snap); err != nil {
		t.Fatal(err)
	}
}
