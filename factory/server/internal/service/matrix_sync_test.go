// 阶段5：弱网入队、幂等汇聚、Intent 收敛与上传记录 1.1～6.1。
package service_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

var matrix05IDs = []string{
	"1.1", "1.2",
	"2.1", "2.2", "2.3",
	"3.1", "3.2",
	"4.1", "4.2", "4.3", "4.4",
	"5.1", "5.2", "5.3", "5.4",
	"6.1",
}

func TestMatrix05(t *testing.T) {
	ran := map[string]bool{}
	run := func(id string, fn func(*testing.T)) {
		t.Helper()
		t.Run(id, func(t *testing.T) {
			ran[id] = true
			fn(t)
		})
	}
	t.Cleanup(func() {
		for _, id := range matrix05IDs {
			if !ran[id] {
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})
	testSyncMatrix(t, run)
}

func testSyncMatrix(t *testing.T, run func(string, func(*testing.T))) {
	t.Helper()
	e := newEnv04(t)
	aud := mustCreateRole(t, e.ctx, e.fac, e.sa, "aud-a", "aud-pass", factory.RoleAuditor, factory.ScopeFactory, nil)
	cloud := []byte("cloud-body-secret")

	cid, bag := e.bound(t, e.op.acc.ID)
	bag.AssetAllowed = true
	bag.Connected = false
	direct := factory.WorkContext{Direct: true}

	var fact factory.PendingFact
	run("1.1", func(t *testing.T) {
		var err error
		fact, err = e.fac.EnqueueFact(e.ctx, &bag, e.clocks, "op-a", "op-pass", direct)
		if err != nil {
			t.Fatal(err)
		}
		if len(bag.PendingFacts) != 1 {
			t.Fatalf("pending %d", len(bag.PendingFacts))
		}
		if _, err := e.fac.GetFact(e.ctx, e.sa, fact.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("leaked to store: %v", err)
		}
	})

	run("1.2", func(t *testing.T) {
		_, audBag := e.bound(t, aud.acc.ID)
		audBag.AssetAllowed = true
		audBag.Connected = false
		if _, err := e.fac.EnqueueFact(e.ctx, &audBag, e.clocks, "aud-a", "aud-pass", direct); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("auditor: %v", err)
		}
		if len(audBag.PendingFacts) != 0 {
			t.Fatal("auditor queued")
		}

		cidB := id.New()
		pub, priv, err := nodekey.Generate()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.facB.AcceptBinding(e.ctx, cidB, pub, 1); err != nil {
			t.Fatal(err)
		}
		facPub, err := e.facB.SigningPublicKey(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		rt, err := e.facB.IssueRuntimeGrant(e.ctx, e.saB, cidB, e.nb, e.na)
		if err != nil {
			t.Fatal(err)
		}
		pc, err := e.facB.IssuePersonOfflineGrant(e.ctx, e.saB, e.peB.acc.ID, cidB, e.nb, e.na)
		if err != nil {
			t.Fatal(err)
		}
		bagB := bagOf(e.seedB.ID, cidB, pub, priv, facPub)
		bagB.Connected = false
		bagB.AssetAllowed = true
		bagB.ApplyRuntime(rt)
		bagB.ApplyPerson(pc)
		if _, err := e.fac.EnqueueFact(e.ctx, &bagB, e.clocks, "pe-b", "pe-b-pass", direct); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("cross factory: %v", err)
		}
	})

	run("2.1", func(t *testing.T) {
		bag.Connected = true
		if err := e.fac.Reconnect(e.ctx, &bag); err != nil {
			t.Fatal(err)
		}
		if len(bag.PendingFacts) != 0 {
			t.Fatal("queue not empty")
		}
		got, err := e.fac.GetFact(e.ctx, e.sa, fact.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.CreatorID != e.op.acc.ID || got.OrgUnitID != nil {
			t.Fatalf("got %+v", got)
		}
	})

	run("2.2", func(t *testing.T) {
		bag.PendingFacts = append(bag.PendingFacts, fact)
		if err := e.fac.FlushPending(e.ctx, &bag); err != nil {
			t.Fatal(err)
		}
		listed, err := e.fac.ListFactsByCreator(e.ctx, e.sa, e.op.acc.ID)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, row := range listed {
			if row.ID == fact.ID {
				n++
				if row.OrgUnitID != nil {
					t.Fatalf("path changed %+v", row)
				}
			}
		}
		if n != 1 {
			t.Fatalf("dup %d", n)
		}
	})

	run("2.3", func(t *testing.T) {
		bad := fact
		nid := id.New()
		bad.OrgUnitID = &nid
		bad.OrgPath = []factory.PathNode{{ID: nid, Name: "假"}}
		bag.PendingFacts = append(bag.PendingFacts, bad)
		if err := e.fac.FlushPending(e.ctx, &bag); !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("got %v", err)
		}
		got, err := e.fac.GetFact(e.ctx, e.sa, fact.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.OrgUnitID != nil {
			t.Fatalf("overwritten %+v", got)
		}
		bag.PendingFacts = nil
	})

	var retry factory.PendingFact
	run("3.1", func(t *testing.T) {
		bag.Connected = false
		var err error
		retry, err = e.fac.EnqueueFact(e.ctx, &bag, e.clocks, "op-a", "op-pass", direct)
		if err != nil {
			t.Fatal(err)
		}
		bag.Connected = true
		bag.FailFlush = true
		if err := e.fac.FlushPending(e.ctx, &bag); !errors.Is(err, domain.ErrSyncRetry) {
			t.Fatalf("got %v", err)
		}
		if len(bag.PendingFacts) != 1 {
			t.Fatalf("queue %d", len(bag.PendingFacts))
		}
		if _, err := e.fac.GetFact(e.ctx, e.sa, retry.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("stored on fail: %v", err)
		}
	})

	run("3.2", func(t *testing.T) {
		bag.FailFlush = false
		if err := e.fac.FlushPending(e.ctx, &bag); err != nil {
			t.Fatal(err)
		}
		if len(bag.PendingFacts) != 0 {
			t.Fatal("queue")
		}
		if _, err := e.fac.GetFact(e.ctx, e.sa, retry.ID); err != nil {
			t.Fatal(err)
		}
	})

	proj := e.pubProj(t, "同步工程", []byte("sync-proj"), nil)
	if err := e.fac.GrantClientProject(e.ctx, e.sa, proj.ID, cid); err != nil {
		t.Fatal(err)
	}
	if err := e.fac.DistributeToClient(e.ctx, e.pe.tok, proj.ID, cid, &bag, e.clocks); err != nil {
		t.Fatal(err)
	}

	run("4.4", func(t *testing.T) {
		if err := e.fac.RevokeClientProject(e.ctx, e.sa, proj.ID, cid); err != nil {
			t.Fatal(err)
		}
		bag.Connected = true
		if err := e.fac.ActivateProject(e.ctx, &bag, e.clocks, proj.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("online after revoke: %v", err)
		}
		bag.Connected = false
		if err := e.fac.ActivateProject(e.ctx, &bag, e.clocks, proj.ID); err != nil {
			t.Fatal(err)
		}
		bag.ActiveID = nil
		bag.ActiveRevision = 0
		bag.Connected = true
	})

	credV1 := *bag.Person
	_, bagOff := e.bound(t, e.op.acc.ID)
	bagOff.AssetAllowed = true
	bagOff.Connected = false

	run("4.2", func(t *testing.T) {
		if err := e.fac.ConvergeIntent(e.ctx, &bagOff); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("offline converge: %v", err)
		}
		login, err := e.fac.LoginOffline(e.ctx, bagOff, e.clocks, "op-a", "op-pass")
		if err != nil || login.Decision != factory.NodeAllow {
			t.Fatalf("old grant %+v %v", login, err)
		}
	})

	run("4.1", func(t *testing.T) {
		if err := e.fac.DisableAccount(e.ctx, e.sa, e.op.acc.ID); err != nil {
			t.Fatal(err)
		}
		v2, err := e.fac.IssuePersonOfflineGrant(e.ctx, e.sa, e.op.acc.ID, cid, e.nb, e.na)
		if err != nil {
			t.Fatal(err)
		}
		if v2.Revision <= credV1.Revision || v2.Active {
			t.Fatalf("v2 %+v", v2)
		}
		if err := e.fac.ConvergeIntent(e.ctx, &bag); err != nil {
			t.Fatal(err)
		}
		if bag.AcceptedPersonRevision != v2.Revision {
			t.Fatalf("rev %d", bag.AcceptedPersonRevision)
		}
		ev, err := e.fac.EvaluateOfflineOp(e.ctx, bag, e.clocks, "op-a", "op-pass")
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("still allowed %+v %v", ev, err)
		}
	})

	run("4.3", func(t *testing.T) {
		before := bag.AcceptedPersonRevision
		bag.ApplyPerson(credV1)
		if bag.AcceptedPersonRevision != before {
			t.Fatalf("rolled back to %d", bag.AcceptedPersonRevision)
		}
		login, err := e.fac.LoginOffline(e.ctx, bag, e.clocks, "op-a", "op-pass")
		if err != nil || login.Decision != factory.NodeDeny {
			t.Fatalf("stale grant %+v %v", login, err)
		}
	})

	cidU, bagU := e.bound(t, e.pe.acc.ID)
	bagU.AssetAllowed = true
	bagU.Connected = false
	var up factory.PendingUpload
	run("5.1", func(t *testing.T) {
		var err error
		up, err = e.fac.EnqueueUpload(e.ctx, &bagU, e.clocks, "pe-a", "pe-pass", factory.UploadPointCloud, cloud)
		if err != nil {
			t.Fatal(err)
		}
		bagU.Connected = true
		if err := e.fac.Reconnect(e.ctx, &bagU); err != nil {
			t.Fatal(err)
		}
		rec, err := e.fac.GetUpload(e.ctx, e.sa, up.ID)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind != factory.UploadPointCloud || rec.ClientID != cidU {
			t.Fatalf("%+v", rec)
		}
		body, err := e.fac.ReadUploadContent(e.ctx, e.sa, up.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(body, cloud) {
			t.Fatal("body mismatch")
		}
	})

	run("5.2", func(t *testing.T) {
		bagU.PendingUploads = append(bagU.PendingUploads, up)
		if err := e.fac.FlushPending(e.ctx, &bagU); err != nil {
			t.Fatal(err)
		}
		rec, err := e.fac.GetUpload(e.ctx, e.sa, up.ID)
		if err != nil {
			t.Fatal(err)
		}
		if rec.ID != up.ID {
			t.Fatal("new row")
		}
	})

	run("5.3", func(t *testing.T) {
		bad := up
		bad.Content = []byte("other-cloud")
		bad.Digest = digest.Sum(bad.Content)
		bagU.PendingUploads = append(bagU.PendingUploads, bad)
		if err := e.fac.FlushPending(e.ctx, &bagU); !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("got %v", err)
		}
		body, err := e.fac.ReadUploadContent(e.ctx, e.sa, up.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(body, cloud) {
			t.Fatal("original replaced")
		}
		bagU.PendingUploads = nil
	})

	run("5.4", func(t *testing.T) {
		_, saBag := e.bound(t, e.seed.SuperAdminID)
		saBag.AssetAllowed = true
		if _, err := e.fac.EnqueueUpload(e.ctx, &saBag, e.clocks, "sa-a", "sa-pass", factory.UploadImage, []byte("img")); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("sa: %v", err)
		}
		_, audBag := e.bound(t, aud.acc.ID)
		audBag.AssetAllowed = true
		if _, err := e.fac.EnqueueUpload(e.ctx, &audBag, e.clocks, "aud-a", "aud-pass", factory.UploadImage, []byte("img")); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("aud: %v", err)
		}
	})

	run("6.1", func(t *testing.T) {
		rows, err := e.fac.ListAudit(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !hasAction(rows, "enqueue_fact", audit.Allow) || !hasAction(rows, "enqueue_fact", audit.Deny) {
			t.Fatal("enqueue_fact traces")
		}
		if !hasAction(rows, "sync_flush", audit.Allow) || !hasAction(rows, "sync_flush", audit.Deny) {
			t.Fatal("sync_flush traces")
		}
		if !hasAction(rows, "converge_intent", audit.Allow) || !hasAction(rows, "converge_intent", audit.Deny) {
			t.Fatal("converge traces")
		}
		if !hasAction(rows, "enqueue_upload", audit.Allow) || !hasAction(rows, "enqueue_upload", audit.Deny) {
			t.Fatal("upload traces")
		}
		dump := audit.Dump(rows)
		if audit.ContainsAny(dump, "op-pass", "pe-pass", "sa-pass", "aud-pass", string(cloud)) {
			t.Fatal("secret leaked")
		}
		if bag.PrivateKey != nil && audit.ContainsAny(dump, hex.EncodeToString(bag.PrivateKey)) {
			t.Fatal("key leaked")
		}
	})
}

func hasAction(rows []audit.Row, action, result string) bool {
	for _, r := range rows {
		if r.Action == action && r.Result == result {
			return true
		}
	}
	return false
}
