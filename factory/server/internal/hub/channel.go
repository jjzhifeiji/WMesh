package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/provision"
	"wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
	"wmesh/factory/internal/wanchannel"
)

// StartChannel 厂出站连 WAN：已认领的厂保持 MQTT，断了重连。
func (h *Hub) StartChannel(ctx context.Context, wanURL, mqttURL string) {
	// 先占住通道表，再记下平台地址。
	h.presenceMu.Lock()
	// 记下平台根地址，空的就不出站。
	h.wanURL = wanURL
	// 记下代理地址，显式给出的优先使用。
	h.mqttURL = mqttURL
	// 记下进程生命周期，重连跟随它取消。
	h.run = ctx
	// 地址记完就放开，后面按厂去连接。
	h.presenceMu.Unlock()
	// 没有平台地址就不出站，厂可以离线开工。
	if wanURL == "" {
		return
	}
	// 列出本机已认领的厂，准备逐家连接。
	ids, err := provision.ListFactoryIDs(h.admin)
	// 列厂失败就先不出站，记下来再停这轮。
	if err != nil {
		// 记下没能列出厂，避免静默没有通道。
		slog.Warn("list factories for wan channel", "err", err)
		return
	}
	// 每家已认领厂都接上出站通道。
	for _, id := range ids {
		// 这家厂打得开，才尝试挂上拉包源。
		if svc, err := h.Service(ctx, id); err == nil {
			// 有签发钥才挂拉包源，没有就只连通道。
			if k, err := svc.Store().SigningKey(ctx); err == nil {
				// 用厂钥把拉包源挂上，同步不靠当时在线。
				h.attachSoftwareSource(id, svc, wanURL, k.PrivateKey)
			}
		}
		// 这家厂还没有出站循环就拉起一条。
		h.ensureChannel(id)
	}
}

// wanSoftwareSrc 用厂钥向 WAN 拉最高厂包/APK。
type wanSoftwareSrc struct {
	wanURL    string    // 平台网页根地址，拉包时拼在前面。
	factoryID uuid.UUID // 这家厂的稳定身份，签名时要写上。
	priv      []byte    // 这家厂签发私钥的副本，只用于签名。
}

// Latest 已认领厂会话看该种类当前最高版本。
func (s wanSoftwareSrc) Latest(ctx context.Context, kind string) (service.SoftwareMeta, error) {
	// 向平台问该种类的最高版，不含包字节。
	m, err := wanchannel.LatestSoftware(ctx, s.wanURL, s.factoryID, s.priv, kind)
	// 问不到最高版就交回，不要当成本地已有。
	if err != nil {
		return service.SoftwareMeta{}, err
	}
	return service.SoftwareMeta{Kind: m.Kind, Version: m.Version, VersionName: m.VersionName, Digest: m.Digest}, nil
}

// Pull 已认领厂会话按版本拉包字节；边下边落暂存，断了下次接着下。
func (s wanSoftwareSrc) Pull(ctx context.Context, kind string, version int64, want []byte) ([]byte, error) {
	// 按版本把包字节拉回来，摘要对不上则不要安装。
	return wanchannel.PullSoftwareBody(ctx, s.wanURL, s.factoryID, s.priv, kind, version, want)
}

// Received 本厂已收下这一版，删掉这家厂对应的拉包暂存。
func (s wanSoftwareSrc) Received(kind string, version int64, sum []byte) {
	// 按厂、种类、版本和摘要找到暂存删掉，没有也不出错。
	wanchannel.DropSoftwarePart(s.factoryID, kind, version, sum)
}

// 把厂→WAN 拉包源挂到本厂服务。
func (h *Hub) attachSoftwareSource(factoryID uuid.UUID, svc *service.Service, wanURL string, priv []byte) {
	// 没有地址或没有钥就不挂，避免空签名去拉。
	if strings.TrimSpace(wanURL) == "" || len(priv) == 0 {
		return
	}
	// 把拉包源交给更新服务，同版本由它跳过。
	svc.Updates.SetSoftwareSource(wanSoftwareSrc{
		wanURL: wanURL, factoryID: factoryID, priv: append([]byte(nil), priv...),
	})
}

