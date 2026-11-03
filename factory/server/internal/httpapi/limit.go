package httpapi

import (
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"wmesh/factory/internal/platform/domain"
)

// 登录限速参数：网页和示教器登录共用一套，按账号和按来源各数各的。
const (
	loginAccountFails = 10               // 同一账号连错这么多次就锁
	loginSourceFails  = 30               // 同一来源连错这么多次就锁；厂网可能多台共一个出口，放宽些
	loginLockFor      = 5 * time.Minute  // 锁多久，示教器提示「5 分钟后再试」
	loginFailWindow   = 15 * time.Minute // 上次出错超过这么久，旧的失败不再累计
	loginSweepEvery   = time.Minute      // 至多每分钟清一次过期条目
	loginMaxEntries   = 10000            // 条目封顶，超出的新键不再记，免得撑大内存
)

// loginGuard 记登录失败次数并按账号、来源上锁；不起后台协程，判定时顺手清过期条目。
type loginGuard struct {
	mu        sync.Mutex             // 护住计数表和清理时刻
	fails     map[string]*loginFails // 键到失败计数
	now       func() time.Time       // 取当前时刻，测试可注入
	lastSweep time.Time              // 上次清理的时刻
}

// loginFails 一个键的连续失败次数、最近一次失败和锁到何时。
type loginFails struct {
	n     int       // 窗口内连续失败次数
	last  time.Time // 最近一次失败的时刻
	until time.Time // 锁到这个时刻；零值表示没锁
}

// newLoginGuard 建一个空的登录限速器。
func newLoginGuard() *loginGuard {
	// 计数表起步为空，时钟用真实时间。
	return &loginGuard{fails: map[string]*loginFails{}, now: time.Now}
}

// loginKeys 拼出这次登录的账号键和来源键；带上工厂身份，各厂互不牵连。
func loginKeys(r *http.Request, loginName string) (account, source string) {
	// 工厂身份取路径里的原文，非法的由后面开库时拒绝。
	fid := r.PathValue("id")
	// 登录名去空白、转小写，换大小写不能绕开计数。
	account = fid + "|acct|" + strings.ToLower(strings.TrimSpace(loginName))
	// 来源只认直连地址，转发头可被伪造。
	source = fid + "|src|" + sourceKey(r.RemoteAddr)
	return account, source
}

// sourceKey 把远端地址收成来源键：IPv4 用整址，IPv6 用 /64。
func sourceKey(remote string) string {
	// 拆出地址和端口，端口不参与限速。
	ap, err := netip.ParseAddrPort(remote)
	// 解析不了就按原串算一个来源。
	if err != nil {
		return remote
	}
	// 先还原 IPv4 映射地址，去掉区域，避免同址分成多个来源。
	addr := ap.Addr().Unmap().WithZone("")
	// IPv4 按整个地址算一个来源。
	if addr.Is4() {
		// IPv4 整址就是来源键。
		return addr.String()
	}
	// IPv6 按 /64 算一个来源，换址也算同源。
	pfx, err := addr.Prefix(64)
	// 取不到前缀就退回整址。
	if err != nil {
		// 退回整址，仍能按来源限速。
		return addr.String()
	}
	// 用 /64 前缀当来源键。
	return pfx.String()
}

// check 账号或来源正锁着就交回错太多次；没锁放行。
func (g *loginGuard) check(account, source string) error {
	// 计数表并发读写，整段加锁。
	g.mu.Lock()
	// 离开时解锁，别的请求才能判。
	defer g.mu.Unlock()
	// 取一次当前时刻，清理和判定用同一个。
	now := g.now()
	// 顺手清掉过期条目。
	g.sweepLocked(now)
	// 两个键任一锁着就拒绝。
	for _, k := range []string{account, source} {
		// 有条目且还在锁期内就拒绝。
		if f, ok := g.fails[k]; ok && now.Before(f.until) {
			return domain.ErrTooManyAttempts
		}
	}
	return nil
}

// fail 记一次密码错：账号和来源各加一，到线就锁。
func (g *loginGuard) fail(account, source string) {
	// 计数表并发读写，整段加锁。
	g.mu.Lock()
	// 离开时解锁。
	defer g.mu.Unlock()
	// 取一次当前时刻，两个键用同一个。
	now := g.now()
	// 账号按账号上限计。
	g.bumpLocked(account, loginAccountFails, now)
	// 来源按来源上限计。
	g.bumpLocked(source, loginSourceFails, now)
}

// ok 登录成功清掉这个账号的失败计数；来源计数留着，免得穿插一次成功就能清零。
func (g *loginGuard) ok(account string) {
	// 计数表并发读写，整段加锁。
	g.mu.Lock()
	// 离开时解锁。
	defer g.mu.Unlock()
	// 成功即清零这个账号。
	delete(g.fails, account)
}

// bumpLocked 已占住锁时给一个键加一次失败，到上限就锁并从零再数。
func (g *loginGuard) bumpLocked(key string, limit int, now time.Time) {
	// 找这个键已有的计数。
	f, ok := g.fails[key]
	// 新键先看表满没满，满了就不再记。
	if !ok {
		// 表已满就不记新键，防大量假账号撑大内存。
		if len(g.fails) >= loginMaxEntries {
			return
		}
		// 给新键建计数，从零起步。
		f = &loginFails{}
		// 登记这个键。
		g.fails[key] = f
	}
	// 上次失败太久以前，旧的不再累计。
	if !f.last.IsZero() && now.Sub(f.last) > loginFailWindow {
		// 窗口过了，从零重新数。
		f.n = 0
	}
	// 记一次失败。
	f.n++
	// 记下最近一次失败的时刻。
	f.last = now
	// 到上限就锁住，计数从零再数。
	if f.n >= limit {
		// 锁到五分钟后。
		f.until = now.Add(loginLockFor)
		// 解锁后重新数满才再锁。
		f.n = 0
	}
}

// sweepLocked 已占住锁时至多每分钟清一次：没锁且窗口已过的条目删掉。
func (g *loginGuard) sweepLocked(now time.Time) {
	// 距上次清理不到一分钟就不扫。
	if now.Sub(g.lastSweep) <= loginSweepEvery {
		return
	}
	// 逐条看是否已过期。
	for k, f := range g.fails {
		// 锁已过且失败窗口也过了，删了不丢状态。
		if !now.Before(f.until) && now.Sub(f.last) > loginFailWindow {
			// 删掉过期条目，表不会越积越大。
			delete(g.fails, k)
		}
	}
	// 记下这次清理时刻。
	g.lastSweep = now
}

// guardLogin 包住一次登录：锁着就直接回错太多次；密码错记一次；成功清零账号计数。
func (h *Handler) guardLogin(r *http.Request, loginName string, login func() error) error {
	// 拼出账号键和来源键。
	account, source := loginKeys(r, loginName)
	// 锁着就不再核密码，直接拒绝。
	if err := h.logins.check(account, source); err != nil {
		return err
	}
	// 交给业务核密码开会话。
	err := login()
	// 只有密码不对才计数，停用、库错等不算猜密码。
	if errors.Is(err, domain.ErrInvalidCredentials) {
		// 记一次密码错。
		h.logins.fail(account, source)
		return err
	}
	// 登录成功就清零这个账号的计数。
	if err == nil {
		// 成功清零账号计数。
		h.logins.ok(account)
	}
	return err
}
