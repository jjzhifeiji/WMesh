package hub

import (
	"context"
	"encoding/json"
	"errors"
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

// Pull 已认领厂会话按版本拉包字节。
func (s wanSoftwareSrc) Pull(ctx context.Context, kind string, version int64) ([]byte, error) {
	// 按版本把包字节拉回来，失败则不要安装。
	return wanchannel.PullSoftwareBody(ctx, s.wanURL, s.factoryID, s.priv, kind, version)
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

// 新进程起来后读 updater 状态，成功才记已装。
func (h *Hub) completeStaged(svc *service.Service) {
	// 看落盘位置能不能回报换装结果。
	r, ok := h.pendingSink.(interface {
		// 读更新器是否已经换完，成功才记已装。
		Report() (kind string, version int64, ok bool, present bool, err error)
	})
	// 不能回报换装结果就跳过，不当成已装完。
	if !ok {
		return
	}
	// 读更新器是否已经换完，成功才记已装。
	kind, version, applied, present, err := r.Report()
	// 没换成功就不记已装，避免库和磁盘不一致。
	if err != nil || !present || !applied {
		return
	}
	// 换装成功才把库里的包记成已装。
	_ = svc.Updates.MarkInstalled(context.Background(), kind, version)
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

// 断线退避重连；注销后停，避免空转。
func (h *Hub) holdChannel(ctx context.Context, factoryID uuid.UUID) {
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
	// 通道连上才允许解厂库正文。
	svc.Store().SetContentChannelOnline(true)
	// 离开时标成离线，没通道就不再解正文。
	defer svc.Store().SetContentChannelOnline(false)
	// 连上后补送焊汇总，失败只记日志。
	go func() {
		// 补送焊汇总限时，避免堵住通道建立。
		pctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		// 补送结束就释放这次超时。
		defer cancel()
		// 补送焊汇总失败只记日志，不断开通道。
		if err := svc.Stats.PushWANSummaries(pctx); err != nil {
			// 记下汇总没送上，下一轮连接再试。
			slog.Warn("push weld summaries", "factory", factoryID, "err", err)
		}
	}()
	// 把平台推来的治理状态落到本厂库。
	err = wanchannel.Hold(ctx, mqttURL, wanURL, factoryID, priv, func(st wanchannel.State) error {
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
			// 作废只解除绑定，没有这份就当已经完成。
			err := svc.Node.VoidBinding(ctx, in.ClientID)
			// 本来就没有这份绑定，当作废已经完成。
			if errors.Is(err, domain.ErrNotFound) {
				return nil
			}
			return err
		}
		// 把分配或改分落库，失败则平台会再推。
		_, err := svc.Node.AcceptBinding(ctx, in.ClientID, in.Name, in.PublicKey, in.Revision)
		// 绑定落不上就退回，平台会再推一次。
		if err != nil {
			return err
		}
		// 带了短码才写入，空的不去清掉已有短码。
		if in.ShortCode != "" {
			// 短码写不进去就退回，避免和平台对不上。
			if err := svc.Store().PutClientShortCode(ctx, in.ClientID, in.ShortCode); err != nil {
				return err
			}
		}
		// WAN 登记的识别号随绑定落到本厂，供连臂时匹配。
		if strings.TrimSpace(in.DeviceSerial) != "" {
			// 把识别号钉上，连上机械臂时才能对上这台。
			_, err = svc.Store().PinDeviceSerial(ctx, in.ClientID, in.DeviceSerial)
			return err
		}
		return nil
	}, func(keep []uuid.UUID) error { // 按索引对账仍有效的绑定，漏掉的应作废。
		// 漏掉的绑定应作废，失败则退出重连。
		return svc.Node.ReconcileBindings(ctx, keep)
	}, func(raw json.RawMessage) error { // 闭包正文落到只读副本，失败不断开通道。
		// 已发布平台级：写入只读副本，失败只记日志，不断通道。
		var snap service.ClosureSnapshot
		// 闭包正文解不开只记日志，不去覆盖已有副本。
		if err := json.Unmarshal(raw, &snap); err != nil {
			// 记下坏的闭包正文，连接继续留着。
			slog.Warn("platform closure json", "factory", factoryID, "err", err)
			return nil
		}
		// 没有正文就不覆盖已经收下的副本。
		if !closureHasBody(snap) {
			return nil
		}
		// 写入只读副本失败只记日志，不断开通道。
		if err := svc.Closure.AcceptPlatformDelivery(ctx, snap); err != nil {
			// 记下没能收下闭包，连接继续留着。
			slog.Warn("accept platform closure", "factory", factoryID, "err", err)
		}
		return nil
	}, func(raw json.RawMessage) error { // 模版落到只读副本，不改已有正文。
		// 当前内容模版：写入只读副本，不改已有正文；失败只记日志。
		var snap service.TemplateSnapshot
		// 模版解不开只记日志，不改已有正文。
		if err := json.Unmarshal(raw, &snap); err != nil {
			// 记下坏的模版正文，连接继续留着。
			slog.Warn("content template json", "factory", factoryID, "err", err)
			return nil
		}
		// 写入模版副本失败只记日志，不断开。
		if err := svc.Closure.AcceptTemplateDelivery(ctx, snap); err != nil {
			// 记下没能收下模版，连接继续留着。
			slog.Warn("accept content template", "factory", factoryID, "err", err)
		}
		return nil
	}, func(raw json.RawMessage) error { // 软件通知交给本厂，没字节再按版本去拉。
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
			// 本厂没有这份才去拉，失败只记日志。
			if err := svc.Updates.EnsureSoftware(ctx, offer.Kind, offer.Version); err != nil {
				// 记下没拉到软件包，连接继续留着。
				slog.Warn("ensure software", "factory", factoryID, "kind", offer.Kind, "err", err)
			}
			return nil
		}
		// 通知里带了字节就收下，失败只记日志。
		if err := svc.Updates.IngestSoftware(ctx, offer); err != nil {
			// 记下没能收下软件包，连接继续留着。
			slog.Warn("accept software", "factory", factoryID, "err", err)
		}
		return nil
	}, func(assetID uuid.UUID) error { // 平台删除落到本厂列表，失败只记日志。
		// 云端删除：列表撤回，失败只记日志，不断通道。
		if err := svc.Closure.RetractPlatformDelivery(ctx, assetID); err != nil {
			// 记下没能撤回副本，连接继续留着。
			slog.Warn("retract platform replica", "factory", factoryID, "err", err)
		}
		return nil
	}, func(raw json.RawMessage) error { // 按平台目录对齐本厂副本，失败只记日志。
		// 云端目录搬家：按 WAN 文件夹身份对齐本厂已到达的副本。
		var layout service.PlatformFSLayout
		// 目录解不开只记日志，不去改本厂文件夹。
		if err := json.Unmarshal(raw, &layout); err != nil {
			// 记下坏的目录正文，连接继续留着。
			slog.Warn("platform fs json", "factory", factoryID, "err", err)
			return nil
		}
		// 目录对齐失败只记日志，不断开通道。
		if err := svc.Assets.ApplyPlatformFSLayout(ctx, layout); err != nil {
			// 记下没能对齐目录，连接继续留着。
			slog.Warn("apply platform fs", "factory", factoryID, "err", err)
		}
		return nil
	}, func(lease wanchannel.Lease) error { // 把解包钥放进内存，用来解开本厂正文。
		// 把 WAN 发来的解包钥放进内存，解开本厂 MK。
		return svc.ApplyContentLease(ctx, lease.Key, lease.NotAfter)
	}, func(typ, _, kind, assetID string) (json.RawMessage, json.RawMessage, error) { // 升档问询交给本厂回答，失败不断开去重试。
		// 按问询类型回清单或快照，失败则这问失败。
		return h.answerAsset(ctx, svc, typ, kind, assetID)
	}, out)
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
	// 读不到签发钥就交回，这次汇总不送。
	if err != nil {
		return err
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
