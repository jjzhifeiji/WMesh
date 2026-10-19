package wanchannel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
	"wmesh/factory/internal/platform/release"
)

// 关掉 Paho 默认日志，避免噪声进 stdout。
func init() {
	// 把库自带日志丢进空设备，避免噪声打进标准输出。
	mqtt.ERROR, mqtt.CRITICAL, mqtt.WARN, mqtt.DEBUG = log.New(io.Discard, "", 0), log.New(io.Discard, "", 0), log.New(io.Discard, "", 0), log.New(io.Discard, "", 0)
}

var leaseEvery = time.Hour           // 内容租约续期间隔
var aliveEvery = 20 * time.Second    // Broker 死后必须退回外层重拨，不能干等到租约点
var softwareEvery = 15 * time.Minute // 通道一直连着也要问有没有新包
var retryBase = 5 * time.Second      // 拉取或落地失败后第一次补拉前的等待
var retryCap = 5 * time.Minute       // 补拉退避封顶，弱网下也不会越等越久
var downQueue = 64                   // 下行队列长度，满了丢弃并标全量补拉，不堵收包
var pageDebounce = 30 * time.Second  // 同一种类进页补拉的去抖间隔，期间重复的只拉一次

const retryMaxFails = 8 // 同一份脏标记连续补拉失败到这个次数就停，等新标脏再试

// ClientSyncHandler 按索引里仍有效的绑定，把漏掉的本厂绑定作废。
type ClientSyncHandler func(keep []uuid.UUID) error

