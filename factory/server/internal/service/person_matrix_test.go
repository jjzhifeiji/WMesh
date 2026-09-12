// 阶段2：本厂有效账号在本厂设备上登录与操作 11.1～13.4；不签发人员授权。
package service_test

import (
	"context"
	"testing"
	"time"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

func testPersonMatrix(t *testing.T, run func(string, func(*testing.T))) {
	t.Helper()
	ctx := context.Background()

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
	seedB, facB, err := h.Provision(ctx, "sa-b", "超管B")
	if err != nil {
		t.Fatal(err)
	}
	if err := facB.Activate(ctx, "sa-b", seedB.ActivationToken, "sb-pass"); err != nil {
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
	if _, err := facA.AcceptBinding(ctx, cidA, "Client-A1", pubA, 1); err != nil {
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

	op, err := facA.CreatePerson(ctx, saTok, "op-a", "操作员A")
	if err != nil {
		t.Fatal(err)
	}
	mustAdoptPassword(t, ctx, facA, "op-a", "op-pass")
	if _, err := facA.GrantRole(ctx, saTok, op.ID, factory.RoleOperator, factory.ScopeFactory, nil); err != nil {
		t.Fatal(err)
	}

	bagA := func() factory.Bag {
		b := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		b.AssetAllowed = true
		b.ApplyRuntime(runtime)
		return b
	}

	run("11.1", func(t *testing.T) {
		b := bagA()
		login, err := facA.LoginOffline(ctx, b, valid, "op-a", "op-pass")
		if err != nil || login.Decision != factory.NodeAllow {
			t.Fatalf("login %+v %v", login, err)
		}
		listed, err := facA.ListClients(ctx, saTok)
		if err != nil {
			t.Fatal(err)
		}
		var saw bool
		for _, row := range listed {
			if row.ID == cidA {
				if row.OperatorLogin != "op-a" || row.OperatorDisplay != "操作员A" {
					t.Fatalf("11.1 operator %+v", row)
				}
				saw = true
			}
		}
		if !saw {
			t.Fatal("11.1 missing client")
		}
		ev, err := facA.EvaluateOfflineOp(ctx, b, valid, "op-a", "op-pass")
		if err != nil || ev.Decision != factory.NodeAllow {
			t.Fatalf("op %+v %v", ev, err)
		}
		rows, err := facA.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if audit.ContainsAny(audit.Dump(rows), "op-pass") {
			t.Fatal("secret leaked")
		}
	})
	run("12.1", func(t *testing.T) {
		b := bagA()
		ev, err := facA.LoginOffline(ctx, b, valid, "op-a", "")
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("no password %+v %v", ev, err)
		}
	})
	cidB := id.New()
	pubB, privB, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := facB.AcceptBinding(ctx, cidB, "Client-B1", pubB, 1); err != nil {
		t.Fatal(err)
	}
	facPubB, err := facB.SigningPublicKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cidA2 := id.New()
	pubA2, privA2, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := facA.AcceptBinding(ctx, cidA2, "Client-A2", pubA2, 1); err != nil {
		t.Fatal(err)
	}
	run("12.2", func(t *testing.T) {
		b := bagOf(seedA.ID, cidA2, pubA2, privA2, facPubA)
		ev, err := facA.LoginOffline(ctx, b, valid, "op-a", "op-pass")
		if err != nil || ev.Decision != factory.NodeAllow {
			t.Fatalf("same factory other device %+v %v", ev, err)
		}
	})
	_, err = facA.CreatePerson(ctx, saTok, "op-c", "操作员C")
	if err != nil {
		t.Fatal(err)
	}
	mustAdoptPassword(t, ctx, facA, "op-c", "c-pass")
	run("12.3", func(t *testing.T) {
		b := bagA()
		ev, err := facA.LoginOffline(ctx, b, valid, "op-a", "c-pass")
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("other password %+v %v", ev, err)
		}
	})
	run("12.4", func(t *testing.T) {
		b := bagOf(seedB.ID, cidB, pubB, privB, facPubB)
		ev, err := facB.LoginOffline(ctx, b, valid, "op-a", "op-pass")
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("other factory %+v %v", ev, err)
		}
	})

	run("13.1", func(t *testing.T) {
		b := bagA()
		b.Runtime = nil
		login, err := facA.LoginOffline(ctx, b, valid, "op-a", "op-pass")
		if err != nil || login.Decision != factory.NodeAllow {
			t.Fatalf("person still %+v %v", login, err)
		}
		ev, err := facA.EvaluateOfflineOp(ctx, b, valid, "op-a", "op-pass")
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("node missing %+v %v", ev, err)
		}
	})
	aud, err := facA.CreatePerson(ctx, saTok, "aud-a", "审计员")
	if err != nil {
		t.Fatal(err)
	}
	mustAdoptPassword(t, ctx, facA, "aud-a", "aud-pass")
	if _, err := facA.GrantRole(ctx, saTok, aud.ID, factory.RoleAuditor, factory.ScopeFactory, nil); err != nil {
		t.Fatal(err)
	}
	run("13.2", func(t *testing.T) {
		b := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		b.AssetAllowed = true
		b.ApplyRuntime(runtime)
		login, err := facA.LoginOffline(ctx, b, valid, "aud-a", "aud-pass")
		if err != nil || login.Decision != factory.NodeAllow {
			t.Fatalf("auditor login %+v %v", login, err)
		}
		ev, err := facA.EvaluateOfflineOp(ctx, b, valid, "aud-a", "aud-pass")
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("auditor op %+v %v", ev, err)
		}
	})
	run("13.3", func(t *testing.T) {
		b := bagA()
		ev, err := facA.EvaluateOfflineOp(ctx, b, valid, "op-a", "op-pass")
		if err != nil || ev.Decision != factory.NodeAllow {
			t.Fatalf("all allow %+v %v", ev, err)
		}
	})
	run("13.4", func(t *testing.T) {
		b := bagA()
		b.AssetAllowed = false
		ev, err := facA.EvaluateOfflineOp(ctx, b, valid, "op-a", "op-pass")
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("asset deny %+v %v", ev, err)
		}
	})
}
