package clientmqtt

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	mochi "github.com/mochi-mqtt/server/v2"

	"wmesh/factory/internal/platform/id"
)

// 本机只能订自己的主题，别人的要被拒绝。
func TestBrokerACLLocksOwnTopics(t *testing.T) {
	// 新建这一份后面要用的对象。
	fid := id.New()
	// 新建这一份后面要用的对象。
	a, b := id.New(), id.New()
	// 占住端口，准备接本机连接。
	bus, err := Listen("127.0.0.1:0", Hooks{
		// 测试里只认这组身份，别的令牌拒绝。
		Auth: func(factoryID, clientID uuid.UUID, token string) (uuid.UUID, error) {
			// 对不上就换一路，避免把不符的当成通过。
			if factoryID != fid {
				// 交回出错小信封，再交给后面的结果。
				return uuid.Nil, errIntent("factory")
			}
			// 对上了才走这一路，其余分开处理。
			if (clientID == a && token == "tokA") || (clientID == b && token == "tokB") {
				return clientID, nil
			}
			// 交回出错小信封，再交给后面的结果。
			return uuid.Nil, errIntent("token")
		},
	})
	// 没能出错小信封，再交给后面就停住本用例。
	if err != nil {
		// 没能出错小信封，再交给后面就停住本用例。
		t.Fatal(err)
	}
	// 用例结束关掉代理，避免占着端口。
	t.Cleanup(func() { _ = bus.Close() })

	// 取出实际监听的地址。
	cliA := connect(t, bus.Addr(), fid, a, "tokA")
	// 取出实际监听的地址。
	cliB := connect(t, bus.Addr(), fid, b, "tokB")
	// 按需要的长度把缓冲准备好。
	gotA := make(chan string, 4)
	// 按需要的长度把缓冲准备好。
	gotB := make(chan string, 4)
	// 拼出只给这台机器的下行主题。
	sub(t, cliA, DownTopic(fid, a), gotA)
	// 拼出只给这台机器的下行主题。
	sub(t, cliB, DownTopic(fid, b), gotB)

	// 他机 Topic 订不上：B 订 A 的 down，A 的下行 B 收不到。
	tok := cliB.Subscribe(DownTopic(fid, a), 1, func(_ mqtt.Client, m mqtt.Message) {
		// 等取消或时间到，先发生的那一路先处理。
		select {
		// 送得进就送进通道，满了也不能堵住回调。
		case gotB <- string(m.Payload()):
		// 送不进通道就丢掉，避免堵住接收。
		default:
		}
	})
	// 等连接或订阅做完再往下。
	_ = tok.WaitTimeout(2 * time.Second)

	// 准备一份测试或签名用的字节。
	payload := []byte(`{"typ":"policy","revision":2}`)
	// 没能把这条指令投给这台机器就停住本用例。
	if err := bus.Publish(fid, a, payload); err != nil {
		// 没能把这条指令投给这台机器就停住本用例。
		t.Fatal(err)
	}
	// 等取消或时间到，先发生的那一路先处理。
	select {
	// 等自己的下行，用来核对已经收到。
	case m := <-gotA:
		// 结果和预期不符就进入失败。
		if m != string(payload) {
			// 结果和预期不符就停住。
			t.Fatalf("a got %s", m)
		}
		// 结果和预期不符就进入失败。
		if HasBody([]byte(m)) {
			// 下行里夹了正文就停住。
			t.Fatal("body on down")
		}
	// 等太久还没有就当成失败。
	case <-time.After(3 * time.Second):
		// 该收到的下行没有到。
		t.Fatal("a missed own down")
	}
	// 等取消或时间到，先发生的那一路先处理。
	select {
	// 看另一侧有没有误收到消息。
	case m := <-gotB:
		// 另一侧不该收到却收到了。
		t.Fatalf("b saw a down: %s", m)
	// 短等一下，不该收到就当成通过。
	case <-time.After(400 * time.Millisecond):
	}

	// 取出实际监听的地址。
	wrong := connectRaw(t, bus.Addr(), fid, a, "nope")
	// 结果和预期不符就进入失败。
	if wrong.IsConnected() {
		// 坏令牌竟能连上就停住。
		t.Fatal("bad token connected")
	}
}

