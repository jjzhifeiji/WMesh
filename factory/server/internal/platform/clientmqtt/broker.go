package clientmqtt

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
	"golang.org/x/time/rate"
)

// 本机上行的包长和频率上限：示教器只发很小的在场回报。
const (
	maxPacketSize  = 16 << 10    // 单包上限，超了引擎直接断开这条连接。
	upEvery        = time.Second // 每条连接每隔这么久补一个上行名额。
	upBurst        = 10          // 每条连接最多攒这么多上行名额。
	upDropLogEvery = 100         // 限速丢掉的条数每满这么多再打一条日志。
)

// ErrUnavailable 厂暂时不能服务（租约没领到、库连不上），不是令牌失效；
// Auth 包着它交回时回「服务不可用」，示教器照常重试，不当会话作废登出。
var ErrUnavailable = errors.New("client mqtt temporarily unavailable")

// ErrClientMismatch 令牌有效，但本次登录钉的不是 CONNECT 报的这台设备（D-59）；
// Auth 包着它交回时回「服务不可用」：拿自己的令牌报别台的 clientId 连不上，正主换设备要重新登录，不当令牌失效登出。
var ErrClientMismatch = errors.New("client mqtt session not bound to this client")

// Hooks 把鉴权和回执交给应用服务；Broker 自己不查库。
type Hooks struct {
	Auth    func(factoryID, clientID uuid.UUID, token string) (uuid.UUID, error) // CONNECT 验本机会话，回登录人；暂不可用包 ErrUnavailable
	Online  func(factoryID, personID uuid.UUID)                                  // 会话已建立
	Offline func(factoryID, personID uuid.UUID)                                  // 会话断开
	Up      func(factoryID, clientID, personID uuid.UUID, payload []byte)        // 本机上行回执
}

// Broker 是厂进程内给本厂 Client 用的 MQTT 服务。
type Broker struct {
	server  *mqtt.Server                // 内嵌的消息服务，只给本厂设备连。
	addr    string                      // 实际监听地址，占住端口之后才知道。
	hooks   Hooks                       // 鉴权和回执交给外面的应用服务。
	mu      sync.Mutex                  // 挡住并发，避免两路同时改这份状态。
	persons map[*mqtt.Client]connPerson // 按每条连接记登录人，同号抢占时不串人。
}

// 一条连接鉴权时认下的登录人，以及它是否已报过上线。
type connPerson struct {
	person  uuid.UUID     // 这条连接鉴权时认下的登录人。
	online  bool          // 已报上线，之后由它自己的断开回调来删。
	limit   *rate.Limiter // 这条连接的上行令牌桶，超出的不交给业务。
	dropped uint64        // 这条连接被限速丢掉的上行条数，日志按它节流。
}

// DownTopic 仅该 Client 可订的下行 Topic。
func DownTopic(factoryID, clientID uuid.UUID) string {
	// 交回收成普通文本再拿去比较或拼接的结果。
	return "factory/" + factoryID.String() + "/client/" + clientID.String() + "/down"
}

// UpTopic 仅该 Client 可发的上行 Topic。
func UpTopic(factoryID, clientID uuid.UUID) string {
	// 交回收成普通文本再拿去比较或拼接的结果。
	return "factory/" + factoryID.String() + "/client/" + clientID.String() + "/up"
}