// 新进程起来后按 updater 结果补记：成功记已装，失败或切回记落地失败。
func (h *Hub) completeStaged(svc *service.Service) {
	// 没结果或还在换都不记；补记没写上只警告，查进度时还会再补。
	if err := svc.Updates.SettleStaged(context.Background()); err != nil {
		// 记下哪家厂没补上，便于排查。
		slog.Warn("settle staged software", "factory", svc.Store().FactoryID(), "err", err)
	}
}

// 每厂只起一条出站循环，已有则跳过。
func (h *Hub) ensureChannel(factoryID uuid.UUID) {
	// 占住通道表，避免两路同时拉起同一家。
	h.presenceMu.Lock()
	// 离开时放开通道锁。
	defer h.presenceMu.Unlock()
	// 没地址或进程已停就不要拉起出站。
	if h.wanURL == "" || h.run == nil {
		return
	}
	// 已经有出站循环就跳过，不重复重连。
	if _, ok := h.presence[factoryID]; ok {
		return
	}
	// 跟随进程生命周期取消，注销后能够停下。
	ctx, cancel := context.WithCancel(h.run)
	// 已经有出站循环就跳过，不重复重连。
	h.presence[factoryID] = cancel
	// 后台断线重连，主流程不被这一家堵住。
	go h.holdChannel(ctx, factoryID)
}

// RequestWANSync 经已钉死通道向 WAN 要当前快照；通道不在就丢掉，页面不阻塞。
func (h *Hub) RequestWANSync(factoryID uuid.UUID, typ, kind string) {
	// 读补拉信箱前先占住通道表。
	h.presenceMu.Lock()
	// 取出这家厂的补拉信箱，不在线就没有。
	ch := h.syncReq[factoryID]
	// 读完就放开，投递不必一直占着锁。
	h.presenceMu.Unlock()
	// 通道不在就丢掉这次补拉，页面不被堵住。
	if ch == nil {
		return
	}
	// 信箱有空位才送，满了就丢掉这次补拉。
	select {
	// 把补拉放进信箱，通道在线才会去拉。
	case ch <- wanchannel.SyncRequest{Typ: typ, Kind: kind}:
	// 信箱满了就丢掉，页面不能被这次补拉堵住。
	default:
	}
}

// 取消该厂出站循环，不再重连。
func (h *Hub) stopChannel(factoryID uuid.UUID) {
	// 占住通道表，再取消这家厂的重连。
	h.presenceMu.Lock()
	// 取消完就放开通道锁。
	defer h.presenceMu.Unlock()
	// 有出站循环才取消，没有就不用动。
	if cancel, ok := h.presence[factoryID]; ok {
		// 取消重连，这家厂不再去拨平台。
		cancel()
		// 清掉登记，避免过一会儿又被拉起。
		delete(h.presence, factoryID)
	}
}

// 断线退避重连；注销后停，避免空转。续租另起 HTTPS 循环，不跟 MQTT 拨号绑在一起。
func (h *Hub) holdChannel(ctx context.Context, factoryID uuid.UUID) {
	// 续租独立跑，MQTT 拨不通也照续，随本条通道一起停。
	go h.keepLease(ctx, factoryID)
	// 第一次断线只等一秒再拨。
	wait := time.Second
	// 断线就退避再拨，注销或取消才停。
	for {
		// 已经取消就停止重连，不再空转。
		if ctx.Err() != nil {
			return
		}
		// 记下这次拨号时刻，连稳了就把退避收回。
		started := time.Now()
		// 用签发钥去连，失败就退避后再试。
		err := h.dialHold(ctx, factoryID)
		// 拨号途中已取消就停止，不再退避。
		if ctx.Err() != nil {
			return
		}
		// 注销后不再重连，避免对着已作废的名录空转。
		if errors.Is(err, domain.ErrFactoryRetired) {
			// 记下已注销，不再对着作废名录重连。
			slog.Info("factory retired, stop wan channel", "factory", factoryID)
			// 停掉这条出站，避免注销后还在空转。
			h.stopChannel(factoryID)
			return
		}
		// 这次拨号失败就记下来，然后按退避再试。
		if err != nil {
			// 记下通道掉了，外层会按退避再拨。
			slog.Warn("wan channel dropped", "factory", factoryID, "err", err)
		}
		// 连稳超过十秒就把退避收回，下次尽快重拨。
		if time.Since(started) > 10*time.Second {
			// 连稳过就回到一秒，避免越等越久。
			wait = time.Second
		}
		// 等到取消或退避结束，再决定要不要重拨。
		select {
		// 取消了就停止重连。
		case <-ctx.Done():
			return
		// 退避到点再拨，避免断线后立刻打满。
		case <-time.After(wait):
		}
		// 未授权就拉长退避，避免无效签名打满平台。
		if errors.Is(err, domain.ErrUnauthorized) {
			// 未授权改等半分钟再拨。
			wait = 30 * time.Second
			continue
		}
		// 退避封顶半分钟，避免断线后越等越久。
		if wait < 30*time.Second {
			// 这次失败就把等待加倍，但不超过半分钟。
			wait *= 2
		}
	}
}