// 用签好的口令连上，失败就停住本用例。
func connect(t *testing.T, addr string, fid, cid uuid.UUID, token string) mqtt.Client {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 准备这条测试连接的参数。
	opts := mqtt.NewClientOptions()
	// 填上要连的代理地址。
	opts.AddBroker("tcp://" + addr)
	// 收成普通文本再拿去比较或拼接。
	opts.SetClientID(cid.String())
	// 收成普通文本再拿去比较或拼接。
	opts.SetUsername(fid.String())
	// 把口令填进连接参数。
	opts.SetPassword(token)
	// 关掉自动重连，失败就要停住。
	opts.SetAutoReconnect(false)
	// 限制这次连接最多等多久。
	opts.SetConnectTimeout(3 * time.Second)
	// 按参数建一个消息客户端。
	cli := mqtt.NewClient(opts)
	// 按这些参数真正连上去。
	tok := cli.Connect()
	// 结果和预期不符就进入失败。
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		// 连接结果不对就停住。
		t.Fatalf("connect %s: %v", cid, tok.Error())
	}
	// 用例结束断开连接，避免留下会话。
	t.Cleanup(func() { cli.Disconnect(250) })
	return cli
}

// 用未签的口令去连，用来试拒绝路径。
func connectRaw(t *testing.T, addr string, fid, cid uuid.UUID, token string) mqtt.Client {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 准备这条测试连接的参数。
	opts := mqtt.NewClientOptions()
	// 填上要连的代理地址。
	opts.AddBroker("tcp://" + addr)
	// 收成普通文本再拿去比较或拼接。
	opts.SetClientID(cid.String())
	// 收成普通文本再拿去比较或拼接。
	opts.SetUsername(fid.String())
	// 把口令填进连接参数。
	opts.SetPassword(token)
	// 关掉自动重连，失败就要停住。
	opts.SetAutoReconnect(false)
	// 限制这次连接最多等多久。
	opts.SetConnectTimeout(2 * time.Second)
	// 按参数建一个消息客户端。
	cli := mqtt.NewClient(opts)
	// 按这些参数真正连上去。
	tok := cli.Connect()
	// 等连接或订阅做完再往下。
	_ = tok.WaitTimeout(3 * time.Second)
	return cli
}

// 订这个主题，并把收到的正文送进通道。
func sub(t *testing.T, cli mqtt.Client, topic string, ch chan string) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 收到一条就把正文送进通道。
	tok := cli.Subscribe(topic, 1, func(_ mqtt.Client, m mqtt.Message) {
		// 等取消或时间到，先发生的那一路先处理。
		select {
		// 把收到的正文送进通道。
		case ch <- string(m.Payload()):
		// 送不进通道就丢掉，避免堵住接收。
		default:
		}
	})
	// 结果和预期不符就进入失败。
	if !tok.WaitTimeout(3*time.Second) || tok.Error() != nil {
		// 等连接或订阅做完再往下和预期不符就停住。
		t.Fatal(tok.Error())
	}
}

// 带了成员或正文的控制面要被认出来。
func TestHasBodyDetectsMembers(t *testing.T) {
	// 把结构收成字节，再交给后面。
	raw, _ := json.Marshal(map[string]any{"typ": TypClosure, "members": []any{}})
	// 结果和预期不符就进入失败。
	if !HasBody(raw) {
		// 不该出现的成员出现了。
		t.Fatal("members")
	}
}

// 记下上下线和上行回调认下的人，给抢占用例核对。
type presenceLog struct {
	mu      sync.Mutex  // 回调在代理协程里跑，读写要上锁。
	online  []uuid.UUID // 依次报过上线的人。
	offline []uuid.UUID // 依次报过离线的人。
	up      []uuid.UUID // 依次收到上行时认下的人。
}

