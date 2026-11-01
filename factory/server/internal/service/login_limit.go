package service

import (
	"context"
	"strings"
	"sync"
	"time"
)

// 登录失败限速：同一登录名在同一来源连续失败到上限就锁一段时间。
const (
	loginFailMax  = 5               // 连续失败几次后上锁
	loginLockFor  = 5 * time.Minute // 上锁多久，到点自动解开
	loginKeepMany = 4096            // 记账条数超过它就顺手清掉过期的
)

// 请求来源地址在上下文里的键。
type clientIPKey struct{}

// WithClientIP 把请求来源地址放进上下文，登录限速按它分开计数。
func WithClientIP(ctx context.Context, ip string) context.Context {
	// 带上来源地址交回，业务层只读不改
	return context.WithValue(ctx, clientIPKey{}, ip)
}

// 取上下文里的来源地址；没带就当空，同名共用一份计数。
func clientIPOf(ctx context.Context) string {
	// 取出来源地址，类型不对就当没带
	ip, _ := ctx.Value(clientIPKey{}).(string)
	return ip
}

// 一个登录名加来源的失败记账。
type loginFails struct {
	n     int       // 连续失败次数，成功一次清零
	until time.Time // 锁到什么时候；零值表示没锁
	last  time.Time // 最近一次失败的时间，清理过期记账用
}

// loginLimiter 只在本进程内存里记账，重启即清，不进库。
type loginLimiter struct {
	mu   sync.Mutex             // 护着记账表，并发登录不会写乱
	m    map[string]*loginFails // 键是登录名加来源
	now  func() time.Time       // 取当前时刻；测试可换
	keep int                    // 记账条数上限，超了就清过期的
}

// 登录名不分大小写、去掉首尾空白，再拼上来源。
func loginKey(ctx context.Context, loginName string) string {
	// 拼成记账键，同一人换大小写也算同一份
	return strings.ToLower(strings.TrimSpace(loginName)) + "|" + clientIPOf(ctx)
}

// 取当前时刻，测试没换就用墙钟。
func (l *loginLimiter) clock() time.Time {
	// 测试换了钟就用测试的
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

// locked 这个键当前是否还在锁定期内。
func (l *loginLimiter) locked(key string) bool {
	// 占住记账表，读完就放
	l.mu.Lock()
	// 读完就放开
	defer l.mu.Unlock()
	// 没有记账就没锁
	f, ok := l.m[key]
	// 没记过账就当没锁
	if !ok {
		return false
	}
	// 锁定时刻还没过就算锁着
	return l.clock().Before(f.until)
}

// fail 记一次失败；到上限就上锁并清零计数。
func (l *loginLimiter) fail(key string) {
	// 占住记账表，写完就放
	l.mu.Lock()
	// 写完就放开
	defer l.mu.Unlock()
	// 先取当前时刻，计数和上锁都按它
	now := l.clock()
	// 第一次用时才建表
	if l.m == nil {
		// 建空表，后面直接写
		l.m = map[string]*loginFails{}
	}
	// 记账太多就先清掉早已过期的
	if len(l.m) >= l.limit() {
		// 清掉过期记账，表不会一直长
		l.pruneLocked(now)
	}
	// 取出这个键的记账，没有就新建
	f, ok := l.m[key]
	// 第一次失败就新建一条
	if !ok {
		// 新建记账，从零开始数
		f = &loginFails{}
		// 放进表里，下次接着数
		l.m[key] = f
	}
	// 失败次数加一，记下时间
	f.n++
	// 记下最近失败时间，清理时按它判断
	f.last = now
	// 到了上限就上锁，计数归零重新数
	if f.n >= loginFailMax {
		// 锁到这个时刻，期间一律拒绝
		f.until = now.Add(loginLockFor)
		// 计数归零，解锁后重新数
		f.n = 0
	}
}

// ok 登录成功就清掉这个键的记账。
func (l *loginLimiter) ok(key string) {
	// 占住记账表，删完就放
	l.mu.Lock()
	// 删完就放开
	defer l.mu.Unlock()
	// 成功一次就不再记前面的失败
	delete(l.m, key)
}

// 记账条数上限，没设就用默认值。
func (l *loginLimiter) limit() int {
	// 测试设了上限就用测试的
	if l.keep > 0 {
		return l.keep
	}
	return loginKeepMany
}

// 清掉已解锁且最近一次失败早于锁定时长的记账；调用方已持锁。
func (l *loginLimiter) pruneLocked(now time.Time) {
	// 逐条看是否已经过期
	for k, f := range l.m {
		// 没锁着且最近失败已很久，就删掉
		if !now.Before(f.until) && now.Sub(f.last) > loginLockFor {
			// 删掉过期记账，表才不会一直长
			delete(l.m, k)
		}
	}
}
