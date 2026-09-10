// 阶段2第4圈：厂内侧节点凭证 1.1～10.1。
package service_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

func testNodeMatrix(t *testing.T, run func(string, func(*testing.T))) {
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
	if _, err := facA.AcceptBinding(ctx, cidA, pubA, 1); err != nil {
		t.Fatal(err)
	}
	facPubA, err := facA.SigningPublicKey(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var cred1 factory.RuntimeCred
	run("1.1", func(t *testing.T) {
		var err error
		cred1, err = facA.IssueRuntimeGrant(ctx, saTok, cidA, nb, na)
		if err != nil {
			t.Fatal(err)
		}
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		bag.ApplyRuntime(cred1)
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		if err != nil || ev.Decision != factory.NodeAllow {
			t.Fatalf("eval %+v %v", ev, err)
		}
	})
	run("1.2", func(t *testing.T) {
		cred2, err := facA.IssueRuntimeGrant(ctx, saTok, cidA, nb, na)
		if err != nil {
			t.Fatal(err)
		}
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		bag.ApplyRuntime(cred2)
		bag.ApplyRuntime(cred1)
		if bag.AcceptedRevision != cred2.Revision || bag.Runtime.Revision != cred2.Revision {
			t.Fatalf("stale overwrite %d", bag.AcceptedRevision)
		}
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		if err != nil || ev.Decision != factory.NodeAllow {
			t.Fatalf("eval %+v %v", ev, err)
		}
	})

	run("2.1", func(t *testing.T) {
		if _, err := facA.IssueRuntimeGrant(ctx, saTok, id.New(), nb, na); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("unbound: %v", err)
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
	run("2.2", func(t *testing.T) {
		if _, err := facA.IssueRuntimeGrant(ctx, saTok, cidB, nb, na); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("other factory: %v", err)
		}
	})
	run("2.3", func(t *testing.T) {
		if err := facA.VoidBinding(ctx, cidA); err != nil {
			t.Fatal(err)
		}
		if _, err := facA.IssueRuntimeGrant(ctx, saTok, cidA, nb, na); !errors.Is(err, domain.ErrBindingVoid) {
			t.Fatalf("voided: %v", err)
		}
		if _, err := facA.AcceptBinding(ctx, cidA, pubA, 2); err != nil {
			t.Fatal(err)
		}
		cred1, err = facA.IssueRuntimeGrant(ctx, saTok, cidA, nb, na)
		if err != nil {
			t.Fatal(err)
		}
	})

	facPubB, err := facB.SigningPublicKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	run("3.1", func(t *testing.T) {
		bag := bagOf(seedB.ID, cidB, pubB, privB, facPubB)
		bag.ApplyRuntime(cred1)
		ev, err := facB.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("cross factory %+v %v", ev, err)
		}
	})
	run("3.2", func(t *testing.T) {
		bag := bagOf(seedB.ID, cidB, pubB, privB, facPubB)
		bag.AssetAllowed = true
		bag.ApplyRuntime(cred1)
		ev, err := facB.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("copied data %+v %v", ev, err)
		}
	})

	run("4.1", func(t *testing.T) {
		bag := bagOf(seedA.ID, cidA, pubA, nil, facPubA)
		bag.Runtime = &cred1
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("no private key %+v %v", ev, err)
		}
	})
	run("4.2", func(t *testing.T) {
		bag := factory.Bag{AssetAllowed: true}
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("asset only %+v %v", ev, err)
		}
	})

	run("5.1", func(t *testing.T) {
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		bag.ApplyRuntime(cred1)
		expired := factory.Clocks{Server: na.Add(time.Hour), Local: na.Add(time.Hour)}
		ev, err := facA.EvaluateNode(ctx, bag, expired, factory.NodeOpen)
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("expired open %+v %v", ev, err)
		}
	})
	run("5.2", func(t *testing.T) {
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		bag.Welding = true
		bag.ApplyRuntime(cred1)
		expired := factory.Clocks{Server: na.Add(time.Hour), Local: na.Add(time.Hour)}
		cont, err := facA.EvaluateNode(ctx, bag, expired, factory.NodeContinue)
		if err != nil || cont.Decision != factory.NodeContinueWeld {
			t.Fatalf("weld %+v %v", cont, err)
		}
		open, err := facA.EvaluateNode(ctx, bag, expired, factory.NodeOpen)
		if err != nil || open.Decision != factory.NodeDeny {
			t.Fatalf("expired new %+v %v", open, err)
		}
	})

	revoked, err := facA.RevokeRuntimeGrant(ctx, saTok, cidA, nb, na)
	if err != nil {
		t.Fatal(err)
	}
	run("6.1", func(t *testing.T) {
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		bag.Connected = true
		bag.ApplyRuntime(cred1)
		bag.ApplyRuntime(revoked)
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("online revoke %+v %v", ev, err)
		}
	})
	run("7.1", func(t *testing.T) {
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		bag.Connected = false
		bag.ApplyRuntime(cred1)
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		if err != nil || ev.Decision != factory.NodeAllow {
			t.Fatalf("offline no revoke %+v %v", ev, err)
		}
	})
	run("7.2", func(t *testing.T) {
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		bag.Connected = true
		bag.ApplyRuntime(cred1)
		bag.ApplyRuntime(revoked)
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("reconnect %+v %v", ev, err)
		}
	})
	run("8.1", func(t *testing.T) {
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		bag.ApplyRuntime(revoked)
		bag.ApplyRuntime(cred1)
		ev, err := facA.EvaluateNode(ctx, bag, valid, factory.NodeOpen)
		if err != nil || ev.Decision != factory.NodeDeny {
			t.Fatalf("no rollback %+v %v", ev, err)
		}
	})

	fresh, err := facA.IssueRuntimeGrant(ctx, saTok, cidA, nb, na)
	if err != nil {
		t.Fatal(err)
	}
	run("9.1", func(t *testing.T) {
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		bag.Connected = true
		bag.ApplyRuntime(fresh)
		skew := factory.Clocks{Server: now, Local: na.Add(2 * time.Hour)}
		ev, err := facA.EvaluateNode(ctx, bag, skew, factory.NodeOpen)
		if err != nil || ev.Decision != factory.NodeAllow || ev.TimeSource != audit.Server {
			t.Fatalf("server clock %+v %v", ev, err)
		}
	})
	run("9.2", func(t *testing.T) {
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		bag.Connected = false
		bag.ApplyRuntime(fresh)
		skew := factory.Clocks{Server: now, Local: na.Add(2 * time.Hour)}
		ev, err := facA.EvaluateNode(ctx, bag, skew, factory.NodeOpen)
		if err != nil || ev.Decision != factory.NodeDeny || ev.TimeSource != audit.Local {
			t.Fatalf("local clock %+v %v", ev, err)
		}
	})
	run("10.1", func(t *testing.T) {
		bag := bagOf(seedA.ID, cidA, pubA, privA, facPubA)
		bag.ApplyRuntime(fresh)
		if err := facA.RecordAnomaly(ctx, cidA, "root"); err != nil {
			t.Fatal(err)
		}
		key, err := facA.Store().SigningKey(ctx)
		if err != nil || len(key.PrivateKey) == 0 {
			t.Fatalf("key wiped %v", err)
		}
		cl, err := facA.Store().ClientByID(ctx, cidA)
		if err != nil || cl.Status != factory.ClientStatusBound {
			t.Fatalf("client disabled %+v %v", cl, err)
		}
		if !bytes.Equal(bag.PrivateKey, privA) {
			t.Fatal("bag key cleared")
		}
		rows, err := facA.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if audit.ContainsAny(audit.Dump(rows), hex.EncodeToString(privA), hex.EncodeToString(key.PrivateKey)) {
			t.Fatal("private key in audit")
		}
	})
}

func bagOf(factoryID, clientID uuid.UUID, pub, priv, facPub []byte) factory.Bag {
	return factory.Bag{
		ClientID:      clientID,
		PublicKey:     pub,
		PrivateKey:    priv,
		FactoryID:     factoryID,
		FactoryPublic: facPub,
	}
}
