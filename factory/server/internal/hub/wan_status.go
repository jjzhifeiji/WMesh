package hub

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
	"wmesh/factory/internal/wanchannel"
)

// WANStatus 是本厂到 WAN 通道的现状，给管理页横幅看；只在内存，重启后从断开算起。
type WANStatus struct {
	Configured      bool       `json:"configured"`                // 本进程配了 WAN 地址；没配就不出站，也不提示断开
	Connected       bool       `json:"connected"`                 // MQTT 通道当前是否连着
	LastConnectedAt *time.Time `json:"lastConnectedAt,omitempty"` // 最近一次连上的时间；本进程还没连上过为空
	LeaseExpiresAt  *time.Time `json:"leaseExpiresAt,omitempty"`  // 内容租约到期，按 WAN 给的时间；还没领到为空
}

// wanStates 各厂通道现状，按厂身份存。
type wanStates struct {
	mu   sync.Mutex               // 护着下面的表，通道和接口两路同时读写
	byID map[uuid.UUID]*WANStatus // 每厂一份现状；没记过的厂当断开；首次写入时才建
}

// update 占锁改这一厂的现状，没有就先建。
func (t *wanStates) update(factoryID uuid.UUID, fn func(*WANStatus)) {
	// 占住表，避免和接口读交错。
	t.mu.Lock()
	// 改完就放开。
	defer t.mu.Unlock()
	// 表还没建就先建，零值的 Hub 也能直接用。
	if t.byID == nil {
		// 建空表，后面按厂写入。
		t.byID = map[uuid.UUID]*WANStatus{}
	}
	// 取这一厂的现状，没记过就建一份。
	st, ok := t.byID[factoryID]
	// 还没有就新建，从断开算起。
	if !ok {
		// 建一份空现状。
		st = &WANStatus{}
		// 记进表里，后面直接改它。
		t.byID[factoryID] = st
	}
	// 交给调用方改。
	fn(st)
}

// noteWANConnected 记下通道连上或断开；连上顺带刷新最近连上时间。
func (h *Hub) noteWANConnected(factoryID uuid.UUID, connected bool) {
	// 取当前时刻，连上时记下来。
	now := time.Now().UTC()
	// 改这一厂的在线标记。
	h.wan.update(factoryID, func(st *WANStatus) {
		// 记下当前是否连着。
		st.Connected = connected
		// 连上才刷新最近连上时间，断开保留上次的。
		if connected {
			// 记下这次连上的时刻。
			st.LastConnectedAt = &now
		}
	})
}

// noteWANLease 记下领到的租约到期，横幅断线时提示还能撑多久。
func (h *Hub) noteWANLease(factoryID uuid.UUID, notAfter time.Time) {
	// 统一成 UTC 再存。
	at := notAfter.UTC()
	// 改这一厂的租约到期。
	h.wan.update(factoryID, func(st *WANStatus) {
		// 记下最新领到的到期时刻。
		st.LeaseExpiresAt = &at
	})
}

// WANStatus 交回这一厂到 WAN 通道的现状副本。
func (h *Hub) WANStatus(factoryID uuid.UUID) WANStatus {
	// 读平台地址前先占住通道表。
	h.presenceMu.Lock()
	// 配了平台地址才会出站。
	configured := h.wanURL != ""
	// 地址读完就放开通道锁。
	h.presenceMu.Unlock()
	// 占住现状表再读。
	h.wan.mu.Lock()
	// 读完就放开。
	defer h.wan.mu.Unlock()
	// 先当断开，记过才拷出来。
	out := WANStatus{}
	// 记过这一厂就拷一份，免得调用方改到表里。
	if st, ok := h.wan.byID[factoryID]; ok {
		// 拷值，指针字段只读不改。
		out = *st
	}
	// 是否配了平台地址按当前进程算。
	out.Configured = configured
	return out
}

// clientLoginsFor 给通道保活取本厂各台最近登录；读库失败就这轮不带。
func clientLoginsFor(ctx context.Context, factoryID uuid.UUID, svc *service.Service) func() []wanchannel.ClientLoginUp {
	// 交回取数函数，每次保活现读一遍库。
	return func() []wanchannel.ClientLoginUp {
		// 每台设备取最近一条已对上本机的登录。
		rows, err := svc.Store().LatestClientLogins(ctx)
		// 读不出就这轮不带，下一轮保活再试。
		if err != nil {
			// 记下读失败，不影响保活本身。
			slog.Warn("latest client logins for wan", "factory", factoryID, "err", err)
			return nil
		}
		// 按需要预留位置，避免后面反复扩容。
		out := make([]wanchannel.ClientLoginUp, 0, len(rows))
		// 逐台转成上报格式。
		for _, r := range rows {
			// 时间用 RFC3339，WAN 按这个解析。
			out = append(out, wanchannel.ClientLoginUp{
				ClientID: r.ClientID.String(), At: r.OccurredAt.UTC().Format(time.RFC3339Nano),
				AppVersion: r.AppVersion, AppVersionName: r.AppVersionName,
			})
		}
		return out
	}
}
