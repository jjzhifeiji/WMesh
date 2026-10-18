// 第 4 圈：工厂初始化、激活、登录退出、账号状态。
package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
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
	// 停用已把会话删掉，旧令牌应按未登录被拒绝。
	if _, err := facA.RequireActive(ctx, tempTok); !errors.Is(err, domain.ErrUnauthorized) {
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
	// 再启用后停用前的旧令牌仍须按未登录被拒，复活即失败。
	if _, err := facA.RequireActive(ctx, tempTok); !errors.Is(err, domain.ErrUnauthorized) {
		// 条目 10.4 没对上，旧会话随启用又活了。
		t.Fatalf("10.4 old token after enable: %v", err)
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

// 同一待启用账号连错五次后，第六次即使码对也被挡住。
func TestActivateThrottle(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	created, fac, err := h.Provision(ctx, "sa-t", "限次超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("bootstrap: %v", err)
	}
	// 连错五次，每次都应按激活码不对拒绝。
	for i := 0; i < 5; i++ {
		// 错码应被拒绝，放行说明比对坏了。
		if err := fac.Activate(ctx, "sa-t", "wrong-code", "sa-pass"); !errors.Is(err, domain.ErrInvalidActivation) {
			// 前五次就被挡，说明限次给得太少。
			t.Fatalf("try %d: %v", i, err)
		}
	}
	// 名额已用完，码对也应先被限次挡住。
	if err := fac.Activate(ctx, "sa-t", created.ActivationToken, "sa-pass"); !errors.Is(err, domain.ErrTooManyAttempts) {
		// 没挡住说明限次没生效，激活码能被一直猜。
		t.Fatalf("throttled: %v", err)
	}
	// 被挡的那次不应激活，账号仍待启用。
	if _, err := fac.Login(ctx, "sa-t", "sa-pass"); !errors.Is(err, domain.ErrAccountPending) {
		// 能登录说明被挡的请求仍然改了口令。
		t.Fatalf("still pending: %v", err)
	}
	// 拉出审计流水，失败则无法核对有没有记账。
	rows, err := fac.ListAudit(ctx)
	// 拉审计失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把拉审计的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 数出激活拒绝的条数，被挡的那次也要记。
	denies := 0
	// 逐行看审计，只数激活拒绝。
	for _, r := range rows {
		// 激活且拒绝的才算进来。
		if r.Action == "activate" && r.Result == audit.Deny {
			// 再记一条激活拒绝。
			denies++
		}
	}
	// 五次错码加一次被挡，至少六条拒绝。
	if denies < 6 {
		// 少了说明被挡的那次没留审计。
		t.Fatalf("activate deny rows %d", denies)
	}
}

// 认领重试换了口令：新口令生效、旧口令和旧会话作废，同口令再来按幂等成功。
func TestAcceptEnrollmentRetryNewPassword(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 打开数据库连接，失败说明测试库起不来。
	admin := testpg.Open(t)
	// 另开一座空库，认领前不能先有初始超管。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_fac")
	// 迁好库并装上厂服务，失败说明库还不能用。
	fac := service.NewService(store.Open(testpg.OpenMigrated(t, dsn), id.New()))
	// 签发租约失败就停，否则登录过不了开厂检查。
	if err := fac.Store().GrantLocalLease(ctx); err != nil {
		// 把签发租约的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 按 WAN 约定的身份认领，模拟建厂码换来的超管编号。
	saID := id.New()
	// 第一次认领用旧口令，确认前弱网失败也已落库。
	if _, err := fac.AcceptEnrollment(ctx, saID, "sa-e", "认领超管", "enroll-p1"); err != nil {
		// 第一次认领就失败，后面的重试无从谈起。
		t.Fatalf("first claim: %v", err)
	}
	// 用旧口令登录拿会话，后面要验它被作废。
	oldTok, err := fac.Login(ctx, "sa-e", "enroll-p1")
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 旧口令登不上，说明第一次认领没激活。
		t.Fatalf("login p1: %v", err)
	}
	// 同一张建厂码重试，这次填了新口令。
	acc, err := fac.AcceptEnrollment(ctx, saID, "sa-e", "认领超管", "enroll-p2")
	// 重试失败就停，否则后面没有可靠结果。
	if err != nil {
		// 重试被拒说明没按同一次认领处理。
		t.Fatalf("retry claim p2: %v", err)
	}
	// 重试后账号仍应有效，状态变了说明写坏了。
	if acc.Status != "active" {
		// 状态不是有效，说明重试改动了账号状态。
		t.Fatalf("retry status %s", acc.Status)
	}
	// 新口令应能登录，登不上说明新口令被丢了。
	newTok, err := fac.Login(ctx, "sa-e", "enroll-p2")
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 新口令登不上，正是这次要修的问题。
		t.Fatalf("login p2: %v", err)
	}
	// 旧口令应被拒绝，放行说明没覆盖成新口令。
	if _, err := fac.Login(ctx, "sa-e", "enroll-p1"); !errors.Is(err, domain.ErrInvalidCredentials) {
		// 旧口令仍能登录，说明口令没换掉。
		t.Fatalf("login p1 after retry: %v", err)
	}
	// 旧会话应已作废，放行说明换口令没清会话。
	if _, err := fac.RequireActive(ctx, oldTok); !errors.Is(err, domain.ErrUnauthorized) {
		// 旧会话还能用，第一次操作的人仍握着入口。
		t.Fatalf("old session after retry: %v", err)
	}
	// 用同一个新口令再认领一次，应按幂等成功。
	if _, err := fac.AcceptEnrollment(ctx, saID, "sa-e", "认领超管", "enroll-p2"); err != nil {
		// 同口令重试被拒，说明幂等分支坏了。
		t.Fatalf("idempotent claim p2: %v", err)
	}
	// 幂等重试不该清会话，新会话应仍有效。
	if _, err := fac.RequireActive(ctx, newTok); err != nil {
		// 新会话被清掉，说明幂等分支多做了事。
		t.Fatalf("new session after idempotent: %v", err)
	}
	// 新口令仍应能登录，登不上说明幂等时改了口令。
	if _, err := fac.Login(ctx, "sa-e", "enroll-p2"); err != nil {
		// 新口令失效，说明幂等重试动了哈希。
		t.Fatalf("login p2 after idempotent: %v", err)
	}
	// 拉出审计流水，失败则无法核对有没有记账。
	rows, err := fac.ListAudit(ctx)
	// 拉审计失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把拉审计的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 数出建厂允许的条数，换口令那次也要记。
	allows := 0
	// 逐行看审计，只数建厂允许。
	for _, r := range rows {
		// 建厂且允许的才算进来。
		if r.Action == "enroll_factory" && r.Result == audit.Allow {
			// 再记一条建厂允许。
			allows++
		}
	}
	// 首次认领加换口令那次，至少两条允许。
	if allows < 2 {
		// 少了说明换口令那次没留审计。
		t.Fatalf("enroll allow rows %d", allows)
	}
	// 审计里不应出现口令或令牌，出现了就算泄密。
	if audit.ContainsAny(audit.Dump(rows), "enroll-p1", "enroll-p2", oldTok, newTok) {
		// 结果里出现了不该有的敏感词，说明已经泄密。
		t.Fatalf("secret leaked in enroll audit")
	}
}

// 登录名去首尾空白：建档存规范名，网页和示教器都能登，空名、超长和去空白后撞名都拒绝。
func TestLoginNameTrimmed(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂时超管名带首尾空格，模拟建厂表单原样送来。
	created, fac, err := h.Provision(ctx, " boss ", "去空白超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 带空格的名字建不了厂，说明没先规范。
		t.Fatalf("bootstrap: %v", err)
	}
	// 按稳定身份读回超管，看库里存的名字。
	sa, err := fac.Store().PersonByID(ctx, created.SuperAdminID)
	// 读不回或名字仍带空格就停，说明建档没去空白。
	if err != nil || sa.LoginName != "boss" {
		// 库里名字不是 boss，示教器会登不上。
		t.Fatalf("stored sa login %q %v", sa.LoginName, err)
	}
	// 用规范名激活应能成功，失败说明激活没对上名字。
	if err := fac.Activate(ctx, "boss", created.ActivationToken, "boss-pass"); err != nil {
		// 规范名激活不了，说明库里名字和规范不一致。
		t.Fatalf("activate boss: %v", err)
	}
	// 用规范名网页登录，拿到超管会话。
	saTok, err := fac.Login(ctx, "boss", "boss-pass")
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 规范名登不上，说明建档或登录没去空白。
		t.Fatalf("login boss: %v", err)
	}
	// 网页登录带空格也应认得同一个人。
	if _, err := fac.Login(ctx, " boss\t", "boss-pass"); err != nil {
		// 带空格登不上，说明网页登录没去空白。
		t.Fatalf("login padded boss: %v", err)
	}
	// 超管建一个带首尾空格的账号，模拟从表格复制。
	zhang, err := fac.CreatePerson(ctx, saTok, " zhang3 ", "张三")
	// 建人员失败就停，否则后面没有可靠结果。
	if err != nil {
		// 带空格的名字建不出账号，说明没先规范。
		t.Fatalf("create padded: %v", err)
	}
	// 交回的账号名应已去掉空格。
	if zhang.LoginName != "zhang3" {
		// 名字仍带空格，库里存的也是原样。
		t.Fatalf("created login %q", zhang.LoginName)
	}
	// 默认口令按规范名拼，网页登录应能过。
	if _, err := fac.Login(ctx, "zhang3", personPass("zhang3")); err != nil {
		// 网页登不上，说明默认口令按原样名字拼了。
		t.Fatalf("web login zhang3: %v", err)
	}
	// 示教器登录也用规范名和默认口令，应能过。
	if _, err := fac.LoginPad(ctx, "zhang3", personPass("zhang3")); err != nil {
		// 示教器登不上，正是这次要修的问题。
		t.Fatalf("pad login zhang3: %v", err)
	}
	// 全是空白的名字应被拒绝，放行说明没判空。
	if _, err := fac.CreatePerson(ctx, saTok, " \t\u3000 ", "空名"); !errors.Is(err, domain.ErrInvalidName) {
		// 空名建出了账号，说明去空白后没判空。
		t.Fatalf("blank login: %v", err)
	}
	// 六十五个字的名字应被拒绝，放行说明没限长。
	if _, err := fac.CreatePerson(ctx, saTok, strings.Repeat("长", 65), "超长"); !errors.Is(err, domain.ErrInvalidName) {
		// 超长名建出了账号，说明没按字数限长。
		t.Fatalf("long login: %v", err)
	}
	// 六十四个字刚好到上限，应能建出。
	if _, err := fac.CreatePerson(ctx, saTok, strings.Repeat("长", 64), "上限"); err != nil {
		// 上限内的名字被拒，说明限长按字节数算了。
		t.Fatalf("max login: %v", err)
	}
	// 去空白后和已有账号同名，应按登录名已占用拒绝。
	if _, err := fac.CreatePerson(ctx, saTok, "zhang3 ", "张三二"); !errors.Is(err, domain.ErrLoginNameTaken) {
		// 两个张三并存，说明去空白前就落了库。
		t.Fatalf("taken after trim: %v", err)
	}
	// 张三改成默认口令以外的口令并拿会话，后面要改名。
	zTok := mustAdoptPassword(t, ctx, fac, "zhang3", "z-pass")
	// 改成全空白的名字应被拒绝，放行说明改名没判空。
	if err := fac.Rename(ctx, zTok, "张三", "  "); !errors.Is(err, domain.ErrInvalidName) {
		// 改出了空名，账号将再也登不上。
		t.Fatalf("rename blank: %v", err)
	}
	// 改名带首尾空格，库里应存去掉后的名字。
	if err := fac.Rename(ctx, zTok, "张三", " zhang-new "); err != nil {
		// 带空格的新名改不了，说明改名没先规范。
		t.Fatalf("rename padded: %v", err)
	}
	// 示教器用改后的规范名登录，应能过。
	if _, err := fac.LoginPad(ctx, "zhang-new", "z-pass"); err != nil {
		// 改后的名字登不上，说明改名存了原样。
		t.Fatalf("pad login renamed: %v", err)
	}
}

