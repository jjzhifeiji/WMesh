package httpapi

import (
	"net/http"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// 按来源限速的参数：登录和匿名入口共用一套。
const (
	gateEvery      = 12 * time.Second // 每个来源每十二秒补一次名额
	gateBurst      = 10               // 每个来源最多连发十次
	gateSweepEvery = time.Minute      // 至多每分钟清一次空闲来源
	gateIdle       = 10 * time.Minute // 空闲超过十分钟就清掉，名额早已补满
	gateMaxPeers   = 10000            // 来源条目封顶，超出的新来源共用一个闸
)

// ipGate 按来源地址限速；不起后台协程，在放行时顺手清理空闲来源。
type ipGate struct {
	mu        sync.Mutex       // 护住来源表和清理时刻
	peers     map[string]*peer // 来源键到各自限速器
	every     rate.Limit       // 每个来源补名额的速度
	burst     int              // 每个来源可连发的次数
	maxPeers  int              // 来源条目上限，测试可调小
	now       func() time.Time // 取当前时刻，测试可注入
	lastSweep time.Time        // 上次清理空闲来源的时刻
	overflow  *rate.Limiter    // 条目满后新来源共用的限速器
}

// peer 是一个来源的限速器和最近一次出现的时刻。
type peer struct {
	lim  *rate.Limiter // 这个来源自己的名额
	seen time.Time     // 最近一次来请求的时刻
}

// 新建按来源限速的闸，补速和连发数由调用方定。
func newIPGate(every time.Duration, burst int) *ipGate {
	// 来源表起步为空，溢出闸与单来源同参数。
	return &ipGate{
		peers:    map[string]*peer{},
		every:    rate.Every(every),
		burst:    burst,
		maxPeers: gateMaxPeers,
		now:      time.Now,
		overflow: rate.NewLimiter(rate.Every(every), burst),
	}
}

// allow 按来源扣一次名额；没名额就不放行。不信 X-Forwarded-For。
func (g *ipGate) allow(r *http.Request) bool {
	// 只认直连地址，转发头可被伪造。
	key := peerKey(r.RemoteAddr)
	// 来源表并发读写，整段加锁。
	g.mu.Lock()
	// 离开时解锁，别的请求才能判。
	defer g.mu.Unlock()
	// 取一次当前时刻，清理和扣名额用同一个。
	now := g.now()
	// 距上次清理超过一分钟才扫表，避免每次都扫。
	if now.Sub(g.lastSweep) > gateSweepEvery {
		// 逐条看来源是否已空闲太久。
		for k, p := range g.peers {
			// 空闲超过十分钟名额早已补满，删了不丢状态。
			if now.Sub(p.seen) > gateIdle {
				// 删掉空闲来源，表不会越积越大。
				delete(g.peers, k)
			}
		}
		// 记下这次清理时刻，下一分钟内不再扫。
		g.lastSweep = now
	}
	// 找这个来源已有的限速器。
	p, ok := g.peers[key]
	// 新来源先看表满没满，满了就共用溢出闸。
	if !ok {
		// 表已满，新来源共用一个闸，防大段地址撑大表。
		if len(g.peers) >= g.maxPeers {
			// 溢出来源共用名额，扣不到就拒绝。
			return g.overflow.AllowN(now, 1)
		}
		// 给新来源建自己的限速器，名额满额起步。
		p = &peer{lim: rate.NewLimiter(g.every, g.burst)}
		// 登记这个来源，后面的请求共用它。
		g.peers[key] = p
	}
	// 记下最近出现时刻，清理时据此判断空闲。
	p.seen = now
	// 扣一次名额，扣不到就拒绝。
	return p.lim.AllowN(now, 1)
}

// peerKey 把远端地址收成来源键：IPv4 用整址，IPv6 用 /64。
func peerKey(remote string) string {
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
	// IPv6 按 /64 算一个来源，一户一段换址也算同源。
	pfx, err := addr.Prefix(64)
	// 取不到前缀就退回整址。
	if err != nil {
		// 退回整址，仍能按来源限速。
		return addr.String()
	}
	// 用 /64 前缀当来源键。
	return pfx.String()
}
