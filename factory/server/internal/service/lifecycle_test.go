// 本厂治理状态：停用后拒绝登录，修订只向前。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/service"
)

// 工厂停用后拒绝登录，恢复后才重新放开。
func TestFactoryLifecycle(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	created, fac, err := h.Provision(ctx, "sa-a", "初始超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa-a", created.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录换取会话令牌，失败说明口令或状态不对。
	tok, err := fac.Login(ctx, "sa-a", "sa-pass")
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把登录的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 写厂名失败就停，否则后面没有可靠结果。
	if err := fac.Store().PutFactoryName(ctx, "一号厂"); err != nil {
		// 把写厂名的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 套用工厂生命周期，失败说明当前状态不许转。
	out, err := fac.ApplyLifecycle(ctx, service.FactoryDisabled, 1)
	// 转生命周期失败或状态不对就停，说明没达预期。
	if err != nil || out.Status != service.FactoryDisabled || out.Name != "一号厂" {
		// 停用之后仍能操作，说明停用没有生效。
		t.Fatalf("disable %+v %v", out, err)
	}
	// 登录应因工厂已停用被拒绝，放行说明没拦住。
	if _, err := fac.Login(ctx, "sa-a", "sa-pass"); !errors.Is(err, domain.ErrFactoryDisabled) {
		// 停用之后仍能操作，说明停用没有生效。
		t.Fatalf("login disabled: %v", err)
	}
	// 验会话应因工厂已停用被拒绝，放行说明没拦住。
	if _, err := fac.RequireActive(ctx, tok); !errors.Is(err, domain.ErrFactoryDisabled) {
		// 停用之后仍能操作，说明停用没有生效。
		t.Fatalf("protect disabled: %v", err)
	}
	// 套用工厂生命周期，失败说明当前状态不许转。
	same, err := fac.ApplyLifecycle(ctx, service.FactoryActive, 0)
	// 转生命周期失败或修订不对就停，说明没达预期。
	if err != nil || same.Status != service.FactoryDisabled || same.Revision != 1 {
		// 旧修订盖住了新稿，说明先后比较写反了。
		t.Fatalf("stale %+v %v", same, err)
	}
	// 转生命周期失败就停，否则后面没有可靠结果。
	if _, err := fac.ApplyLifecycle(ctx, service.FactoryActive, 2); err != nil {
		// 把转生命周期的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录失败就停，否则后面没有可靠结果。
	if _, err := fac.Login(ctx, "sa-a", "sa-pass"); err != nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("login after enable: %v", err)
	}
	// 转生命周期失败就停，否则后面没有可靠结果。
	if _, err := fac.ApplyLifecycle(ctx, service.FactoryRetired, 3); err != nil {
		// 把转生命周期的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录应因工厂已退役被拒绝，放行说明没拦住。
	if _, err := fac.Login(ctx, "sa-a", "sa-pass"); !errors.Is(err, domain.ErrFactoryRetired) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("login retired: %v", err)
	}
}

// 从平台关闭工厂后，厂内登录和作业都要被拒绝。
func TestCloseFromWAN(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	created, fac, err := h.Provision(ctx, "sa-a", "初始超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa-a", created.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 平台关厂失败就停，否则后面没有可靠结果。
	if err := fac.CloseFromWAN(ctx); err != nil {
		// 把平台关厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录应因工厂已退役被拒绝，放行说明没拦住。
	if _, err := fac.Login(ctx, "sa-a", "sa-pass"); !errors.Is(err, domain.ErrFactoryRetired) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("login after wan gone: %v", err)
	}
}
