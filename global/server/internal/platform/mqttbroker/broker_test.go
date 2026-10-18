package mqttbroker

import (
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	"wmesh/global/internal/platform/nodekey"
)

// 记下钩子收到的上下线，供断言先后和次数。
type presenceLog struct {
	mu     sync.Mutex // 钩子在各连接的协程里跑，记事要独占
	events []string   // 依次记下的事件，形如 on:厂身份、off:厂身份
}

// 记一条事件。
func (p *presenceLog) add(ev string) {
	// 多个连接同时记事，先独占。
	p.mu.Lock()
	// 按到达先后追加。
	p.events = append(p.events, ev)
	// 记完就放开。
	p.mu.Unlock()
}

// 拷一份当前事件，免得断言时和钩子交错。
func (p *presenceLog) snapshot() []string {
	// 读之前先独占。
	p.mu.Lock()
	// 读完就放开。
	defer p.mu.Unlock()
	// 拷出副本交给断言。
	return append([]string(nil), p.events...)
}

// 数某条事件出现了几次。
func (p *presenceLog) count(ev string) int {
	// 从副本里数，不占着锁。
	n := 0
	// 逐条比对事件。
	for _, e := range p.snapshot() {
		// 对上就记一次。
		if e == ev {
			// 次数加一。
			n++
		}
	}
	return n
}

// 取最后一条事件，没有就给空。
func (p *presenceLog) last() string {
	// 先拷一份再取尾。
	evs := p.snapshot()
	// 还没有事件就给空。
	if len(evs) == 0 {
		return ""
	}
	// 交出最后到的那条。
	return evs[len(evs)-1]
}

// 不起监听，只拼一份能直接调钩子的服务。
func hookOnly(hooks Hooks) *factoryHook {
	// 表都备好，钩子才能登记连接。
	b := &Broker{
		hooks: hooks,
		wait:  map[string]chan []byte{},
		locks: map[uuid.UUID]*sync.Mutex{},
		conns: map[uuid.UUID]*mqtt.Client{},
	}
	return &factoryHook{broker: b}
}

// 造一个不带网络的厂连接，会话号就是厂身份。
func fakeClient(srv *mqtt.Server, fid uuid.UUID) *mqtt.Client {
	// 不接真连接，只拿来比对是哪一根。
	return srv.NewClient(nil, "test", fid.String(), false)
}

// 新连接已上线后旧连接才断开，不能把厂标成离线。
func TestStaleDisconnectSkipped(t *testing.T) {
	// 收集上下线事件。
	log := &presenceLog{}
	// 钩子只记事，不碰库。
	h := hookOnly(Hooks{
		// 上线就记一条在线。
		Online: func(id uuid.UUID) { log.add("on:" + id.String()) },
		// 离线就记一条离线。
		Offline: func(id uuid.UUID) { log.add("off:" + id.String()) },
	})
	// 只借服务来造连接，不起监听。
	srv := mqtt.New(&mqtt.Options{})
	// 同一厂的新旧两根连接。
	fid := uuid.New()
	// 旧连接。
	oldCl := fakeClient(srv, fid)
	// 接管旧会话的新连接。
	newCl := fakeClient(srv, fid)
	// 旧连接先上线。
	h.OnSessionEstablished(oldCl, packets.Packet{})
	// 新连接接管后上线。
	h.OnSessionEstablished(newCl, packets.Packet{})
	// 旧连接的离线晚到。
	h.OnDisconnect(oldCl, nil, false)
	// 晚到的旧离线不该落到名录上。
	if n := log.count("off:" + fid.String()); n != 0 {
		// 被顶掉的旧连接把厂标成了离线。
		t.Fatalf("stale disconnect marked offline %d times: %v", n, log.snapshot())
	}
	// 当前连接断开才标离线。
	h.OnDisconnect(newCl, nil, false)
	// 本连接断开应照常标离线一次。
	if n := log.count("off:" + fid.String()); n != 1 {
		// 当前连接断开却没标离线。
		t.Fatalf("current disconnect offline count %d: %v", n, log.snapshot())
	}
	// 最后一条应是离线。
	if got := log.last(); got != "off:"+fid.String() {
		// 名录最终状态不对。
		t.Fatalf("last event %q", got)
	}
}