// Listen 在 addr 起 Broker；空则 :1884。
func Listen(addr string, hooks Hooks) (*Broker, error) {
	// 是空的就改用默认，或按没有处理。
	if addr == "" {
		// 定下地址，再交给后面。
		addr = ":1884"
	}
	// 占住端口，准备接本机连接。
	ln, err := net.Listen("tcp", addr)
	// 没能占住端口，准备接本机连接就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	// 收成普通文本再拿去比较或拼接。
	b := &Broker{addr: ln.Addr().String(), hooks: hooks, persons: map[*mqtt.Client]connPerson{}}
	// 取引擎默认能力，只在上面收紧包长。
	caps := mqtt.NewDefaultServerCapabilities()
	// 超长包在读包头时就断开，不让任意字节灌进业务。
	caps.MaximumPacketSize = maxPacketSize
	// 建内嵌消息服务，日志先丢掉。
	server := mqtt.New(&mqtt.Options{
		InlineClient: true,
		Capabilities: caps,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	// 先当还没找到，找到再改成命中。
	hook := &clientHook{broker: b}
	// 没能把鉴权钩子挂到代理上就停，避免带着残缺继续。
	if err := server.AddHook(hook, nil); err != nil {
		// 把句柄关掉，避免一直占着。
		_ = ln.Close()
		return nil, err
	}
	// 没能新建网络，再交给后面就停，避免带着残缺继续。
	if err := server.AddListener(listeners.NewNet("tcp", ln)); err != nil {
		// 把句柄关掉，避免一直占着。
		_ = ln.Close()
		return nil, err
	}
	// 定下这一段，再交给后面。
	b.server = server
	// 在后台接连接，启动失败不挡住返回。
	go func() { _ = server.Serve() }()
	return b, nil
}

// Addr 返回实际监听地址。
func (b *Broker) Addr() string { return b.addr }

// Close 停掉 Broker。
func (b *Broker) Close() error {
	// 代理还没起来就直接返回，避免空着去操作。
	if b == nil || b.server == nil {
		return nil
	}
	// 关掉并把关闭时的错误一起交回去。
	return b.server.Close()
}

// Publish 向该 Client 下行投一条指令；机不在线则进会话队列。
func (b *Broker) Publish(factoryID, clientID uuid.UUID, payload []byte) error {
	// 代理还没起来就直接返回，避免空着去操作。
	if b == nil || b.server == nil {
		return nil
	}
	// 交回拼出只给这台机器的下行主题的结果。
	return b.server.Publish(DownTopic(factoryID, clientID), payload, false, 1)
}

// 接在代理上的鉴权和上下线钩子。
type clientHook struct {
	mqtt.HookBase         // 没接管的回调走默认实现，避免漏接。
	broker        *Broker // 回到代理才能查在线的人和主题。
}

// ID 给 Broker 区分钩子。
func (h *clientHook) ID() string { return "wmesh-client-auth" }

// Provides 声明本钩子接管的回调。
func (h *clientHook) Provides(b byte) bool {
	// 交回看文本里有没有这段的结果。
	return bytes.Contains([]byte{
		mqtt.OnConnect,
		mqtt.OnConnectAuthenticate,
		mqtt.OnSessionEstablished,
		mqtt.OnDisconnect,
		mqtt.OnACLCheck,
		mqtt.OnPublished,
	}, []byte{b})
}

// OnConnect 验工厂身份、本机身份和人员会话令牌，并自己回 CONNACK 拒绝码：
// 令牌失效回认证失败（3.1.1 上译成未授权），示教器据此登出；厂暂不可用、这台正被别人连着、
// 或令牌钉的不是这台设备（D-59）回「服务不可用」，示教器只重试。
func (h *clientHook) OnConnect(cl *mqtt.Client, pk packets.Packet) error {
	// 引擎自己的内联连接不按外部客户端拦。
	if cl.Net.Inline {
		return nil
	}
	// 先当令牌不对，暂不可用时再改。
	code := packets.ErrBadUsernameOrPassword
	// 验身份和令牌，认下登录人。
	personID, err := h.authenticate(pk)
	// 厂暂不可用、令牌没钉这台设备都不是令牌失效，改回服务不可用，免得示教器登出。
	if errors.Is(err, ErrUnavailable) || errors.Is(err, ErrClientMismatch) {
		// 改成服务不可用，示教器按断线重试。
		code = packets.ErrServerUnavailable
	}
	// 同号已有别人的连接活着就不让顶，免得拿自己的令牌冒充别台把人家挤下线。
	if err == nil && h.broker.heldByOther(cl, personID) {
		// 记下谁想顶哪台，便于排查冒充。
		slog.Warn("client mqtt takeover refused", "client", cl.ID, "person", personID)
		// 回服务不可用，正主不受影响，真换人的等旧连接断开再连上。
		code, err = packets.ErrServerUnavailable, errTakeover
	}
	// 没通过就自己回拒绝码再断开，引擎不再往下走。
	if err != nil {
		// 回拒绝码，写失败也照样断开。
		_ = h.broker.server.SendConnack(cl, code, false, nil)
		return code
	}
	// 按这条连接记下登录人，同号旧连接各记各的。
	h.broker.rememberPerson(cl, personID)
	return nil
}

// errTakeover 同号已有别人的连接活着，这次不让顶替。
var errTakeover = errors.New("client id held by another person")

// authenticate 解出工厂和本机身份后交给应用服务验令牌，回登录人。
func (h *clientHook) authenticate(pk packets.Packet) (uuid.UUID, error) {
	// 没接鉴权就一律拒绝。
	if h.broker.hooks.Auth == nil {
		return uuid.Nil, errNoAuth
	}
	// 把文本收成稳定身份。
	fid, err := uuid.Parse(string(pk.Connect.Username))
	// 没能把文本收成稳定身份就停，避免带着残缺继续。
	if err != nil {
		return uuid.Nil, err
	}
	// 把文本收成稳定身份。
	cid, err := uuid.Parse(pk.Connect.ClientIdentifier)
	// 没能把文本收成稳定身份就停，避免带着残缺继续。
	if err != nil {
		return uuid.Nil, err
	}
	// 交给应用服务验令牌，认下登录人。
	personID, err := h.broker.hooks.Auth(fid, cid, string(pk.Connect.Password))
	// 没能鉴权就停，原因只进日志。
	if err != nil {
		// 记下哪台被拒和原因，日志里不写令牌。
		slog.Warn("client mqtt auth failed", "factory", fid, "client", cid, "err", err)
		return uuid.Nil, err
	}
	return personID, nil
}

// errNoAuth 没接鉴权钩子，谁都不让连。
var errNoAuth = errors.New("client mqtt auth hook missing")

// OnConnectAuthenticate 鉴权已在 OnConnect 做完，这里只认已记下登录人的连接。
func (h *clientHook) OnConnectAuthenticate(cl *mqtt.Client, _ packets.Packet) bool {
	// 内联连接放行，外部连接要在 OnConnect 里记下过登录人。
	return cl.Net.Inline || h.broker.hasPerson(cl)
}

// OnSessionEstablished 记下本机上线，不写令牌。
func (h *clientHook) OnSessionEstablished(cl *mqtt.Client, _ packets.Packet) {
	// 引擎自己的内联连接不按外部客户端拦。
	if cl.Net.Inline {
		return
	}
	// 从连接里取出工厂和本机身份。
	fid, cid, ok := sessionIDs(cl)
	// 没有命中就走另一路，不用零值冒充有值。
	if !ok {
		return
	}
	// 取出这条连接的登录人，并标成已上线。
	personID := h.broker.markOnline(cl)
	// 已经有值就按有内容处理，不要覆盖成空。
	if h.broker.hooks.Online != nil && personID != uuid.Nil {
		// 上线，再交给后面，再交给后面。
		h.broker.hooks.Online(fid, personID)
	}
	// 写一条信息级日志看信封。
	slog.Info("client mqtt online", "factory", fid, "client", cid, "person", personID)
}

// OnDisconnect 记下本机离线。
func (h *clientHook) OnDisconnect(cl *mqtt.Client, _ error, _ bool) {
	// 引擎自己的内联连接不按外部客户端拦。
	if cl.Net.Inline {
		return
	}
	// 从连接里取出工厂和本机身份。
	fid, cid, ok := sessionIDs(cl)
	// 没有命中就走另一路，不用零值冒充有值。
	if !ok {
		return
	}
	// 只删这条连接自己的项，离线记到它自己的登录人。
	personID := h.broker.forgetPerson(cl)
	// 已经有值就按有内容处理，不要覆盖成空。
	if h.broker.hooks.Offline != nil && personID != uuid.Nil {
		// 下线，再交给后面，再交给后面。
		h.broker.hooks.Offline(fid, personID)
	}
	// 写一条信息级日志看信封。
	slog.Info("client mqtt offline", "factory", fid, "client", cid, "person", personID)
}

// OnACLCheck 只允许订自己的 down、发自己的 up。
func (h *clientHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	// 引擎自己的内联连接不按外部客户端拦。
	if cl.Net.Inline {
		return true
	}
	// 从连接里取出工厂和本机身份。
	fid, cid, ok := sessionIDs(cl)
	// 没有命中就走另一路，不用零值冒充有值。
	if !ok {
		return false
	}
	// 调用方要求落盘才真正写出去。
	if write {
		// 交回拼出只给这台机器的上行主题的结果。
		return topic == UpTopic(fid, cid)
	}
	// 交回拼出只给这台机器的下行主题的结果。
	return topic == DownTopic(fid, cid)
}

// OnPublished 把本机上行交给业务，不当 MQTT 信任根。
func (h *clientHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
	// 引擎自己的内联连接不按外部客户端拦。
	if cl.Net.Inline || h.broker.hooks.Up == nil {
		return
	}
	// 从连接里取出工厂和本机身份。
	fid, cid, ok := sessionIDs(cl)
	// 没有命中就走另一路，不用零值冒充有值。
	if !ok {
		return
	}
	// 按这条连接的令牌桶放行，超出的只计数不交业务。
	personID, allowed, dropped := h.broker.admitUp(cl)
	// 没有名额就丢掉这条，免得刷爆审计和厂库。
	if !allowed {
		// 头一条和每满一批才打日志，免得日志也被刷满。
		if dropped == 1 || (dropped > 0 && dropped%upDropLogEvery == 0) {
			// 记下哪台哪个人在超频上行，便于排查。
			slog.Warn("client mqtt up throttled", "factory", fid, "client", cid, "person", personID, "dropped", dropped)
		}
		return
	}
	// 交给业务，登录人取这条连接鉴权时认下的。
	h.broker.hooks.Up(fid, cid, personID, pk.Payload)
}

// 记住这条连接对应的登录人，顺带清掉同号的残留项。
func (b *Broker) rememberPerson(cl *mqtt.Client, personID uuid.UUID) {
	// 对象还是空的就直接返回，避免碰到空指针。
	if b == nil || cl == nil {
		return
	}
	// 先占住锁，避免并发写乱。
	b.mu.Lock()
	// 离开时放开锁，免得把别人堵住。
	defer b.mu.Unlock()
	// 是空的就换一路，避免空着往下用。
	if b.persons == nil {
		// 新建空表，再往里记。
		b.persons = map[*mqtt.Client]connPerson{}
	}
	// 逐条看旧项，鉴权后没走到上线就断的不会再有断开回调。
	for old, p := range b.persons {
		// 同号、没上线、已关掉的才清；已上线的等它自己的断开回调。
		if old != cl && old.ID == cl.ID && !p.online && old.Closed() {
			// 删掉这条残留，免得同号反复重连越积越多。
			delete(b.persons, old)
		}
	}
	// 按连接记下登录人并配上自己的令牌桶，等会话建好再标上线。
	b.persons[cl] = connPerson{person: personID, limit: rate.NewLimiter(rate.Every(upEvery), upBurst)}
}

// 取出这条连接的登录人并标成已上线，之后由它自己报离线。
func (b *Broker) markOnline(cl *mqtt.Client) uuid.UUID {
	// 对象还是空的就直接返回，避免碰到空指针。
	if b == nil {
		return uuid.Nil
	}
	// 先占住锁，避免并发写乱。
	b.mu.Lock()
	// 离开时放开锁，免得把别人堵住。
	defer b.mu.Unlock()
	// 取出这条连接记下的那一项。
	p, ok := b.persons[cl]
	// 已被当残留清掉就不补，免得多报一次上线。
	if !ok {
		return uuid.Nil
	}
	// 标成已上线，同号新连接鉴权时不会把它清掉。
	p.online = true
	// 写回表里，让上线标记生效。
	b.persons[cl] = p
	return p.person
}

// 这条连接在 OnConnect 里认下过登录人才算鉴权通过。
func (b *Broker) hasPerson(cl *mqtt.Client) bool {
	// 对象还是空的就当没认下。
	if b == nil || cl == nil {
		return false
	}
	// 先占住锁，避免和断开同时改表。
	b.mu.Lock()
	// 离开时放开锁，免得把别人堵住。
	defer b.mu.Unlock()
	// 按连接查有没有记下的项。
	_, ok := b.persons[cl]
	return ok
}

// heldByOther 同号已有另一个人的连接上线且没断，这次换人不能顶掉它；同一个人重连照常顶。
func (b *Broker) heldByOther(cl *mqtt.Client, personID uuid.UUID) bool {
	// 对象还是空的就当没人占着。
	if b == nil || cl == nil {
		return false
	}
	// 先占住锁，避免和断开同时改表。
	b.mu.Lock()
	// 离开时放开锁，免得把别人堵住。
	defer b.mu.Unlock()
	// 逐条看同号的旧连接。
	for old, p := range b.persons {
		// 同号、已上线、还没断、不是同一个人，才算被别人占着。
		if old != cl && old.ID == cl.ID && p.online && !old.Closed() && p.person != personID {
			return true
		}
	}
	return false
}

// DropPerson 断开这个人在本厂的全部连接，交回断了几条；账号停用、重置密码、收回角色后叫它，
// 示教器重连时令牌已作废会被拒并登出，不用等下一次访问厂服。
func (b *Broker) DropPerson(factoryID, personID uuid.UUID) int {
	// 代理还没起来或没给人，就没什么可断。
	if b == nil || b.server == nil || personID == uuid.Nil {
		return 0
	}
	// 先占住锁，只抄出要断的连接。
	b.mu.Lock()
	// 准备收下要断的连接。
	var drop []*mqtt.Client
	// 逐条看已认下的连接。
	for cl, p := range b.persons {
		// 只断这个人的，别人的不动。
		if p.person != personID {
			continue
		}
		// 取出这条连接的工厂身份。
		fid, _, ok := sessionIDs(cl)
		// 别的厂或身份读不出的不动。
		if !ok || fid != factoryID {
			continue
		}
		// 收下这条，锁外再断。
		drop = append(drop, cl)
	}
	// 抄完就放开，断开回调还要拿这把锁。
	b.mu.Unlock()
	// 逐条断开，示教器重连时再按令牌判。
	for _, cl := range drop {
		// 按未授权断开，断失败也不影响别的。
		_ = b.server.DisconnectClient(cl, packets.ErrNotAuthorized)
	}
	// 交回断了几条，调用方可记日志。
	return len(drop)
}

// 取出这条连接对应的登录人。
func (b *Broker) personOf(cl *mqtt.Client) uuid.UUID {
	// 对象还是空的就直接返回，避免碰到空指针。
	if b == nil {
		return uuid.Nil
	}
	// 先占住锁，避免并发写乱。
	b.mu.Lock()
	// 离开时放开锁，免得把别人堵住。
	defer b.mu.Unlock()
	return b.persons[cl].person
}

// 取出这条连接的登录人，并按它的令牌桶决定这条上行收不收。
func (b *Broker) admitUp(cl *mqtt.Client) (uuid.UUID, bool, uint64) {
	// 对象还是空的就直接返回，避免碰到空指针。
	if b == nil {
		return uuid.Nil, false, 0
	}
	// 先占住锁，避免和鉴权、断开同时改表。
	b.mu.Lock()
	// 离开时放开锁，免得把别人堵住。
	defer b.mu.Unlock()
	// 取出这条连接鉴权时记下的那一项。
	p, ok := b.persons[cl]
	// 没鉴权记下的连接不认人，上行一律不交业务。
	if !ok || p.limit == nil {
		return uuid.Nil, false, 0
	}
	// 桶里还有名额就放行，交回登录人。
	if p.limit.Allow() {
		return p.person, true, 0
	}
	// 没名额就只累加这条连接的丢弃数。
	p.dropped++
	// 写回表里，下次日志节流按新数算。
	b.persons[cl] = p
	return p.person, false, p.dropped
}

// 连接断开时只忘掉它自己的登录人。
func (b *Broker) forgetPerson(cl *mqtt.Client) uuid.UUID {
	// 对象还是空的就直接返回，避免碰到空指针。
	if b == nil {
		return uuid.Nil
	}
	// 先占住锁，避免并发写乱。
	b.mu.Lock()
	// 离开时放开锁，免得把别人堵住。
	defer b.mu.Unlock()
	// 取出这条连接自己记下的那一项。
	p := b.persons[cl]
	// 只删这条连接的项，同号新连接的不动。
	delete(b.persons, cl)
	return p.person
}

// 会话钉死的厂和本机身份；自报字段一律忽略。
func sessionIDs(cl *mqtt.Client) (uuid.UUID, uuid.UUID, bool) {
	// 把文本收成稳定身份。
	fid, err := uuid.Parse(string(cl.Properties.Username))
	// 没能把文本收成稳定身份就停，避免带着残缺继续。
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	// 把文本收成稳定身份。
	cid, err := uuid.Parse(cl.ID)
	// 没能把文本收成稳定身份就停，避免带着残缺继续。
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	return fid, cid, true
}
