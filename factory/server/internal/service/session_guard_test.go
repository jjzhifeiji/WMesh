// 会话门：库故障不当未登录、账号变化立刻让示教器下线、没角色的人用不了示教器、MQTT 不能报别人的身份。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// 库连不上（这里用已取消的上下文模拟）不能把有效会话判成未登录，否则示教器会被当成会话失效登出（D-47）。
func TestRequireActiveDBErrorIsNotUnauthorized(t *testing.T) {
	// 准备无取消的上下文，建厂和登录都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下初始超管，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 超管用默认口令登录拿会话。
	tok := mustLogin(t, ctx, fac, "sa", personPass("sa"))
	// 造一个已取消的上下文，读会话必然失败。
	dead, cancel := context.WithCancel(ctx)
	// 立刻取消，模拟库连不上。
	cancel()
	// 读会话失败应照实交回，不是未登录。
	_, err = fac.RequireActive(dead, tok)
	// 判成未登录就会把在线的人踢下线。
	if err == nil || errors.Is(err, domain.ErrUnauthorized) {
		// 把错误打出来并停住。
		t.Fatalf("db error mapped to %v", err)
	}
	// 库恢复后同一会话照常有效。
	if _, err := fac.RequireActive(ctx, tok); err != nil {
		// 会话被误删就停住。
		t.Fatalf("session after db back: %v", err)
	}
	// 真没有这条会话仍回未登录。
	if _, err := fac.RequireActive(ctx, "no-such-token"); !errors.Is(err, domain.ErrUnauthorized) {
		// 把错误打出来并停住。
		t.Fatalf("missing session: %v", err)
	}
}

// 重置密码、停用账号后立刻断开此人在线的示教器连接（D-73）。
func TestResetAndDisableDropOnlineApp(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下初始超管，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 接上假的下行通道，记下按人断开。
	rec := &recDown{}
	// 装上假通道。
	fac.SetClientDown(rec)
	// 超管用默认口令登录拿会话。
	saTok := mustLogin(t, ctx, fac, "sa", personPass("sa"))
	// 建一名操作员。
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass-1", factory.RoleOperator)
	// 操作员在示教器上登录。
	if _, err := fac.LoginPad(ctx, "op", "op-pass-1"); err != nil {
		// 登不上就停住。
		t.Fatalf("pad login: %v", err)
	}
	// 超管重置他的密码。
	if _, err := fac.ResetPassword(ctx, saTok, op.acc.ID); err != nil {
		// 重置失败就停住。
		t.Fatalf("reset: %v", err)
	}
	// 重置后应立刻断开他的示教器连接。
	if n := rec.droppedOf(op.acc.ID); n != 1 {
		// 没断开就停住。
		t.Fatalf("dropped after reset = %d", n)
	}
	// 超管停用他。
	if err := fac.DisableAccount(ctx, saTok, op.acc.ID); err != nil {
		// 停用失败就停住。
		t.Fatalf("disable: %v", err)
	}
	// 停用后同样立刻断开。
	if n := rec.droppedOf(op.acc.ID); n != 2 {
		// 没断开就停住。
		t.Fatalf("dropped after disable = %d", n)
	}
	// 超管自己不该被连带断开。
	if n := rec.droppedOf(mustMe(t, ctx, fac, saTok)); n != 0 {
		// 误断就停住。
		t.Fatalf("sa dropped = %d", n)
	}
}

