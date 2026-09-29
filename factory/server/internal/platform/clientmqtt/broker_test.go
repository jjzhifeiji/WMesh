package clientmqtt

import (
	"encoding/json"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"

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
