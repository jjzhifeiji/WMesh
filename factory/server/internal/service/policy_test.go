// F1：本厂一行 Client 策略；超管可改，非超管拒绝，修订只向前。
package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

func TestClientPolicy(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	got, err := fac.GetClientPolicy(ctx, saTok)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 0 || got.MaxCachedProjects != 2 || got.CacheScope != factory.CacheScopeAll ||
		got.PersistUnwrapKey || got.KeyTTLSeconds != 0 || !bytes.Equal(got.Extra, []byte(`{}`)) {
		t.Fatalf("default %+v extra=%s", got, got.Extra)
	}

	wantExtra := json.RawMessage(`{"note":1}`)
	saved, err := fac.SetClientPolicy(ctx, saTok, factory.ClientPolicy{
		MaxCachedProjects: 4,
		CacheScope:        factory.CacheScopeCurrent,
		PersistUnwrapKey:  true,
		KeyTTLSeconds:     3600,
		Extra:             wantExtra,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 || saved.MaxCachedProjects != 4 || saved.CacheScope != factory.CacheScopeCurrent ||
		!saved.PersistUnwrapKey || saved.KeyTTLSeconds != 3600 {
		t.Fatalf("saved %+v", saved)
	}
	var extra map[string]any
	if err := json.Unmarshal(saved.Extra, &extra); err != nil || extra["note"] != float64(1) {
		t.Fatalf("extra %s", saved.Extra)
	}

	if _, err := fac.SetClientPolicy(ctx, saTok, factory.ClientPolicy{
		MaxCachedProjects: 4, CacheScope: "both", Extra: json.RawMessage(`{}`),
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("bad scope: %v", err)
	}
	if _, err := fac.SetClientPolicy(ctx, saTok, factory.ClientPolicy{
		MaxCachedProjects: 4, CacheScope: factory.CacheScopeCurrent, KeyTTLSeconds: -1, Extra: json.RawMessage(`{}`),
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("bad ttl: %v", err)
	}
	if _, err := fac.SetClientPolicy(ctx, saTok, factory.ClientPolicy{
		MaxCachedProjects: 0, CacheScope: factory.CacheScopeCurrent, Extra: json.RawMessage(`{}`),
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("bad max: %v", err)
	}
	still, err := fac.GetClientPolicy(ctx, saTok)
	if err != nil || still.Revision != 1 || still.MaxCachedProjects != 4 {
		t.Fatalf("unchanged after invalid %+v %v", still, err)
	}

	if _, err := fac.GetClientPolicy(ctx, op.tok); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("op get: %v", err)
	}
	if _, err := fac.SetClientPolicy(ctx, op.tok, factory.ClientPolicy{
		MaxCachedProjects: 9, CacheScope: factory.CacheScopeAll, Extra: json.RawMessage(`{}`),
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("op set: %v", err)
	}
	afterDeny, err := fac.GetClientPolicy(ctx, saTok)
	if err != nil || afterDeny.Revision != 1 || afterDeny.MaxCachedProjects != 4 {
		t.Fatalf("op must not write %+v %v", afterDeny, err)
	}

	if err := fac.SetCacheLimit(ctx, saTok, 5); err != nil {
		t.Fatal(err)
	}
	afterLimit, err := fac.GetClientPolicy(ctx, saTok)
	if err != nil || afterLimit.Revision != 2 || afterLimit.MaxCachedProjects != 5 ||
		afterLimit.CacheScope != factory.CacheScopeCurrent || !afterLimit.PersistUnwrapKey {
		t.Fatalf("cache limit %+v %v", afterLimit, err)
	}

	rows, err := fac.ListAudit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bad := audit.Incomplete(rows); len(bad) > 0 {
		t.Fatalf("incomplete audit %#v", bad[0])
	}
	dump := audit.Dump(rows)
	if !audit.ContainsAny(dump, "set_client_policy") {
		t.Fatalf("missing set_client_policy: %s", dump)
	}
	if audit.ContainsAny(dump, "sa-pass", "op-pass", seed.ActivationToken, saTok) {
		t.Fatalf("secret leaked")
	}
}