// 经 HTTPS 独立续内容租约；厂内在线跟它走，WAN 名录在线仍看 MQTT presence。
func (h *Hub) keepLease(ctx context.Context, factoryID uuid.UUID) {
	// 取齐服务、平台地址和签发钥，取不齐就不续。
	svc, wanURL, priv, ok := h.leaseMaterial(ctx, factoryID)
	// 已取消或本厂还没有签发钥就不续，后者由拨号那路停通道。
	if !ok {
		return
	}
	// 续租循环离开就标离线，墙钟过期的租约不再给满窗。
	defer svc.Store().SetContentChannelOnline(false)
	// 按点续租，失败退避，注销或取消才返回。
	err := wanchannel.KeepLease(ctx, wanURL, factoryID, priv, func(l wanchannel.Lease) error { // 领到的钥放进内存，解开本厂正文。
		// 和 MQTT 那路首轮补拉同一口径：先标在线再落地。
		return h.applyHTTPSLease(ctx, factoryID, svc, l)
	})
	// 不是注销就到此为止，取消时通道已在停。
	if !errors.Is(err, domain.ErrFactoryRetired) {
		return
	}
	// 记下续租时平台说已注销，通道随之停掉。
	slog.Info("factory retired on lease, stop wan channel", "factory", factoryID)
	// 注销落到本厂库才停通道；落不上按退避重落，取消了就交给停通道的那一方。
	if !retryCloseFromWAN(ctx, factoryID, svc.Auth.CloseFromWAN, time.Second) {
		return
	}
	// 停掉这条出站，注销后不再拨也不再续。
	h.stopChannel(factoryID)
}

// applyHTTPSLease 经 HTTPS 领到的租约先标在线再落地；续租循环和 MQTT 那路首轮、整轮补拉共用。
func (h *Hub) applyHTTPSLease(ctx context.Context, factoryID uuid.UUID, svc *service.Service, l wanchannel.Lease) error {
	// 通道已在停就不再标在线，免得停后又被标回去。
	if err := ctx.Err(); err != nil {
		return err
	}
	// 经 HTTPS 领到租约即算在线，厂钟快了也收下这把。
	svc.Store().SetContentChannelOnline(true)
	// 把解包钥放进内存，解开本厂 MK。
	if err := svc.ApplyContentLease(ctx, l.Key, l.NotAfter); err != nil {
		return err
	}
	// 落上了才记到期，横幅断线时按它提示。
	h.noteWANLease(factoryID, l.NotAfter)
	return nil
}

// retryCloseFromWAN 把注销落到本厂库，落不上按退避重试；落上交回 true，取消交回 false。
// 注销由续租那路先看到时，通道要等注销落库后才停，免得停了通道却没人再落。
func retryCloseFromWAN(ctx context.Context, factoryID uuid.UUID, closeFn func(context.Context) error, base time.Duration) bool {
	// 第一次落不上先等起步时长再落。
	wait := base
	// 落上为止，取消才离开。
	for {
		// 把注销落到本厂库，登录立即关掉。
		err := closeFn(ctx)
		// 落上了就交回，调用方随后停通道。
		if err == nil {
			return true
		}
		// 已经取消就不再重落，停通道的那一方会收尾。
		if ctx.Err() != nil {
			return false
		}
		// 记下这次没落上和下次等待，库恢复后会再落。
		slog.Warn("close factory from wan, will retry", "factory", factoryID, "wait", wait, "err", err)
		// 等到取消或退避结束。
		select {
		// 取消了就不再重落。
		case <-ctx.Done():
			return false
		// 退避到点再落一次。
		case <-time.After(wait):
		}
		// 退避翻倍，封顶半分钟。
		wait = min(wait*2, 30*time.Second)
	}
}

