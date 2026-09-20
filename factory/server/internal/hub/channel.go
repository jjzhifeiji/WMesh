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
	h.presenceMu.Lock()
	h.wanURL = wanURL
	h.mqttURL = mqttURL
	h.run = ctx
	h.presenceMu.Unlock()
	if wanURL == "" {
		return
	}
	ids, err := provision.ListFactoryIDs(h.admin)
	if err != nil {
		slog.Warn("list factories for wan channel", "err", err)
		return
	}
	for _, id := range ids {
		if svc, err := h.Service(ctx, id); err == nil {
			if k, err := svc.Store().SigningKey(ctx); err == nil {
				h.attachSoftwareSource(id, svc, wanURL, k.PrivateKey)
			}
		}
		h.ensureChannel(id)
	}
}

// wanSoftwareSrc 用厂钥向 WAN 拉最高厂包/APK。
type wanSoftwareSrc struct {
	wanURL    string
	factoryID uuid.UUID
	priv      []byte
}

// Latest 已认领厂会话看该种类当前最高版本。
func (s wanSoftwareSrc) Latest(ctx context.Context, kind string) (service.SoftwareMeta, error) {
	m, err := wanchannel.LatestSoftware(ctx, s.wanURL, s.factoryID, s.priv, kind)
	if err != nil {
		return service.SoftwareMeta{}, err
	}
	return service.SoftwareMeta{Kind: m.Kind, Version: m.Version, VersionName: m.VersionName, Digest: m.Digest}, nil
}

// Pull 已认领厂会话按版本拉包字节。
func (s wanSoftwareSrc) Pull(ctx context.Context, kind string, version int64) ([]byte, error) {
	return wanchannel.PullSoftwareBody(ctx, s.wanURL, s.factoryID, s.priv, kind, version)
}

// 把厂→WAN 拉包源挂到本厂服务。
func (h *Hub) attachSoftwareSource(factoryID uuid.UUID, svc *service.Service, wanURL string, priv []byte) {
	if strings.TrimSpace(wanURL) == "" || len(priv) == 0 {
		return
	}
	svc.Updates.SetSoftwareSource(wanSoftwareSrc{
		wanURL: wanURL, factoryID: factoryID, priv: append([]byte(nil), priv...),
	})
}

// 新进程起来后读 updater 状态，成功才记已装。
func (h *Hub) completeStaged(svc *service.Service) {
	r, ok := h.pendingSink.(interface {
		Report() (kind string, version int64, ok bool, present bool, err error)
	})
	if !ok {
		return
	}
	kind, version, applied, present, err := r.Report()
	if err != nil || !present || !applied {
		return
	}
	_ = svc.Updates.MarkInstalled(context.Background(), kind, version)
}

// 每厂只起一条出站循环，已有则跳过。
func (h *Hub) ensureChannel(factoryID uuid.UUID) {
	h.presenceMu.Lock()
	defer h.presenceMu.Unlock()
	if h.wanURL == "" || h.run == nil {
		return
	}
	if _, ok := h.presence[factoryID]; ok {
		return
	}
	ctx, cancel := context.WithCancel(h.run)
	h.presence[factoryID] = cancel
	go h.holdChannel(ctx, factoryID)
}

// RequestWANSync 经已钉死通道向 WAN 要当前快照；通道不在就丢掉，页面不阻塞。
func (h *Hub) RequestWANSync(factoryID uuid.UUID, typ, kind string) {
	h.presenceMu.Lock()
	ch := h.syncReq[factoryID]
	h.presenceMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- wanchannel.SyncRequest{Typ: typ, Kind: kind}:
	default:
	}
}

// 取消该厂出站循环，不再重连。
func (h *Hub) stopChannel(factoryID uuid.UUID) {
	h.presenceMu.Lock()
	defer h.presenceMu.Unlock()
	if cancel, ok := h.presence[factoryID]; ok {
		cancel()
		delete(h.presence, factoryID)
	}
}

