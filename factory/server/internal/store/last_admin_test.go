// 最后一名超管保护的并发验收：两名超管同时互停或互收，恰好一边成功，库里仍剩一名有效超管。
package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/store"
)

// raceRounds 每种并发组合重放的轮数，多跑几轮才压得出交错。
const raceRounds = 20

// twoActiveSA 建两名有效、各握一条厂级超管角色的账号，交回人和授予。
func twoActiveSA(t *testing.T, ctx context.Context, s *store.Store) ([2]store.Person, [2]store.RoleGrant) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 准备承接两名超管账号。
	var people [2]store.Person
	// 准备承接两条厂级超管授予。
	var grants [2]store.RoleGrant
	// 逐个建人、授超管，两人条件对等。
	for i, name := range []string{"sa1", "sa2"} {
		// 建有效账号，建档时就带上密码。
		p, err := s.CreatePerson(ctx, name, name, "hash-"+name, false)
		// 建账号失败就停测，避免后面没有前提。
		if err != nil {
			// 建超管账号失败就停测。
			t.Fatalf("create %s: %v", name, err)
		}
		// 授予厂级超管角色，凑成两名有效超管。
		g, err := s.GrantRole(ctx, p.ID, store.RoleFactorySuperAdmin)
		// 授予失败就停测，避免后面没有前提。
		if err != nil {
			// 授超管失败就停测。
			t.Fatalf("grant %s: %v", name, err)
		}
		// 记下这名超管，后面按下标对上动作。
		people[i] = p
		// 记下这条授予，收回时要用。
		grants[i] = g
	}
	return people, grants
}

// raceTwo 两个动作等同一声起跑后并发执行，按下标交回各自的结果。
func raceTwo(a, b func() error) [2]error {
	// 准备承接两边的结果，各写各的下标不打架。
	var errs [2]error
	// 起跑信号，两边都就位后一起放行。
	start := make(chan struct{})
	// 等两边都跑完再交回结果。
	var wg sync.WaitGroup
	// 两个动作各开一个协程。
	for i, f := range []func() error{a, b} {
		// 先登记一个待完成的协程。
		wg.Add(1)
		// 开协程等起跑后执行这一边的动作。
		go func() {
			// 跑完就报完成，不管成败。
			defer wg.Done()
			// 等起跑信号，尽量让两边同时撞上。
			<-start
			// 记下这一边的结果。
			errs[i] = f()
		}()
	}
	// 一起放行两边。
	close(start)
	// 等两边都跑完。
	wg.Wait()
	return errs
}

// assertOneWins 两边恰好一边成功、另一边因最后一名超管被拒，库里仍剩一名有效超管。
func assertOneWins(t *testing.T, ctx context.Context, s *store.Store, round int, errs [2]error) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 准备数成功的一边。
	wins := 0
	// 逐边看结果，成功计数，失败必须是最后一名超管。
	for i, err := range errs {
		// 这一边成功就计一次。
		if err == nil {
			// 记下又有一边成功。
			wins++
			continue
		}
		// 失败只能是最后一名超管的拒绝，别的错说明锁或事务出了问题。
		if !errors.Is(err, domain.ErrLastAdmin) {
			// 拒绝原因不对就停测。
			t.Fatalf("round %d side %d: want ErrLastAdmin, got %v", round, i, err)
		}
	}
	// 必须恰好一边成功，两边都成就会一个超管不剩。
	if wins != 1 {
		// 成功数不对就停测。
		t.Fatalf("round %d: wins=%d errs=%v", round, wins, errs)
	}
	// 读回库里有效超管数。
	n, err := s.ActiveFactorySuperAdminCount(ctx)
	// 计数失败或不是一名都算没守住。
	if err != nil || n != 1 {
		// 有效超管不是一名就停测。
		t.Fatalf("round %d: active super admins n=%d err=%v", round, n, err)
	}
}

// 两名超管同时互相停用，恰好一个成功，另一个因最后一名超管被拒。
func TestDisableKeepAdminConcurrent(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 建两名有效超管。
	people, _ := twoActiveSA(t, ctx, s)
	// 多跑几轮，每轮从两名有效超管起跑。
	for round := 0; round < raceRounds; round++ {
		// sa1 停 sa2、sa2 停 sa1 同时发起。
		errs := raceTwo(
			// sa1 去停 sa2。
			func() error { return s.DisablePersonKeepAdmin(ctx, people[1].ID) },
			// sa2 去停 sa1。
			func() error { return s.DisablePersonKeepAdmin(ctx, people[0].ID) },
		)
		// 恰好一边成功，库里仍剩一名。
		assertOneWins(t, ctx, s, round, errs)
		// 把两人都恢复成有效，下一轮再从两名超管起跑。
		for _, p := range people {
			// 恢复失败就停测，避免下一轮前提不对。
			if err := s.SetPersonStatus(ctx, p.ID, store.StatusActive); err != nil {
				// 恢复账号失败就停测。
				t.Fatalf("round %d restore %s: %v", round, p.LoginName, err)
			}
		}
	}
}