// 用同一会话号先后连两次，接管后名录最终仍是在线。
func TestTakeoverKeepsOnline(t *testing.T) {
	// 收集上下线事件。
	log := &presenceLog{}
	// 离线故意慢一点，旧连接的离线必然晚于新连接上线。
	b, err := Listen("127.0.0.1:0", Hooks{
		// 测例不验厂钥，签名格式对就放行。
		Auth: func(uuid.UUID, int64, []byte) error { return nil },
		// 上线就记一条在线。
		Online: func(id uuid.UUID) { log.add("on:" + id.String()) },
		// 离线先等一会再记，模拟库写慢。
		Offline: func(id uuid.UUID) {
			// 拖慢离线，放大接管时的先后错位。
			time.Sleep(200 * time.Millisecond)
			// 等完再记离线。
			log.add("off:" + id.String())
		},
	})
	// 起不来就停测。
	if err != nil {
		// 通道起不来，后面都测不了。
		t.Fatal(err)
	}
	// 测完关掉通道，避免端口占着。
	t.Cleanup(func() { _ = b.Close() })
	// 这一厂的身份。
	fid := uuid.New()
	// 第一次连接，模拟弱网前的旧连接。
	first := dialFactory(t, b.Addr(), fid)
	// 等到第一次上线。
	waitCount(t, log, "on:"+fid.String(), 1)
	// 同一会话号再连一次，触发接管。
	second := dialFactory(t, b.Addr(), fid)
	// 等到第二次上线。
	waitCount(t, log, "on:"+fid.String(), 2)
	// 再等过慢离线的时长，让晚到的旧离线有机会落下。
	time.Sleep(600 * time.Millisecond)
	// 接管后最后一条必须是在线。
	if got := log.last(); got != "on:"+fid.String() {
		// 旧连接的离线盖住了新连接。
		t.Fatalf("after takeover last event %q: %v", got, log.snapshot())
	}
	// 旧客户端已被顶掉，断开它不该改名录。
	first.Disconnect(50)
	// 当前连接主动断开。
	second.Disconnect(250)
	// 当前连接断开后名录应落到离线。
	waitLast(t, log, "off:"+fid.String())
}

// A 厂的上线卡住时，B 厂上线不该被拖住。
func TestOnlineNotBlockedAcrossFactories(t *testing.T) {
	// 两家厂的身份。
	fa, fb := uuid.New(), uuid.New()
	// A 厂上线进了钩子就报一声。
	entered := make(chan struct{})
	// 放开 A 厂卡住的上线。
	release := make(chan struct{})
	// B 厂上线完成就报一声。
	bDone := make(chan struct{})
	// A 厂上线卡住，B 厂上线直接完成。
	h := hookOnly(Hooks{
		// 按厂区分是卡住还是放行。
		Online: func(id uuid.UUID) {
			// A 厂就卡在这里，直到测例放开。
			if id == fa {
				// 告诉测例 A 厂已占住自己的锁。
				close(entered)
				// 一直等测例放开。
				<-release
				return
			}
			// B 厂上线完成。
			close(bDone)
		},
	})
	// 只借服务来造连接，不起监听。
	srv := mqtt.New(&mqtt.Options{})
	// 测完放开 A 厂，避免协程漏掉。
	defer close(release)
	// A 厂在后台上线并卡住。
	go h.OnSessionEstablished(fakeClient(srv, fa), packets.Packet{})
	// 等 A 厂真的卡在钩子里。
	<-entered
	// B 厂在后台上线。
	go h.OnSessionEstablished(fakeClient(srv, fb), packets.Packet{})
	// B 厂应很快完成，不受 A 厂拖累。
	select {
	// B 厂上线已完成。
	case <-bDone:
	// 等太久说明被 A 厂堵住了。
	case <-time.After(2 * time.Second):
		// 一家厂卡住拖住了别家。
		t.Fatal("factory B online blocked by factory A")
	}
}

// 用厂身份连上测例通道，连不上就停测。
func dialFactory(t *testing.T, addr string, fid uuid.UUID) paho.Client {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 准备连接参数，和厂端保持一致。
	opts := paho.NewClientOptions()
	// 指到本机通道，不连外网。
	opts.AddBroker("tcp://" + addr)
	// 会话号用厂身份，两次连接才会接管。
	opts.SetClientID(fid.String())
	// 用户名同样用厂身份。
	opts.SetUsername(fid.String())
	// 密码只要格式对，测例钩子不验签名。
	opts.SetPassword(nodekey.FormatMQTTPassword(time.Now().Unix(), []byte("sig")))
	// 和厂端一样保留会话。
	opts.SetCleanSession(false)
	// 关掉自动重连，被顶掉后不再回拨。
	opts.SetAutoReconnect(false)
	// 建客户端准备连接。
	cli := paho.NewClient(opts)
	// 发起连接。
	tok := cli.Connect()
	// 时限内没连上就停测。
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		// 通道没连上，后面测不了。
		t.Fatalf("mqtt connect: %v", tok.Error())
	}
	// 测完断开，避免会话留到下一例。
	t.Cleanup(func() { cli.Disconnect(50) })
	return cli
}

// 等某条事件累计到给定次数，超时停测。
func waitCount(t *testing.T, log *presenceLog, ev string, want int) {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 最多等几秒。
	deadline := time.Now().Add(5 * time.Second)
	// 时限内反复看。
	for time.Now().Before(deadline) {
		// 次数够了就返回。
		if log.count(ev) >= want {
			return
		}
		// 稍等再看。
		time.Sleep(20 * time.Millisecond)
	}
	// 超时还没等到。
	t.Fatalf("wait %s x%d: %v", ev, want, log.snapshot())
}

// 等最后一条事件变成给定的那条，超时停测。
func waitLast(t *testing.T, log *presenceLog, ev string) {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 最多等几秒。
	deadline := time.Now().Add(5 * time.Second)
	// 时限内反复看。
	for time.Now().Before(deadline) {
		// 最后一条对上就返回。
		if log.last() == ev {
			return
		}
		// 稍等再看。
		time.Sleep(20 * time.Millisecond)
	}
	// 超时还没等到。
	t.Fatalf("wait last %s: %v", ev, log.snapshot())
}