// 按令牌认人，并把上下线和上行都记进日志。
func (l *presenceLog) hooks(fid, cid uuid.UUID, tokens map[string]uuid.UUID) Hooks {
	return Hooks{
		// 只认这台机器和表里的令牌，别的拒绝。
		Auth: func(factoryID, clientID uuid.UUID, token string) (uuid.UUID, error) {
			// 按令牌查出登录人，查不到就算没命中。
			person, ok := tokens[token]
			// 厂、机器或令牌对不上就拒绝连接。
			if factoryID != fid || clientID != cid || !ok {
				// 交回令牌不对的错误，拒绝这次连接。
				return uuid.Nil, errIntent("token")
			}
			return person, nil
		},
		// 记下报上线的人。
		Online: func(_, personID uuid.UUID) {
			// 占住日志锁，避免和读的一侧撞上。
			l.mu.Lock()
			// 追加这次上线的人。
			l.online = append(l.online, personID)
			// 放开日志锁。
			l.mu.Unlock()
		},
		// 记下报离线的人。
		Offline: func(_, personID uuid.UUID) {
			// 占住日志锁，避免和读的一侧撞上。
			l.mu.Lock()
			// 追加这次离线的人。
			l.offline = append(l.offline, personID)
			// 放开日志锁。
			l.mu.Unlock()
		},
		// 记下上行认下的人。
		Up: func(_, _, personID uuid.UUID, _ []byte) {
			// 占住日志锁，避免和读的一侧撞上。
			l.mu.Lock()
			// 追加这条上行认下的人。
			l.up = append(l.up, personID)
			// 放开日志锁。
			l.mu.Unlock()
		},
	}
}

// 拷出当前三份记录，避免拿着锁去比较。
func (l *presenceLog) snapshot() (online, offline, up []uuid.UUID) {
	// 占住日志锁，避免读到写一半的切片。
	l.mu.Lock()
	// 读完放开日志锁。
	defer l.mu.Unlock()
	// 拷出副本，后面比较不受回调影响。
	online = append([]uuid.UUID(nil), l.online...)
	// 拷出副本，后面比较不受回调影响。
	offline = append([]uuid.UUID(nil), l.offline...)
	// 拷出副本，后面比较不受回调影响。
	up = append([]uuid.UUID(nil), l.up...)
	return online, offline, up
}

// 等到三份记录分别到这么多条，超时就停住本用例。
func (l *presenceLog) waitCounts(t *testing.T, online, offline, up int) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 定下最晚等到什么时候。
	deadline := time.Now().Add(5 * time.Second)
	// 反复看记录，回调是异步跑的。
	for {
		// 拷出当前记录来数条数。
		on, off, ups := l.snapshot()
		// 条数都到了就不再等。
		if len(on) == online && len(off) == offline && len(ups) == up {
			return
		}
		// 超时还没到就停住，并打出当前记录。
		if time.Now().After(deadline) {
			// 条数一直对不上，打出来便于看错在哪。
			t.Fatalf("want online=%d offline=%d up=%d, got online=%v offline=%v up=%v", online, offline, up, on, off, ups)
		}
		// 稍等一下再看，别空转。
		time.Sleep(20 * time.Millisecond)
	}
}

// 等到上行至少这么多条，补桶多放的几条留给调用方按容差核对。
func (l *presenceLog) waitUpAtLeast(t *testing.T, up int) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 定下最晚等到什么时候。
	deadline := time.Now().Add(5 * time.Second)
	// 反复看记录，回调是异步跑的。
	for {
		// 拷出当前上行记录来数条数。
		_, _, ups := l.snapshot()
		// 条数够了就不再等，多出的不在这里判。
		if len(ups) >= up {
			return
		}
		// 超时还不够就停住，并打出当前条数。
		if time.Now().After(deadline) {
			// 上行一直不够，打出来便于看错在哪。
			t.Fatalf("want up >= %d, got %d", up, len(ups))
		}
		// 稍等一下再看，别空转。
		time.Sleep(20 * time.Millisecond)
	}
}