// 取续租要用的服务、平台地址和签发钥；库暂时打不开就退避再取，没钥或已取消交回 false。
func (h *Hub) leaseMaterial(ctx context.Context, factoryID uuid.UUID) (*service.Service, string, []byte, bool) {
	// 第一次取不到只等一秒再取。
	wait := time.Second
	// 取到为止，取消或没钥才离开。
	for {
		// 打开这家厂，打不开就退避再试。
		svc, err := h.Service(ctx, factoryID)
		// 厂库开得了才去读签发钥。
		if err == nil {
			// 读出本厂签发私钥，续租要用它签名。
			var k store.SigningKey
			// 读钥，库错按退避再取。
			k, err = svc.Store().SigningKey(ctx)
			// 读到钥就取平台地址交回。
			if err == nil && len(k.PrivateKey) > 0 {
				// 读平台地址前先占住通道表。
				h.presenceMu.Lock()
				// 取出平台地址，续租按它拼请求。
				wanURL := h.wanURL
				// 地址取完就放开通道锁。
				h.presenceMu.Unlock()
				return svc, wanURL, k.PrivateKey, true
			}
			// 还没有签发钥就不续，拨号那路会停掉通道。
			if err == nil || errors.Is(err, domain.ErrNotFound) {
				return nil, "", nil, false
			}
		}
		// 记下这次没取齐，退避后再取。
		slog.Warn("prepare content lease", "factory", factoryID, "err", err)
		// 等到取消或退避结束。
		select {
		// 取消了就不再取。
		case <-ctx.Done():
			return nil, "", nil, false
		// 退避到点再取。
		case <-time.After(wait):
		}
		// 退避翻倍，封顶半分钟。
		wait = min(wait*2, 30*time.Second)
	}
}

