// Package mqttbroker 管 WAN 内嵌 MQTT Broker：验厂钥、按厂锁 Topic、转发小指令。
// 不管闭包/软件正文，也不判业务对错。
package mqttbroker

import (
	"bytes"
	"context"
	"crypto/tls"
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
	server  *mqtt.Server               // 内嵌的消息服务
	addr    string                     // 实际监听地址
	tlsAddr string                     // TLS 实际监听地址；没开 TLS 为空
	hooks   Hooks                      // 鉴权和在线交给应用服务
	mu      sync.Mutex                 // 保护等回执的槽位
	wait    map[string]chan []byte     // 厂+问询号，等升档回执
	pmu     sync.Mutex                 // 只护每厂锁和当前连接两张表的取放，不包库写
	locks   map[uuid.UUID]*sync.Mutex  // 每厂一把锁，同厂上下线按连接先后落库
	conns   map[uuid.UUID]*mqtt.Client // 每厂当前登记的连接，旧连接晚到的离线据此跳过
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

// Config 是 Broker 的监听参数；TLS 为空时只开明文。
type Config struct {
	Addr    string      // 明文监听地址，空则 :1883
	TLSAddr string      // TLS 监听地址，空则 :8883；TLS 为空时不开
	TLS     *tls.Config // 服务端证书配置；nil 表示不开 TLS 监听
}

// Listen 在 addr 起 Broker；空则 :1883。
func Listen(addr string, hooks Hooks) (*Broker, error) {
	// 只开明文监听，和原来的用法一致。
	return ListenWith(Config{Addr: addr}, hooks)
}

// ListenWith 按配置起 Broker；配了 TLS 就同一服务再开一个 TLS 监听，共用鉴权和钩子。
func ListenWith(cfg Config, hooks Hooks) (*Broker, error) {
	// 没给地址就听默认端口。
	if cfg.Addr == "" {
		// 默认给厂端连接用。
		cfg.Addr = ":1883"
	}
	// 先占住端口，拿到实际地址。
	ln, err := net.Listen("tcp", cfg.Addr)
	// 端口占不到就起不来。
	if err != nil {
		return nil, err
	}
	// 先记下地址和钩子，等槽还是空的。
	b := &Broker{
		addr:  ln.Addr().String(),
		hooks: hooks,
		wait:  map[string]chan []byte{},
		locks: map[uuid.UUID]*sync.Mutex{},
		conns: map[uuid.UUID]*mqtt.Client{},
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
	// 配了证书才再开 TLS 监听，鉴权和上下线走同一套钩子。
	if cfg.TLS != nil {
		// 挂上 TLS 监听，失败就把已占的端口都放开。
		if err := b.listenTLS(server, cfg); err != nil {
			// 明文端口也放开，避免半起的服务占着。
			_ = ln.Close()
			return nil, err
		}
	}
	// 服务准备好，关闭时才能停掉。
	b.server = server
	// 监听放后台，调用方先拿到地址。
	go func() { _ = server.Serve() }()
	return b, nil
}

// listenTLS 在 TLSAddr 上占端口并包一层 TLS，挂到同一个服务上。
func (b *Broker) listenTLS(server *mqtt.Server, cfg Config) error {
	// 没给地址就听默认的 MQTTS 端口。
	if cfg.TLSAddr == "" {
		// 默认给走 TLS 的厂端用。
		cfg.TLSAddr = ":8883"
	}
	// 先占住 TLS 端口，拿到实际地址。
	raw, err := net.Listen("tcp", cfg.TLSAddr)
	// 端口占不到就起不来。
	if err != nil {
		return err
	}
	// 记下实际地址，测试和日志要用。
	b.tlsAddr = raw.Addr().String()
	// 握手在首次读时完成，证书由配置给出。
	ln := tls.NewListener(raw, cfg.TLS)
	// 和明文共用一个服务，鉴权与 ACL 不分叉。
	if err := server.AddListener(listeners.NewNet("tls", ln)); err != nil {
		// 挂不上就放开 TLS 端口。
		_ = raw.Close()
		return err
	}
	return nil
}

// Addr 返回实际监听地址。
func (b *Broker) Addr() string { return b.addr }

// TLSAddr 返回 TLS 实际监听地址；没开 TLS 为空。
func (b *Broker) TLSAddr() string { return b.tlsAddr }

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

// 取该厂的上下线锁，头一次见到这一厂就新建一把。
func (b *Broker) factoryLock(id uuid.UUID) *sync.Mutex {
	// 取放表项前先独占小锁。
	b.pmu.Lock()
	// 取完就放开，不让别厂等着。
	defer b.pmu.Unlock()
	// 先看这一厂有没有锁。
	lock := b.locks[id]
	// 头一次上线就给这一厂配一把。
	if lock == nil {
		// 新建一把锁，此后同厂都排在这把上。
		lock = &sync.Mutex{}
		// 记进表里，下次同厂取到同一把。
		b.locks[id] = lock
	}
	return lock
}

// 把本连接登记成该厂当前连接，盖掉之前的。
func (b *Broker) setConn(id uuid.UUID, cl *mqtt.Client) {
	// 改登记表前先独占小锁。
	b.pmu.Lock()
	// 这一厂此后以本连接为准。
	b.conns[id] = cl
	// 登记完就放开小锁。
	b.pmu.Unlock()
}

// 登记的仍是本连接才清掉并报真；已被顶掉则不动。
func (b *Broker) dropConn(id uuid.UUID, cl *mqtt.Client) bool {
	// 查改登记表前先独占小锁。
	b.pmu.Lock()
	// 查改完就放开小锁。
	defer b.pmu.Unlock()
	// 登记的已是别的连接，说明本连接被顶掉了。
	if b.conns[id] != cl {
		return false
	}
	// 本连接就是当前连接，断开后清掉登记。
	delete(b.conns, id)
	return true
}

// 在该厂锁内登记本连接再标在线；已被顶掉的旧连接不登记。
func (b *Broker) online(id uuid.UUID, cl *mqtt.Client) {
	// 取该厂的锁，同厂上下线排队、别厂不受影响。
	lock := b.factoryLock(id)
	// 占住该厂锁，登记和落库按连接先后来。
	lock.Lock()
	// 落库完再放开，同厂后到的上下线接着跑。
	defer lock.Unlock()
	// 已被新连接顶掉就不登记，免得盖住新连接。
	if cl.IsTakenOver() {
		return
	}
	// 记下本连接是这一厂的当前连接。
	b.setConn(id, cl)
	// 装了上线回调才去动名录。
	if b.hooks.Online != nil {
		// 把这一厂标成在线。
		b.hooks.Online(id)
	}
	// 留下上线记录，方便对通道。
	slog.Info("factory mqtt online", "factory", id)
}

// 在该厂锁内只让当前连接标离线；被顶掉的旧连接晚到就跳过。
func (b *Broker) offline(id uuid.UUID, cl *mqtt.Client) {
	// 取该厂的锁，和上线用同一把。
	lock := b.factoryLock(id)
	// 占住该厂锁，避免和新连接的上线交错。
	lock.Lock()
	// 落库完再放开，同厂后到的上下线接着跑。
	defer lock.Unlock()
	// 登记的已是新连接，旧连接断开不改名录。
	if !b.dropConn(id, cl) {
		// 留下跳过记录，方便对接管时序。
		slog.Info("factory mqtt stale disconnect skipped", "factory", id)
		return
	}
	// 装了离线回调才去动名录。
	if b.hooks.Offline != nil {
		// 把这一厂标成离线。
		b.hooks.Offline(id)
	}
	// 留下离线记录，方便对通道。
	slog.Info("factory mqtt offline", "factory", id)
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

// OnSessionEstablished 登记本连接为该厂当前连接，名录标在线。
func (h *factoryHook) OnSessionEstablished(cl *mqtt.Client, _ packets.Packet) {
	// 本进程下发的连接不进名录。
	if cl.Net.Inline {
		return
	}
	// 会话号就是厂身份。
	fid, err := uuid.Parse(cl.ID)
	// 解析失败就不能标在线。
	if err != nil {
		return
	}
	// 按厂排队登记并标在线，旧连接晚到也盖不住新连接。
	h.broker.online(fid, cl)
}

// OnDisconnect 当前连接断开才标离线，被接管的旧连接跳过。
func (h *factoryHook) OnDisconnect(cl *mqtt.Client, _ error, _ bool) {
	// 本进程下发的连接不进名录。
	if cl.Net.Inline {
		return
	}
	// 会话号就是厂身份。
	fid, err := uuid.Parse(cl.ID)
	// 解析失败就不能标离线。
	if err != nil {
		return
	}
	// 只有登记的仍是本连接才标离线。
	h.broker.offline(fid, cl)
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