// Hold 用厂钥连 WAN MQTT，订下行、拉索引和正文，并回答升档问询；补拉在后台协程里跑。
func Hold(ctx context.Context, mqttURL, wanHTTP string, factoryID uuid.UUID, privateKey []byte, apply func(State) error, applyClient func(ClientIntent) error, syncClients ClientSyncHandler, applyClosure ClosureHandler, applyTemplate ClosureHandler, applySoftware SoftwareHandler, applyRetract RetractHandler, applyFS ClosureHandler, applyLease LeaseHandler, onRequest RequestHandler, have HaveHandler, outbound <-chan SyncRequest) error {
	// 没有签发钥就拒绝连接，避免匿名拨上平台。
	if len(privateKey) == 0 {
		return domain.ErrNotFound
	}
	// 解析代理地址，没有主机就不要拨。
	broker, err := resolveMQTT(mqttURL, wanHTTP)
	// 代理地址解析失败就退出，外层会再拨。
	if err != nil {
		return err
	}
	// 没有平台地址就当不可达，避免去拉空主机。
	if strings.TrimSpace(wanHTTP) == "" {
		return domain.ErrWANUnreachable
	}
	// 拨号前先问一次平台取时差，本机钟偏了密码也能按 WAN 时间签。
	probeClock(ctx, wanHTTP)
	// 准备连接参数，后面补上身份、签名和超时。
	opts := mqtt.NewClientOptions()
	// 写上代理地址，拨错主机就连不上。
	opts.AddBroker(broker)
	// 用工厂身份当会话名，平台据此认厂。
	opts.SetClientID(factoryID.String())
	// 用户名同样用工厂身份，和签名配套。
	opts.SetUsername(factoryID.String())
	// 用按 WAN 校正后的时间签名当密码，本机钟偏了也不被拒。
	opts.SetPassword(nodekey.SignMQTTPassword(privateKey, factoryID, wanNow().Unix()))
	// 保留会话，短断线不丢还没处理的下行。
	opts.SetCleanSession(false)
	// HMAC 有时间窗，禁止 Paho 自动重连；断线必须退回外层用新签名再拨。
	opts.SetAutoReconnect(false)
	// 十五秒探活，死连接不能一直占着。
	opts.SetKeepAlive(15 * time.Second)
	// 探活应答超过十秒就当断开。
	opts.SetPingTimeout(10 * time.Second)
	// 十秒拨不上就放弃，交给外层再试。
	opts.SetConnectTimeout(10 * time.Second)
	// 准备断线通知，外层才能用新签名重拨。
	lost := make(chan error, 1)
	// 断线时把原因送出，外层重新签名再拨。
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		// 断线时只送出一次，已经有人在等就不再塞。
		select {
		// 把断线送出去，外层用新签名重拨。
		case lost <- err:
		// 已经有断线在等就不再塞，避免堵住回调。
		default:
		}
	})
	// 按这些参数建立会话，此时尚未拨号。
	cli := mqtt.NewClient(opts)
	// 发起拨号，超时或拒绝就不要订下行。
	tok := cli.Connect()
	// 拨号超时或被拒绝就收成未授权或不可达。
	if !tok.WaitTimeout(15*time.Second) || tok.Error() != nil {
		// 把拒绝收成未授权或不可达，促使外层重拨。
		err := mapMQTTConnect(tok.Error())
		// 被拒时带上当前时差，运维能分清是钟偏还是厂钥不对。
		if errors.Is(err, domain.ErrUnauthorized) {
			// 记下被拒和时差，错误照旧交回外层退避。
			slog.Warn("wan mqtt connect unauthorized", "factory", factoryID, "skewSeconds", clockSkew.Load())
		}
		return err
	}
	// 离开时断开，避免会话占着代理。
	defer cli.Disconnect(250)

	// 拼下行主题，只订这一家厂。
	down := "wan/" + factoryID.String() + "/down"
	up := "wan/" + factoryID.String() + "/up"
	// 记下拉取失败、落地失败或下行溢出要补的种类，到点按退避补拉。
	dirty := newDirtySet(factoryID)
	// 离开时停掉补拉节拍，不留计时器。
	defer dirty.stop()
	// 下行先进有限队列，主循环逐条取出落地。
	msgs := make(chan []byte, downQueue)
	// 收到下行就拷进队列；满了丢掉并标全量，绝不堵住收包协程。
	sub := cli.Subscribe(down, 1, func(_ mqtt.Client, m mqtt.Message) {
		// 拷一份载荷，避免底层缓冲被下一条覆盖。
		payload := append([]byte(nil), m.Payload()...)
		// 有空位就入队，满了立刻丢掉，不等主循环也不看通道寿命。
		select {
		// 把下行送进处理队列。
		case msgs <- payload:
		// 队列满了就丢这条，按整轮索引补回，升档问询只能等平台超时重问。
		default:
			// 记下丢弃，便于排查积压。
			slog.Warn("wan down queue full, dropped, will resync", "factory", factoryID, "queue", cap(msgs))
			// 标全量脏，补拉整轮索引把丢掉的下行补回。
			dirty.mark("")
		}
	})
	// 订不上下行就当平台不可达，调用方会重试。
	if !sub.WaitTimeout(10*time.Second) || sub.Error() != nil {
		return domain.ErrWANUnreachable
	}
	// 连上后把本进程前端/服务版本报给 WAN，名录只在在线时展示。
	if err := publishUp(cli, up, presenceCmd()); err != nil {
		return err
	}

	// 用厂钥签后面的拉取，没有签名会被拒绝。
	pull := newPuller(wanHTTP, factoryID, privateKey)
	// 问包和拉包挂在内部可取消的上下文上，主循环只负责投递。
	sctx, scancel := context.WithCancel(ctx)
	// 后台软件任务，同种类同版本只跑一路。
	jobs := newSoftwareJobs(sctx, pull, applySoftware)
	// 离开前先取消软件任务再等它们退出，进度留在暂存文件里。
	defer func() {
		// 取消问包和拉包，正在拉的请求随之中断。
		scancel()
		// 等软件任务真正退出再返回。
		jobs.wait()
	}()
	// 落库串行，避免主循环和后台补拉交错改本厂；下载正文不占它。
	var mu sync.Mutex
	// 记下整轮同步期间落地的绑定，对账时并进名单。
	binds := &bindLog{}
	// 落地一条指令：正文在锁外先拉好，再占锁落库。
	handle := func(hctx context.Context, cmd Cmd) error {
		// 闭包和模版先在锁外拉正文，本厂已有同修订就整条跳过。
		body, ok := fetchBody(hctx, pull, cmd, have, dirty.failed, applyClosure, applyTemplate)
		// 已有、没人接或没拉到就不必落库。
		if !ok {
			return nil
		}
		// 占住落库，避免和另一路交错改本厂。
		mu.Lock()
		// 离开时放开，下一条才能落库。
		defer mu.Unlock()
		// 按类型落库，正文类失败只标脏，别的失败交回调用方。
		err := applyCmd(cli, up, cmd, body, dirty.failed, apply, applyClient, applyClosure, applyTemplate, jobs, applyRetract, applyFS, applyLease, onRequest)
		// 绑定落上了就记一笔，后台对账不能把它作废。
		if err == nil && cmd.Typ == CmdClientBind {
			// 记下刚落地的设备，整轮对账会并上它。
			binds.record(cmd.ClientID)
		}
		return err
	}
	// 对账时占住落库，并上整轮期间落地的绑定，中途插不进新绑定。
	var reconcile ClientSyncHandler
	// 有人接对账才包这一层，没人接就保持空，整轮照旧跳过对账。
	if syncClients != nil {
		// 包一层：占住落库后把期间落地的绑定并进名单再对账。
		reconcile = func(keep []uuid.UUID) error {
			// 占住落库，对账期间不再落新绑定。
			mu.Lock()
			// 对账完放开，主循环接着落下行。
			defer mu.Unlock()
			// 把期间落地的绑定并进名单，再交给本厂对账。
			return syncClients(binds.merge(keep))
		}
	}

	// 回连首轮同步跑：租约、治理状态和绑定对账要先落地才进长循环。
	if err := pullAndApply(ctx, pull, handle, reconcile, jobs, binds); err != nil {
		return err
	}
	// 后台补拉跟随本条连接，派生子上下文好在离开时先停它。
	bctx, bcancel := context.WithCancel(ctx)
	// 记着后台协程，离开前等它退出。
	var wg sync.WaitGroup
	// 后台遇到注销交回主循环，留一格不堵它。
	fatal := make(chan error, 1)
	// 记一个后台协程，离开时要等它。
	wg.Add(1)
	// 起后台补拉：失败退避、进页补拉和溢出补拉都在这里跑。
	go func() {
		// 协程退出就销账，等它的一方才能返回。
		defer wg.Done()
		// 按脏标记补拉，直到连接离开或遇到注销。
		runResync(bctx, pull, dirty, handle, reconcile, jobs, binds, fatal)
	}()
	// 离开前先停后台补拉并等它退出，旧连接不和新连接首轮同时落库。
	defer func() {
		// 取消后台补拉，正在拉的请求随之中断。
		bcancel()
		// 等后台协程真正退出再返回。
		wg.Wait()
	}()

	// 按点续租，到期前换新解包钥。
	renew := time.NewTicker(leaseEvery)
	// 离开时停掉续租节拍。
	defer renew.Stop()
	// 短周期探活，死代理不能干等到续租。
	alive := time.NewTicker(aliveEvery)
	// 离开时停掉探活节拍。
	defer alive.Stop()
	// 连着也定时问有没有新包。
	soft := time.NewTicker(softwareEvery)
	// 离开时停掉问包节拍。
	defer soft.Stop()
	// 谁先到就处理谁，取消才离开。
	for {
		// 下行、续租、探活和进页补拉谁先到就处理谁。
		select {
		// 取消就结束本次连接，把原因交回外层。
		case <-ctx.Done():
			// 把取消原因交回，外层不再重拨。
			return ctx.Err()
		// 代理断线就交回，外层用新签名重拨。
		case err := <-lost:
			// 断线一律当不可达，促使外层重拨。
			return mapMQTTDisconnect(err)
		// 后台补拉遇到注销就交回，外层停掉通道。
		case err := <-fatal:
			// 把注销交回外层，不再重拨。
			return err
		// 到点就探活，死连接要立刻退回。
		case <-alive.C:
			// 已经断开就当不可达，不要空报在线。
			if !cli.IsConnected() {
				return domain.ErrWANUnreachable
			}
			// 半开 TCP 上探活会失败，立刻退回外层重拨。
			if err := publishUp(cli, up, presenceCmd()); err != nil {
				return err
			}
		// 取出一条下行再落地。
		case raw := <-msgs:
			// 预备装这一条指令。
			var cmd Cmd
			// 解不开就丢掉，不断开通道。
			if json.Unmarshal(raw, &cmd) != nil {
				continue
			}
			// 落地失败就退出循环，外层重拨。
			if err := handle(ctx, cmd); err != nil {
				return err
			}
		// 到点就问有没有新包，一直连着也不漏。
		case <-soft.C:
			// 通道一直连着也去问最高版，放到后台问，不占主循环。
			jobs.sync()
		// 到点就续租，没人接收则跳过。
		case <-renew.C:
			// 没人接收租约就跳过，不空打平台。
			if applyLease == nil {
				continue
			}
			// 预备装新的解包钥。
			var lr leaseResp
			// 续租失败只记日志，不断开通道。
			if err := pull.post(ctx, "/v1/channel/lease", &lr); err != nil {
				// 记下续租失败，下一拍再试。
				slog.Warn("renew content lease", "err", err)
				continue
			}
			// 新钥落不上只记日志，不断开。
			if err := applyLeaseCmd(applyLease, Cmd{Typ: CmdLease, Lease: lr.Lease, NotAfter: lr.NotAfter}); err != nil {
				slog.Warn("apply content lease", "err", err)
			}
		// 页面要补拉时只标脏，后台按种类轻量补拉。
		case req := <-outbound:
			// 标上要补的种类，同类去抖期内只拉一次。
			dirty.request(req.Kind)
		}
	}
}

