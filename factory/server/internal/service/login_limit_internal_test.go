// 登录限速记账：到点自动解锁，成功清零，表满时清掉过期记账。
package service

import (
	"testing"
	"time"
)

// 锁满五分钟后自动解开，成功一次清掉前面的失败，过期记账会被清。
func TestLoginLimiterUnlockAndPrune(t *testing.T) {
	// 可拨的假钟，起点随便取一个整点。
	now := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	// 记账器换上假钟，表上限压到 2 好测清理。
	l := &loginLimiter{now: func() time.Time { return now }, keep: 2}
	// 连错五次把这个键锁上。
	for i := 0; i < loginFailMax; i++ {
		// 记一次失败。
		l.fail("a|1")
	}
	// 刚锁上应该是锁着的。
	if !l.locked("a|1") {
		// 到了上限没锁。
		t.Fatal("not locked after max fails")
	}
	// 拨到锁定期最后一刻之前，仍应锁着。
	now = now.Add(loginLockFor - time.Second)
	// 还差一秒，应仍锁着。
	if !l.locked("a|1") {
		// 提前解锁了。
		t.Fatal("unlocked too early")
	}
	// 拨过锁定期，应自动解开。
	now = now.Add(2 * time.Second)
	// 过了时限，应已解开。
	if l.locked("a|1") {
		// 到点没解开。
		t.Fatal("still locked after window")
	}
	// 另一个键错四次后成功一次。
	for i := 0; i < loginFailMax-1; i++ {
		// 记一次失败。
		l.fail("b|1")
	}
	// 成功一次清零。
	l.ok("b|1")
	// 清零后再错一次不应锁。
	l.fail("b|1")
	// 只错了一次，不该锁。
	if l.locked("b|1") {
		// 成功没清零。
		t.Fatal("ok did not reset")
	}
	// 拨到很久以后，前面的记账都已过期。
	now = now.Add(time.Hour)
	// 先记一个新键，表到上限。
	l.fail("c|1")
	// 再记一个新键，表已满会先清过期记账。
	l.fail("d|1")
	// 早已过期的 b 应已清掉。
	if _, ok := l.m["b|1"]; ok {
		// 过期记账没清，表会一直长。
		t.Fatal("stale entry not pruned")
	}
	// 刚记的 c 不该被清。
	if _, ok := l.m["c|1"]; !ok {
		// 把还在计数的记账清掉了。
		t.Fatal("fresh entry pruned")
	}
}
