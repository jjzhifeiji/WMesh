// 软件更新服务层矩阵：厂自拉、确认、本机袋。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/blob"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

var matrixSoftwareIDs = []string{
	"U2", "U3", "U4", "U5", "U8", "U9", "U10", "U11", "U12", "U13",
	"U14", "U16", "U17", "U19", "U20", "U21", "U22", "U23", "U28", "U29", "U30", "U31",
}

type memSource struct {
	mu     sync.Mutex
	deny   bool
	offers map[string]factory.SoftwareOffer
	pulls  int
}

func (m *memSource) put(o factory.SoftwareOffer) {
	if m.offers == nil {
		m.offers = map[string]factory.SoftwareOffer{}
	}
	m.offers[o.Kind] = o
}

func (m *memSource) Latest(_ context.Context, kind string) (factory.SoftwareMeta, error) {
	if m.deny {
		return factory.SoftwareMeta{}, domain.ErrForbidden
	}
	o, ok := m.offers[kind]
	if !ok {
		return factory.SoftwareMeta{}, domain.ErrNotFound
	}
	return factory.SoftwareMeta{Kind: o.Kind, Version: o.Version, VersionName: o.VersionName, Digest: o.Digest}, nil
}

func (m *memSource) Pull(_ context.Context, kind string, version int64) ([]byte, error) {
	m.mu.Lock()
	m.pulls++
	m.mu.Unlock()
	if m.deny {
		return nil, domain.ErrForbidden
	}
	o, ok := m.offers[kind]
	if !ok || o.Version != version {
		return nil, domain.ErrNotFound
	}
	return o.Body, nil
}

