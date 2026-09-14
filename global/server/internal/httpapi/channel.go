package httpapi

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

// 挂厂出站 WSS：认领、hello 验签、心跳。
func (h *Handler) mountChannel(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/channel", h.channel)
}

// 厂出站通道一帧，字段按 typ 选用。
type channelMsg struct {
	Typ              string                    `json:"typ"`                        // enroll / enrolled / claimed / welcome / hello / challenge / hello_ack / ready / ping / pong / content_lease / content_lease_renew / factory_state / client_bind / client_void / platform_closure / platform_closure_body / platform_retract / content_template / asset_list / asset_snapshot / asset_list_ok / asset_snapshot_ok / error
	EnrollmentCode   string                    `json:"enrollmentCode,omitempty"`   // 一次性建厂码，仅 enroll
	FactoryPublicKey []byte                    `json:"factoryPublicKey,omitempty"` // 本厂签发公钥，仅 claimed
	FactoryID        string                    `json:"factoryId,omitempty"`        // 工厂稳定身份
	Name             string                    `json:"name,omitempty"`             // 工厂显示名
	SAPersonID       string                    `json:"saPersonId,omitempty"`       // 约定的初始超管身份
	SALogin          string                    `json:"saLogin,omitempty"`          // 初始超管登录名
	SADisplay        string                    `json:"saDisplay,omitempty"`        // 初始超管显示名
	Nonce            []byte                    `json:"nonce,omitempty"`            // hello 挑战随机数
	Signature        []byte                    `json:"signature,omitempty"`        // 厂钥对 nonce 的签名
	Status           string                    `json:"status,omitempty"`           // 工厂治理状态
	Revision         int64                     `json:"revision,omitempty"`         // 治理修订，厂端只向前
	ClientID         string                    `json:"clientId,omitempty"`         // 现场设备固定识别号
	ClientName       string                    `json:"clientName,omitempty"`       // 给人看的设备名
	ClientShortCode  string                    `json:"clientShortCode,omitempty"`  // Client 短码，随绑定下发
	FactoryShortCode string                    `json:"factoryShortCode,omitempty"` // 本厂短码，认领或握手补齐
	ClientPublicKey  []byte                    `json:"clientPublicKey,omitempty"`  // 本机公钥，可空
	BindingRevision  int64                     `json:"bindingRevision,omitempty"`  // 绑定修订，厂端只向前
	ReqID            string                    `json:"reqId,omitempty"`            // 问询与回执配对
	Kind             string                    `json:"kind,omitempty"`             // process / project，仅 asset_list
	AssetID          string                    `json:"assetId,omitempty"`          // 升档快照身份，或撤回的平台级身份
	Assets           []promotableAsset         `json:"assets,omitempty"`           // 厂端升档清单元数据，不含正文
	Snapshot         *service.AssetSnapshot    `json:"snapshot,omitempty"`         // 升档快照，含正文
	Closure          *service.ClosureSnapshot  `json:"closure,omitempty"`          // 平台级闭包（可用或停用修订）
	Template         *service.TemplateSnapshot `json:"template,omitempty"`         // 当前内容模版
	Lease            []byte                    `json:"lease,omitempty"`            // 内容租约钥 L，仅 content_lease
	NotAfter         string                    `json:"notAfter,omitempty"`         // 租约到期 RFC3339，WAN 钟
	Error            string                    `json:"error,omitempty"`            // 英文业务错误
}

// 升档清单元数据，不含正文。
type promotableAsset struct {
	ID       string `json:"id"`       // 稳定身份
	Kind     string `json:"kind"`     // process / project
	Level    string `json:"level"`    // factory / personal / platform
	Name     string `json:"name"`     // 显示名
	Code     string `json:"code"`     // 只读编号
	Revision int64  `json:"revision"` // 当前修订
	Digest   []byte `json:"digest"`   // 内容摘要
	Status   string `json:"status"`   // draft / available / disabled
	Copyable bool   `json:"copyable"` // 原样带回
}

const channelIdle = 45 * time.Second // 超过这个时间没收心跳就当断线

// channel 先认领或验签，再把套接字钉死在这一家厂上。
func (h *Handler) channel(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := r.Context()
	writeErr := func(err error) {
		_ = wsjson.Write(ctx, c, channelMsg{Typ: "error", Error: err.Error()})
	}

	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	var msg channelMsg
	err = wsjson.Read(readCtx, c, &msg)
	cancel()
	if err != nil {
		writeErr(domain.ErrUnauthorized)
		return
	}
	switch msg.Typ {
	case "enroll":
		h.enrollThenServe(ctx, c, msg.EnrollmentCode)
	case "hello":
		h.helloThenServe(ctx, c, msg.FactoryID)
	default:
		writeErr(domain.ErrUnauthorized)
	}
}

