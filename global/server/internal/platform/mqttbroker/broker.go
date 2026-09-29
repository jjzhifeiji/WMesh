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
	Online  func(factoryID uuid.UUID)                               // 会话已建立
	Offline func(factoryID uuid.UUID)                               // 会话断开
	Up      func(factoryID uuid.UUID, payload []byte)               // 厂端上行，无问询号时交给业务
}

// Broker 是 WAN 进程内的 MQTT 服务。
type Broker struct {
	server *mqtt.Server           // 内嵌的消息服务
	addr   string                 // 实际监听地址
	hooks  Hooks                  // 鉴权和在线交给应用服务
	mu     sync.Mutex             // 保护等回执的槽位
	wait   map[string]chan []byte // 厂+问询号，等升档回执
}

// DownTopic 仅该厂可订的下行 Topic。
func DownTopic(factoryID uuid.UUID) string {
	// 下行主题带上厂身份，别厂订不到。
	return "wan/" + factoryID.String() + "/down"
}

// UpTopic 仅该厂可发的上行 Topic。
func UpTopic(factoryID uuid.UUID) string {
	// 上行主题带上厂身份，别厂发不进来。
	return "wan/" + factoryID.String() + "/up"
}

// Listen 在 addr 起 Broker；空则 :1883。
func Listen(addr string, hooks Hooks) (*Broker, error) {
	// 没给地址就听默认端口。
	if addr == "" {
		// 默认给厂端连接用。
		addr = ":1883"
	}
	// 先占住端口，拿到实际地址。
	ln, err := net.Listen("tcp", addr)
	// 端口占不到就起不来。
	if err != nil {
		return nil, err
	}
	// 先记下地址和钩子，等槽还是空的。
	b := &Broker{
		addr:  ln.Addr().String(),
		hooks: hooks,
		wait:  map[string]chan []byte{},
	}
	// 内嵌客户端用于本进程下发，日志丢掉。
	server := mqtt.New(&mqtt.Options{
		InlineClient: true,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	// 鉴权钩子指回这份服务。
	hook := &factoryHook{broker: b}
	// 钩子挂不上就放开端口。
	if err := server.AddHook(hook, nil); err != nil {
		// 避免端口一直被占着。
		_ = ln.Close()
		return nil, err
	}
	// 用已经打开的连接来听。
	if err := server.AddListener(listeners.NewNet("tcp", ln)); err != nil {
		// 听不上同样放开端口。
		_ = ln.Close()
		return nil, err
	}
	// 服务准备好，关闭时才能停掉。
	b.server = server
	// 监听放后台，调用方先拿到地址。
	go func() { _ = server.Serve() }()
	return b, nil
}

// Addr 返回实际监听地址。
func (b *Broker) Addr() string { return b.addr }

// Close 停掉 Broker。
func (b *Broker) Close() error {
	// 没起来就没有可停的服务。
	if b == nil || b.server == nil {
		return nil
	}
	// 停掉监听和已有会话。
	return b.server.Close()
}

// Publish 向该厂下行投一条指令；厂不在线则进会话队列。
func (b *Broker) Publish(factoryID uuid.UUID, payload []byte) error {
	// 没起来就丢弃，避免空指针。
	if b == nil || b.server == nil {
		return nil
	}
	// 投到该厂下行，至少一次、不保留。
	return b.server.Publish(DownTopic(factoryID), payload, false, 1)
}

// Call 下行问询并等带同一问询号的上行回执。
func (b *Broker) Call(ctx context.Context, factoryID uuid.UUID, reqID string, payload []byte) ([]byte, error) {
	// 只留一条回执，避免堵住上行。
	ch := make(chan []byte, 1)
	// 先占上槽位，回执才不会丢。
	b.park(factoryID, reqID, ch)
	// 无论成败都清掉槽位。
	defer b.unpark(factoryID, reqID)
	// 下行发出去，厂端按问询号回。
	if err := b.Publish(factoryID, payload); err != nil {
		return nil, err
	}
	// 要么取消，要么等到回执。
	select {
	// 调用方取消就不再等。
	case <-ctx.Done():
		// 把调用方取消的原因交回。
		return nil, ctx.Err()
	// 带同一问询号的上行到了。
	case raw := <-ch:
		return raw, nil
	}
}

// Drop 踢掉该厂当前 MQTT 会话。
func (b *Broker) Drop(factoryID uuid.UUID) {
	// 没起来就没有会话可踢。
	if b == nil || b.server == nil {
		return
	}
	// 按厂身份找当前会话。
	cl, ok := b.server.Clients.Get(factoryID.String())
	// 这一厂不在线就不用踢。
	if !ok {
		return
	}
	// 以未授权断开，让厂端重新签入。
	_ = b.server.DisconnectClient(cl, packets.ErrNotAuthorized)
}

// 登记等回执的槽位。
func (b *Broker) park(id uuid.UUID, reqID string, ch chan []byte) {
	// 改等回执的槽之前先独占。
	b.mu.Lock()
	// 这个厂的这次问询等在这条通道上。
	b.wait[waitKey(id, reqID)] = ch
	// 登记完就放开这把锁。
	b.mu.Unlock()
}

// 问询结束清掉等槽。
func (b *Broker) unpark(id uuid.UUID, reqID string) {
	// 改等回执的槽之前先独占。
	b.mu.Lock()
	// 问询结束，回执不再往这条通道送。
	delete(b.wait, waitKey(id, reqID))
	// 清完就放开这把锁。
	b.mu.Unlock()
}

// 厂身份加问询号。
func waitKey(id uuid.UUID, reqID string) string {
	// 厂身份和问询号拼成槽位键，避免串厂。
	return id.String() + "/" + reqID
}

// 把带问询号的上行交给 Call；没有等槽则交给业务。
func (b *Broker) deliver(id uuid.UUID, payload []byte) bool {
	// 只偷看问询号，其余交给业务。
	var peek struct {
		ReqID string `json:"reqId"` // 上行里的问询号，空则不是回执
	}
	// 拆不出问询号就不是在等的回执。
	if json.Unmarshal(payload, &peek) != nil || peek.ReqID == "" {
		return false
	}
	// 查槽前先独占，避免和清槽交错。
	b.mu.Lock()
	// 取出这条问询的等待通道。
	ch := b.wait[waitKey(id, peek.ReqID)]
	// 查完就放开，发送不再占锁。
	b.mu.Unlock()
	// 没有等槽就交给业务上行。
	if ch == nil {
		return false
	}
	// 送进等待通道；满了就丢掉，不堵住上行。
	select {
	// 通道空着就把回执送进去。
	case ch <- payload:
	// 已经有一条在等，这条不再塞。
	default:
	}
	return true
}

// 厂端连接的鉴权钩子，只覆盖要用的回调。
type factoryHook struct {
	mqtt.HookBase         // 钩子缺省实现，只覆盖要用的回调
	broker        *Broker // 回指这份服务，才能验钥和投递
}

// ID 给 Broker 区分钩子。
func (h *factoryHook) ID() string { return "wmesh-factory-auth" }

// Provides 声明本钩子接管的回调。
func (h *factoryHook) Provides(b byte) bool {
	// 只接管鉴权、权限、在线和上行，其余用缺省。
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
	// 本进程下发不走厂钥。
	if cl.Net.Inline {
		return true
	}
	// 没装鉴权就不能让厂端进来。
	if h.broker.hooks.Auth == nil {
		return false
	}
	// 用户名必须是厂的稳定身份。
	fid, err := uuid.Parse(string(pk.Connect.Username))
	// 身份解析失败就拒绝连接。
	if err != nil {
		return false
	}
	// 客户标识必须和厂身份相同，防止冒充。
	if pk.Connect.ClientIdentifier != fid.String() {
		return false
	}
	// 密码里是时间窗和签名。
	unix, sig, err := nodekey.ParseMQTTPassword(string(pk.Connect.Password))
	// 密码格式不对就拒绝，并留下原因。
	if err != nil {
		// 记下是哪一厂，口令原文不进日志。
		slog.Warn("factory mqtt auth failed", "factory", fid, "err", err)
		return false
	}
	// 交给应用服务验厂钥和时间窗。
	if err := h.broker.hooks.Auth(fid, unix, sig); err != nil {
		// 验失败留下原因，不把签名打出来。
		slog.Warn("factory mqtt auth failed", "factory", fid, "err", err)
		return false
	}
	return true
}

// OnACLCheck 只允许订自己的 down、发自己的 up。
func (h *factoryHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	// 本进程下发不受主题限制。
	if cl.Net.Inline {
		return true
	}
	// 从会话取出厂身份。
	fid, err := uuid.Parse(string(cl.Properties.Username))
	// 身份不对就没有任何主题权限。
	if err != nil {
		return false
	}
	// 写只允许发自己的上行。
	if write {
		// 别的主题这一厂不能发。
		return topic == UpTopic(fid)
	}
	// 订只允许自己的下行。
	return topic == DownTopic(fid)
}

// OnSessionEstablished 名录标在线。
func (h *factoryHook) OnSessionEstablished(cl *mqtt.Client, _ packets.Packet) {
	// 本进程或没装上线回调就不动名录。
	if cl.Net.Inline || h.broker.hooks.Online == nil {
		return
	}
	// 会话号就是厂身份。
	fid, err := uuid.Parse(cl.ID)
	// 解析失败就不能标在线。
	if err != nil {
		return
	}
	// 把这一厂标成在线。
	h.broker.hooks.Online(fid)
	// 留下上线记录，方便对通道。
	slog.Info("factory mqtt online", "factory", fid)
}

// OnDisconnect 名录标离线。
func (h *factoryHook) OnDisconnect(cl *mqtt.Client, _ error, _ bool) {
	// 本进程或没装离线回调就不动名录。
	if cl.Net.Inline || h.broker.hooks.Offline == nil {
		return
	}
	// 会话号就是厂身份。
	fid, err := uuid.Parse(cl.ID)
	// 解析失败就不能标离线。
	if err != nil {
		return
	}
	// 把这一厂标成离线。
	h.broker.hooks.Offline(fid)
	// 留下离线记录，方便对通道。
	slog.Info("factory mqtt offline", "factory", fid)
}

// OnPublished 升档回执交给 Call，其余交给业务。
func (h *factoryHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
	// 本进程发出的下行不再回灌。
	if cl.Net.Inline {
		return
	}
	// 会话号就是厂身份。
	fid, err := uuid.Parse(cl.ID)
	// 解析失败就丢掉这条上行。
	if err != nil {
		return
	}
	// 带问询号的回执交给等待方。
	if h.broker.deliver(fid, pk.Payload) {
		return
	}
	// 其余上行有业务回调才送过去。
	if h.broker.hooks.Up != nil {
		// 没有等槽的上行交给业务。
		h.broker.hooks.Up(fid, pk.Payload)
	}
}