// 拉取或落地失败、进页补拉待补的种类；退避、去抖、封顶和连败放弃都记在这里。
type dirtySet struct {
	mu      sync.Mutex           // 主循环、后台协程和收包回调都会改，先占住再动
	factory uuid.UUID            // 本厂身份，日志里分清是哪家厂
	full    bool                 // 撤回失败、溢出或种类不明，要整轮重拉
	kinds   map[string]struct{}  // 失败后只需按种类补拉的那几类
	want    map[string]struct{}  // 进页要补的种类，立刻补、本身不算失败
	last    map[string]time.Time // 各种类上次接下进页补拉的时刻，供去抖
	fails   int                  // 这份标记已连续补拉失败几次
	wait    time.Duration        // 下一次补拉前要等多久
	timer   *time.Timer          // 退避节拍，后台协程一直盯着同一个
	wake    chan struct{}        // 进页补拉叫醒后台协程，留一格不堵
	armed   bool                 // 已排上退避补拉，期间新标记只并入
	running bool                 // 正在补拉，期间新标记算这一轮失败
}

// 建一份空脏集合，节拍先停着，标脏后才拨。
func newDirtySet(factory uuid.UUID) *dirtySet {
	// 建一个先停着的节拍，标脏时再拨到退避时长。
	t := time.NewTimer(time.Hour)
	// 立刻停掉，没标脏前不该醒。
	t.Stop()
	return &dirtySet{
		factory: factory, kinds: map[string]struct{}{}, want: map[string]struct{}{},
		last: map[string]time.Time{}, timer: t, wake: make(chan struct{}, 1),
	}
}

// 退避节拍的通道，后台协程据此醒来补拉。
func (d *dirtySet) due() <-chan time.Time {
	return d.timer.C
}

// 拉取或落地失败：内容本身坏的只记日志，其余按种类标脏等补拉。
func (d *dirtySet) failed(kind, what string, err error) {
	// 内容坏的重拉也一样，只记日志不再补。
	if contentRejected(err) {
		// 记下被拒的内容，人工看日志排查。
		slog.Warn(what+" rejected", "factory", d.factory, "kind", kind, "err", err)
		return
	}
	// 记下这次失败，后面会按退避补拉。
	slog.Warn(what+" failed, will resync", "factory", d.factory, "kind", kind, "err", err)
	// 按种类标脏，后台协程到点重拉。
	d.mark(kind)
}

// 按种类标脏，不认识的种类记成全量；没排上补拉就从头排。
func (d *dirtySet) mark(kind string) {
	// 占住集合，避免和取出补拉交错。
	d.mu.Lock()
	// 改完就放开。
	defer d.mu.Unlock()
	// 只认工艺和工程两类，别的一律整轮重拉。
	if kind == "process" || kind == "project" {
		// 记下这一类待补。
		d.kinds[kind] = struct{}{}
	} else { // 种类不明就整轮重拉，免得漏补。
		// 记成全量，整轮索引都重拉。
		d.full = true
	}
	// 没排上补拉就从头排上。
	d.armLocked()
}

// 进页补拉：按种类记下并叫醒后台；同类去抖期内重复的丢掉。
func (d *dirtySet) request(kind string) {
	// 占住集合，避免和取出补拉交错。
	d.mu.Lock()
	// 改完就放开。
	defer d.mu.Unlock()
	// 取当前时刻，和各种类上次接下的时刻比。
	now := time.Now()
	// 逐个要补的种类看去抖，期内的丢掉。
	for _, k := range pageKinds(kind) {
		// 去抖期内已接下过这一类就不再补。
		if at, ok := d.last[k]; ok && now.Sub(at) < pageDebounce {
			continue
		}
		// 记下这一类接下的时刻，供下次去抖。
		d.last[k] = now
		// 记下这一类待补，后台立刻按种类拉。
		d.want[k] = struct{}{}
	}
	// 没有待补的进页种类就不叫醒后台。
	if len(d.want) == 0 {
		return
	}
	// 已放弃的失败标记借这次机会从头再排。
	if !d.armed && !d.running {
		// 连败从零算，算新的一份机会。
		d.fails = 0
		// 退避回到起步时长。
		d.wait = retryBase
	}
	// 叫醒后台协程，进页的人不必等退避。
	d.signalLocked()
}

// 进页补拉要补的种类：工艺或工程只补那一类，空的两类都补。
func pageKinds(kind string) []string {
	// 指明了工艺或工程就只补这一类。
	if kind == "process" || kind == "project" {
		return []string{kind}
	}
	return []string{"process", "project"}
}

// 叫醒后台协程；已有一格在等就不再塞。
func (d *dirtySet) signalLocked() {
	// 有空位就塞一格，满了说明后台已会醒。
	select {
	// 塞一格，后台协程下一轮就补。
	case d.wake <- struct{}{}:
	// 已有一格在等，不必再塞。
	default:
	}
}

