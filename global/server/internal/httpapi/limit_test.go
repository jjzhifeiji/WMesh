// 按来源限速：来源怎么归并、空闲怎么清、条目满了怎么办。
package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// 组一条只带远端地址的请求，限速只看它。
func fromAddr(remote string) *http.Request {
	// 只填远端地址，其余字段限速用不到。
	return &http.Request{RemoteAddr: remote}
}

// 新建一个时间冻结的闸，测试自己拨钟。
func frozenGate(start time.Time) (*ipGate, *time.Time) {
	// 先按线上参数建闸。
	g := newIPGate(gateEvery, gateBurst)
	// 当前时刻由测试掌握，不随真实时间走。
	now := start
	// 让闸读测试的钟。
	g.now = func() time.Time {
		// 交回测试拨好的时刻。
		return now
	}
	return g, &now
}

// 同一 IPv4 连发十次放行，第十一次拒绝；换个地址不受影响。
func TestIPGateBurstPerIPv4(t *testing.T) {
	// 时间冻结，名额不会中途补上。
	g, _ := frozenGate(time.Unix(1_700_000_000, 0))
	// 连发十次都应放行。
	for i := range gateBurst {
		// 突发以内被拒就停测。
		if !g.allow(fromAddr("1.2.3.4:1000")) {
			// 突发以内就被拒，带上次数停测。
			t.Fatalf("attempt %d denied", i+1)
		}
	}
	// 第十一次应被拒。
	if g.allow(fromAddr("1.2.3.4:1001")) {
		// 超出突发仍放行，限速没生效。
		t.Fatal("11th allowed")
	}
	// 另一个 IPv4 有自己的名额。
	if !g.allow(fromAddr("1.2.3.5:1000")) {
		// 不同来源被连坐，就停测。
		t.Fatal("other ipv4 denied")
	}
}

// IPv4 映射地址和普通 IPv4 算同一来源，共用名额。
func TestIPGateMappedIPv4(t *testing.T) {
	// 时间冻结，名额不会中途补上。
	g, _ := frozenGate(time.Unix(1_700_000_000, 0))
	// 一半用映射形式，一半用普通形式，共十次。
	for i := range gateBurst {
		// 单双次换着形式发，应共用名额。
		addr := "[::ffff:1.2.3.4]:1"
		// 双次改用普通 IPv4 形式。
		if i%2 == 0 {
			// 普通形式，端口也不同。
			addr = "1.2.3.4:2"
		}
		// 突发以内被拒就停测。
		if !g.allow(fromAddr(addr)) {
			// 突发以内就被拒，带上次数停测。
			t.Fatalf("attempt %d denied", i+1)
		}
	}
	// 名额已用完，两种形式都应被拒。
	if g.allow(fromAddr("[::ffff:1.2.3.4]:1")) || g.allow(fromAddr("1.2.3.4:2")) {
		// 两种形式没共用名额，就停测。
		t.Fatal("mapped ipv4 not shared")
	}
}

// 同一 /64 的 IPv6 共用名额，不同 /64 各算各的。
func TestIPGateIPv6Prefix(t *testing.T) {
	// 时间冻结，名额不会中途补上。
	g, _ := frozenGate(time.Unix(1_700_000_000, 0))
	// 同一 /64 下换着主机位发十次。
	for i := range gateBurst {
		// 主机位每次不同，前缀相同。
		addr := fmt.Sprintf("[2001:db8:1:2::%x]:443", i+1)
		// 突发以内被拒就停测。
		if !g.allow(fromAddr(addr)) {
			// 突发以内就被拒，带上次数停测。
			t.Fatalf("attempt %d denied", i+1)
		}
	}
	// 同一 /64 的新主机位也应被拒。
	if g.allow(fromAddr("[2001:db8:1:2:ffff::1]:443")) {
		// 同段换址就绕过了限速，就停测。
		t.Fatal("same /64 not shared")
	}
	// 另一个 /64 不受影响。
	if !g.allow(fromAddr("[2001:db8:1:3::1]:443")) {
		// 不同段被连坐，就停测。
		t.Fatal("other /64 denied")
	}
}

// 空闲超过十分钟的来源在下次放行时被清掉。
func TestIPGateSweep(t *testing.T) {
	// 时间冻结，测试自己往前拨。
	g, now := frozenGate(time.Unix(1_700_000_000, 0))
	// 先登记两个来源。
	g.allow(fromAddr("1.2.3.4:1"))
	// 再登记一个来源。
	g.allow(fromAddr("1.2.3.5:1"))
	// 两个来源都应在表里。
	if len(g.peers) != 2 {
		// 登记条数不对，带上现场停测。
		t.Fatalf("peers %d", len(g.peers))
	}
	// 往前拨十一分钟，两条都已空闲。
	*now = now.Add(11 * time.Minute)
	// 新来源触发清理。
	g.allow(fromAddr("1.2.3.6:1"))
	// 只应剩下新来源。
	if len(g.peers) != 1 || g.peers["1.2.3.6"] == nil {
		// 空闲条目没清掉，表会越积越大。
		t.Fatalf("after sweep %v", g.peers)
	}
}

// 条目满了以后，新来源共用一个溢出闸，表不再变大。
func TestIPGateOverflow(t *testing.T) {
	// 时间冻结，名额不会中途补上。
	g, _ := frozenGate(time.Unix(1_700_000_000, 0))
	// 上限调到两条，方便测满。
	g.maxPeers = 2
	// 先占满两条。
	g.allow(fromAddr("10.0.0.1:1"))
	// 再占一条，表就满了。
	g.allow(fromAddr("10.0.0.2:1"))
	// 满了以后的新来源共用溢出闸，连发十次都放行。
	for i := range gateBurst {
		// 每次换一个新来源，都落到溢出闸。
		if !g.allow(fromAddr(fmt.Sprintf("10.0.1.%d:1", i+1))) {
			// 溢出闸突发以内被拒，就停测。
			t.Fatalf("overflow attempt %d denied", i+1)
		}
	}
	// 溢出闸名额已用完，再来的新来源应被拒。
	if g.allow(fromAddr("10.0.2.1:1")) {
		// 溢出来源没共用名额，就停测。
		t.Fatal("overflow not shared")
	}
	// 表里仍只有两条，没被新来源撑大。
	if len(g.peers) != 2 {
		// 表被撑大了，带上条数停测。
		t.Fatalf("peers %d", len(g.peers))
	}
}