// 断线退避重连；注销后停，避免空转。
func (h *Hub) holdChannel(ctx context.Context, factoryID uuid.UUID) {
	wait := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		err := h.dialHold(ctx, factoryID)
		if ctx.Err() != nil {
			return
		}
		// 注销后不再重连，避免对着已作废的名录空转。
		if errors.Is(err, domain.ErrFactoryRetired) {
			slog.Info("factory retired, stop wan channel", "factory", factoryID)
			h.stopChannel(factoryID)
			return
		}
		if err != nil {
			slog.Warn("wan channel dropped", "factory", factoryID, "err", err)
		}
		if time.Since(started) > 10*time.Second {
			wait = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if errors.Is(err, domain.ErrUnauthorized) {
			wait = 30 * time.Second
			continue
		}
		if wait < 30*time.Second {
			wait *= 2
		}
	}
}

// 用本厂签发钥连 WAN MQTT，并把下行落到本厂。
func (h *Hub) dialHold(ctx context.Context, factoryID uuid.UUID) error {
	svc, err := h.Service(ctx, factoryID)
	if err != nil {
		return err
	}
	var priv []byte
	// 取出本厂签发私钥做 CONNECT。
	k, err := svc.Store().SigningKey(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if err == nil {
		priv = k.PrivateKey
	}
	if len(priv) == 0 {
		slog.Info("no factory signing key, skip wan channel", "factory", factoryID)
		h.stopChannel(factoryID)
		return domain.ErrNotFound
	}
	h.presenceMu.Lock()
	wanURL := h.wanURL
	mqttURL := h.mqttURL
	h.presenceMu.Unlock()
	// 超管点同步走同一套厂会话 HTTPS，不依赖 MQTT 当时在线。
	h.attachSoftwareSource(factoryID, svc, wanURL, priv)
	out := make(chan wanchannel.SyncRequest, 1)
	h.presenceMu.Lock()
	h.syncReq[factoryID] = out
	h.presenceMu.Unlock()
	defer func() {
		h.presenceMu.Lock()
		if h.syncReq[factoryID] == out {
			delete(h.syncReq, factoryID)
		}
		h.presenceMu.Unlock()
	}()
	// 通道连上才允许解厂库正文。
	svc.Store().SetContentChannelOnline(true)
	defer svc.Store().SetContentChannelOnline(false)
	go func() {
		pctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := svc.Stats.PushWANSummaries(pctx); err != nil {
			slog.Warn("push weld summaries", "factory", factoryID, "err", err)
		}
	}()
	err = wanchannel.Hold(ctx, mqttURL, wanURL, factoryID, priv, func(st wanchannel.State) error {
		if st.ShortCode != "" {
			if err := svc.Store().PutFactoryShortCode(ctx, st.ShortCode); err != nil {
				return err
			}
		}
		// 把 WAN 推来的停用/启用/注销落到本厂库。
		out, err := svc.Auth.ApplyLifecycle(ctx, st.Status, st.Revision)
		if err != nil {
			return err
		}
		if out.Status == store.FactoryRetired {
			return domain.ErrFactoryRetired
		}
		return nil
	}, func(in wanchannel.ClientIntent) error {
		// WAN 分配或改分：本厂直接落库，不必人手抄身份和公钥。
		if in.Typ == "client_void" {
			err := svc.Node.VoidBinding(ctx, in.ClientID)
			if errors.Is(err, domain.ErrNotFound) {
				return nil
			}
			return err
		}
		_, err := svc.Node.AcceptBinding(ctx, in.ClientID, in.Name, in.PublicKey, in.Revision)
		if err != nil {
			return err
		}
		if in.ShortCode != "" {
			if err := svc.Store().PutClientShortCode(ctx, in.ClientID, in.ShortCode); err != nil {
				return err
			}
		}
		// WAN 登记的识别号随绑定落到本厂，供连臂时匹配。
		if strings.TrimSpace(in.DeviceSerial) != "" {
			_, err = svc.Store().PinDeviceSerial(ctx, in.ClientID, in.DeviceSerial)
			return err
		}
		return nil
	}, func(keep []uuid.UUID) error {
		return svc.Node.ReconcileBindings(ctx, keep)
	}, func(raw json.RawMessage) error {
		// 已发布平台级：写入只读副本，失败只记日志，不断通道。
		var snap service.ClosureSnapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			slog.Warn("platform closure json", "factory", factoryID, "err", err)
			return nil
		}
		if !closureHasBody(snap) {
			return nil
		}
		if err := svc.Closure.AcceptPlatformDelivery(ctx, snap); err != nil {
			slog.Warn("accept platform closure", "factory", factoryID, "err", err)
		}
		return nil
	}, func(raw json.RawMessage) error {
		// 当前内容模版：写入只读副本，不改已有正文；失败只记日志。
		var snap service.TemplateSnapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			slog.Warn("content template json", "factory", factoryID, "err", err)
			return nil
		}
		if err := svc.Closure.AcceptTemplateDelivery(ctx, snap); err != nil {
			slog.Warn("accept content template", "factory", factoryID, "err", err)
		}
		return nil
	}, func(raw json.RawMessage) error {
		// 软件包：本厂没有完整副本才去拉，失败只记日志，不断通道。
		var offer service.SoftwareOffer
		if err := json.Unmarshal(raw, &offer); err != nil {
			slog.Warn("software update json", "factory", factoryID, "err", err)
			return nil
		}
		if len(offer.Body) == 0 {
			if err := svc.Updates.EnsureSoftware(ctx, offer.Kind, offer.Version); err != nil {
				slog.Warn("ensure software", "factory", factoryID, "kind", offer.Kind, "err", err)
			}
			return nil
		}
		if err := svc.Updates.IngestSoftware(ctx, offer); err != nil {
			slog.Warn("accept software", "factory", factoryID, "err", err)
		}
		return nil
	}, func(assetID uuid.UUID) error {
		// 云端删除：列表撤回，失败只记日志，不断通道。
		if err := svc.Closure.RetractPlatformDelivery(ctx, assetID); err != nil {
			slog.Warn("retract platform replica", "factory", factoryID, "err", err)
		}
		return nil
	}, func(lease wanchannel.Lease) error {
		// 把 WAN 发来的解包钥放进内存，解开本厂 MK。
		return svc.ApplyContentLease(ctx, lease.Key, lease.NotAfter)
	}, func(typ, _, kind, assetID string) (json.RawMessage, json.RawMessage, error) {
		return h.answerAsset(ctx, svc, typ, kind, assetID)
	}, out)
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
	switch typ {
	case "asset_list":
		rows, err := svc.Assets.ListPromotable(ctx, kind)
		if err != nil {
			return nil, nil, err
		}
		b, err := json.Marshal(rows)
		return b, nil, err
	case "asset_snapshot":
		id, err := uuid.Parse(assetID)
		if err != nil {
			return nil, nil, domain.ErrNotFound
		}
		snap, err := svc.Assets.SnapshotForChannel(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		b, err := json.Marshal(snap)
		return nil, b, err
	default:
		return nil, nil, domain.ErrNotFound
	}
}

