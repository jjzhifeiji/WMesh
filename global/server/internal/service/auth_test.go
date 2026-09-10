// 第 4 圈：WAN 单管理员、工厂名录、拒绝代管厂内账号。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
)

func TestAuthCircle(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	const wanLogin = "w"
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, wanLogin, wanPass); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if err := h.WAN.BootstrapAdmin(ctx, "w2", "x"); !errors.Is(err, domain.ErrWANAdminExists) {
		t.Fatalf("1.1 second wan: %v", err)
	}
	if err := h.WAN.InviteWANAdmin(ctx, "", "anyone"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("1.2 invite: %v", err)
	}
	wanTok, err := h.WAN.Login(ctx, wanLogin, wanPass)
	if err != nil {
		t.Fatalf("1.3 login: %v", err)
	}
	if err := h.WAN.InviteWANAdmin(ctx, wanTok, "sa-a"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("1.2 grant: %v", err)
	}
	created, err := h.WAN.CreateFactory(ctx, wanTok, "厂A", "sa-a", "初始超管")
	if err != nil {
		t.Fatalf("2.1 create factory: %v", err)
	}
	if err := h.WAN.IssueInitialSuperAdmin(ctx, wanTok, created.Factory.ID); !errors.Is(err, domain.ErrInitialSAExists) {
		t.Fatalf("2.2 second sa: %v", err)
	}
	if _, err := h.WAN.CreateFactory(ctx, wanTok, "厂B", "sa-b", "厂B超管"); err != nil {
		t.Fatalf("create B: %v", err)
	}
	if err := h.WAN.CreateFactoryPerson(ctx, wanTok, created.Factory.ID, "p1"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("3.1: %v", err)
	}
	if err := h.WAN.CreateFactoryOrg(ctx, wanTok, created.Factory.ID, "车间"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("3.2: %v", err)
	}
	if err := h.WAN.GrantFactoryRole(ctx, wanTok, created.Factory.ID, "p1"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("3.3: %v", err)
	}
	if _, err := h.WAN.Login(ctx, wanLogin, "bad"); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("17.2 wan bad pass: %v", err)
	}
	dir, err := h.WAN.Directory(ctx, wanTok)
	if err != nil {
		t.Fatalf("18.3 directory: %v", err)
	}
	if len(dir.Factories) != 2 || len(dir.Initials) != 2 {
		t.Fatalf("18.3 got fac=%d init=%d", len(dir.Factories), len(dir.Initials))
	}
	if err := h.WAN.ListFactoryPeople(ctx, wanTok, created.Factory.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("18.1: %v", err)
	}
	if err := h.WAN.ReadAuthSecret(ctx, wanTok, created.Factory.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("18.2: %v", err)
	}
	for _, table := range []string{"people", "org_types", "org_units", "role_grants"} {
		ok, err := h.WAN.HasTable(ctx, table)
		if err != nil || ok {
			t.Fatalf("18 wan table %s present=%v err=%v", table, ok, err)
		}
	}
	wanAudit, err := h.WAN.ListAudit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dump := audit.Dump(wanAudit)
	if audit.ContainsAny(dump, wanPass, wanTok) {
		t.Fatalf("17 secret leaked in audit")
	}
	if !audit.HasResult(wanAudit, "bootstrap_wan_admin", audit.Deny) {
		t.Fatalf("1.1 no deny audit")
	}
	if !audit.HasResult(wanAudit, "invite_wan_admin", audit.Deny) {
		t.Fatalf("1.2 no deny audit")
	}
	if !audit.HasResult(wanAudit, "login", audit.Allow) || !audit.HasResult(wanAudit, "login", audit.Deny) {
		t.Fatalf("17.2 wan login audit missing")
	}
	if !audit.HasResult(wanAudit, "create_factory", audit.Allow) {
		t.Fatalf("2.1 no allow audit")
	}
}
