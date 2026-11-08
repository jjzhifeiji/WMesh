// 示教器退出时调厂端退出接口，示教器令牌随即失效，退出后迟到的写请求厂端不再收（D-81）。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

// 示教器令牌走同一个退出接口吊销，吊销后再用就被拒。
func TestPadLogoutRevokesAppToken(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下初始超管，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建一个焊工，示教器用他登录。
	mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator)
	// 用平板登录厂服，失败说明账号没有对上。
	sess, err := fac.LoginPad(ctx, "op", "op-pass")
	// 平板登录失败就停，否则后面没有令牌可退。
	if err != nil {
		// 把平板登录的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 退出前令牌应可用，不可用说明登录本身有问题。
	if _, err := fac.RequireActive(ctx, sess.Token); err != nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("pad token before logout: %v", err)
	}
	// 示教器退出吊销令牌，失败说明退出接口不认示教器令牌。
	if err := fac.Logout(ctx, sess.Token); err != nil {
		// 把退出的错误打出来并停住，避免继续往下验。
		t.Fatalf("pad logout: %v", err)
	}
	// 退出后旧令牌应被拒，放行说明迟到的上传还会被收下。
	if _, err := fac.RequireActive(ctx, sess.Token); !errors.Is(err, domain.ErrUnauthorized) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("pad token after logout: %v", err)
	}
}