// 已占住锁时排补拉：空集合、已排或正在补都不动。
func (d *dirtySet) armLocked() {
	// 没有待补、已经排上或正在补就不再排。
	if (!d.full && len(d.kinds) == 0) || d.armed || d.running {
		return
	}
	// 新的一份标记，连败次数从零算。
	d.fails = 0
	// 第一次补拉按起步时长等。
	d.wait = retryBase
	// 记下已排上，期间新标记只并入。
	d.armed = true
	// 拨好节拍，到点后台协程来补。
	d.timer.Reset(d.wait)
}

// 取出要补的内容并清空，期间新标记另行并入。
func (d *dirtySet) take() (bool, []string) {
	// 占住集合，避免和标脏交错。
	d.mu.Lock()
	// 取完就放开。
	defer d.mu.Unlock()
	// 停掉退避节拍，待补的这一轮都取走了。
	d.timer.Stop()
	// 节拍已不再排着，这一轮不再算排上。
	d.armed = false
	// 标成正在补，期间的标脏算这一轮失败。
	d.running = true
	// 记下要不要整轮重拉。
	full := d.full
	// 按种类数预留，逐个补拉。
	kinds := make([]string, 0, len(d.kinds)+len(d.want))
	// 把失败待补的种类抄出来。
	for k := range d.kinds {
		// 收下这一类，后面逐个补拉。
		kinds = append(kinds, k)
	}
	// 进页要补的也并进来，已在失败名单里的不重复。
	for k := range d.want {
		// 失败名单里没有才收下，避免同类拉两次。
		if _, dup := d.kinds[k]; !dup {
			// 收下这一类，后面逐个补拉。
			kinds = append(kinds, k)
		}
	}
	// 清掉全量标记，补拉失败会再标上。
	d.full = false
	// 清掉种类标记，补拉失败会再标上。
	d.kinds = map[string]struct{}{}
	// 清掉进页标记，这一轮已经补它们。
	d.want = map[string]struct{}{}
	return full, kinds
}

// 一轮补拉结束：干净就清零；又脏了就翻倍退避，连败到顶就停。
func (d *dirtySet) finish() {
	// 占住集合，避免和标脏交错。
	d.mu.Lock()
	// 改完就放开。
	defer d.mu.Unlock()
	// 这一轮补完，之后的标脏可以重新排。
	d.running = false
	// 没有再标脏就算补齐，退避回到起步。
	if !d.full && len(d.kinds) == 0 {
		// 连败清零，下次失败从头算。
		d.fails = 0
		return
	}
	// 这一轮又标脏了，记一次连败。
	d.fails++
	// 连败到顶就停，等新下行、进页补拉或回连再试。
	if d.fails >= retryMaxFails {
		// 记下放弃补拉，标记留着等下次新机会。
		slog.Error("wan resync gave up", "factory", d.factory, "fails", d.fails, "full", d.full, "kinds", len(d.kinds))
		return
	}
	// 等待翻倍，弱网下不要打满平台。
	d.wait *= 2
	// 超过封顶就按封顶等。
	if d.wait > retryCap {
		// 收回到封顶时长。
		d.wait = retryCap
	}
	// 记下已排上，期间新标记只并入。
	d.armed = true
	// 拨好下一次节拍。
	d.timer.Reset(d.wait)
}

// 停掉补拉节拍，通道退出时不留计时器。
func (d *dirtySet) stop() {
	// 停掉节拍，退出后不再醒来。
	d.timer.Stop()
}

// 内容本身坏的错误，重拉也一样，不必补。
func contentRejected(err error) bool {
	// 逐个比对内容坏的几种业务错误。
	for _, bad := range []error{domain.ErrIntegrity, domain.ErrForbidden, domain.ErrTemplateInvalid, domain.ErrClosureIncomplete, domain.ErrClosureMismatch, domain.ErrAssetCodeConflict} {
		// 对上任何一种就算内容坏。
		if errors.Is(err, bad) {
			return true
		}
	}
	return false
}

// 后台补拉协程：退避到点或进页叫醒就补一轮，遇到注销交回主循环。
func runResync(ctx context.Context, pull *puller, dirty *dirtySet, handle func(context.Context, Cmd) error, reconcile ClientSyncHandler, jobs *softwareJobs, binds *bindLog, fatal chan<- error) {
	// 一直等下一次补拉，连接离开才退出。
	for {
		// 退避节拍、进页叫醒或离开，谁先到走谁。
		select {
		// 连接离开就退出，不再补拉。
		case <-ctx.Done():
			return
		// 失败退避到点，按脏标记补。
		case <-dirty.due():
		// 进页补拉叫醒，立刻补。
		case <-dirty.wake:
		}
		// 补一轮，只有注销会交回来。
		if err := resync(ctx, pull, dirty, handle, reconcile, jobs, binds); err != nil {
			// 交给主循环退出；已有一条在等就不再塞。
			select {
			// 把注销送给主循环。
			case fatal <- err:
			// 已有一条在等，这条丢掉也一样。
			default:
			}
			return
		}
	}
}

// 按脏标记补拉一轮：全量走整轮同步，否则逐类轻量补；失败的重新标脏。
func resync(ctx context.Context, pull *puller, dirty *dirtySet, handle func(context.Context, Cmd) error, reconcile ClientSyncHandler, jobs *softwareJobs, binds *bindLog) error {
	// 取出要补的内容，期间的新标脏另算。
	full, kinds := dirty.take()
	// 补完再结算，决定清零还是退避。
	defer dirty.finish()
	// 全量盖住各种类，只走一次整轮同步。
	if full {
		// 整轮重拉：租约、索引、软件和对账都走一遍。
		err := pullAndApply(ctx, pull, handle, reconcile, jobs, binds)
		// 结算这一轮，注销交回，别的失败重新标全量。
		return settle(ctx, dirty, "", err)
	}
	// 逐个种类轻量补拉，一类失败不挡别的。
	for _, kind := range kinds {
		// 只拉这一类索引，只落闭包、目录和模版。
		err := pullKind(ctx, pull, kind, handle)
		// 注销要交回，别的失败按种类重新标脏。
		if err := settle(ctx, dirty, kind, err); err != nil {
			return err
		}
	}
	return nil
}