// 用本厂签发钥连 WAN MQTT，并把下行落到本厂。
func (h *Hub) dialHold(ctx context.Context, factoryID uuid.UUID) error {
	// 打开这家厂，打不开就等下一轮再试。
	svc, err := h.Service(ctx, factoryID)
	// 厂库打不开就交回，外层退避后再试。
	if err != nil {
		return err
	}
	// 先留空私钥，读到签发钥再填上。
	var priv []byte
	// 取出本厂签发私钥做 CONNECT。
	k, err := svc.Store().SigningKey(ctx)
	// 不是没有钥的失败就交回，不要当成该跳过。
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	// 读到钥才采用，没有就停掉这条通道。
	if err == nil {
		// 用本厂签发私钥做连接签名。
		priv = k.PrivateKey
	}
	// 还没有签发钥就停这条通道，避免匿名去拨。
	if len(priv) == 0 {
		// 记下没有签发钥，这次先不连平台。
		slog.Info("no factory signing key, skip wan channel", "factory", factoryID)
		// 停掉这条出站，有钥之后再重新拉起。
		h.stopChannel(factoryID)
		return domain.ErrNotFound
	}
	// 读平台地址前先占住通道表。
	h.presenceMu.Lock()
	// 取出平台地址，后面签名拉取要用。
	wanURL := h.wanURL
	// 取出代理地址，空的就按平台主机去拼。
	mqttURL := h.mqttURL
	// 地址取完就放开，拉包源不必占着锁。
	h.presenceMu.Unlock()
	// 超管点同步走同一套厂会话 HTTPS，不依赖 MQTT 当时在线。
	h.attachSoftwareSource(factoryID, svc, wanURL, priv)
	// 准备补拉信箱，页面点同步时投到这里。
	out := make(chan wanchannel.SyncRequest, 1)
	// 读到补拉信箱前再占住，避免和页面交错。
	h.presenceMu.Lock()
	// 挂上信箱，通道在线时页面补拉才进得来。
	h.syncReq[factoryID] = out
	// 信箱挂上就放开通道锁。
	h.presenceMu.Unlock()
	// 通道结束就摘掉补拉信箱，避免往已死的通道塞。
	defer func() {
		// 摘信箱前先占住，避免和页面投递交错。
		h.presenceMu.Lock()
		// 仍是这一轮的信箱才摘，避免摘掉新的一轮。
		if h.syncReq[factoryID] == out {
			// 摘掉信箱，页面再点同步会被丢掉。
			delete(h.syncReq, factoryID)
		}
		// 摘完就放开通道锁。
		h.presenceMu.Unlock()
	}()
	// 读出本厂已钉的 WAN 指令验签公钥，没有就等首轮 HTTPS 索引来钉。
	wanKey, err := svc.Node.WANKey(ctx)
	// 读库出错就交回，外层退避后再试，不无钉钥地连。
	if err != nil {
		return err
	}
	// 首次钉钥交给本厂服务落库并留审计。
	trust := wanchannel.Trust{Key: wanKey, Pin: func(pub []byte) error {
		// 钉住或核对 WAN 公钥，换钥会被拒绝。
		return svc.Node.PinWANKey(ctx, pub)
	}}
	// 连通道时请后台补送焊汇总，和汇聚共用一条串行循环。
	svc.Stats.RequestWANPush()
	// 把平台推来的治理状态落到本厂库。
	err = wanchannel.Hold(ctx, mqttURL, wanURL, factoryID, priv, trust, func(st wanchannel.State) error {
		// 平台给了短码才更新，空的不去清掉已有短码。
		if st.ShortCode != "" {
			// 短码写不进厂库就把这次落地当失败。
			if err := svc.Store().PutFactoryShortCode(ctx, st.ShortCode); err != nil {
				return err
			}
		}
		// 厂名写不进厂库就把这次落地当失败。
		if err := svc.Store().PutFactoryName(ctx, st.Name); err != nil {
			return err
		}
		// 把 WAN 推来的停用/启用/注销落到本厂库。
		out, err := svc.Auth.ApplyLifecycle(ctx, st.Status, st.Revision)
		// 治理状态落不上就退回，避免通道和库不一致。
		if err != nil {
			return err
		}
		// 已经注销就通知外层停止这条通道。
		if out.Status == store.FactoryRetired {
			return domain.ErrFactoryRetired
		}
		return nil
	}, func(in wanchannel.ClientIntent) error { // 绑定或作废落到本厂，缺身份则拒绝这次。
		// WAN 分配或改分：本厂直接落库，不必人手抄身份和公钥。
		if in.Typ == "client_void" {
			// 带上作废指令的修订，迟到的旧作废不能把已落下的新绑定作废。
			err := svc.Node.ApplyWANVoid(ctx, in.ClientID, in.Revision)
			// 本来就没有这份绑定，当作废已经完成。
			if errors.Is(err, domain.ErrNotFound) {
				return nil
			}
			return err
		}
		// 落绑定、短码和臂号都交给本厂服务；单台冲突它只记日志跳过，库错才退回重连。
		return svc.Node.ApplyWANBinding(ctx, service.WANBinding{
			ClientID: in.ClientID, Name: in.Name, PublicKey: in.PublicKey, Revision: in.Revision,
			ShortCode: in.ShortCode, DeviceSerial: in.DeviceSerial,
		})
	}, func(keep []uuid.UUID) error { // 按索引对账仍有效的绑定，漏掉的应作废。
		// 漏掉的绑定应作废，失败则退出重连。
		return svc.Node.ReconcileBindings(ctx, keep)
	}, func(raw json.RawMessage) error { // 闭包正文落到只读副本，收不下交回通道补拉。
		// 已发布平台级：写入只读副本；收不下交回通道，它分辨内容坏还是该补拉。
		var snap service.ClosureSnapshot
		// 闭包正文解不开只记日志，重拉也一样解不开。
		if err := json.Unmarshal(raw, &snap); err != nil {
			// 记下坏的闭包正文，连接继续留着。
			slog.Warn("platform closure json", "factory", factoryID, "err", err)
			return nil
		}
		// 没有正文就不覆盖已经收下的副本。
		if !closureHasBody(snap) {
			return nil
		}
		// 收不下就交回：内容坏的通道只记日志，库错或租约未到按退避补拉。
		return svc.Closure.AcceptPlatformDelivery(ctx, snap)
	}, func(raw json.RawMessage) error { // 模版落到只读副本，收不下交回通道补拉。
		// 当前内容模版：写入只读副本，不改已有正文；收不下交回通道。
		var snap service.TemplateSnapshot
		// 模版解不开只记日志，重拉也一样解不开。
		if err := json.Unmarshal(raw, &snap); err != nil {
			// 记下坏的模版正文，连接继续留着。
			slog.Warn("content template json", "factory", factoryID, "err", err)
			return nil
		}
		// 收不下就交回：内容坏的通道只记日志，库错等按退避补拉。
		return svc.Closure.AcceptTemplateDelivery(ctx, snap)
	}, func(sctx context.Context, raw json.RawMessage) error { // 软件通知在后台交给本厂，没字节再按版本去拉。
		// 软件包：本厂没有完整副本才去拉，失败只记日志，不断通道。
		var offer service.SoftwareOffer
		// 软件通知解不开只记日志，不去拉包。
		if err := json.Unmarshal(raw, &offer); err != nil {
			// 记下坏的软件通知，连接继续留着。
			slog.Warn("software update json", "factory", factoryID, "err", err)
			return nil
		}
		// 通知没带字节就按版本去拉，失败只记日志。
		if len(offer.Body) == 0 {
			// 本厂没有这份才去拉；通道离开只是不再等，下载照常在后台跑完。
			err := svc.Updates.EnsureSoftware(sctx, offer.Kind, offer.Version)
			// 比已装还旧是常态，通道离开也不算失败，都不打警告。
			if err != nil && !errors.Is(err, domain.ErrStaleRevision) && sctx.Err() == nil {
				// 记下没拉到软件包，连接继续留着。
				slog.Warn("ensure software", "factory", factoryID, "kind", offer.Kind, "err", err)
			}
			return nil
		}
		// 通知里带了字节就收下，失败只记日志。
		if err := svc.Updates.IngestSoftware(sctx, offer); err != nil {
			// 记下没能收下软件包，连接继续留着。
			slog.Warn("accept software", "factory", factoryID, "err", err)
		}
		return nil
	}, func(assetID uuid.UUID) error { // 平台删除落到本厂列表，失败交回通道补拉。
		// 云端删除：列表撤回；撤不下交回通道，它标全量按退避补拉。
		return svc.Closure.RetractPlatformDelivery(ctx, assetID)
	}, func(raw json.RawMessage) error { // 按平台目录对齐本厂副本，失败交回通道补拉。
		// 云端目录搬家：按 WAN 文件夹身份对齐本厂已到达的副本。
		var layout service.PlatformFSLayout
		// 目录解不开只记日志，重拉也一样解不开。
		if err := json.Unmarshal(raw, &layout); err != nil {
			// 记下坏的目录正文，连接继续留着。
			slog.Warn("platform fs json", "factory", factoryID, "err", err)
			return nil
		}
		// 对不齐就交回：内容坏的通道只记日志，库错等按种类补拉。
		return svc.Assets.ApplyPlatformFSLayout(ctx, layout)
	}, func(lease wanchannel.Lease) error { // 首轮和整轮补拉经 HTTPS 领到的钥，先标在线再放进内存。
		// 和续租循环同一口径：续租还没成过一次，厂钟偏快也收下这把。
		return h.applyHTTPSLease(ctx, factoryID, svc, lease)
	}, func(typ, _, kind, assetID string) (json.RawMessage, json.RawMessage, error) { // 升档问询交给本厂回答，失败不断开去重试。
		// 按问询类型回清单或快照，失败则这问失败。
		return h.answerAsset(ctx, svc, typ, kind, assetID)
	}, func(typ string, id uuid.UUID, revision int64) (bool, error) { // 问本厂是否已收下这一修订，已有就不再拉。
		// 闭包和模版各按自己的副本判断，别的一律当没有。
		switch typ {
		// 闭包看根修订、撤回和依赖修订。
		case wanchannel.CmdClosure:
			// 根在、未撤回、依赖都在才算已有。
			return svc.Closure.HasPlatformDelivery(ctx, id, revision)
		// 模版看同修订是否已收齐。
		case wanchannel.CmdTemplate:
			// 同修订在且收齐才算已有。
			return svc.Closure.HasTemplateDelivery(ctx, id, revision)
		// 别的指令不跳过，照常去拉。
		default:
			return false, nil
		}
	}, out, wanchannel.WithStatus(func(connected bool) { // 连上、断开都记下，管理页横幅要看。
		// 记下这一厂通道当前是否连着。
		h.noteWANConnected(factoryID, connected)
	}), wanchannel.WithClientLogins(clientLoginsFor(ctx, factoryID, svc)))
	// 注销若发生在租约之前，也要落到本厂库。
	if errors.Is(err, domain.ErrFactoryRetired) {
		// 注销指令在租约之前到达时，也要落到本厂库。
		if closeErr := svc.Auth.CloseFromWAN(ctx); closeErr != nil {
			return closeErr
		}
	}
	return err
}

