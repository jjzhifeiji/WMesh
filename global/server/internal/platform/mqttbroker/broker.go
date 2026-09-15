// Package mqttbroker 管 WAN 内嵌 MQTT Broker：验厂钥、按厂锁 Topic、转发小指令。
// 不管闭包/软件正文，也不判业务对错。
package mqttbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/google/uuid"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"

	"wmesh/global/internal/platform/nodekey"
)

// Hooks 把鉴权和在线交给应用服务；Broker 自己不查库。
type Hooks struct {
	Auth    func(factoryID uuid.UUID, unix int64, sig []byte) error // CONNECT 验厂钥
	Online  func(factoryID uuid.UUID)                             // 会话已建立
	Offline func(factoryID uuid.UUID)                            // 会话断开
	Up      func(factoryID uuid.UUID, payload []byte)            // 厂端上行，无问询号时交给业务
}

// Broker 是 WAN 进程内的 MQTT 服务。
type Broker struct {
	server *mqtt.Server
	addr   string
	hooks  Hooks
	mu     sync.Mutex
	wait   map[string]chan []byte // 厂+问询号，等升档回执
}

// DownTopic 仅该厂可订的下行 Topic。
func DownTopic(factoryID uuid.UUID) string {
	return "wan/" + factoryID.String() + "/down"
}

// UpTopic 仅该厂可发的上行 Topic。
func UpTopic(factoryID uuid.UUID) string {
	return "wan/" + factoryID.String() + "/up"
}

// Listen 在 addr 起 Broker；空则 :1883。
func Listen(addr string, hooks Hooks) (*Broker, error) {
	if addr == "" {
		addr = ":1883"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	b := &Broker{
		addr:  ln.Addr().String(),
		hooks: hooks,
		wait:  map[string]chan []byte{},
	}
	server := mqtt.New(&mqtt.Options{
		InlineClient: true,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	hook := &factoryHook{broker: b}
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

// Publish 向该厂下行投一条指令；厂不在线则进会话队列。
func (b *Broker) Publish(factoryID uuid.UUID, payload []byte) error {
	if b == nil || b.server == nil {
		return nil
	}
	return b.server.Publish(DownTopic(factoryID), payload, false, 1)
}

// Call 下行问询并等带同一问询号的上行回执。
func (b *Broker) Call(ctx context.Context, factoryID uuid.UUID, reqID string, payload []byte) ([]byte, error) {
	ch := make(chan []byte, 1)
	b.park(factoryID, reqID, ch)
	defer b.unpark(factoryID, reqID)
	if err := b.Publish(factoryID, payload); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case raw := <-ch:
		return raw, nil
	}
}

// Drop 踢掉该厂当前 MQTT 会话。
func (b *Broker) Drop(factoryID uuid.UUID) {
	if b == nil || b.server == nil {
		return
	}
	cl, ok := b.server.Clients.Get(factoryID.String())
	if !ok {
		return
	}
	_ = b.server.DisconnectClient(cl, packets.ErrNotAuthorized)
}

// 登记等回执的槽位。
func (b *Broker) park(id uuid.UUID, reqID string, ch chan []byte) {
	b.mu.Lock()
	b.wait[waitKey(id, reqID)] = ch
	b.mu.Unlock()
}

// 问询结束清掉等槽。
func (b *Broker) unpark(id uuid.UUID, reqID string) {
	b.mu.Lock()
	delete(b.wait, waitKey(id, reqID))
	b.mu.Unlock()
}

// 厂身份加问询号。
func waitKey(id uuid.UUID, reqID string) string {
	return id.String() + "/" + reqID
}

// 把带问询号的上行交给 Call；没有等槽则交给业务。
func (b *Broker) deliver(id uuid.UUID, payload []byte) bool {
	var peek struct {
		ReqID string `json:"reqId"`
	}
	if json.Unmarshal(payload, &peek) != nil || peek.ReqID == "" {
		return false
	}
	b.mu.Lock()
	ch := b.wait[waitKey(id, peek.ReqID)]
	b.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- payload:
	default:
	}
	return true
}

type factoryHook struct {
	mqtt.HookBase
	broker *Broker
}

// ID 给 Broker 区分钩子。
func (h *factoryHook) ID() string { return "wmesh-factory-auth" }

// Provides 声明本钩子接管的回调。
func (h *factoryHook) Provides(b byte) bool {
	return bytes.Contains([]byte{
		mqtt.OnConnectAuthenticate,
		mqtt.OnACLCheck,
		mqtt.OnSessionEstablished,
		mqtt.OnDisconnect,
		mqtt.OnPublished,
	}, []byte{b})
}

// OnConnectAuthenticate 验厂身份、厂钥签名和时间窗。
func (h *factoryHook) OnConnectAuthenticate(cl *mqtt.Client, pk packets.Packet) bool {
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
	if pk.Connect.ClientIdentifier != fid.String() {
		return false
	}
	unix, sig, err := nodekey.ParseMQTTPassword(string(pk.Connect.Password))
	if err != nil {
		return false
	}
	return h.broker.hooks.Auth(fid, unix, sig) == nil
}

// OnACLCheck 只允许订自己的 down、发自己的 up。
func (h *factoryHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	if cl.Net.Inline {
		return true
	}
	fid, err := uuid.Parse(string(cl.Properties.Username))
	if err != nil {
		return false
	}
	if write {
		return topic == UpTopic(fid)
	}
	return topic == DownTopic(fid)
}

// OnSessionEstablished 名录标在线。
func (h *factoryHook) OnSessionEstablished(cl *mqtt.Client, _ packets.Packet) {
	if cl.Net.Inline || h.broker.hooks.Online == nil {
		return
	}
	fid, err := uuid.Parse(cl.ID)
	if err != nil {
		return
	}
	h.broker.hooks.Online(fid)
}

// OnDisconnect 名录标离线。
func (h *factoryHook) OnDisconnect(cl *mqtt.Client, _ error, _ bool) {
	if cl.Net.Inline || h.broker.hooks.Offline == nil {
		return
	}
	fid, err := uuid.Parse(cl.ID)
	if err != nil {
		return
	}
	h.broker.hooks.Offline(fid)
}

// OnPublished 升档回执交给 Call，其余交给业务。
func (h *factoryHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
	if cl.Net.Inline {
		return
	}
	fid, err := uuid.Parse(cl.ID)
	if err != nil {
		return
	}
	if h.broker.deliver(fid, pk.Payload) {
		return
	}
	if h.broker.hooks.Up != nil {
		h.broker.hooks.Up(fid, pk.Payload)
	}
}
