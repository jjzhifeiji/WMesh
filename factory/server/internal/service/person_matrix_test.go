// 阶段2第5圈：人员离线授权 11.1～13.4。
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

func TestPersonMatrix(t *testing.T) {
	ctx := context.Background()
	ids := []string{"11.1", "12.1", "12.2", "12.3", "12.4", "13.1", "13.2", "13.3", "13.4"}
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

	op, act, err := facA.CreatePerson(ctx, saTok, "op-a", "操作员A")
	if err != nil {
		t.Fatal(err)
	}
	if err := facA.Activate(ctx, "op-a", act, "op-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := facA.GrantRole(ctx, saTok, op.ID, factory.RoleOperator, factory.ScopeFactory, nil); err != nil {
		t.Fatal(err)
	}
	personCred, err := facA.IssuePersonOfflineGrant(ctx, saTok, op.ID, cidA, nb, na)
	if err != nil {
		t.Fatal(err)
	}

	bagA := func() factory.Bag {
		b := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		b.AssetAllowed = true
		b.ApplyRuntime(runtime)
		b.ApplyPerson(personCred)
		return b
	}

	run("11.1", func(t *testing.T) {
		b := bagA()
		login, err := facA.LoginOffline(ctx, b, valid, "op-a", "op-pass")
		if err != nil || login.Decision != factory.NodeAllow {
			t.Fatalf("login %+v %v", login, err)
		}
		ev, err := facA.EvaluateOfflineOp(ctx, b, valid, "op-a", "op-pass")
		if err != nil || ev.Decision != factory.NodeAllow {
			t.Fatalf("op %+v %v", ev, err)
		}
		rows, err := facA.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if audit.ContainsAny(audit.Dump(rows), "op-pass", personCred.PasswordHash) {
			t.Fatal("secret leaked")
		}
	})
	run("12.1", func(t *testing.T) {
		b := bagA()
		ev, err := facA.LoginOffline(ctx, b, valid, "op-a", "")
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("copied grant without password %+v %v", ev, err)
		}
	})
	cidB := id.New()
	pubB, privB, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := facB.AcceptBinding(ctx, cidB, pubB, 1); err != nil {
		t.Fatal(err)
	}
	facPubB, err := facB.SigningPublicKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	run("12.2", func(t *testing.T) {
		b := bagOf(seedA.ID, cidB, pubB, privB, facPubA)
		b.ApplyPerson(personCred)
		ev, err := facA.LoginOffline(ctx, b, valid, "op-a", "op-pass")
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("other client %+v %v", ev, err)
		}
	})
	other, otherAct, err := facA.CreatePerson(ctx, saTok, "op-c", "操作员C")
	if err != nil {
		t.Fatal(err)
	}
	if err := facA.Activate(ctx, "op-c", otherAct, "c-pass"); err != nil {
		t.Fatal(err)
	}
	_ = other
	run("12.3", func(t *testing.T) {
		b := bagA()
		ev, err := facA.LoginOffline(ctx, b, valid, "op-a", "c-pass")
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("other password %+v %v", ev, err)
		}
	})
	run("12.4", func(t *testing.T) {
		b := bagOf(seedB.ID, cidB, pubB, privB, facPubB)
		b.ApplyPerson(personCred)
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
	aud, audAct, err := facA.CreatePerson(ctx, saTok, "aud-a", "审计员")
	if err != nil {
		t.Fatal(err)
	}
	if err := facA.Activate(ctx, "aud-a", audAct, "aud-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := facA.GrantRole(ctx, saTok, aud.ID, factory.RoleAuditor, factory.ScopeFactory, nil); err != nil {
		t.Fatal(err)
	}
	audCred, err := facA.IssuePersonOfflineGrant(ctx, saTok, aud.ID, cidA, nb, na)
	if err != nil {
		t.Fatal(err)
	}
	run("13.2", func(t *testing.T) {
		b := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		b.AssetAllowed = true
		b.ApplyRuntime(runtime)
		b.ApplyPerson(audCred)
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

func TestIssuePersonOfflineRejectsUnbound(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	tok, err := fac.Login(ctx, "sa", "sa-pass")
	if err != nil {
		t.Fatal(err)
	}
	p, act, err := fac.CreatePerson(ctx, tok, "op", "操作员")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "op", act, "op-pass"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := fac.IssuePersonOfflineGrant(ctx, tok, p.ID, id.New(), now, now.Add(time.Hour)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unbound: %v", err)
	}
}