// 回答 WAN 的升档列表或快照；失败不拆连接。
func (h *Hub) answerAsset(ctx context.Context, svc *service.Service, typ, kind, assetID string) (json.RawMessage, json.RawMessage, error) {
	// 按问询类型回清单或快照，不认识就当没有。
	switch typ {
	// 把可升档清单编回去，失败则这问失败。
	case "asset_list":
		// 读取可升档清单，读失败就让这问失败。
		rows, err := svc.Assets.ListPromotable(ctx, kind)
		// 清单读失败就交回，平台不要当空列表。
		if err != nil {
			return nil, nil, err
		}
		// 把清单编成回执，编不出就让这问失败。
		b, err := json.Marshal(rows)
		return b, nil, err
	// 把平台目录编回去，失败则这问失败。
	case "fs_list":
		// 读取平台目录，读失败就让这问失败。
		rows, err := svc.Assets.ListFSForChannel(ctx, kind)
		// 目录读失败就交回，平台不要当空目录。
		if err != nil {
			return nil, nil, err
		}
		// 把目录编成回执，编不出就让这问失败。
		b, err := json.Marshal(rows)
		return b, nil, err
	// 编号合法才给快照，否则当没有这份资产。
	case "asset_snapshot":
		// 资产编号不合法就当没有这份。
		id, err := uuid.Parse(assetID)
		// 坏编号拒绝，避免把别的资产编出去。
		if err != nil {
			return nil, nil, domain.ErrNotFound
		}
		// 按编号取快照，没有就让这问失败。
		snap, err := svc.Assets.SnapshotForChannel(ctx, id)
		// 快照取不到就交回，不要回空正文。
		if err != nil {
			return nil, nil, err
		}
		// 把快照编成回执，编不出就让这问失败。
		b, err := json.Marshal(snap)
		return nil, b, err
	// 不认识的问询当没有，避免回一份空包。
	default:
		return nil, nil, domain.ErrNotFound
	}
}

