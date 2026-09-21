// 本厂治理状态：停用后拒绝登录，修订只向前。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/service"
)

func TestFactoryLifecycle(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	created, fac, err := h.Provision(ctx, "sa-a", "初始超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa-a", created.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	tok, err := fac.Login(ctx, "sa-a", "sa-pass")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Store().PutFactoryName(ctx, "一号厂"); err != nil {
		t.Fatal(err)
	}

	out, err := fac.ApplyLifecycle(ctx, service.FactoryDisabled, 1)
	if err != nil || out.Status != service.FactoryDisabled || out.Name != "一号厂" {
		t.Fatalf("disable %+v %v", out, err)
	}
	if _, err := fac.Login(ctx, "sa-a", "sa-pass"); !errors.Is(err, domain.ErrFactoryDisabled) {
		t.Fatalf("login disabled: %v", err)
	}
	if _, err := fac.RequireActive(ctx, tok); !errors.Is(err, domain.ErrFactoryDisabled) {
		t.Fatalf("protect disabled: %v", err)
	}
	same, err := fac.ApplyLifecycle(ctx, service.FactoryActive, 0)
	if err != nil || same.Status != service.FactoryDisabled || same.Revision != 1 {
		t.Fatalf("stale %+v %v", same, err)
	}
	if _, err := fac.ApplyLifecycle(ctx, service.FactoryActive, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.Login(ctx, "sa-a", "sa-pass"); err != nil {
		t.Fatalf("login after enable: %v", err)
	}
	if _, err := fac.ApplyLifecycle(ctx, service.FactoryRetired, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.Login(ctx, "sa-a", "sa-pass"); !errors.Is(err, domain.ErrFactoryRetired) {
		t.Fatalf("login retired: %v", err)
	}
}

func TestCloseFromWAN(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	created, fac, err := h.Provision(ctx, "sa-a", "初始超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa-a", created.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	if err := fac.CloseFromWAN(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.Login(ctx, "sa-a", "sa-pass"); !errors.Is(err, domain.ErrFactoryRetired) {
		t.Fatalf("login after wan gone: %v", err)
	}
}