// 控制面无正文，避免空包覆盖已有副本。
func closureHasBody(snap service.ClosureSnapshot) bool {
	for _, m := range snap.Members {
		if len(m.Content) > 0 {
			return true
		}
	}
	return false
}

// PostWeldSummaries 用本厂签发钥把无人员组织的焊汇总交给 WAN。
func (h *Hub) PostWeldSummaries(ctx context.Context, factoryID uuid.UUID, rows []service.WeldWANSummary) error {
	h.presenceMu.Lock()
	wan := h.wanURL
	h.presenceMu.Unlock()
	if strings.TrimSpace(wan) == "" {
		return nil
	}
	h.mu.Lock()
	t := h.tenants[factoryID]
	h.mu.Unlock()
	if t == nil {
		return nil
	}
	k, err := t.svc.Store().SigningKey(ctx)
	if err != nil {
		return err
	}
	out := make([]wanchannel.WeldSummaryRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, wanchannel.WeldSummaryRow{
			Day: r.Day, ProjectName: r.ProjectName, WeldKind: r.WeldKind,
			RunCount: r.RunCount, LengthMM: r.LengthMM, DurationSec: r.DurationSec,
		})
	}
	return wanchannel.PostWeldSummaries(ctx, wan, factoryID, k.PrivateKey, out)
}
