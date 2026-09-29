// 第 4 圈：WAN 单管理员、工厂名录、拒绝代管厂内账号。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
)

// 验单管理员、建厂，并拒绝代管厂内账号。
func TestAuthCircle(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 固定管理员登录名，改口令后还用它登。
	const wanLogin = "w"
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, wanLogin, wanPass); err != nil {
		// 立云端超管失败就停，立不住后面没有人能登录。
		t.Fatalf("bootstrap: %v", err)
	}
	// 立云端超管应被拒为已有管理员，放行或错类都算没拦住。
	if err := h.WAN.BootstrapAdmin(ctx, "w2", "x"); !errors.Is(err, domain.ErrWANAdminExists) {
		// 不符即停：立云端超管应被拒为已有管理员。
		t.Fatalf("1.1 second wan: %v", err)
	}
	// 邀请云端管理员应被拒为越权，放行或错类都算没拦住。
	if err := h.WAN.InviteWANAdmin(ctx, "", "anyone"); !errors.Is(err, domain.ErrForbidden) {
		// 不符即停：邀请云端管理员应被拒为越权。
		t.Fatalf("1.2 invite: %v", err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	wanTok, err := h.WAN.Login(ctx, wanLogin, wanPass)
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatalf("1.3 login: %v", err)
	}
	// 邀请云端管理员应被拒为越权，放行或错类都算没拦住。
	if err := h.WAN.InviteWANAdmin(ctx, wanTok, "sa-a"); !errors.Is(err, domain.ErrForbidden) {
		// 不符即停：邀请云端管理员应被拒为越权。
		t.Fatalf("1.2 grant: %v", err)
	}
	// 登记工厂「厂A」，建不成后面没有厂可授权。
	created, err := h.WAN.CreateFactory(ctx, wanTok, "厂A", "sa-a", "初始超管")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatalf("2.1 create factory: %v", err)
	}
	// 再发初始超管应被拒为超管已发过，放行或错类都算没拦住。
	if err := h.WAN.IssueInitialSuperAdmin(ctx, wanTok, created.Factory.ID); !errors.Is(err, domain.ErrInitialSAExists) {
		// 不符即停：再发初始超管应被拒为超管已发过。
		t.Fatalf("2.2 second sa: %v", err)
	}
	// 登记工厂「厂B」失败就停，建不成后面没有厂可授权。
	if _, err := h.WAN.CreateFactory(ctx, wanTok, "厂B", "sa-b", "厂B超管"); err != nil {
		// 不符即停：登记工厂「厂B」失败。
		t.Fatalf("create B: %v", err)
	}
	// 代建厂内人员应被拒为越权，放行或错类都算没拦住。
	if err := h.WAN.CreateFactoryPerson(ctx, wanTok, created.Factory.ID, "p1"); !errors.Is(err, domain.ErrForbidden) {
		// 不符即停：代建厂内人员应被拒为越权。
		t.Fatalf("3.1: %v", err)
	}
	// 代建厂内组织应被拒为越权，放行或错类都算没拦住。
	if err := h.WAN.CreateFactoryOrg(ctx, wanTok, created.Factory.ID, "车间"); !errors.Is(err, domain.ErrForbidden) {
		// 不符即停：代建厂内组织应被拒为越权。
		t.Fatalf("3.2: %v", err)
	}
	// 代授厂内角色应被拒为越权，放行或错类都算没拦住。
	if err := h.WAN.GrantFactoryRole(ctx, wanTok, created.Factory.ID, "p1"); !errors.Is(err, domain.ErrForbidden) {
		// 不符即停：代授厂内角色应被拒为越权。
		t.Fatalf("3.3: %v", err)
	}
	// 登录拿会话应被拒为口令不对，放行或错类都算没拦住。
	if _, err := h.WAN.Login(ctx, wanLogin, "bad"); !errors.Is(err, domain.ErrInvalidCredentials) {
		// 不符即停：登录拿会话应被拒为口令不对。
		t.Fatalf("17.2 wan bad pass: %v", err)
	}
	// 读工厂名录，名录读不到就无法对厂。
	dir, err := h.WAN.Directory(ctx, wanTok)
	// 读工厂名录失败就停，名录读不到就无法对厂。
	if err != nil {
		// 不符即停：读工厂名录失败。
		t.Fatalf("18.3 directory: %v", err)
	}
	// 名录条数不是2就停，这一步不能算通过。
	if len(dir.Factories) != 2 || len(dir.Initials) != 2 {
		// 不符即停：名录条数不是2。
		t.Fatalf("18.3 got fac=%d init=%d", len(dir.Factories), len(dir.Initials))
	}
	// 列厂内人员应被拒为越权，放行或错类都算没拦住。
	if err := h.WAN.ListFactoryPeople(ctx, wanTok, created.Factory.ID); !errors.Is(err, domain.ErrForbidden) {
		// 不符即停：列厂内人员应被拒为越权。
		t.Fatalf("18.1: %v", err)
	}
	// 读厂内口令应被拒为越权，放行或错类都算没拦住。
	if err := h.WAN.ReadAuthSecret(ctx, wanTok, created.Factory.ID); !errors.Is(err, domain.ErrForbidden) {
		// 不符即停：读厂内口令应被拒为越权。
		t.Fatalf("18.2: %v", err)
	}
	// 修改口令失败就停，口令改不了旧票失效验不成。
	if err := h.WAN.ChangePassword(ctx, wanTok, "wan-pass-2"); err != nil {
		// 不符即停：修改口令失败。
		t.Fatalf("change password: %v", err)
	}
	// 登录拿会话应被拒为口令不对，放行或错类都算没拦住。
	if _, err := h.WAN.Login(ctx, wanLogin, wanPass); !errors.Is(err, domain.ErrInvalidCredentials) {
		// 不符即停：登录拿会话应被拒为口令不对。
		t.Fatalf("old wan pass: %v", err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok2, err := h.WAN.Login(ctx, wanLogin, "wan-pass-2")
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatalf("login after change: %v", err)
	}
	// 重置管理员口令失败就停，重置失败旧口令可能还能用。
	if err := h.WAN.ResetAdminPassword(ctx, wanLogin, "wan-pass-3"); err != nil {
		// 不符即停：重置管理员口令失败。
		t.Fatalf("reset wan: %v", err)
	}
	// 校验管理员会话应被拒为未登录，放行或错类都算没拦住。
	if _, err := h.WAN.RequireAdmin(ctx, tok2); !errors.Is(err, domain.ErrUnauthorized) {
		// 不符即停：校验管理员会话应被拒为未登录。
		t.Fatalf("reset must drop sessions: %v", err)
	}
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if _, err := h.WAN.Login(ctx, wanLogin, "wan-pass-3"); err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatalf("login after reset: %v", err)
	}
	// 逐项检查，漏一项这一步就不能算通过。
	for _, table := range []string{"people", "org_types", "org_units", "role_grants"} {
		ok, err := h.WAN.HasTable(ctx, table)
		if err != nil || ok {
			// 查库表是否存在失败就停，表不在就说明库被串了。
			t.Fatalf("18 wan table %s present=%v err=%v", table, ok, err)
		}
	}
	// 读审计，读不到就无法核对有没有记。
	wanAudit, err := h.WAN.ListAudit(ctx)
	// 读审计失败就停，读不到就无法核对有没有记。
	if err != nil {
		// 不符即停：读审计失败。
		t.Fatal(err)
	}
	// 把审计打成文本，用来查口令有没有漏出。
	dump := audit.Dump(wanAudit)
	// 审计里不能出现口令或正文，出现就是泄密。
	if audit.ContainsAny(dump, wanPass, "wan-pass-2", "wan-pass-3", wanTok, tok2) {
		// 不符即停：审计里不能出现口令或正文。
		t.Fatalf("17 secret leaked in audit")
	}
	// 审计必须记下这一笔，缺了就没法对账。
	if !audit.HasResult(wanAudit, "bootstrap_wan_admin", audit.Deny) {
		// 不符即停：审计必须记下这一笔。
		t.Fatalf("1.1 no deny audit")
	}
	// 审计必须记下这一笔，缺了就没法对账。
	if !audit.HasResult(wanAudit, "invite_wan_admin", audit.Deny) {
		// 不符即停：审计必须记下这一笔。
		t.Fatalf("1.2 no deny audit")
	}
	// 审计必须记下这一笔，缺了就没法对账。
	if !audit.HasResult(wanAudit, "login", audit.Allow) || !audit.HasResult(wanAudit, "login", audit.Deny) {
		// 不符即停：审计必须记下这一笔。
		t.Fatalf("17.2 wan login audit missing")
	}
	// 审计必须记下这一笔，缺了就没法对账。
	if !audit.HasResult(wanAudit, "change_password", audit.Allow) || !audit.HasResult(wanAudit, "reset_wan_admin", audit.Allow) {
		// 不符即停：审计必须记下这一笔。
		t.Fatalf("wan password audit missing")
	}
	// 审计必须记下这一笔，缺了就没法对账。
	if !audit.HasResult(wanAudit, "create_factory", audit.Allow) {
		// 不符即停：审计必须记下这一笔。
		t.Fatalf("2.1 no allow audit")
	}
}
