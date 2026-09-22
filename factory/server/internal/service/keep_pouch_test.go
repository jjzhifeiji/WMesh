// 每人退出后是否保留示教器库文件：默认留，只有工厂超管能改。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

func TestPersonKeepPouch(t *testing.T) {
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
	if !op.acc.KeepPouch {
		t.Fatal("new person should keep pouch")
	}
	if _, err := fac.SetKeepPouch(ctx, op.tok, op.acc.ID, false); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("operator set: %v", err)
	}
	got, err := fac.SetKeepPouch(ctx, saTok, op.acc.ID, false)
	if err != nil || got.KeepPouch {
		t.Fatalf("sa clear: %+v %v", got, err)
	}
	sess, err := fac.LoginPad(ctx, "op", "op-pass")
	if err != nil || sess.Account.KeepPouch {
		t.Fatalf("pad login keep: %+v %v", sess.Account, err)
	}
	if _, err := fac.SetKeepPouch(ctx, saTok, op.acc.ID, true); err != nil {
		t.Fatal(err)
	}
	sess, err = fac.LoginPad(ctx, "op", "op-pass")
	if err != nil || !sess.Account.KeepPouch {
		t.Fatalf("pad login keep again: %+v %v", sess.Account, err)
	}
	if !audit.ContainsAny(mustAudit(t, ctx, fac), "set_keep_pouch") {
		t.Fatal("missing set_keep_pouch audit")
	}
}

func mustAudit(t *testing.T, ctx context.Context, fac *factory.Service) string {
	t.Helper()
	return audit.Dump(mustRows(t, ctx, fac))
}

func mustRows(t *testing.T, ctx context.Context, fac *factory.Service) []audit.Row {
	t.Helper()
	rows, err := fac.ListAudit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
