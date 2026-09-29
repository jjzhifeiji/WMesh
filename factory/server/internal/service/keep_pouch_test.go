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

// 退出后默认保留示教器库，只有超管能改这开关。
func TestPersonKeepPouch(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 留库开关应是这一步的预期值，反了说明没改成。
	if !op.acc.KeepPouch {
		// 留库开关和预期相反，说明缺省或修改没生效。
		t.Fatal("new person should keep pouch")
	}
	// 改留库应因越权被拒绝，放行说明没拦住。
	if _, err := fac.SetKeepPouch(ctx, op.tok, op.acc.ID, false); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("operator set: %v", err)
	}
	// 设置退出后是否留库，失败说明无权修改。
	got, err := fac.SetKeepPouch(ctx, saTok, op.acc.ID, false)
	// 改留库失败或留库开关不对就停，说明没达预期。
	if err != nil || got.KeepPouch {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("sa clear: %+v %v", got, err)
	}
	// 用平板登录厂服，失败说明账号没有对上。
	sess, err := fac.LoginPad(ctx, "op", "op-pass")
	// 平板登录失败或留库开关不对就停，说明没达预期。
	if err != nil || sess.Account.KeepPouch {
		// 留库开关和预期相反，说明缺省或修改没生效。
		t.Fatalf("pad login keep: %+v %v", sess.Account, err)
	}
	// 改留库失败就停，否则后面没有可靠结果。
	if _, err := fac.SetKeepPouch(ctx, saTok, op.acc.ID, true); err != nil {
		// 把改留库的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 用平板登录厂服，失败说明账号没有对上。
	sess, err = fac.LoginPad(ctx, "op", "op-pass")
	// 平板登录失败或留库开关不对就停，说明没达预期。
	if err != nil || !sess.Account.KeepPouch {
		// 留库开关和预期相反，说明缺省或修改没生效。
		t.Fatalf("pad login keep again: %+v %v", sess.Account, err)
	}
	// 审计里不应出现口令或正文，出现了就算泄密。
	if !audit.ContainsAny(mustAudit(t, ctx, fac), "set_keep_pouch") {
		// 应存在的记录没有出现，说明这一步没落下。
		t.Fatal("missing set_keep_pouch audit")
	}
}

// 把审计打成文本交回，拉不到则查不了泄密。
func mustAudit(t *testing.T, ctx context.Context, fac *factory.Service) string {
	// 标成辅助步骤，失败时行号指向真正的用例。
	t.Helper()
	// 把审计打成文本交回，打不出则查不了泄密。
	return audit.Dump(mustRows(t, ctx, fac))
}

// 把审计行交回，拉不到则对不了允许和拒绝。
func mustRows(t *testing.T, ctx context.Context, fac *factory.Service) []audit.Row {
	// 标成辅助步骤，失败时行号指向真正的用例。
	t.Helper()
	// 拉出审计流水，失败则无法核对有没有记账。
	rows, err := fac.ListAudit(ctx)
	// 拉审计失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把拉审计的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	return rows
}