// 用建厂码发待认领身份，等厂钥确认后再钉死通道。
func (h *Handler) enrollThenServe(ctx context.Context, c *websocket.Conn, enrollmentCode string) {
	writeErr := func(err error) {
		_ = wsjson.Write(ctx, c, channelMsg{Typ: "error", Error: err.Error()})
	}
	// 用建厂码换身份，码此时仍可重试。
	offer, err := h.svc.OfferEnroll(ctx, enrollmentCode)
	if err != nil {
		writeErr(err)
		return
	}
	if err := wsjson.Write(ctx, c, channelMsg{
		Typ:        "enrolled",
		FactoryID:  offer.FactoryID.String(),
		Name:       offer.Name,
		FactoryShortCode: offer.ShortCode,
		SAPersonID: offer.SAPersonID.String(),
		SALogin:    offer.SALogin,
		SADisplay:  offer.SADisplay,
	}); err != nil {
		return
	}
	var msg channelMsg
	if err := wsjson.Read(ctx, c, &msg); err != nil {
		writeErr(domain.ErrUnauthorized)
		return
	}
	if msg.Typ != "claimed" {
		writeErr(domain.ErrUnauthorized)
		return
	}
	// 登记厂钥并作废建厂码。
	if err := h.svc.ConfirmEnroll(ctx, offer.FactoryID, msg.FactoryPublicKey); err != nil {
		writeErr(err)
		return
	}
	st := h.lifecycleMsg(ctx, offer.FactoryID, "welcome")
	if err := wsjson.Write(ctx, c, st); err != nil {
		return
	}
	h.pinFactory(ctx, c, offer.FactoryID)
}

// 用已登记厂钥验签，通过后再钉死通道。
func (h *Handler) helloThenServe(ctx context.Context, c *websocket.Conn, factoryID string) {
	writeErr := func(err error) {
		_ = wsjson.Write(ctx, c, channelMsg{Typ: "error", Error: err.Error()})
	}
	fid, err := uuid.Parse(factoryID)
	if err != nil {
		writeErr(domain.ErrUnauthorized)
		return
	}
	// 没登记厂钥就拒绝 hello。
	if err := h.svc.RequireFactoryKey(ctx, fid); err != nil {
		writeErr(err)
		return
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		writeErr(domain.ErrUnauthorized)
		return
	}
	if err := wsjson.Write(ctx, c, channelMsg{Typ: "challenge", FactoryID: fid.String(), Nonce: nonce}); err != nil {
		return
	}
	var msg channelMsg
	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = wsjson.Read(readCtx, c, &msg)
	cancel()
	if err != nil {
		writeErr(domain.ErrUnauthorized)
		return
	}
	if msg.Typ != "hello_ack" {
		writeErr(domain.ErrUnauthorized)
		return
	}
	// 用已登记厂钥验 nonce 签名。
	if err := h.svc.AcceptHello(ctx, fid, nonce, msg.Signature); err != nil {
		writeErr(err)
		return
	}
	st := h.lifecycleMsg(ctx, fid, "ready")
	if err := wsjson.Write(ctx, c, st); err != nil {
		return
	}
	h.pinFactory(ctx, c, fid)
}

// 钉死后先发租约再回放，避免密文早于解包钥。
func (h *Handler) pinFactory(ctx context.Context, c *websocket.Conn, factoryID uuid.UUID) {
	gen := h.live.acquire(factoryID)
	defer func() {
		// 只有当前世代断开才标离线。
		if h.live.release(factoryID, gen) {
			_ = h.svc.MarkChannelOffline(context.Background(), factoryID)
		}
	}()
	// 钉死后才标在线，握手窗口不算。
	if err := h.svc.MarkChannelOnline(ctx, factoryID); err != nil {
		_ = wsjson.Write(ctx, c, channelMsg{Typ: "error", Error: err.Error()})
		return
	}
	// 租约先于任何闭包；回放写完再 attach，避免 fanout 抢在租约前写套接字。
	if err := h.pushContentLease(ctx, c, factoryID); err != nil {
		_ = wsjson.Write(ctx, c, channelMsg{Typ: "error", Error: err.Error()})
		return
	}
	h.replayClients(ctx, c, factoryID)
	h.replayClosures(ctx, c, factoryID)
	h.replayTemplates(ctx, c, factoryID)
	h.replayRetractions(ctx, c)
	h.live.attach(factoryID, gen, c)
	// 补上握手窗口里 HTTP 发布丢掉的修订。
	h.replayClosures(ctx, c, factoryID)
	for {
		readCtx, cancel := context.WithTimeout(ctx, channelIdle)
		var msg channelMsg
		err := wsjson.Read(readCtx, c, &msg)
		cancel()
		if err != nil {
			return
		}
		switch msg.Typ {
		case "ping":
			// 心跳续在线时刻。
			_ = h.svc.TouchChannel(ctx, factoryID)
			if err := wsjson.Write(ctx, c, channelMsg{Typ: "pong"}); err != nil {
				return
			}
		case "content_lease_renew":
			if err := h.pushContentLease(ctx, c, factoryID); err != nil {
				return
			}
			// 续期成功后再补闭包，补上通道还在时验收失败的修订。
			h.replayClosures(ctx, c, factoryID)
		case "asset_list_ok", "asset_snapshot_ok", "error":
			h.live.deliver(factoryID, msg)
		}
	}
}