// 两名超管同时互收超管角色，恰好一个成功，另一个因最后一名超管被拒。
func TestRevokeKeepAdminConcurrent(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 建两名有效超管和各自的授予。
	people, grants := twoActiveSA(t, ctx, s)
	// 多跑几轮，每轮从两名有效超管起跑。
	for round := 0; round < raceRounds; round++ {
		// 取出本轮两条授予，闭包里不受后面重授影响。
		g0, g1 := grants[0].ID, grants[1].ID
		// 两边同时收对方的超管角色。
		errs := raceTwo(
			// sa2 去收 sa1 的超管。
			func() error { return s.RevokeRoleKeepAdmin(ctx, g0) },
			// sa1 去收 sa2 的超管。
			func() error { return s.RevokeRoleKeepAdmin(ctx, g1) },
		)
		// 恰好一边成功，库里仍剩一名。
		assertOneWins(t, ctx, s, round, errs)
		// 被收的那位重新授超管，下一轮再从两名超管起跑。
		for i, err := range errs {
			// 这一边没收成，授予还有效，不用重授。
			if err != nil {
				continue
			}
			// 给被收的人重新授厂级超管。
			g, gerr := s.GrantRole(ctx, people[i].ID, store.RoleFactorySuperAdmin)
			// 重授失败就停测，避免下一轮前提不对。
			if gerr != nil {
				// 重授超管失败就停测。
				t.Fatalf("round %d regrant %s: %v", round, people[i].LoginName, gerr)
			}
			// 换上新授予，下一轮收这一条。
			grants[i] = g
		}
	}
}

// 一边停用 sa1、一边收 sa2 的超管同时发起，两条路排同一把锁，也只成一个。
func TestDisableAndRevokeKeepAdminConcurrent(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 建两名有效超管和各自的授予。
	people, grants := twoActiveSA(t, ctx, s)
	// 多跑几轮，每轮从两名有效超管起跑。
	for round := 0; round < raceRounds; round++ {
		// 取出本轮 sa2 的授予，闭包里不受后面重授影响。
		g1 := grants[1].ID
		// 停 sa1 和收 sa2 的超管同时发起。
		errs := raceTwo(
			// 停用 sa1。
			func() error { return s.DisablePersonKeepAdmin(ctx, people[0].ID) },
			// 收回 sa2 的超管。
			func() error { return s.RevokeRoleKeepAdmin(ctx, g1) },
		)
		// 恰好一边成功，库里仍剩一名。
		assertOneWins(t, ctx, s, round, errs)
		// 停用成功的话把 sa1 恢复成有效。
		if errs[0] == nil {
			// 恢复失败就停测，避免下一轮前提不对。
			if err := s.SetPersonStatus(ctx, people[0].ID, store.StatusActive); err != nil {
				// 恢复账号失败就停测。
				t.Fatalf("round %d restore sa1: %v", round, err)
			}
		}
		// 收回成功的话给 sa2 重授超管。
		if errs[1] == nil {
			// 给 sa2 重新授厂级超管。
			g, err := s.GrantRole(ctx, people[1].ID, store.RoleFactorySuperAdmin)
			// 重授失败就停测，避免下一轮前提不对。
			if err != nil {
				// 重授超管失败就停测。
				t.Fatalf("round %d regrant sa2: %v", round, err)
			}
			// 换上新授予，下一轮收这一条。
			grants[1] = g
		}
	}
}

// 不是超管或还有别的超管时照常停用和收回；授予不存在交回不存在。
func TestKeepAdminAllowsNonLast(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 建两名有效超管和各自的授予。
	people, grants := twoActiveSA(t, ctx, s)
	// 再建一名普通账号，停用它不碰超管入口。
	op, err := s.CreatePerson(ctx, "op", "操作员", "", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建普通账号失败就停测。
		t.Fatalf("create op: %v", err)
	}
	// 普通账号停用必须成功。
	if err := s.DisablePersonKeepAdmin(ctx, op.ID); err != nil {
		// 普通账号停不掉说明保护误伤。
		t.Fatalf("disable op: %v", err)
	}
	// 还有两名超管时收回其中一名必须成功。
	if err := s.RevokeRoleKeepAdmin(ctx, grants[1].ID); err != nil {
		// 多余的超管收不回说明保护过宽。
		t.Fatalf("revoke extra sa: %v", err)
	}
	// 已收回的授予再收一次交回不存在。
	if err := s.RevokeRoleKeepAdmin(ctx, grants[1].ID); !errors.Is(err, domain.ErrNotFound) {
		// 重复收回没有交回不存在就停测。
		t.Fatalf("revoke again: %v", err)
	}
	// 只剩 sa1 一名有效超管，停用他必须被拒。
	if err := s.DisablePersonKeepAdmin(ctx, people[0].ID); !errors.Is(err, domain.ErrLastAdmin) {
		// 最后一名超管能被停用就停测。
		t.Fatalf("disable last: %v", err)
	}
	// 收回最后一名超管的授予也必须被拒。
	if err := s.RevokeRoleKeepAdmin(ctx, grants[0].ID); !errors.Is(err, domain.ErrLastAdmin) {
		// 最后一名超管能被收回就停测。
		t.Fatalf("revoke last: %v", err)
	}
	// 已没了超管角色的 sa2 停用不碰入口，必须成功。
	if err := s.DisablePersonKeepAdmin(ctx, people[1].ID); err != nil {
		// 非超管停不掉说明保护误伤。
		t.Fatalf("disable former sa: %v", err)
	}
}