// 收回最后一种角色后：旧会话对账、拉包、焊次上报、MQTT 都拒，重新登录也拒，在线连接立刻断开；
// 还剩别的角色则照常用（D-76）。
func TestRevokeLastRoleBlocksPad(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下初始超管，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 接上假的下行通道，记下按人断开。
	rec := &recDown{}
	// 装上假通道。
	fac.SetClientDown(rec)
	// 超管用默认口令登录拿会话。
	saTok := mustLogin(t, ctx, fac, "sa", personPass("sa"))
	// 建一名只有操作员角色的人。
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass-1", factory.RoleOperator)
	// 他在示教器上登录。
	sess, err := fac.LoginPad(ctx, "op", "op-pass-1")
	// 登不上就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatalf("pad login: %v", err)
	}
	// 收回前对账照常。
	if _, err := fac.PadClientInbox(ctx, sess.Token); err != nil {
		// 对账失败就停住。
		t.Fatalf("inbox before revoke: %v", err)
	}
	// 超管收回他唯一的角色。
	if err := fac.RevokeRole(ctx, saTok, op.grant); err != nil {
		// 收回失败就停住。
		t.Fatalf("revoke: %v", err)
	}
	// 在线的示教器连接应立刻断开。
	if n := rec.droppedOf(op.acc.ID); n != 1 {
		// 没断开就停住。
		t.Fatalf("dropped after revoke = %d", n)
	}
	// 旧令牌对账应按未登录拒，示教器据此登出。
	if _, err := fac.PadClientInbox(ctx, sess.Token); !errors.Is(err, domain.ErrUnauthorized) {
		// 放行就停住。
		t.Fatalf("inbox after revoke: %v", err)
	}
	// 旧令牌拉包同样拒。
	if _, err := fac.PadPullClientClosure(ctx, sess.Token, id.New()); !errors.Is(err, domain.ErrUnauthorized) {
		// 放行就停住。
		t.Fatalf("pull after revoke: %v", err)
	}
	// 旧令牌上报焊次同样拒。
	if _, err := fac.AuthorizeWeldFlush(ctx, sess.Token); !errors.Is(err, domain.ErrUnauthorized) {
		// 放行就停住。
		t.Fatalf("weld flush after revoke: %v", err)
	}
	// 旧令牌连 MQTT 同样拒。
	if _, err := fac.AuthClientMQTT(ctx, op.acc.ID, sess.Token); !errors.Is(err, domain.ErrUnauthorized) {
		// 放行就停住。
		t.Fatalf("mqtt after revoke: %v", err)
	}
	// 没角色的人口令对也不能再登示教器。
	if _, err := fac.LoginPad(ctx, "op", "op-pass-1"); !errors.Is(err, domain.ErrForbidden) {
		// 放行就停住。
		t.Fatalf("pad login without role: %v", err)
	}

	// 另一人握着管理员和操作员两种角色。
	both := mustCreateRole(t, ctx, fac, saTok, "both", "both-pass-1", factory.RoleOperator)
	// 再授管理员。
	if _, err := fac.GrantRole(ctx, saTok, both.acc.ID, factory.RoleOrgAdmin); err != nil {
		// 授不上就停住。
		t.Fatalf("grant admin: %v", err)
	}
	// 他在示教器上登录。
	bs, err := fac.LoginPad(ctx, "both", "both-pass-1")
	// 登不上就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatalf("pad login both: %v", err)
	}
	// 只收回操作员，还剩管理员。
	if err := fac.RevokeRole(ctx, saTok, both.grant); err != nil {
		// 收回失败就停住。
		t.Fatalf("revoke operator of both: %v", err)
	}
	// 还有角色，示教器照常对账。
	if _, err := fac.PadClientInbox(ctx, bs.Token); err != nil {
		// 被误拒就停住。
		t.Fatalf("inbox with admin left: %v", err)
	}
	// 还有角色，不能被断开。
	if n := rec.droppedOf(both.acc.ID); n != 0 {
		// 误断就停住。
		t.Fatalf("both dropped = %d", n)
	}
}

// 绕过收回入口直接改库收走角色，留下的旧会话照样进不了示教器入口（兜底）。
func TestPadEntryRejectsRolelessSession(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下初始超管，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 超管用默认口令登录拿会话。
	saTok := mustLogin(t, ctx, fac, "sa", personPass("sa"))
	// 建一名操作员。
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass-1", factory.RoleOperator)
	// 他在示教器上登录。
	sess, err := fac.LoginPad(ctx, "op", "op-pass-1")
	// 登不上就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatalf("pad login: %v", err)
	}
	// 直接在库里收走角色，会话不动。
	if err := fac.Store().RevokeRoleKeepAdmin(ctx, op.grant); err != nil {
		// 改库失败就停住。
		t.Fatalf("store revoke: %v", err)
	}
	// 旧会话对账应按未登录拒。
	if _, err := fac.PadClientInbox(ctx, sess.Token); !errors.Is(err, domain.ErrUnauthorized) {
		// 放行就停住。
		t.Fatalf("inbox roleless: %v", err)
	}
}

// MQTT 未登记的机器号只能是本人身份，报别人的身份不让连，免得订到别人的下行（D-59）。
func TestAuthClientMQTTUnregisteredMustBeSelf(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下初始超管，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 超管用默认口令登录拿会话。
	saTok := mustLogin(t, ctx, fac, "sa", personPass("sa"))
	// 建两名操作员。
	a := mustCreateRole(t, ctx, fac, saTok, "opa", "opa-pass-1", factory.RoleOperator)
	// 第二名操作员。
	b := mustCreateRole(t, ctx, fac, saTok, "opb", "opb-pass-1", factory.RoleOperator)
	// 甲在示教器上登录。
	sa, err := fac.LoginPad(ctx, "opa", "opa-pass-1")
	// 登不上就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatalf("pad login a: %v", err)
	}
	// 甲用本人身份当机器号连，照常放行。
	if _, err := fac.AuthClientMQTT(ctx, a.acc.ID, sa.Token); err != nil {
		// 被误拒就停住。
		t.Fatalf("self id: %v", err)
	}
	// 甲报乙的身份当机器号，应拒。
	if _, err := fac.AuthClientMQTT(ctx, b.acc.ID, sa.Token); !errors.Is(err, domain.ErrForbidden) {
		// 放行就停住。
		t.Fatalf("other person id: %v", err)
	}
	// 甲报一个谁都不是的机器号，同样拒。
	if _, err := fac.AuthClientMQTT(ctx, id.New(), sa.Token); !errors.Is(err, domain.ErrForbidden) {
		// 放行就停住。
		t.Fatalf("unknown id: %v", err)
	}
}

// mustMe 按会话认出本人身份，认不出就停住。
func mustMe(t *testing.T, ctx context.Context, fac *factory.Service, tok string) uuid.UUID {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 按会话认出本人。
	acc, err := fac.RequireActive(ctx, tok)
	// 认不出就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatal(err)
	}
	return acc.ID
}