func offerOf(kind string, version int64, name string, body []byte) factory.SoftwareOffer {
	return factory.SoftwareOffer{
		Kind: kind, Version: version, VersionName: name, Digest: digest.Sum(body), Body: body,
	}
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
	_, facB, err := h.Provision(ctx, "sa-b", "超管B")
	if err != nil {
		t.Fatal(err)
	}
	op := mustCreateRole(t, ctx, facA, saA, "op-a", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	pe := mustCreateRole(t, ctx, facA, saA, "pe-a", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	now := time.Now().UTC()
	clocks := factory.Clocks{Server: now, Local: now}
	nb, na := now.Add(-time.Hour), now.Add(24*time.Hour)

	src := &memSource{}
	src.put(offerOf(factory.SoftwareFactoryService, 5, "1.5.0", []byte("factory-svc-5")))
	facA.Updates.SetSoftwareSource(src)

	run("U2", func(t *testing.T) {
		got, err := facA.SyncFactorySoftware(ctx, saA, factory.SoftwareFactoryService)
		if err != nil || got.Version != 5 {
			t.Fatalf("%+v %v", got, err)
		}
	})
	run("U3", func(t *testing.T) {
		got, err := facA.Store().InstalledSoftware(ctx, factory.SoftwareFactoryService)
		if err != nil || got != 0 {
			t.Fatalf("installed %d %v", got, err)
		}
		p, err := facA.PendingFactorySoftware(ctx, saA)
		if err != nil || p == nil || p.Version != 5 {
			t.Fatalf("pending %+v %v", p, err)
		}
	})
	run("U5", func(t *testing.T) {
		if err := facA.ConfirmFactoryUpdate(ctx, op.tok, factory.SoftwareFactoryService, 5); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	var weldingBag factory.Bag
	run("U4", func(t *testing.T) {
		cid, bag := boundBag(t, ctx, facA, saA, seedA.ID, op.acc.ID, nb, na)
		_ = cid
		bag.Welding = true
		weldingBag = bag
		facA.Updates.SetApplyOutcome(factory.ApplyOK)
		if err := facA.ConfirmFactoryUpdate(ctx, saA, factory.SoftwareFactoryService, 5); err != nil {
			t.Fatal(err)
		}
		got, err := facA.Store().InstalledSoftware(ctx, factory.SoftwareFactoryService)
		if err != nil || got != 5 {
			t.Fatalf("installed %d %v", got, err)
		}
	})
	run("U22", func(t *testing.T) {
		if !weldingBag.Welding {
			t.Fatal("welding cleared")
		}
		ev := factory.EvaluateRuntime(weldingBag, clocks, factory.NodeContinue)
		if ev.Decision != factory.NodeContinueWeld && ev.Decision != factory.NodeAllow {
			t.Fatalf("weld decision %v", ev.Decision)
		}
	})
	run("U8", func(t *testing.T) {
		src.put(offerOf(factory.SoftwareClientAPK, 3, "6.1.0", []byte("apk-3")))
		if _, err := facA.SyncFactorySoftware(ctx, saA, factory.SoftwareClientAPK); err != nil {
			t.Fatal(err)
		}
	})
	cid1, bag1 := boundBag(t, ctx, facA, saA, seedA.ID, op.acc.ID, nb, na)
	_, bag2 := boundBag(t, ctx, facA, saA, seedA.ID, pe.acc.ID, nb, na)
	_ = cid1
	run("U9", func(t *testing.T) {
		if err := facA.ConfirmClientUpdate(ctx, &bag1, clocks, 3); err != nil {
			t.Fatal(err)
		}
		if bag1.SoftwareVersion != 3 {
			t.Fatalf("ver %d", bag1.SoftwareVersion)
		}
	})
	run("U10", func(t *testing.T) {
		bag2.Welding = true
		if err := facA.ConfirmClientUpdate(ctx, &bag2, clocks, 3); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		if bag2.SoftwareVersion != 0 {
			t.Fatal("installed while welding")
		}
		bag2.Welding = false
	})
	run("U11", func(t *testing.T) {
		loggedOut := bag2
		loggedOut.OperatorID = nil
		if err := facA.ConfirmClientUpdate(ctx, &loggedOut, clocks, 3); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("U12", func(t *testing.T) {
		p, err := facA.PendingFactorySoftware(ctx, saA)
		if err != nil || p != nil {
			t.Fatalf("factory pending after install %+v %v", p, err)
		}
		src.put(offerOf(factory.SoftwareClientAPK, 4, "6.2.0", []byte("apk-4")))
		if _, err := facA.SyncFactorySoftware(ctx, saA, factory.SoftwareClientAPK); err != nil {
			t.Fatal(err)
		}
		if bag2.SoftwareVersion != 0 {
			t.Fatal("unconfirmed tablet changed")
		}
	})
	run("U23", func(t *testing.T) {
		if bag1.SoftwareVersion != 3 || bag2.SoftwareVersion != 0 {
			t.Fatalf("mixed %d %d", bag1.SoftwareVersion, bag2.SoftwareVersion)
		}
	})
	run("U14", func(t *testing.T) {
		src.put(offerOf(factory.SoftwareClientAPK, 6, "6.3.0", []byte("apk-6")))
		if _, err := facA.SyncFactorySoftware(ctx, saA, factory.SoftwareClientAPK); err != nil {
			t.Fatal(err)
		}
		if err := facA.ConfirmClientUpdate(ctx, &bag2, clocks, 4); !errors.Is(err, domain.ErrStaleRevision) {
			t.Fatalf("got %v", err)
		}
	})
	run("U29", func(t *testing.T) {
		rows, err := facA.ListFactorySoftware(ctx, saA, "")
		if err != nil {
			t.Fatal(err)
		}
		keep := map[string]string{}
		for _, row := range rows {
			keep[row.Kind+":"+strconv.FormatInt(row.Version, 10)] = row.Keep
		}
		if keep[factory.SoftwareClientAPK+":6"] != factory.SoftwareKeepLatest {
			t.Fatalf("apk latest %q", keep[factory.SoftwareClientAPK+":6"])
		}
		if keep[factory.SoftwareFactoryService+":5"] != factory.SoftwareKeepInstalled {
			t.Fatalf("svc installed %q", keep[factory.SoftwareFactoryService+":5"])
		}
		if keep[factory.SoftwareClientAPK+":3"] != "" || keep[factory.SoftwareClientAPK+":4"] != "" {
			t.Fatalf("old keep %+v", keep)
		}
		if err := facA.DeleteFactorySoftware(ctx, saA, factory.SoftwareClientAPK, 3); err != nil {
			t.Fatal(err)
		}
		n, err := facA.PruneFactorySoftware(ctx, saA, factory.SoftwareClientAPK)
		if err != nil || n != 1 {
			t.Fatalf("prune %d %v", n, err)
		}
	})
	run("U30", func(t *testing.T) {
		if err := facA.DeleteFactorySoftware(ctx, saA, factory.SoftwareClientAPK, 6); !errors.Is(err, domain.ErrReferenced) {
			t.Fatalf("latest %v", err)
		}
		if err := facA.DeleteFactorySoftware(ctx, saA, factory.SoftwareFactoryService, 5); !errors.Is(err, domain.ErrReferenced) {
			t.Fatalf("installed %v", err)
		}
		if err := facA.DeleteFactorySoftware(ctx, op.tok, factory.SoftwareClientAPK, 4); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("op %v", err)
		}
	})
	run("U31", func(t *testing.T) {
		if err := facA.RequestImagePrune(ctx, saA, ""); err != nil {
			t.Fatal(err)
		}
		if err := facA.RequestImagePrune(ctx, op.tok, ""); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("op %v", err)
		}
	})
	run("U13", func(t *testing.T) {
		if err := facA.IngestSoftware(ctx, offerOf(factory.SoftwareFactoryService, 4, "1.4.0", []byte("factory-svc-4"))); !errors.Is(err, domain.ErrStaleRevision) {
			t.Fatalf("got %v", err)
		}
	})
	run("U16", func(t *testing.T) {
		bad := offerOf(factory.SoftwareFactoryService, 8, "1.8.0", []byte("factory-svc-8"))
		bad.Body = []byte("tampered")
		if err := facA.IngestSoftware(ctx, bad); !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("got %v", err)
		}
	})
	run("U17", func(t *testing.T) {
		srcB := &memSource{deny: true}
		facB.Updates.SetSoftwareSource(srcB)
		if _, err := facB.Store().SoftwareReplica(ctx, factory.SoftwareFactoryService, 5); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("b replica %v", err)
		}
	})
	run("U19", func(t *testing.T) {
		if err := facA.IngestSoftware(ctx, offerOf(factory.SoftwareWANService, 1, "w", []byte("wan"))); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("U20", func(t *testing.T) {
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
	run("U21", func(t *testing.T) {
		if err := facA.IngestSoftware(ctx, offerOf(factory.SoftwareFactoryService, 7, "1.7.0", []byte("factory-svc-7"))); err != nil {
			t.Fatal(err)
		}
		facA.Updates.SetApplyOutcome(factory.ApplyFail)
		if err := facA.ConfirmFactoryUpdate(ctx, saA, factory.SoftwareFactoryService, 7); !errors.Is(err, domain.ErrSoftwareInstallFailed) {
			t.Fatalf("got %v", err)
		}
		got, err := facA.Store().InstalledSoftware(ctx, factory.SoftwareFactoryService)
		if err != nil || got != 5 {
			t.Fatalf("rolled %d %v", got, err)
		}
		facA.Updates.SetApplyOutcome(factory.ApplyDefer)
	})
	run("U28", func(t *testing.T) {
		src.mu.Lock()
		src.pulls = 0
		src.mu.Unlock()
		if err := facA.Updates.EnsureSoftware(ctx, factory.SoftwareFactoryService, 7); err != nil {
			t.Fatal(err)
		}
		src.mu.Lock()
		first := src.pulls
		src.mu.Unlock()
		if first != 0 {
			t.Fatalf("re-pulled complete %d", first)
		}
		src.put(offerOf(factory.SoftwareFactoryService, 8, "1.8.0", []byte("factory-svc-8")))
		var wg sync.WaitGroup
		errCh := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errCh <- facA.Updates.EnsureSoftware(ctx, factory.SoftwareFactoryService, 8)
			}()
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			if err != nil {
				t.Fatal(err)
			}
		}
		src.mu.Lock()
		got := src.pulls
		src.mu.Unlock()
		if got != 1 {
			t.Fatalf("pulls %d", got)
		}
		p, err := facA.PendingFactorySoftware(ctx, saA)
		if err != nil || p == nil || p.Version != 8 {
			t.Fatalf("pending %+v %v", p, err)
		}
		facA.Updates.SetBlobs(blob.NewMemory())
		p, err = facA.PendingFactorySoftware(ctx, saA)
		if err != nil || p != nil {
			t.Fatalf("incomplete pending %+v %v", p, err)
		}
		if err := facA.ConfirmFactoryUpdate(ctx, saA, factory.SoftwareFactoryService, 8); !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("confirm incomplete %v", err)
		}
	})

	rows, err := facA.ListAudit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dump := audit.Dump(rows)
	if audit.ContainsAny(dump, "factory-svc-5") {
		t.Fatal("package bytes in audit")
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

func TestPadClientSoftwarePull(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa-pad", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa-pad", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	tok := mustLogin(t, ctx, fac, "sa-pad", "sa-pass")
	got, err := fac.PadClientSoftware(ctx, tok)
	if err != nil || got != nil {
		t.Fatalf("empty %+v %v", got, err)
	}
	if _, err := fac.PadClientSoftware(ctx, ""); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("anon %v", err)
	}
	apk := []byte("apk-pad-51")
	if err := fac.IngestSoftware(ctx, offerOf(factory.SoftwareClientAPK, 51, "6.1.0", apk)); err != nil {
		t.Fatal(err)
	}
	row, err := fac.PadClientSoftware(ctx, tok)
	if err != nil || row == nil || row.Version != 51 || row.VersionName != "6.1.0" {
		t.Fatalf("meta %+v %v", row, err)
	}
	body, err := fac.PullPadClientSoftware(ctx, tok, 51)
	if err != nil || !bytes.Equal(body, apk) {
		t.Fatalf("pull %q %v", body, err)
	}
	if _, err := fac.PullPadClientSoftware(ctx, tok, 9); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing %v", err)
	}
	if _, err := fac.PullPadClientSoftware(ctx, "", 51); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("anon pull %v", err)
	}
}

func TestStorageUsage(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa-st", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa-st", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	tok := mustLogin(t, ctx, fac, "sa-st", "sa-pass")
	if err := fac.IngestSoftware(ctx, offerOf(factory.SoftwareClientAPK, 2, "6.0.0", []byte("apk-st"))); err != nil {
		t.Fatal(err)
	}
	got, err := fac.StorageUsage(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.OSS.Used != int64(len("apk-st")) || got.OSS.Objects != 1 {
		t.Fatalf("oss %+v", got.OSS)
	}
	if got.Disk.Total <= 0 || got.Database <= 0 {
		t.Fatalf("disk/db %+v", got)
	}
	if got.Images.Count != 0 || got.Images.Used != 0 {
		t.Fatalf("images %+v", got.Images)
	}
	if _, err := fac.StorageUsage(ctx, ""); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("anon %v", err)
	}
}

type reportJanitor struct {
	raw []byte
	ref string
}

func (j *reportJanitor) RequestPrune(ref string) error {
	j.ref = ref
	return nil
}

func (j *reportJanitor) PruneResult() (string, bool, bool, error) { return "0B", true, true, nil }

func (j *reportJanitor) ImagesJSON() ([]byte, error) { return j.raw, nil }

func TestImageOccupancy(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa-img", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa-img", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	tok := mustLogin(t, ctx, fac, "sa-img", "sa-pass")
	jan := &reportJanitor{raw: []byte(`{"kind":"factory_service","used":16,"count":2,"items":[{"ref":"app:dev","id":"y","size":8,"keep":"current"},{"ref":"app:old","id":"z","size":8,"keep":""}]}`)}
	fac.SetImageJanitor(jan)
	got, err := fac.StorageUsage(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.Images.Kind != "factory_service" || got.Images.Used != 16 || got.Images.Count != 2 || len(got.Images.Items) != 2 {
		t.Fatalf("images %+v", got.Images)
	}
	if err := fac.RequestImagePrune(ctx, tok, "app:dev"); !errors.Is(err, domain.ErrReferenced) {
		t.Fatalf("current %v", err)
	}
	if err := fac.RequestImagePrune(ctx, tok, "app:old"); err != nil {
		t.Fatal(err)
	}
	if jan.ref != "app:old" {
		t.Fatalf("ref %q", jan.ref)
	}
}
