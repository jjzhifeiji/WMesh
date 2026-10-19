package mqttbroker

import (
	"crypto/tls"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"

	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/platform/testtls"
)

// 起一个明文加 TLS 双监听的 Broker，Auth 记下最后连进来的厂。
func startTLSBroker(t *testing.T, pki testtls.PKI) (*Broker, *atomic.Value) {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 记下鉴权钩子看到的厂身份。
	seen := &atomic.Value{}
	// 两个端口都随机占，避免撞端口。
	b, err := ListenWith(Config{Addr: "127.0.0.1:0", TLSAddr: "127.0.0.1:0", TLS: pki.ServerConfig()}, Hooks{
		// 测例只记身份，不验签名。
		Auth: func(factoryID uuid.UUID, _ int64, _ []byte) error {
			// 记下这次连进来的厂。
			seen.Store(factoryID)
			return nil
		},
	})
	// 起不来就停测。
	if err != nil {
		// 停测并带上原因。
		t.Fatalf("listen: %v", err)
	}
	// 测完关掉，端口让出来。
	t.Cleanup(func() { _ = b.Close() })
	// 开了 TLS 就该有实际地址。
	if b.TLSAddr() == "" {
		// TLS 监听没起来。
		t.Fatal("no tls addr")
	}
	return b, seen
}

// 按给定地址和 TLS 配置拨一次，交回连接结果。
func tryDial(broker string, cfg *tls.Config, fid uuid.UUID) error {
	// 参数和厂端一致：会话名、用户名都是厂身份。
	opts := paho.NewClientOptions()
	// 指到测例 Broker。
	opts.AddBroker(broker)
	// 会话名用厂身份。
	opts.SetClientID(fid.String())
	// 用户名同样用厂身份。
	opts.SetUsername(fid.String())
	// 密码只要格式对，测例钩子不验签名。
	opts.SetPassword(nodekey.FormatMQTTPassword(time.Now().Unix(), []byte("sig")))
	// 不自动重连，失败立刻交回。
	opts.SetAutoReconnect(false)
	// 握手失败要快点交回。
	opts.SetConnectTimeout(3 * time.Second)
	// 给了 TLS 配置才套上。
	if cfg != nil {
		// 交给 paho 握手时用。
		opts.SetTLSConfig(cfg)
	}
	// 建客户端准备拨号。
	cli := paho.NewClient(opts)
	// 发起连接。
	tok := cli.Connect()
	// 时限内没完成就当失败。
	if !tok.WaitTimeout(5 * time.Second) {
		return errTimeout
	}
	// 连上了就断开，避免会话留到下一步。
	if tok.Error() == nil {
		// 断开测例连接。
		cli.Disconnect(50)
	}
	// 交回连接结果，握手失败就是这里的错误。
	return tok.Error()
}

var errTimeout = errors.New("connect timeout") // 拨号超时，区别于握手被拒

// 带 CA 的客户端走 ssl 能连上并过鉴权；不带 CA、明文连 TLS 端口都连不上；明文端口照常。
func TestListenWithTLS(t *testing.T) {
	// 现生成一套证书。
	pki := testtls.New(t)
	// 起双监听 Broker。
	b, seen := startTLSBroker(t, pki)
	// 一个厂身份，鉴权钩子要看到它。
	fid := uuid.New()
	// 带 CA 走 ssl 应能连上。
	if err := tryDial("ssl://"+b.TLSAddr(), pki.ClientConfig(), fid); err != nil {
		// 合法 TLS 连接被拒了。
		t.Fatalf("tls dial with ca: %v", err)
	}
	// 鉴权钩子要拿到这家厂。
	if got, _ := seen.Load().(uuid.UUID); got != fid {
		// TLS 连接没走同一套鉴权。
		t.Fatalf("auth saw %v want %v", got, fid)
	}
	// 不带 CA 只信系统根证书，握手应失败。
	if err := tryDial("ssl://"+b.TLSAddr(), &tls.Config{MinVersion: tls.VersionTLS12}, uuid.New()); err == nil {
		// 不认识的证书被接受了。
		t.Fatal("tls dial without ca succeeded")
	}
	// 明文客户端连 TLS 端口应失败。
	if err := tryDial("tcp://"+b.TLSAddr(), nil, uuid.New()); err == nil {
		// 明文打进了 TLS 端口。
		t.Fatal("plain dial to tls port succeeded")
	}
	// 另一个厂身份，验明文端口。
	plain := uuid.New()
	// 明文端口照常能连。
	if err := tryDial("tcp://"+b.Addr(), nil, plain); err != nil {
		// 开 TLS 把明文弄坏了。
		t.Fatalf("plain dial: %v", err)
	}
	// 明文连接同样过鉴权。
	if got, _ := seen.Load().(uuid.UUID); got != plain {
		// 明文连接没走鉴权。
		t.Fatalf("auth saw %v want %v", got, plain)
	}
}

// 不给 TLS 时只开明文，TLS 地址为空。
func TestListenWithoutTLS(t *testing.T) {
	// 只给明文地址。
	b, err := ListenWith(Config{Addr: "127.0.0.1:0"}, Hooks{})
	// 起不来就停测。
	if err != nil {
		// 停测并带上原因。
		t.Fatalf("listen: %v", err)
	}
	// 测完关掉。
	t.Cleanup(func() { _ = b.Close() })
	// 没给证书就不该开 TLS。
	if b.TLSAddr() != "" {
		// 意外开了 TLS 监听。
		t.Fatalf("tls addr %q without tls", b.TLSAddr())
	}
}
