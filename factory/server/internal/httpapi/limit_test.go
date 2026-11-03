package httpapi

import (
	"errors"
	"testing"
	"time"

	"wmesh/factory/internal/platform/domain"
)

// 同一账号连错十次就锁五分钟，锁期过了放行；成功清零账号计数。
func TestLoginGuardAccountLock(t *testing.T) {
	// 建一个限速器，时钟手拨。
	g := newLoginGuard()
	// 起始时刻，后面按需往前拨。
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 注入手拨时钟。
	g.now = func() time.Time { return now }
	// 连错九次，还不该锁。
	for range loginAccountFails - 1 {
		// 记一次密码错。
		g.fail("a", "s")
	}
	// 九次后仍应放行。
	if err := g.check("a", "s"); err != nil {
		// 没到上限就锁了。
		t.Fatalf("locked early: %v", err)
	}
	// 成功一次，账号计数清零。
	g.ok("a")
	// 清零后再错九次仍不该锁。
	for range loginAccountFails - 1 {
		// 记一次密码错。
		g.fail("a", "s2")
	}
	// 清零生效就仍放行。
	if err := g.check("a", "s2"); err != nil {
		// 成功没清零，计数还带着旧的。
		t.Fatalf("ok did not reset: %v", err)
	}
	// 第十次错，到线上锁。
	g.fail("a", "s2")
	// 锁了应回错太多次。
	if err := g.check("a", "other"); !errors.Is(err, domain.ErrTooManyAttempts) {
		// 到线没锁，暴力登录拦不住。
		t.Fatalf("not locked: %v", err)
	}
	// 别的账号同来源不受账号锁牵连。
	if err := g.check("b", "other"); err != nil {
		// 账号锁误伤了别人。
		t.Fatalf("other account locked: %v", err)
	}
	// 拨过五分钟。
	now = now.Add(loginLockFor + time.Second)
	// 锁期过了应放行。
	if err := g.check("a", "other"); err != nil {
		// 锁期过了还不放。
		t.Fatalf("still locked: %v", err)
	}
}

// 同一来源错满来源上限就锁，换账号也不行。
func TestLoginGuardSourceLock(t *testing.T) {
	// 建一个限速器，时钟手拨。
	g := newLoginGuard()
	// 固定时刻。
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 注入手拨时钟。
	g.now = func() time.Time { return now }
	// 每次换个账号错，只累计来源。
	for i := range loginSourceFails {
		// 记一次密码错，账号各不相同。
		g.fail(string(rune('A'+i)), "src")
	}
	// 来源到线应锁，新账号也不放。
	if err := g.check("new", "src"); !errors.Is(err, domain.ErrTooManyAttempts) {
		// 换账号就能绕开，来源闸没生效。
		t.Fatalf("source not locked: %v", err)
	}
	// 别的来源不受牵连。
	if err := g.check("new", "src2"); err != nil {
		// 来源锁误伤了别的来源。
		t.Fatalf("other source locked: %v", err)
	}
}