// 同一台机器换人重连抢占，旧连接断开不能删掉新人。
func TestBrokerTakeoverKeepsNewPerson(t *testing.T) {
	// 新建厂、机器和两位登录人。
	fid, cid, p1, p2 := id.New(), id.New(), id.New(), id.New()
	// 准备记录上下线的日志。
	rec := &presenceLog{}
	// 占住端口，两张令牌分别认两个人。
	bus, err := Listen("127.0.0.1:0", rec.hooks(fid, cid, map[string]uuid.UUID{"tok1": p1, "tok2": p2}))
	// 代理起不来就停住本用例。
	if err != nil {
		// 代理起不来就停住本用例。
		t.Fatal(err)
	}
	// 用例结束关掉代理，避免占着端口。
	t.Cleanup(func() { _ = bus.Close() })

	// 人 1 先用这台机器连上。
	connect(t, bus.Addr(), fid, cid, "tok1")
	// 等人 1 报上线。
	rec.waitCounts(t, 1, 0, 0)
	// 人 2 用同一个机器号连上，把旧连接挤掉。
	fresh := connect(t, bus.Addr(), fid, cid, "tok2")
	// 等人 2 上线、旧连接离线都报完。
	rec.waitCounts(t, 2, 1, 0)
	// 拷出当前记录来核对是谁。
	on, off, _ := rec.snapshot()
	// 上线应依次是人 1、人 2。
	if on[0] != p1 || on[1] != p2 {
		// 上线记错人就停住。
		t.Fatalf("online = %v", on)
	}
	// 旧连接的离线只能记到人 1。
	if off[0] != p1 {
		// 离线记到新人头上就停住。
		t.Fatalf("offline = %v, want %v", off, p1)
	}

	// 新连接发一条自己的上行。
	tok := fresh.Publish(UpTopic(fid, cid), 1, false, []byte(`{"typ":"presence"}`))
	// 上行没发出去就停住本用例。
	if !tok.WaitTimeout(3*time.Second) || tok.Error() != nil {
		// 上行没发出去就停住本用例。
		t.Fatalf("publish up: %v", tok.Error())
	}
	// 等上行回调报到。
	rec.waitCounts(t, 2, 1, 1)
	// 拷出上行记录来核对是谁。
	_, _, ups := rec.snapshot()
	// 新连接的上行要认到人 2。
	if ups[0] != p2 {
		// 上行认错人就停住。
		t.Fatalf("up person = %v, want %v", ups[0], p2)
	}

	// 新连接主动断开。
	fresh.Disconnect(250)
	// 等新连接的离线报到。
	rec.waitCounts(t, 2, 2, 1)
	// 拷出离线记录来核对是谁。
	_, off, _ = rec.snapshot()
	// 新连接的离线要记到人 2。
	if off[1] != p2 {
		// 离线记错人就停住。
		t.Fatalf("offline = %v, want second %v", off, p2)
	}
}

// 同一人同一台重连，上线和离线次数要配平。
func TestBrokerSamePersonReconnectBalances(t *testing.T) {
	// 新建厂、机器和登录人。
	fid, cid, p1 := id.New(), id.New(), id.New()
	// 准备记录上下线的日志。
	rec := &presenceLog{}
	// 占住端口，只认这一张令牌。
	bus, err := Listen("127.0.0.1:0", rec.hooks(fid, cid, map[string]uuid.UUID{"tok1": p1}))
	// 代理起不来就停住本用例。
	if err != nil {
		// 代理起不来就停住本用例。
		t.Fatal(err)
	}
	// 用例结束关掉代理，避免占着端口。
	t.Cleanup(func() { _ = bus.Close() })

	// 先连一次。
	connect(t, bus.Addr(), fid, cid, "tok1")
	// 等第一次上线报到。
	rec.waitCounts(t, 1, 0, 0)
	// 同一人同一台再连一次，挤掉旧连接。
	again := connect(t, bus.Addr(), fid, cid, "tok1")
	// 等第二次上线和旧连接离线都报完。
	rec.waitCounts(t, 2, 1, 0)
	// 新连接主动断开。
	again.Disconnect(250)
	// 等新连接的离线报到，两边各两次。
	rec.waitCounts(t, 2, 2, 0)
	// 再稍等一下，确认没有多报。
	time.Sleep(200 * time.Millisecond)
	// 拷出记录核对次数和人。
	on, off, _ := rec.snapshot()
	// 次数不配平就说明在线计数会卡住。
	if len(on) != 2 || len(off) != 2 {
		// 上下线次数不等就停住。
		t.Fatalf("online=%v offline=%v", on, off)
	}
	// 逐条核对离线都记到这一个人。
	for _, p := range off {
		// 离线记到别人或空人就停住。
		if p != p1 {
			// 离线记错人就停住。
			t.Fatalf("offline = %v, want all %v", off, p1)
		}
	}
}