// 同一把 L 续发给已钉死身份的厂。
func (h *Handler) pushContentLease(ctx context.Context, c *websocket.Conn, factoryID uuid.UUID) error {
	lease, err := h.svc.IssueContentLease(ctx, factoryID)
	if err != nil {
		return err
	}
	return wsjson.Write(ctx, c, channelMsg{
		Typ:      "content_lease",
		Lease:    lease.Key,
		NotAfter: lease.NotAfter.UTC().Format(time.RFC3339Nano),
	})
}

// 把已授权平台级再封过站信封推给本厂。
func (h *Handler) replayClosures(ctx context.Context, c *websocket.Conn, factoryID uuid.UUID) {
	// 组本厂已授权可用闭包。
	snaps, err := h.svc.PackAvailableForFactory(ctx, factoryID)
	if err != nil {
		return
	}
	for i := range snaps {
		if err := h.writeSealedClosure(ctx, c, factoryID, snaps[i]); err != nil {
			return
		}
	}
}

// 在线厂立刻推新修订；离线只记账，上线再补密文。
func (h *Handler) fanoutAvailable(ctx context.Context) {
	facs, err := h.svc.Store().ListFactories(ctx)
	if err != nil {
		return
	}
	for _, fac := range facs {
		if fac.Status != service.FactoryActive {
			continue
		}
		// 离线厂也先记下授权和下发记录；通道不在时 push 会丢掉，上线 replayClosures 再补密文。
		snaps, err := h.svc.PackAvailableForFactory(ctx, fac.ID)
		if err != nil {
			continue
		}
		for i := range snaps {
			h.pushSealedClosure(ctx, fac.ID, snaps[i])
		}
	}
}

// 用本厂租约封过站信封；失败先续租再试一次。
func (h *Handler) sealForFactory(ctx context.Context, factoryID uuid.UUID, snap service.ClosureSnapshot) (service.ClosureSnapshot, error) {
	sealed, err := h.svc.SealClosureTransit(ctx, factoryID, snap)
	if err != nil {
		// 封失败时先续租再封一次。
		if _, lerr := h.svc.IssueContentLease(ctx, factoryID); lerr == nil {
			sealed, err = h.svc.SealClosureTransit(ctx, factoryID, snap)
		}
	}
	if err != nil {
		slog.Error("seal closure transit", "factory", factoryID, "err", err)
	}
	return sealed, err
}

// 先推控制面（去掉正文），正文另帧。
func closureControl(snap service.ClosureSnapshot) service.ClosureSnapshot {
	out := snap
	out.Members = append([]service.ClosureMember(nil), snap.Members...)
	for i := range out.Members {
		out.Members[i].Content = nil
	}
	return out
}

// 先写控制面再写密封正文。
func (h *Handler) writeSealedClosure(ctx context.Context, c *websocket.Conn, factoryID uuid.UUID, snap service.ClosureSnapshot) error {
	sealed, err := h.sealForFactory(ctx, factoryID, snap)
	if err != nil {
		return err
	}
	meta := closureControl(sealed)
	if err := wsjson.Write(ctx, c, channelMsg{Typ: "platform_closure", Closure: &meta}); err != nil {
		return err
	}
	return wsjson.Write(ctx, c, channelMsg{Typ: "platform_closure_body", Closure: &sealed})
}

// 经钉死通道推密封闭包；没连接就丢掉。
func (h *Handler) pushSealedClosure(ctx context.Context, factoryID uuid.UUID, snap service.ClosureSnapshot) {
	sealed, err := h.sealForFactory(ctx, factoryID, snap)
	if err != nil {
		return
	}
	meta := closureControl(sealed)
	_ = h.live.push(ctx, factoryID, channelMsg{Typ: "platform_closure", Closure: &meta})
	_ = h.live.push(ctx, factoryID, channelMsg{Typ: "platform_closure_body", Closure: &sealed})
}

