// 第 7 圈：WAN 侧矩阵编号。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	global "wmesh/global/internal/service"
)

var matrixIDs = []string{
	"1.1", "1.2", "1.3",
	"2.1", "2.2",
	"3.1", "3.2", "3.3",
	"17.1", "17.2",
	"18.1", "18.2", "18.3",
}

func TestMatrix(t *testing.T) {
	ctx := context.Background()
	ran := map[string]bool{}
	run := func(id string, fn func(*testing.T)) {
		t.Helper()
		t.Run(id, func(t *testing.T) {
			ran[id] = true
			fn(t)
		})
	}
	t.Cleanup(func() {
		for _, id := range matrixIDs {
			if !ran[id] {
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})

	h := New(t)
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		t.Fatal(err)
	}
	run("1.1", func(t *testing.T) {
		if err := h.WAN.BootstrapAdmin(ctx, "w2", "x"); !errors.Is(err, domain.ErrWANAdminExists) {
			t.Fatalf("got %v", err)
		}
	})
	run("1.2", func(t *testing.T) {
		if err := h.WAN.InviteWANAdmin(ctx, "", "anyone"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	var wanTok string
	run("1.3", func(t *testing.T) {
		tok, err := h.WAN.Login(ctx, "w", wanPass)
		if err != nil {
			t.Fatal(err)
		}
		wanTok = tok
	})

	var a global.CreatedFactory
	run("2.1", func(t *testing.T) {
		var err error
		a, err = h.WAN.CreateFactory(ctx, wanTok, "厂A", "sa-a", "超管A")
		if err != nil {
			t.Fatal(err)
		}
	})
	run("2.2", func(t *testing.T) {
		if err := h.WAN.IssueInitialSuperAdmin(ctx, wanTok, a.Factory.ID); !errors.Is(err, domain.ErrInitialSAExists) {
			t.Fatalf("got %v", err)
		}
	})

	if _, err := h.WAN.CreateFactory(ctx, wanTok, "厂B", "sa-b", "超管B"); err != nil {
		t.Fatal(err)
	}
	run("3.1", func(t *testing.T) {
		if err := h.WAN.CreateFactoryPerson(ctx, wanTok, a.Factory.ID, "p1"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("3.2", func(t *testing.T) {
		if err := h.WAN.CreateFactoryOrg(ctx, wanTok, a.Factory.ID, "车间"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("3.3", func(t *testing.T) {
		if err := h.WAN.GrantFactoryRole(ctx, wanTok, a.Factory.ID, "p1"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("17.2", func(t *testing.T) {
		if _, err := h.WAN.Login(ctx, "ghost", "bad"); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatal(err)
		}
	})
	run("17.1", func(t *testing.T) {
		rows, err := h.WAN.ListAudit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if bad := audit.Incomplete(rows); len(bad) > 0 {
			t.Fatalf("incomplete audit %#v", bad[0])
		}
		if audit.ContainsAny(audit.Dump(rows), wanPass, wanTok) {
			t.Fatalf("secret leaked")
		}
	})
	run("18.1", func(t *testing.T) {
		if err := h.WAN.ListFactoryPeople(ctx, wanTok, a.Factory.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
		for _, table := range []string{"people", "org_types", "org_units", "role_grants", "fact_stubs"} {
			ok, err := h.WAN.HasTable(ctx, table)
			if err != nil || ok {
				t.Fatalf("%s present=%v err=%v", table, ok, err)
			}
		}
	})
	run("18.2", func(t *testing.T) {
		if err := h.WAN.ReadAuthSecret(ctx, wanTok, a.Factory.ID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	run("18.3", func(t *testing.T) {
		dir, err := h.WAN.Directory(ctx, wanTok)
		if err != nil || len(dir.Factories) != 2 || len(dir.Initials) != 2 {
			t.Fatalf("%v %+v", err, dir)
		}
	})
}
