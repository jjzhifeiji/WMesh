// 第 4 圈：工厂初始化、激活、登录退出、账号状态。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
)

func TestAuthCircle(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	created, facA, err := h.Provision(ctx, "sa-a", "初始超管")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if n, _ := facA.Store().PersonCount(ctx); n != 1 {
		t.Fatalf("2.1 one sa, got %d", n)
	}
	if _, err := facA.Login(ctx, "sa-a", "not-yet"); !errors.Is(err, domain.ErrAccountPending) {
		t.Fatalf("10.1 pending login: %v", err)
	}
	if _, err := facA.RequireActive(ctx, "no-session"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("10.1 protect: %v", err)
	}
	if err := facA.Activate(ctx, "sa-a", created.ActivationToken, "sa-pass"); err != nil {
		t.Fatalf("2.4 activate: %v", err)
	}
	saTok, err := facA.Login(ctx, "sa-a", "sa-pass")
	if err != nil {
		t.Fatalf("2.4 login A: %v", err)
	}
	if _, err := facA.RequireActive(ctx, saTok); err != nil {
		t.Fatalf("2.4 protect: %v", err)
	}

	createdB, facB, err := h.Provision(ctx, "sa-b", "厂B超管")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	if err := facB.Activate(ctx, "sa-b", createdB.ActivationToken, "sb-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := facB.Login(ctx, "sa-a", "sa-pass"); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("2.3/6.5 login B: %v", err)
	}
	if _, err := facB.RequireActive(ctx, saTok); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("2.3 manage B: %v", err)
	}

	if _, err := facA.Login(ctx, "ghost", "x"); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("17.2 unknown: %v", err)
	}
	if err := facA.ChangePassword(ctx, saTok, "sa-pass-2"); err != nil {
		t.Fatalf("17.3 change pass: %v", err)
	}

	if err := facA.Logout(ctx, saTok); err != nil {
		t.Fatalf("10.3 logout: %v", err)
	}
	if _, err := facA.RequireActive(ctx, saTok); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("10.3 old session: %v", err)
	}
	saTok, err = facA.Login(ctx, "sa-a", "sa-pass-2")
	if err != nil {
		t.Fatalf("relogin: %v", err)
	}
	temp, act, err := facA.CreatePerson(ctx, saTok, "temp", "临时")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	if err := facA.Activate(ctx, "temp", act, "temp-pass"); err != nil {
		t.Fatalf("activate temp: %v", err)
	}
	tempTok, err := facA.Login(ctx, "temp", "temp-pass")
	if err != nil {
		t.Fatalf("login temp: %v", err)
	}
	if err := facA.DisableAccount(ctx, saTok, temp.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := facA.RequireActive(ctx, tempTok); !errors.Is(err, domain.ErrAccountDisabled) {
		t.Fatalf("10.4 after disable: %v", err)
	}
	if _, err := facA.Login(ctx, "temp", "temp-pass"); !errors.Is(err, domain.ErrAccountDisabled) {
		t.Fatalf("10.2 login disabled: %v", err)
	}

	facAudit, err := facA.ListAudit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dump := audit.Dump(facAudit)
	secrets := []string{"sa-pass", "sa-pass-2", "temp-pass", created.ActivationToken, act, saTok, tempTok}
	if audit.ContainsAny(dump, secrets...) {
		t.Fatalf("17 secret leaked in audit")
	}
	if !audit.HasResult(facAudit, "activate", audit.Allow) || !audit.HasResult(facAudit, "change_password", audit.Allow) {
		t.Fatalf("17.3 factory audit missing")
	}
	if !audit.HasResult(facAudit, "logout", audit.Allow) {
		t.Fatalf("10.3 no logout audit")
	}
}
