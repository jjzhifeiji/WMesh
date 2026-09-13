package hub

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/provision"
	"wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
	"wmesh/factory/internal/wanchannel"
)

// StartChannel 厂出站连 WAN：已认领的厂保持心跳，断了重连。
func (h *Hub) StartChannel(ctx context.Context, wanURL string) {
	h.presenceMu.Lock()
	h.wanURL = wanURL
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
		h.ensureChannel(id)
	}
}

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

func (h *Hub) stopChannel(factoryID uuid.UUID) {
	h.presenceMu.Lock()
	defer h.presenceMu.Unlock()
	if cancel, ok := h.presence[factoryID]; ok {
		cancel()
		delete(h.presence, factoryID)
	}
}

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

func (h *Hub) dialHold(ctx context.Context, factoryID uuid.UUID) error {
	svc, err := h.Service(ctx, factoryID)
	if err != nil {
		return err
	}
	var priv []byte
	k, err := svc.Store().SigningKey(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if err == nil {
		priv = k.PrivateKey
	}
	h.presenceMu.Lock()
	wanURL := h.wanURL
	h.presenceMu.Unlock()
	svc.Store().SetContentChannelOnline(true)
	defer svc.Store().SetContentChannelOnline(false)
	err = wanchannel.Hold(ctx, wanURL, factoryID, priv, func(st wanchannel.State) error {
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
		return err
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
	})
	if errors.Is(err, domain.ErrFactoryRetired) {
		// hello 在 ready 之前就拒绝时，也要落到本厂库。
		if closeErr := svc.Auth.CloseFromWAN(ctx); closeErr != nil {
			return closeErr
		}
	}
	return err
}

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

func closureHasBody(snap service.ClosureSnapshot) bool {
	for _, m := range snap.Members {
		if len(m.Content) > 0 {
			return true
		}
	}
	return false
}