// 同号残留只清没上线又已关掉的，已上线的留给它自己的断开回调。
func TestRememberPersonSweepsOnlyStaleSameID(t *testing.T) {
	// 起一个代理，只拿它来造连接。
	bus, err := Listen("127.0.0.1:0", Hooks{})
	// 代理起不来就停住本用例。
	if err != nil {
		// 代理起不来就停住本用例。
		t.Fatal(err)
	}
	// 用例结束关掉代理，避免占着端口。
	t.Cleanup(func() { _ = bus.Close() })
	// 新建两位登录人。
	p1, p2 := id.New(), id.New()
	// 看表里还有没有这条连接。
	has := func(cl *mochi.Client) bool {
		// 占住锁，避免和代理协程撞上。
		bus.mu.Lock()
		// 看完放开锁。
		defer bus.mu.Unlock()
		// 按连接指针查这一项在不在。
		_, ok := bus.persons[cl]
		return ok
	}

	// 没上线就关掉的旧连接：鉴权过了却不会有断开回调。
	cid := id.New().String()
	// 造一条旧连接并记下人 1。
	stale := bus.server.NewClient(nil, "test", cid, false)
	// 记下旧连接的登录人。
	bus.rememberPerson(stale, p1)
	// 旧连接没上线就关掉。
	stale.Stop(nil)
	// 造同号的新连接。
	fresh := bus.server.NewClient(nil, "test", cid, false)
	// 记新连接时应顺带清掉旧残留。
	bus.rememberPerson(fresh, p2)
	// 旧残留还在就说明会越积越多。
	if has(stale) {
		// 没清掉残留就停住。
		t.Fatal("stale entry kept")
	}
	// 新连接的登录人要是人 2。
	if got := bus.personOf(fresh); got != p2 {
		// 新连接记错人就停住。
		t.Fatalf("fresh person = %v", got)
	}

	// 已上线又关掉的旧连接：断开回调还没跑，不能先删。
	cid2 := id.New().String()
	// 造一条旧连接并记下人 1。
	live := bus.server.NewClient(nil, "test", cid2, false)
	// 记下旧连接的登录人。
	bus.rememberPerson(live, p1)
	// 会话建好，标成已上线。
	bus.markOnline(live)
	// 旧连接被挤掉，断开回调还没跑。
	live.Stop(nil)
	// 造同号的新连接。
	next := bus.server.NewClient(nil, "test", cid2, false)
	// 记新连接时不该动已上线的旧项。
	bus.rememberPerson(next, p2)
	// 已上线的旧项被清掉，离线就会漏记。
	if !has(live) {
		// 误删已上线的旧项就停住。
		t.Fatal("online entry swept")
	}
	// 旧连接自己断开时离线要记到人 1。
	if got := bus.forgetPerson(live); got != p1 {
		// 旧连接离线记错人就停住。
		t.Fatalf("live forget = %v", got)
	}
	// 新连接的登录人仍是人 2。
	if got := bus.personOf(next); got != p2 {
		// 新连接被误删就停住。
		t.Fatalf("next person = %v", got)
	}
}