// 结算一次补拉：注销交回；连接正在离开不算失败；其余失败重新标脏。
func settle(ctx context.Context, dirty *dirtySet, kind string, err error) error {
	// 补上了就什么也不做。
	if err == nil {
		return nil
	}
	// 注销要交回，外层停掉通道。
	if errors.Is(err, domain.ErrFactoryRetired) {
		return err
	}
	// 连接正在离开，补拉被打断不必再标。
	if ctx.Err() != nil {
		return nil
	}
	// 这一类没补上就重新标脏，按退避再试。
	dirty.failed(kind, "wan resync", err)
	return nil
}

// 轻量补拉一类：只拉带种类的索引，只落该类正文，不领租约、不问包、不对账。
func pullKind(ctx context.Context, pull *puller, kind string, handle func(context.Context, Cmd) error) error {
	// 预备装平台返回的这一类指令。
	var idx indexResp
	// 索引拿不到就交回，按退避再补。
	if err := pull.get(ctx, "/v1/channel/index?kind="+url.QueryEscape(kind), &idx); err != nil {
		return err
	}
	// 逐条挑出这一类的正文指令落地。
	for _, cmd := range idx.Cmds {
		// 只认这一类的闭包、目录和模版，绑定和治理状态不碰。
		if cmd.Kind != kind || (cmd.Typ != CmdClosure && cmd.Typ != CmdFSApply && cmd.Typ != CmdTemplate) {
			continue
		}
		// 落不上就交回；正文类失败已在里面标脏。
		if err := handle(ctx, cmd); err != nil {
			return err
		}
	}
	return nil
}

// 整轮同步：先领租约再按全量索引逐条落地；有效厂再问包、对账绑定。
func pullAndApply(ctx context.Context, pull *puller, handle func(context.Context, Cmd) error, reconcile ClientSyncHandler, jobs *softwareJobs, binds *bindLog) error {
	// 预备装当前解包钥，没有它不解正文。
	var lr leaseResp
	// 先拿租约，拿不到就整批不做。
	if err := pull.get(ctx, "/v1/channel/lease", &lr); err != nil {
		return err
	}
	// 租约落不上就整批停止，避免无钥解正文。
	if err := handle(ctx, Cmd{Typ: CmdLease, Lease: lr.Lease, NotAfter: lr.NotAfter}); err != nil {
		return err
	}
	// 从拉索引起记下落地的绑定，对账时并上，免得作废刚绑上的。
	binds.begin()
	// 整轮结束就停记，早退也不留着。
	defer binds.end()
	// 预备装平台返回的指令清单。
	var idx indexResp
	// 全量索引拿不到就整批停止。
	if err := pull.get(ctx, "/v1/channel/index", &idx); err != nil {
		return err
	}
	// 先当厂仍有效，遇到停用再改。
	active := true
	// 收集仍有效的绑定，供后面的对账。
	var keep []uuid.UUID
	// 逐条落地，一条失败就整批停。
	for _, cmd := range idx.Cmds {
		// 非启用就不对账，避免误作废绑定。
		if cmd.Typ == CmdFactoryState && cmd.Status != "" && cmd.Status != "active" {
			// 记下厂已停用或注销。
			active = false
		}
		// 这一条落不上就整批停止。
		if err := handle(ctx, cmd); err != nil {
			return err
		}
		// 绑定才进入对账名单。
		if cmd.Typ == CmdClientBind {
			// 设备身份不合法就略过，不让坏编号进对账。
			id, err := uuid.Parse(cmd.ClientID)
			// 编号合法才留在对账名单里。
			if err == nil {
				// 把仍有效的设备留在对账名单。
				keep = append(keep, id)
			}
		}
	}
	// 厂仍有效才补拉软件，停用厂不去问包。
	if active {
		// 回连补拉厂服务包和漏掉的 APK，放到后台不挡首轮。
		jobs.sync()
	}
	// 没人对账或厂已停用就到此为止。
	if reconcile == nil || !active {
		return nil
	}
	// 把仍有效的绑定交给本厂对账，漏掉的应作废。
	return reconcile(keep)
}

// 整轮同步期间落地的绑定；对账时并进名单，免得把刚绑上的设备作废。
type bindLog struct {
	mu     sync.Mutex             // 落库和整轮同步分属两路，先占住再动
	active int                    // 正在记的整轮同步有几轮
	seen   map[uuid.UUID]struct{} // 这期间落地的设备
}

// 开始记：没有别的整轮在记时从空开始。
func (b *bindLog) begin() {
	// 占住记录，避免和落库交错。
	b.mu.Lock()
	// 改完就放开。
	defer b.mu.Unlock()
	// 没有别的整轮在记就从空开始。
	if b.active == 0 {
		// 换一份空记录，旧的不再算数。
		b.seen = map[uuid.UUID]struct{}{}
	}
	// 多一轮在记，结束时才减回。
	b.active++
}

// 记一台刚落地的设备；没有整轮在记就不必留。
func (b *bindLog) record(raw string) {
	// 占住记录，避免和对账读取交错。
	b.mu.Lock()
	// 记完就放开。
	defer b.mu.Unlock()
	// 没有整轮在记，对账用不上就不留。
	if b.active == 0 {
		return
	}
	// 设备身份不合法就不记，对账也不认它。
	id, err := uuid.Parse(raw)
	// 坏编号丢掉，不进对账名单。
	if err != nil {
		return
	}
	// 记下这台设备，对账时并进名单。
	b.seen[id] = struct{}{}
}

// 把这期间落地的设备并进对账名单，已在名单里的不重复。
func (b *bindLog) merge(keep []uuid.UUID) []uuid.UUID {
	// 占住记录，避免读到一半又被改。
	b.mu.Lock()
	// 读完就放开。
	defer b.mu.Unlock()
	// 先记下名单里已有的设备，避免重复并入。
	inKeep := make(map[uuid.UUID]struct{}, len(keep))
	// 逐台记下已在名单里的设备。
	for _, id := range keep {
		// 记下这台已在名单里。
		inKeep[id] = struct{}{}
	}
	// 逐台看这期间落地的设备。
	for id := range b.seen {
		// 名单里已有就不再并。
		if _, ok := inKeep[id]; ok {
			continue
		}
		// 并进名单，对账不能把它作废。
		keep = append(keep, id)
	}
	return keep
}