// 控制面无正文，避免空包覆盖已有副本。
func closureHasBody(snap service.ClosureSnapshot) bool {
	// 逐个成员看有没有正文。
	for _, m := range snap.Members {
		// 有成员带正文才算这份闭包可以落地。
		if len(m.Content) > 0 {
			return true
		}
	}
	return false
}

// PostWeldSummaries 用本厂签发钥把无人员组织的焊汇总交给 WAN。
func (h *Hub) PostWeldSummaries(ctx context.Context, factoryID uuid.UUID, rows []service.WeldWANSummary) error {
	// 读平台地址前先占住通道表。
	h.presenceMu.Lock()
	// 取出平台地址，空的就不上送汇总。
	wan := h.wanURL
	// 地址取完就放开通道锁。
	h.presenceMu.Unlock()
	// 没有平台地址就不上送，本厂汇总仍留着。
	if strings.TrimSpace(wan) == "" {
		return nil
	}
	// 读这家厂之前先占住厂库表。
	h.mu.Lock()
	// 取出已打开的厂，还没打开就不上送。
	t := h.tenants[factoryID]
	// 取完就放开厂库锁。
	h.mu.Unlock()
	// 这家厂还没打开就不上送，等下次再试。
	if t == nil {
		return nil
	}
	// 读取签发钥，没有钥就不能签名上送。
	k, err := t.svc.Store().SigningKey(ctx)
	// 还没有签发钥就交回，重试也签不了，等下次信号。
	if errors.Is(err, domain.ErrNotFound) {
		return err
	}
	// 读钥时库出错算暂时送不出，后台上送会退避重试。
	if err != nil {
		// 套上不可达再交回，原错误仍留在链里便于排查。
		return fmt.Errorf("%w: signing key: %w", domain.ErrWANUnreachable, err)
	}
	// 按条数预留上送行，人员组织不在里面。
	out := make([]wanchannel.WeldSummaryRow, 0, len(rows))
	// 逐条收成平台要的汇总，不含人员组织。
	for _, r := range rows {
		// 收成一条无人员的汇总，准备上送。
		out = append(out, wanchannel.WeldSummaryRow{
			Day: r.Day, ProjectName: r.ProjectName, WeldKind: r.WeldKind,
			RunCount: r.RunCount, LengthMM: r.LengthMM, DurationSec: r.DurationSec,
		})
	}
	// 签完再送出，平台拒绝则这次上送失败。
	return wanchannel.PostWeldSummaries(ctx, wan, factoryID, k.PrivateKey, out)
}