// 回连补送当前内容模版。
func (h *Handler) replayTemplates(ctx context.Context, c *websocket.Conn, factoryID uuid.UUID) {
	// 取本厂当前内容模版副本。
	snaps, err := h.svc.Templates.SnapshotsForFactory(ctx, factoryID)
	if err != nil {
		return
	}
	for i := range snaps {
		if err := wsjson.Write(ctx, c, channelMsg{Typ: "content_template", Template: &snaps[i]}); err != nil {
			return
		}
	}
}

// 在线厂立刻收到新模版修订。
func (h *Handler) fanoutTemplates(ctx context.Context) {
	facs, err := h.svc.Store().ListFactories(ctx)
	if err != nil {
		return
	}
	for _, fac := range facs {
		if fac.Status != service.FactoryActive {
			continue
		}
		snaps, err := h.svc.Templates.SnapshotsForFactory(ctx, fac.ID)
		if err != nil {
			continue
		}
		for i := range snaps {
			_ = h.live.push(ctx, fac.ID, channelMsg{Typ: "content_template", Template: &snaps[i]})
		}
	}
}

// 回连补送已删平台级身份。
func (h *Handler) replayRetractions(ctx context.Context, c *websocket.Conn) {
	// 取已删平台级身份，回连补撤回。
	ids, err := h.svc.ListRetractions(ctx)
	if err != nil {
		return
	}
	for _, id := range ids {
		if err := wsjson.Write(ctx, c, channelMsg{Typ: "platform_retract", AssetID: id.String()}); err != nil {
			return
		}
	}
}

// 在线厂立刻收到删除撤回。
func (h *Handler) fanoutRetract(ctx context.Context, assetID uuid.UUID) {
	facs, err := h.svc.Store().ListFactories(ctx)
	if err != nil {
		return
	}
	msg := channelMsg{Typ: "platform_retract", AssetID: assetID.String()}
	for _, fac := range facs {
		if fac.Status != service.FactoryActive {
			continue
		}
		_ = h.live.push(ctx, fac.ID, msg)
	}
}

// 回连补送本厂已分配设备。
func (h *Handler) replayClients(ctx context.Context, c *websocket.Conn, factoryID uuid.UUID) {
	// 取本厂已分配设备，回连补送。
	rows, err := h.svc.Store().ListClientsByFactory(ctx, factoryID)
	if err != nil {
		return
	}
	for _, row := range rows {
		if err := wsjson.Write(ctx, c, clientBindMsg(row)); err != nil {
			return
		}
	}
}

// 带上当前治理状态和修订。
func (h *Handler) lifecycleMsg(ctx context.Context, factoryID uuid.UUID, typ string) channelMsg {
	msg := channelMsg{Typ: typ, FactoryID: factoryID.String(), Status: service.FactoryActive}
	// 读名录治理状态，推给厂端只向前。
	fac, err := h.svc.Store().FactoryByID(ctx, factoryID)
	if err != nil {
		return msg
	}
	msg.Status = fac.Status
	msg.Revision = fac.LifecycleRevision
	msg.FactoryShortCode = fac.ShortCode
	return msg
}

// 把停用或启用立刻推给在线厂。
func (h *Handler) pushLifecycle(factoryID uuid.UUID) {
	msg := h.lifecycleMsg(context.Background(), factoryID, "factory_state")
	msg.Typ = "factory_state"
	_ = h.live.push(context.Background(), factoryID, msg)
}

// 收成绑定帧。
func clientBindMsg(row service.Client) channelMsg {
	return channelMsg{
		Typ:             "client_bind",
		ClientID:        row.ID.String(),
		ClientName:       row.Name,
		ClientShortCode:  row.ShortCode,
		ClientPublicKey:  row.PublicKey,
		BindingRevision: row.BindingRevision,
	}
}

// 收成作废帧。
func clientVoidMsg(row service.Client) channelMsg {
	return channelMsg{
		Typ:             "client_void",
		ClientID:        row.ID.String(),
		BindingRevision: row.BindingRevision,
	}
}

// 改分时先通知旧厂作废，再通知新厂绑定。
func (h *Handler) pushClientToFactory(row service.Client, oldFactory *uuid.UUID) {
	ctx := context.Background()
	if oldFactory != nil && (row.FactoryID == nil || *oldFactory != *row.FactoryID) {
		_ = h.live.push(ctx, *oldFactory, clientVoidMsg(row))
	}
	if row.FactoryID != nil {
		_ = h.live.push(ctx, *row.FactoryID, clientBindMsg(row))
	}
}