// 同一连接一口气连发，业务只收到桶容量那么多，连接不被断开。
func TestBrokerThrottlesUpPerConnection(t *testing.T) {
	// 新建厂、机器和登录人。
	fid, cid, p1 := id.New(), id.New(), id.New()
	// 准备记录上下线和上行的日志。
	rec := &presenceLog{}
	// 占住端口，只认这一张令牌。
	bus, err := Listen("127.0.0.1:0", rec.hooks(fid, cid, map[string]uuid.UUID{"tok1": p1}))
	// 代理起不来就停住本用例。
	if err != nil {
		// 代理起不来就停住本用例。
		t.Fatal(err)
	}
	// 用例结束关掉代理，避免占着端口。
	t.Cleanup(func() { _ = bus.Close() })
	// 用这台机器连上。
	cli := connect(t, bus.Addr(), fid, cid, "tok1")
	// 等上线报到，再开始连发。
	rec.waitCounts(t, 1, 0, 0)
	// 记下开始连发的时刻，算补桶最多补了几个。
	start := time.Now()
	// 同一连接连发 50 条上行。
	for i := 0; i < 50; i++ {
		// 发一条自己的在场上行。
		tok := cli.Publish(UpTopic(fid, cid), 1, false, []byte(`{"typ":"presence"}`))
		// 上行没发出去就停住本用例。
		if !tok.WaitTimeout(3*time.Second) || tok.Error() != nil {
			// 打出第几条没发出去。
			t.Fatalf("publish #%d: %v", i, tok.Error())
		}
	}
	// 等业务至少收满桶容量，连发慢时补桶会多放几条。
	rec.waitUpAtLeast(t, upBurst)
	// 再稍等一下，确认超出的确实没交给业务。
	time.Sleep(300 * time.Millisecond)
	// 这段时间里最多能补回来的名额。
	refill := int(time.Since(start) / upEvery)
	// 拷出当前记录核对条数。
	on, off, ups := rec.snapshot()
	// 多出桶容量和补桶数就说明限速没生效。
	if len(ups) < upBurst || len(ups) > upBurst+refill {
		// 打出业务实际收到的条数。
		t.Fatalf("up = %d, want %d (+%d refill)", len(ups), upBurst, refill)
	}
	// 限速只丢消息，不该把连接断掉。
	if len(on) != 1 || len(off) != 0 || !cli.IsConnected() {
		// 连接被断或多报上下线就停住。
		t.Fatalf("online=%v offline=%v connected=%v", on, off, cli.IsConnected())
	}
}

// 超过包长上限的上行在读包时就断开连接，不交给业务。
func TestBrokerDropsOversizedPacket(t *testing.T) {
	// 新建厂、机器和登录人。
	fid, cid, p1 := id.New(), id.New(), id.New()
	// 准备记录上下线和上行的日志。
	rec := &presenceLog{}
	// 占住端口，只认这一张令牌。
	bus, err := Listen("127.0.0.1:0", rec.hooks(fid, cid, map[string]uuid.UUID{"tok1": p1}))
	// 代理起不来就停住本用例。
	if err != nil {
		// 代理起不来就停住本用例。
		t.Fatal(err)
	}
	// 用例结束关掉代理，避免占着端口。
	t.Cleanup(func() { _ = bus.Close() })
	// 用这台机器连上。
	cli := connect(t, bus.Addr(), fid, cid, "tok1")
	// 等上线报到，再开始发。
	rec.waitCounts(t, 1, 0, 0)
	// 先发一条正常大小的在场上行。
	tok := cli.Publish(UpTopic(fid, cid), 1, false, []byte(`{"typ":"presence"}`))
	// 正常上行没发出去就停住本用例。
	if !tok.WaitTimeout(3*time.Second) || tok.Error() != nil {
		// 正常上行没发出去就停住本用例。
		t.Fatalf("publish small: %v", tok.Error())
	}
	// 正常的那条要交给业务。
	rec.waitCounts(t, 1, 0, 1)
	// 造一条正文本身就超过上限的上行。
	big := []byte(`{"typ":"presence","pad":"` + strings.Repeat("x", maxPacketSize) + `"}`)
	// 发出超长上行，代理应当断开连接。
	_ = cli.Publish(UpTopic(fid, cid), 0, false, big)
	// 等离线报到，说明连接被代理断开。
	rec.waitCounts(t, 1, 1, 1)
	// 拷出上行记录核对条数。
	_, _, ups := rec.snapshot()
	// 超长那条交给了业务就停住。
	if len(ups) != 1 {
		// 打出业务实际收到的条数。
		t.Fatalf("up = %d, want 1", len(ups))
	}
}