// 停记：最后一轮整轮同步结束就丢掉记录。
func (b *bindLog) end() {
	// 占住记录，避免和落库交错。
	b.mu.Lock()
	// 改完就放开。
	defer b.mu.Unlock()
	// 少一轮在记。
	b.active--
	// 没有整轮在记了就丢掉记录。
	if b.active == 0 {
		// 清空记录，下一轮从头记。
		b.seen = nil
	}
}

// 后台软件任务：问最高版和拉包都在这里跑，主循环只负责投递。
type softwareJobs struct {
	ctx    context.Context     // 挂在通道内部，离开时取消
	pull   *puller             // 问最高版用的签名会话
	apply  SoftwareHandler     // 本厂接手拉包；空则不问包
	wg     sync.WaitGroup      // 记着在跑的任务，离开前等它们退出
	mu     sync.Mutex          // 护着在跑表和收工标记
	active map[string]struct{} // 正在处理的键：种类加版本，或问最高版
	closed bool                // 已在等收工，不再起新任务
}

// 建一份空的软件任务表，挂在通道内部的上下文上。
func newSoftwareJobs(ctx context.Context, pull *puller, apply SoftwareHandler) *softwareJobs {
	return &softwareJobs{ctx: ctx, pull: pull, apply: apply, active: map[string]struct{}{}}
}

// 起一路后台任务；同一个键正在跑或已收工就跳过。
func (j *softwareJobs) spawn(key string, fn func()) {
	// 占住在跑表，避免两路同时起同一个键。
	j.mu.Lock()
	// 改完就放开。
	defer j.mu.Unlock()
	// 已收工或同一个键还在跑就不再起。
	if _, busy := j.active[key]; busy || j.closed {
		return
	}
	// 占上这个键，后来的同键投递跳过。
	j.active[key] = struct{}{}
	// 记一路在跑，收工时要等它。
	j.wg.Add(1)
	// 放到后台跑，投递的一方立刻返回。
	go func() {
		// 跑完就销账，等收工的一方才能返回。
		defer j.wg.Done()
		// 跑完就让出这个键，下次同键可以再起。
		defer j.release(key)
		// 真正去问包或拉包。
		fn()
	}()
}

// 让出一个键，下次同键投递可以再起。
func (j *softwareJobs) release(key string) {
	// 占住在跑表，避免和起任务交错。
	j.mu.Lock()
	// 改完就放开。
	defer j.mu.Unlock()
	// 删掉这个键。
	delete(j.active, key)
}

// 投递一份软件通知：同种类同版本正在处理就跳过，新版本照常开拉。
func (j *softwareJobs) accept(kind string, version int64, versionName string) {
	// 没人接收或版本无效就不投。
	if j.apply == nil || version < 1 {
		return
	}
	// 按种类加版本起一路，正文由本厂另拉。
	j.spawn(kind+"/"+strconv.FormatInt(version, 10), func() {
		// 只交种类和版本，本厂已有就跳过重下。
		acceptSoftware(j.ctx, j.apply, kind, version, versionName)
	})
}

// 后台问一遍最高版，有就投递；上一遍还没问完就跳过。
func (j *softwareJobs) sync() {
	// 没人接收就不问包，避免空拉。
	if j.apply == nil {
		return
	}
	// 问最高版也放后台，弱网下不挡主循环。
	j.spawn("latest", func() {
		// 问两类最高版，有就逐个投递。
		syncSoftware(j.ctx, j.pull, j)
	})
}

// 收工：不再起新任务，等在跑的都退出。
func (j *softwareJobs) wait() {
	// 占住标记，之后的投递一律跳过。
	j.mu.Lock()
	// 标成收工，不再登记新任务。
	j.closed = true
	// 标完就放开，在跑的任务要来让键。
	j.mu.Unlock()
	// 等在跑的任务都退出。
	j.wg.Wait()
}

// 问当前最高厂服务包和客户端包，有就交给后台去拉；没有或失败只跳过。
func syncSoftware(ctx context.Context, pull *puller, jobs *softwareJobs) {
	// 厂包和客户端包都问最高版。
	for _, kind := range []string{"factory_service", "client_apk"} {
		// 只收种类和版本，字节不走这条。
		var meta struct {
			Kind    string `json:"kind"`    // factory_service / client_apk
			Version int64  `json:"version"` // 当前最高
		}
		// 问不到这一类就跳过，下一拍再问。
		if err := pull.get(ctx, "/v1/software/latest?kind="+url.QueryEscape(kind), &meta); err != nil {
			continue
		}
		// 问到了就投递，同版本在处理就跳过。
		jobs.accept(kind, meta.Version, "")
	}
}

// 只把种类和版本交给本厂去拉；正文不进 MQTT，同版本由本厂跳过重下。
func acceptSoftware(ctx context.Context, applySoftware SoftwareHandler, kind string, version int64, versionName string) {
	// 没人接收或版本无效就不通知。
	if applySoftware == nil || version < 1 {
		return
	}
	// 编出软件通知，编不出就放弃这一条。
	snap, err := json.Marshal(Cmd{Typ: CmdSoftware, Kind: kind, Version: version, VersionName: versionName})
	// 编不出就放弃这一条通知，不断开通道。
	if err != nil {
		return
	}
	// 本厂拒收只记日志；通道正在离开的不算拒收。
	if err := applySoftware(ctx, snap); err != nil && ctx.Err() == nil {
		// 记下拒收原因，下一轮再问。
		slog.Warn("accept software", "kind", kind, "err", err)
	}
}