// 认领时超管名带空格：库里存规范名，网页登录能用，重试仍按同一人幂等成功。
func TestAcceptEnrollmentTrimLogin(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 打开数据库连接，失败说明测试库起不来。
	admin := testpg.Open(t)
	// 另开一座空库，认领前不能先有初始超管。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_fac")
	// 迁好库并装上厂服务，失败说明库还不能用。
	fac := service.NewService(store.Open(testpg.OpenMigrated(t, dsn), id.New()))
	// 签发租约失败就停，否则登录过不了开厂检查。
	if err := fac.Store().GrantLocalLease(ctx); err != nil {
		// 把签发租约的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 按 WAN 约定的身份认领，模拟建厂码换来的超管编号。
	saID := id.New()
	// WAN 建厂表单不去空格，认领时名字带首尾空格。
	acc, err := fac.AcceptEnrollment(ctx, saID, " boss ", "认领超管", "boss-pass")
	// 认领失败就停，否则后面没有可靠结果。
	if err != nil {
		// 带空格的名字认领不了，说明没先规范。
		t.Fatalf("claim padded: %v", err)
	}
	// 交回的账号名应已去掉空格。
	if acc.LoginName != "boss" {
		// 名字仍带空格，库里存的也是原样。
		t.Fatalf("claimed login %q", acc.LoginName)
	}
	// 按稳定身份读回超管，看库里存的名字。
	p, err := fac.Store().PersonByID(ctx, saID)
	// 读不回或名字仍带空格就停，说明建档没去空白。
	if err != nil || p.LoginName != "boss" {
		// 库里名字不是 boss，示教器会登不上。
		t.Fatalf("stored claim login %q %v", p.LoginName, err)
	}
	// 用规范名网页登录，应能过。
	if _, err := fac.Login(ctx, "boss", "boss-pass"); err != nil {
		// 规范名登不上，说明认领存了原样名字。
		t.Fatalf("login boss: %v", err)
	}
	// 同一张建厂码带空格重试，应按同一人幂等成功。
	if _, err := fac.AcceptEnrollment(ctx, saID, " boss ", "认领超管", "boss-pass"); err != nil {
		// 重试被判成别人，说明比对前没去空白。
		t.Fatalf("retry padded: %v", err)
	}
	// 全是空白的超管名应被拒绝，放行说明认领没判空。
	if _, err := fac.AcceptEnrollment(ctx, id.New(), "   ", "空名", "x-pass"); !errors.Is(err, domain.ErrInvalidName) {
		// 空名也能认领，说明去空白后没判空。
		t.Fatalf("claim blank: %v", err)
	}
}
