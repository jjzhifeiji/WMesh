package clientmqtt

import (
	"bytes"
	"io"
	"log/slog"
	"net"

	"github.com/google/uuid"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
)

// Hooks 把鉴权和回执交给应用服务；Broker 自己不查库。
type Hooks struct {
	Auth func(factoryID, clientID uuid.UUID, token string) error // CONNECT 验本机会话
	Up   func(factoryID, clientID uuid.UUID, payload []byte)     // 本机上行回执
}

// Broker 是厂进程内给本厂 Client 用的 MQTT 服务。
type Broker struct {
	server *mqtt.Server
	addr   string
	hooks  Hooks
}

// DownTopic 仅该 Client 可订的下行 Topic。
func DownTopic(factoryID, clientID uuid.UUID) string {
	return "factory/" + factoryID.String() + "/client/" + clientID.String() + "/down"
}

// UpTopic 仅该 Client 可发的上行 Topic。
func UpTopic(factoryID, clientID uuid.UUID) string {
	return "factory/" + factoryID.String() + "/client/" + clientID.String() + "/up"
}

// Listen 在 addr 起 Broker；空则 :1884。
func Listen(addr string, hooks Hooks) (*Broker, error) {
	if addr == "" {
		addr = ":1884"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	b := &Broker{addr: ln.Addr().String(), hooks: hooks}
	server := mqtt.New(&mqtt.Options{
		InlineClient: true,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	hook := &clientHook{broker: b}
	if err := server.AddHook(hook, nil); err != nil {
		_ = ln.Close()
		return nil, err
	}
	if err := server.AddListener(listeners.NewNet("tcp", ln)); err != nil {
		_ = ln.Close()
		return nil, err
	}
	b.server = server
	go func() { _ = server.Serve() }()
	return b, nil
}

// Addr 返回实际监听地址。
func (b *Broker) Addr() string { return b.addr }

// Close 停掉 Broker。
func (b *Broker) Close() error {
	if b == nil || b.server == nil {
		return nil
	}
	return b.server.Close()
}

// Publish 向该 Client 下行投一条指令；机不在线则进会话队列。
func (b *Broker) Publish(factoryID, clientID uuid.UUID, payload []byte) error {
	if b == nil || b.server == nil {
		return nil
	}
	return b.server.Publish(DownTopic(factoryID, clientID), payload, false, 1)
}

type clientHook struct {
	mqtt.HookBase
	broker *Broker
}

// ID 给 Broker 区分钩子。
func (h *clientHook) ID() string { return "wmesh-client-auth" }

// Provides 声明本钩子接管的回调。
func (h *clientHook) Provides(b byte) bool {
	return bytes.Contains([]byte{
		mqtt.OnConnectAuthenticate,
		mqtt.OnSessionEstablished,
		mqtt.OnDisconnect,
		mqtt.OnACLCheck,
		mqtt.OnPublished,
	}, []byte{b})
}

// OnConnectAuthenticate 验工厂身份、本机身份和人员会话令牌。
func (h *clientHook) OnConnectAuthenticate(cl *mqtt.Client, pk packets.Packet) bool {
	if cl.Net.Inline {
		return true
	}
	if h.broker.hooks.Auth == nil {
		return false
	}
	fid, err := uuid.Parse(string(pk.Connect.Username))
	if err != nil {
		return false
	}
	cid, err := uuid.Parse(pk.Connect.ClientIdentifier)
	if err != nil {
		return false
	}
	if err := h.broker.hooks.Auth(fid, cid, string(pk.Connect.Password)); err != nil {
		slog.Warn("client mqtt auth failed", "factory", fid, "client", cid, "err", err)
		return false
	}
	return true
}

// OnSessionEstablished 记下本机上线，不写令牌。
func (h *clientHook) OnSessionEstablished(cl *mqtt.Client, _ packets.Packet) {
	if cl.Net.Inline {
		return
	}
	fid, cid, ok := sessionIDs(cl)
	if !ok {
		return
	}
	slog.Info("client mqtt online", "factory", fid, "client", cid)
}

// OnDisconnect 记下本机离线。
func (h *clientHook) OnDisconnect(cl *mqtt.Client, _ error, _ bool) {
	if cl.Net.Inline {
		return
	}
	fid, cid, ok := sessionIDs(cl)
	if !ok {
		return
	}
	slog.Info("client mqtt offline", "factory", fid, "client", cid)
}

// OnACLCheck 只允许订自己的 down、发自己的 up。
func (h *clientHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	if cl.Net.Inline {
		return true
	}
	fid, cid, ok := sessionIDs(cl)
	if !ok {
		return false
	}
	if write {
		return topic == UpTopic(fid, cid)
	}
	return topic == DownTopic(fid, cid)
}

// OnPublished 把本机上行交给业务，不当 MQTT 信任根。
func (h *clientHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
	if cl.Net.Inline || h.broker.hooks.Up == nil {
		return
	}
	fid, cid, ok := sessionIDs(cl)
	if !ok {
		return
	}
	h.broker.hooks.Up(fid, cid, pk.Payload)
}

// 会话钉死的厂和本机身份；自报字段一律忽略。
func sessionIDs(cl *mqtt.Client) (uuid.UUID, uuid.UUID, bool) {
	fid, err := uuid.Parse(string(cl.Properties.Username))
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	cid, err := uuid.Parse(cl.ID)
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	return fid, cid, true
}