// 闭包和模版在锁外拉正文；本厂已有同修订就跳过，拉不到交给 failed 标脏。
func fetchBody(ctx context.Context, pull *puller, cmd Cmd, have HaveHandler, failed func(kind, what string, err error), applyClosure ClosureHandler, applyTemplate ClosureHandler) (json.RawMessage, bool) {
	// 预备拉取路径和身份，按指令类型填。
	var path, id string
	// 只有闭包和模版要拉正文，别的直接去落库。
	switch cmd.Typ {
	// 闭包按资产身份拉。
	case CmdClosure:
		// 没人接收或缺少身份就跳过，不断开。
		if applyClosure == nil || cmd.AssetID == "" {
			return nil, false
		}
		// 记下闭包拉取路径和身份。
		path, id = "/v1/channel/pull/closure/", cmd.AssetID
	// 模版按模版身份拉。
	case CmdTemplate:
		// 没人接收或缺少模版身份就跳过。
		if applyTemplate == nil || cmd.TemplateID == "" {
			return nil, false
		}
		// 记下模版拉取路径和身份。
		path, id = "/v1/channel/pull/template/", cmd.TemplateID
	// 别的指令没有正文要拉，直接去落库。
	default:
		return nil, true
	}
	// 本厂已有同修订就不下载、不解密、不写审计。
	if alreadyHave(have, cmd.Typ, id, cmd.Revision) {
		return nil, false
	}
	// 预备装拉回来的正文。
	var snap json.RawMessage
	// 拉不到就按种类标脏，稍后补拉，不断开。
	if err := pull.get(ctx, path+id, &snap); err != nil {
		// 记下拉正文失败并标脏，连接继续留着。
		failed(cmd.Kind, "pull "+cmd.Typ, err)
		return nil, false
	}
	return snap, true
}

// 问本厂有没有这一修订；没人答、没带修订或问不出都当没有，照常去拉。
func alreadyHave(have HaveHandler, typ, rawID string, revision int64) bool {
	// 没人答或指令没带修订就照常去拉。
	if have == nil || revision < 1 {
		return false
	}
	// 身份不合法就照常去拉，由平台回错。
	id, err := uuid.Parse(rawID)
	// 坏编号不问本厂，照常去拉。
	if err != nil {
		return false
	}
	// 问本厂是否已收下这一修订。
	ok, err := have(typ, id, revision)
	// 问不出就当没有，宁可多拉一次。
	if err != nil {
		// 记下没问出来，照常去拉。
		slog.Warn("check local revision", "typ", typ, "id", id, "err", err)
		return false
	}
	return ok
}

// 按指令类型落库或回答升档问询；正文已在锁外拉好，正文类失败交给 failed 标脏，不断通道。
func applyCmd(cli mqtt.Client, up string, cmd Cmd, body json.RawMessage, failed func(kind, what string, err error), apply func(State) error, applyClient func(ClientIntent) error, applyClosure ClosureHandler, applyTemplate ClosureHandler, jobs *softwareJobs, applyRetract RetractHandler, applyFS ClosureHandler, applyLease LeaseHandler, onRequest RequestHandler) error {
	// 按指令类型落库或回答问询。
	switch cmd.Typ {
	// 有解包钥才交给本厂，空的直接忽略。
	case CmdLease:
		// 没有接收方或钥是空的就跳过。
		if applyLease == nil || len(cmd.Lease) == 0 {
			return nil
		}
		// 租约落不上只记日志，不断开。
		if err := applyLeaseCmd(applyLease, cmd); err != nil {
			// 记下租约失败，连接继续留着。
			slog.Warn("apply content lease", "err", err)
		}
		return nil
	// 治理状态交给本厂落地，没人接收就跳过。
	case CmdFactoryState:
		// 没人接收治理状态就跳过。
		if apply == nil {
			return nil
		}
		// 把启用、停用或注销交给本厂落地。
		return apply(State{Status: cmd.Status, Revision: cmd.Revision, ShortCode: cmd.ShortCode, Name: cmd.FactoryName})
	// 绑定或作废收成设备意图，解析失败只记日志。
	case CmdClientBind, CmdClientVoid:
		// 没人接收设备意图就跳过。
		if applyClient == nil {
			return nil
		}
		// 收成设备意图，身份不合法则放弃这一条。
		in, err := parseCmdClient(cmd)
		// 解析失败只记日志，不断开通道。
		if err != nil {
			// 记下坏的设备指令，继续处理后面的。
			slog.Warn("client cmd", "err", err)
			return nil
		}
		// 把绑定或作废交给本厂，失败则退出重拨。
		return applyClient(in)
	// 闭包正文已在锁外拉好，这里只落库。
	case CmdClosure:
		// 没人接收就跳过，不断开。
		if applyClosure == nil {
			return nil
		}
		// 本厂落不上就标脏，内容坏的只记日志。
		if err := applyClosure(body); err != nil {
			// 记下闭包没落上并标脏，连接继续留着。
			failed(cmd.Kind, "apply closure", err)
		}
		return nil
	// 模版正文已在锁外拉好，这里只落库。
	case CmdTemplate:
		// 没人接收就跳过，不断开。
		if applyTemplate == nil {
			return nil
		}
		// 本厂落不上就标脏，内容坏的只记日志。
		if err := applyTemplate(body); err != nil {
			// 记下模版没落上并标脏，连接继续留着。
			failed(cmd.Kind, "apply template", err)
		}
		return nil
	// 身份合法才撤回，坏编号直接忽略。
	case CmdRetract:
		// 没人接收或缺少资产身份就跳过。
		if applyRetract == nil || cmd.AssetID == "" {
			return nil
		}
		// 撤回身份不合法就跳过，不断开。
		id, err := uuid.Parse(cmd.AssetID)
		// 坏编号直接忽略，不断开通道。
		if err != nil {
			return nil
		}
		// 撤回只在整轮索引里，失败就标全量稍后补拉。
		if err := applyRetract(id); err != nil {
			// 记下撤回没落上并标全量，连接继续留着。
			failed("", "apply retract", err)
		}
		return nil
	// 目录搬家随指令正文落地，失败标脏补拉。
	case CmdFSApply:
		// 没人接收或没有目录正文就跳过。
		if applyFS == nil || len(cmd.Snapshot) == 0 {
			return nil
		}
		// 目录对齐失败就按种类标脏，不断开。
		if err := applyFS(cmd.Snapshot); err != nil {
			// 记下目录对齐失败并标脏，连接继续留着。
			failed(cmd.Kind, "apply platform fs", err)
		}
		return nil
	// 只接受厂包和客户端包，正文不走这条。
	case CmdSoftware:
		// 别的种类忽略，避免把无关包拉下来。
		if cmd.Kind != "client_apk" && cmd.Kind != "factory_service" {
			return nil
		}
		// 交给后台去拉，主循环立刻回去处理下行。
		jobs.accept(cmd.Kind, cmd.Version, cmd.VersionName)
		return nil
	// 升档问询走上行回执，不在这里拉正文。
	case CmdAssetList, CmdAssetSnapshot, CmdFSList:
		// 问询走上行回执，失败则退出重拨。
		return replyRequest(cli, up, cmd, onRequest)
	// 不认识的指令忽略，不断开通道。
	default:
		return nil
	}
}

