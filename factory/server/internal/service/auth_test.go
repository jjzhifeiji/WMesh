// 第 4 圈：工厂初始化、激活、登录退出、账号状态。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
)

// 验收初始化、激活、登录退出，以及账号状态流转。
func TestAuthCircle(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	created, facA, err := h.Provision(ctx, "sa-a", "初始超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("bootstrap: %v", err)
	}
	// 激活码应是八位，位数不对说明签发坏了。
	if len(created.ActivationToken) != 8 {
		// 资产编号和预期不一致，说明发号规则偏了。
		t.Fatalf("activation code len %d", len(created.ActivationToken))
	}
	// 初始化后应只有一名超管，人数不对说明建厂偏了。
	if n, _ := facA.Store().PersonCount(ctx); n != 1 {
		// 条目 2.1 没对上，这一条矩阵行为偏了。
		t.Fatalf("2.1 one sa, got %d", n)
	}
	// 登录应因账号未激活被拒绝，放行说明没拦住。
	if _, err := facA.Login(ctx, "sa-a", "not-yet"); !errors.Is(err, domain.ErrAccountPending) {
		// 条目 10.1 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.1 pending login: %v", err)
	}
	// 验会话应因未登录被拒绝，放行说明没拦住。
	if _, err := facA.RequireActive(ctx, "no-session"); !errors.Is(err, domain.ErrUnauthorized) {
		// 条目 10.1 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.1 protect: %v", err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := facA.Activate(ctx, "sa-a", created.ActivationToken, "sa-pass"); err != nil {
		// 条目 2.4 没对上，这一条矩阵行为偏了。
		t.Fatalf("2.4 activate: %v", err)
	}
	// 登录换取会话令牌，失败说明口令或状态不对。
	saTok, err := facA.Login(ctx, "sa-a", "sa-pass")
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 条目 2.4 没对上，这一条矩阵行为偏了。
		t.Fatalf("2.4 login A: %v", err)
	}
	// 验会话失败就停，否则后面没有可靠结果。
	if _, err := facA.RequireActive(ctx, saTok); err != nil {
		// 条目 2.4 没对上，这一条矩阵行为偏了。
		t.Fatalf("2.4 protect: %v", err)
	}

	// 建厂并签发超管激活码，失败则没有厂可测。
	createdB, facB, err := h.Provision(ctx, "sa-b", "厂B超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("create B: %v", err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := facB.Activate(ctx, "sa-b", createdB.ActivationToken, "sb-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录应因口令错误被拒绝，放行说明没拦住。
	if _, err := facB.Login(ctx, "sa-a", "sa-pass"); !errors.Is(err, domain.ErrInvalidCredentials) {
		// 条目 2.3 没对上，这一条矩阵行为偏了。
		t.Fatalf("2.3/6.5 login B: %v", err)
	}
	// 验会话应因未登录被拒绝，放行说明没拦住。
	if _, err := facB.RequireActive(ctx, saTok); !errors.Is(err, domain.ErrUnauthorized) {
		// 条目 2.3 没对上，这一条矩阵行为偏了。
		t.Fatalf("2.3 manage B: %v", err)
	}

	// 登录应因口令错误被拒绝，放行说明没拦住。
	if _, err := facA.Login(ctx, "ghost", "x"); !errors.Is(err, domain.ErrInvalidCredentials) {
		// 条目 17.2 没对上，这一条矩阵行为偏了。
		t.Fatalf("17.2 unknown: %v", err)
	}
	// 改口令失败就停，否则后面没有可靠结果。
	if err := facA.ChangePassword(ctx, saTok, "sa-pass-2"); err != nil {
		// 条目 17.3 没对上，这一条矩阵行为偏了。
		t.Fatalf("17.3 change pass: %v", err)
	}

	// 注销失败就停，否则后面没有可靠结果。
	if err := facA.Logout(ctx, saTok); err != nil {
		// 条目 10.3 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.3 logout: %v", err)
	}
	// 验会话应因未登录被拒绝，放行说明没拦住。
	if _, err := facA.RequireActive(ctx, saTok); !errors.Is(err, domain.ErrUnauthorized) {
		// 条目 10.3 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.3 old session: %v", err)
	}
	// 登录换取会话令牌，失败说明口令或状态不对。
	saTok, err = facA.Login(ctx, "sa-a", "sa-pass-2")
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("relogin: %v", err)
	}
	// 新建厂内人员，失败说明登录名冲突或越权。
	temp, err := facA.CreatePerson(ctx, saTok, "temp", "临时")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("create temp: %v", err)
	}
	// 把默认口令改成测试口令，失败说明改密被拒。
	tempTok := mustAdoptPassword(t, ctx, facA, "temp", "temp-pass")
	// 停用账号失败就停，否则后面没有可靠结果。
	if err := facA.DisableAccount(ctx, saTok, temp.ID); err != nil {
		// 停用之后仍能操作，说明停用没有生效。
		t.Fatalf("disable: %v", err)
	}
	// 验会话应因账号已停用被拒绝，放行说明没拦住。
	if _, err := facA.RequireActive(ctx, tempTok); !errors.Is(err, domain.ErrAccountDisabled) {
		// 条目 10.4 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.4 after disable: %v", err)
	}
	// 登录应因账号已停用被拒绝，放行说明没拦住。
	if _, err := facA.Login(ctx, "temp", "temp-pass"); !errors.Is(err, domain.ErrAccountDisabled) {
		// 条目 10.2 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.2 login disabled: %v", err)
	}
	// 恢复账号失败就停，否则后面没有可靠结果。
	if err := facA.EnableAccount(ctx, saTok, temp.ID); err != nil {
		// 条目 10.9 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.9 enable: %v", err)
	}
	// 登录失败就停，否则后面没有可靠结果。
	if _, err := facA.Login(ctx, "temp", "temp-pass"); err != nil {
		// 条目 10.9 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.9 login after enable: %v", err)
	}
	// 重置口令应因越权被拒绝，放行说明没拦住。
	if _, err := facA.ResetPassword(ctx, saTok, created.SuperAdminID); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("reset self: %v", err)
	}
	// 把口令重置回默认，失败说明无权重置。
	resetTok, err := facA.ResetPassword(ctx, saTok, temp.ID)
	// 重置口令失败就停，否则后面没有可靠结果。
	if err != nil {
		// 条目 10.10 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.10 reset: %v", err)
	}
	// 状态应符合这一步的预期，停在旧状态说明没流转。
	if resetTok.Status != "active" {
		// 条目 10.10 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.10 status %s", resetTok.Status)
	}
	// 登录应因口令错误被拒绝，放行说明没拦住。
	if _, err := facA.Login(ctx, "temp", "temp-pass"); !errors.Is(err, domain.ErrInvalidCredentials) {
		// 条目 10.10 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.10 old password: %v", err)
	}
	// 登录失败就停，否则后面没有可靠结果。
	if _, err := facA.Login(ctx, "temp", personPass("temp")); err != nil {
		// 条目 10.10 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.10 default password: %v", err)
	}

	// 拉出审计流水，失败则无法核对有没有记账。
	facAudit, err := facA.ListAudit(ctx)
	// 拉审计失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把拉审计的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 把审计打成可搜文本，打不出则查不了泄密。
	dump := audit.Dump(facAudit)
	// 算出默认口令，首次登录要用它而不是新口令。
	secrets := []string{"sa-pass", "sa-pass-2", "temp-pass", personPass("temp"), created.ActivationToken, saTok, tempTok}
	// 审计里不应出现口令或正文，出现了就算泄密。
	if audit.ContainsAny(dump, secrets...) {
		// 结果里出现了不该有的敏感词，说明已经泄密。
		t.Fatalf("17 secret leaked in audit")
	}
	// 审计应留下允许或拒绝，缺了说明这步没记账。
	if !audit.HasResult(facAudit, "activate", audit.Allow) || !audit.HasResult(facAudit, "change_password", audit.Allow) {
		// 条目 17.3 没对上，这一条矩阵行为偏了。
		t.Fatalf("17.3 factory audit missing")
	}
	// 审计应留下允许或拒绝，缺了说明这步没记账。
	if !audit.HasResult(facAudit, "reset_password", audit.Allow) || !audit.HasResult(facAudit, "reset_password", audit.Deny) {
		// 条目 10.10 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.10 reset audit missing")
	}
	// 审计应留下允许或拒绝，缺了说明这步没记账。
	if !audit.HasResult(facAudit, "logout", audit.Allow) {
		// 条目 10.3 没对上，这一条矩阵行为偏了。
		t.Fatalf("10.3 no logout audit")
	}
}
