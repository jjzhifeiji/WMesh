// 阶段2第6圈：在线收敛与离线事实归属 14.1～16.3。
package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

func TestConvergeMatrix(t *testing.T) {
	ctx := context.Background()
	ids := []string{"14.1", "14.2", "14.3", "15.1", "16.1", "16.2", "16.3"}
	ran := map[string]bool{}
	run := func(id string, fn func(*testing.T)) {
		t.Helper()
		t.Run(id, func(t *testing.T) {
			ran[id] = true
			fn(t)
		})
	}
	t.Cleanup(func() {
		for _, id := range ids {
			if !ran[id] {
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})

	h := New(t)
	seedA, facA, err := h.Provision(ctx, "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	if err := facA.Activate(ctx, "sa-a", seedA.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saTok, err := facA.Login(ctx, "sa-a", "sa-pass")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	valid := factory.Clocks{Server: now, Local: now}
	nb, na := now.Add(-time.Hour), now.Add(24*time.Hour)

	cidA := id.New()
	pubA, privA, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := facA.AcceptBinding(ctx, cidA, pubA, 1); err != nil {
		t.Fatal(err)
	}
	facPubA, err := facA.SigningPublicKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := facA.IssueRuntimeGrant(ctx, saTok, cidA, nb, na)
	if err != nil {
		t.Fatal(err)
	}

	site, err := facA.CreateOrgUnit(ctx, saTok, "场地", nil)
	if err != nil {
		t.Fatal(err)
	}
	shopA, err := facA.CreateOrgUnit(ctx, saTok, "车间A", &site.ID)
	if err != nil {
		t.Fatal(err)
	}
	shopB, err := facA.CreateOrgUnit(ctx, saTok, "车间B", &site.ID)
	if err != nil {
		t.Fatal(err)
	}

	op, act, err := facA.CreatePerson(ctx, saTok, "op-a", "操作员A")
	if err != nil {
		t.Fatal(err)
	}
	if err := facA.Activate(ctx, "op-a", act, "op-pass"); err != nil {
		t.Fatal(err)
	}
	opGrant, err := facA.GrantRole(ctx, saTok, op.ID, factory.RoleOperator, factory.ScopeFactory, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := facA.Assign(ctx, saTok, op.ID, shopA.ID); err != nil {
		t.Fatal(err)
	}
	if err := facA.Assign(ctx, saTok, op.ID, shopB.ID); err != nil {
		t.Fatal(err)
	}

	credV1, err := facA.IssuePersonOfflineGrant(ctx, saTok, op.ID, cidA, nb, na)
	if err != nil {
		t.Fatal(err)
	}

	offline := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
	offline.AssetAllowed = true
	offline.ApplyRuntime(runtime)
	offline.ApplyPerson(credV1)

	online := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
	online.Connected = true
	online.AssetAllowed = true
	online.ApplyRuntime(runtime)
	online.ApplyPerson(credV1)

	var factA16, factA14 factory.FactStub
	var credLatest factory.PersonCred

	run("16.1", func(t *testing.T) {
		var err error
		factA16, err = facA.CreateOfflineFact(ctx, offline, valid, "op-a", "op-pass", factory.WorkContext{OrgUnitID: &shopA.ID})
		if err != nil {
			t.Fatal(err)
		}
		if factA16.OrgUnitID == nil || *factA16.OrgUnitID != shopA.ID {
			t.Fatalf("not A: %+v", factA16)
		}
		if !pathHas(factA16.OrgPath, site.ID) || !pathHas(factA16.OrgPath, shopA.ID) || pathHas(factA16.OrgPath, shopB.ID) {
			t.Fatalf("path drifted to B: %+v", factA16.OrgPath)
		}
	})

	if err := facA.Unassign(ctx, saTok, op.ID, shopA.ID); err != nil {
		t.Fatal(err)
	}
	if err := facA.ReparentOrgUnit(ctx, saTok, shopA.ID, &shopB.ID); err != nil {
		t.Fatal(err)
	}
	if err := facA.RenameOrgUnit(ctx, saTok, shopB.ID, "车间B改名"); err != nil {
		t.Fatal(err)
	}

	run("16.2", func(t *testing.T) {
		got, err := facA.GetFact(ctx, saTok, factA16.ID)
		if err != nil {
			t.Fatal(err)
		}
		if pathName(got.OrgPath, shopA.ID) != "车间A" || pathHas(got.OrgPath, shopB.ID) {
			t.Fatalf("rewritten: %+v", got.OrgPath)
		}
		if err := facA.RewriteFactPath(ctx, saTok, factA16.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("rewrite: %v", err)
		}
	})

	run("16.3", func(t *testing.T) {
		listed, err := facA.ListFactsByCreator(ctx, saTok, op.ID)
		if err != nil {
			t.Fatal(err)
		}
		var nB int
		for _, f := range listed {
			if f.ID == factA16.ID && (f.OrgUnitID == nil || *f.OrgUnitID != shopA.ID || pathHas(f.OrgPath, shopB.ID)) {
				t.Fatalf("16.1 counted as B: %+v", f)
			}
			if f.OrgUnitID != nil && *f.OrgUnitID == shopB.ID {
				nB++
			}
		}
		if nB != 0 {
			t.Fatalf("unselected B counted: %d", nB)
		}
	})

	run("14.2", func(t *testing.T) {
		var err error
		factA14, err = facA.CreateOfflineFact(ctx, offline, valid, "op-a", "op-pass", factory.WorkContext{OrgUnitID: &shopA.ID})
		if err != nil {
			t.Fatal(err)
		}
		if factA14.OrgUnitID == nil || *factA14.OrgUnitID != shopA.ID || pathHas(factA14.OrgPath, shopB.ID) || pathName(factA14.OrgPath, shopA.ID) != "车间A" {
			t.Fatalf("old snapshot lost: %+v", factA14)
		}
		rows, err := facA.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if audit.ContainsAny(audit.Dump(rows), "op-pass", credV1.PasswordHash) {
			t.Fatal("secret leaked")
		}
		found := false
		for _, r := range rows {
			if r.Action == "create_fact" && r.Result == audit.Allow && r.TimeSource == audit.Local && r.Target == factA14.ID.String() {
				found = true
				if r.OrgUnitID == nil || *r.OrgUnitID != shopA.ID || !pathHas(factA14.OrgPath, shopA.ID) {
					t.Fatalf("audit path: %+v", r)
				}
			}
		}
		if !found {
			t.Fatal("missing local create_fact audit")
		}
	})

	run("14.1", func(t *testing.T) {
		v2, err := facA.IssuePersonOfflineGrant(ctx, saTok, op.ID, cidA, nb, na)
		if err != nil {
			t.Fatal(err)
		}
		online.ApplyPerson(v2)
		if _, err := facA.CreateOfflineFact(ctx, online, valid, "op-a", "op-pass", factory.WorkContext{OrgUnitID: &shopA.ID}); !errors.Is(err, domain.ErrWorkContext) {
			t.Fatalf("old unit A: %v", err)
		}
		factB, err := facA.CreateOfflineFact(ctx, online, valid, "op-a", "op-pass", factory.WorkContext{OrgUnitID: &shopB.ID})
		if err != nil {
			t.Fatal(err)
		}
		if factB.OrgUnitID == nil || *factB.OrgUnitID != shopB.ID || pathName(factB.OrgPath, shopB.ID) != "车间B改名" || pathHas(factB.OrgPath, shopA.ID) {
			t.Fatalf("new tree not applied: %+v", factB.OrgPath)
		}
		if err := facA.RevokeRole(ctx, saTok, opGrant.ID); err != nil {
			t.Fatal(err)
		}
		v3, err := facA.IssuePersonOfflineGrant(ctx, saTok, op.ID, cidA, nb, na)
		if err != nil {
			t.Fatal(err)
		}
		online.ApplyPerson(v3)
		ev, err := facA.EvaluateOfflineOp(ctx, online, valid, "op-a", "op-pass")
		if err != nil || ev.Decision != factory.NodeDeny || ev.TimeSource != audit.Server {
			t.Fatalf("revoke %+v %v", ev, err)
		}
		if err := facA.DisableAccount(ctx, saTok, op.ID); err != nil {
			t.Fatal(err)
		}
		v4, err := facA.IssuePersonOfflineGrant(ctx, saTok, op.ID, cidA, nb, na)
		if err != nil {
			t.Fatal(err)
		}
		online.ApplyPerson(v4)
		login, err := facA.LoginOffline(ctx, online, valid, "op-a", "op-pass")
		if err != nil || login.Decision != factory.NodeDeny {
			t.Fatalf("disable %+v %v", login, err)
		}
		listed, err := facA.ListFactsByCreator(ctx, saTok, op.ID)
		if err != nil {
			t.Fatal(err)
		}
		var nA int
		for _, f := range listed {
			if f.OrgUnitID != nil && *f.OrgUnitID == shopA.ID {
				nA++
			}
		}
		if nA != 2 {
			t.Fatalf("new old-range facts: %d", nA)
		}
		credLatest = v4
		rows, err := facA.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !audit.HasResult(rows, "create_fact", audit.Deny) || !audit.HasResult(rows, "create_fact", audit.Allow) {
			t.Fatal("14.1 audit missing")
		}
		if !audit.HasResult(rows, "person_open", audit.Deny) || !audit.HasResult(rows, "person_login", audit.Deny) {
			t.Fatal("14.1 intent audit missing")
		}
	})

	run("14.3", func(t *testing.T) {
		offline.Connected = true
		offline.ApplyPerson(credLatest)
		ev, err := facA.EvaluateOfflineOp(ctx, offline, valid, "op-a", "op-pass")
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("reconnect %+v %v", ev, err)
		}
		got, err := facA.GetFact(ctx, saTok, factA14.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.OrgUnitID == nil || *got.OrgUnitID != shopA.ID || pathName(got.OrgPath, shopA.ID) != "车间A" || pathHas(got.OrgPath, shopB.ID) {
			t.Fatalf("14.2 rewritten: %+v", got.OrgPath)
		}
	})

	run("15.1", func(t *testing.T) {
		offline.ApplyPerson(credV1)
		if offline.AcceptedPersonRevision != credLatest.Revision {
			t.Fatalf("rolled back to %d", offline.AcceptedPersonRevision)
		}
		login, err := facA.LoginOffline(ctx, offline, valid, "op-a", "op-pass")
		if err != nil || login.Decision != factory.NodeDeny {
			t.Fatalf("stale grant %+v %v", login, err)
		}
		rows, err := facA.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !audit.HasResult(rows, "person_login", audit.Deny) {
			t.Fatal("rollback not audited")
		}
	})
}