// 把租约钥交给本厂进程。
func applyLeaseCmd(applyLease LeaseHandler, cmd Cmd) error {
	// 先按更细的时间格式读到期时刻。
	na, err := time.Parse(time.RFC3339Nano, cmd.NotAfter)
	// 这一格式不对就改试到秒的格式。
	if err != nil {
		// 再按到秒的格式读，两种都不认才放弃。
		na, err = time.Parse(time.RFC3339, cmd.NotAfter)
	}
	// 到期时间认不出就丢掉这条租约。
	if err != nil {
		return nil
	}
	// 把解包钥和到期时间交给本厂进程。
	return applyLease(Lease{Key: cmd.Lease, NotAfter: na})
}

// 把绑定/作废指令收成设备意图。
func parseCmdClient(cmd Cmd) (ClientIntent, error) {
	// 设备身份不合法就当没有这条绑定。
	cid, err := uuid.Parse(cmd.ClientID)
	// 坏编号拒绝，避免落错设备。
	if err != nil {
		return ClientIntent{}, domain.ErrNotFound
	}
	return ClientIntent{
		Typ: cmd.Typ, ClientID: cid, Name: cmd.ClientName, ShortCode: cmd.ClientShortCode,
		DeviceSerial: cmd.DeviceSerial, PublicKey: cmd.PublicKey, Revision: cmd.BindingRevision,
	}, nil
}

// 经上行 Topic 回答升档问询。
func replyRequest(cli mqtt.Client, up string, cmd Cmd, onRequest RequestHandler) error {
	// 先带上问询编号，回执才能配对。
	reply := Cmd{ReqID: cmd.ReqID}
	// 没人回答就回找不到，不断开通道。
	if onRequest == nil {
		// 标明这是失败回执，平台不要当清单用。
		reply.Typ = "error"
		reply.Error = domain.ErrNotFound.Error()
		// 把回执发上上行，发不出就当通道不可达。
		return publishUp(cli, up, reply)
	}
	// 向本厂要清单或快照，失败就回错误。
	assets, snap, err := onRequest(cmd.Typ, cmd.ReqID, cmd.Kind, cmd.AssetID)
	// 本厂答不上就回错误，不断开去重试。
	if err != nil {
		// 标明本厂没有答上，平台不要当成功。
		reply.Typ = "error"
		reply.Error = err.Error()
		// 把失败回执发上上行，发不出就当不可达。
		return publishUp(cli, up, reply)
	}
	// 快照问询把正文放进快照字段。
	if cmd.Typ == CmdAssetSnapshot {
		// 标明快照已经答回。
		reply.Typ = CmdAssetSnapOK
		// 放上快照正文，清单字段留空。
		reply.Snapshot = snap
	} else if cmd.Typ == CmdFSList { // 目录问询把结果放进清单字段。
		// 标明目录已经答回。
		reply.Typ = CmdFSListOK
		// 把目录结果放进回执的清单里。
		reply.Assets = assets
	} else { // 其余问询按升档清单答回。
		// 标明清单已经答回。
		reply.Typ = CmdAssetListOK
		// 把可升档清单放进回执里。
		reply.Assets = assets
	}
	// 把答复发上上行，发不出就当不可达。
	return publishUp(cli, up, reply)
}

// 向本厂 up Topic 发一条回执。
func publishUp(cli mqtt.Client, up string, cmd Cmd) error {
	// 编不出回执就交回，避免发出空包。
	raw, err := json.Marshal(cmd)
	// 编码失败就交回调用方。
	if err != nil {
		return err
	}
	// 发到上行主题，平台靠它认回执。
	tok := cli.Publish(up, 1, false, raw)
	// 十秒发不出就当平台不可达。
	if !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
		return domain.ErrWANUnreachable
	}
	return nil
}

// 显式 MQTT 地址优先，否则按 WAN HTTP 主机拼 52183。
func resolveMQTT(mqttURL, wanHTTP string) (string, error) {
	// 去掉空白，空的才改按网页主机拼。
	mqttURL = strings.TrimSpace(mqttURL)
	// 显式地址优先，不再改写端口。
	if mqttURL != "" {
		return mqttURL, nil
	}
	// 从平台地址取出主机，拼默认代理端口。
	u, err := url.Parse(strings.TrimSpace(wanHTTP))
	// 没有主机就当不可达，避免拨错机器。
	if err != nil || u.Hostname() == "" {
		return "", domain.ErrWANUnreachable
	}
	// 按平台主机拼默认代理端口。
	return "tcp://" + net.JoinHostPort(u.Hostname(), "52183"), nil
}

// 本进程正在跑的前端/服务版本，名录在线时展示。
func presenceCmd() Cmd {
	return Cmd{
		Typ: CmdPresence, WebVersion: release.WebCode, WebVersionName: release.WebName,
		ServiceVersion: release.Code, ServiceVersionName: release.Name,
	}
}

// 把 Broker 拒绝收成未授权或不可达。
func mapMQTTConnect(err error) error {
	// 没有底层原因也当不可达。
	if err == nil {
		return domain.ErrWANUnreachable
	}
	// 把拒绝原因转成小写再认，避免大小写对不上。
	msg := strings.ToLower(err.Error())
	// 代理拒绝就当未授权，外层会拉长退避。
	if strings.Contains(msg, "not authorized") || strings.Contains(msg, "bad user") {
		return domain.ErrUnauthorized
	}
	return domain.ErrWANUnreachable
}

// 会话丢失一律当通道不可达，外层用新 HMAC 再拨。
func mapMQTTDisconnect(err error) error {
	// 断线同样收成未授权或不可达，促使外层重拨。
	return mapMQTTConnect(err)
}
