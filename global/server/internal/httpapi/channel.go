package httpapi

import (
	"context"
	"crypto/rand"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

func (h *Handler) mountChannel(mux *http.ServeMux) { // 厂出站 WSS：认领、hello 验签、心跳
	mux.HandleFunc("GET /v1/channel", h.channel)
}

type channelMsg struct {
	Typ              string                   `json:"typ"`                        // enroll / enrolled / claimed / welcome / hello / challenge / hello_ack / ready / ping / pong / factory_state / client_bind / client_void / platform_closure / platform_retract / content_template / asset_list / asset_snapshot / asset_list_ok / asset_snapshot_ok / error
	EnrollmentCode   string                   `json:"enrollmentCode,omitempty"`   // 一次性建厂码，仅 enroll
	FactoryPublicKey []byte                   `json:"factoryPublicKey,omitempty"` // 本厂签发公钥，仅 claimed
	FactoryID        string                   `json:"factoryId,omitempty"`        // 工厂稳定身份
	Name             string                   `json:"name,omitempty"`             // 工厂显示名
	SAPersonID       string                   `json:"saPersonId,omitempty"`       // 约定的初始超管身份
	SALogin          string                   `json:"saLogin,omitempty"`          // 初始超管登录名
	SADisplay        string                   `json:"saDisplay,omitempty"`        // 初始超管显示名
	Nonce            []byte                   `json:"nonce,omitempty"`            // hello 挑战随机数
	Signature        []byte                   `json:"signature,omitempty"`        // 厂钥对 nonce 的签名
	Status           string                   `json:"status,omitempty"`           // 工厂治理状态
	Revision         int64                    `json:"revision,omitempty"`         // 治理修订，厂端只向前
	ClientID         string                   `json:"clientId,omitempty"`         // 现场设备固定识别号
	ClientName       string                   `json:"clientName,omitempty"`       // 给人看的设备名
	ClientPublicKey  []byte                   `json:"clientPublicKey,omitempty"`  // 本机公钥，可空
	BindingRevision  int64                    `json:"bindingRevision,omitempty"`  // 绑定修订，厂端只向前
	ReqID            string                   `json:"reqId,omitempty"`            // 问询与回执配对
	Kind             string                   `json:"kind,omitempty"`             // process / project，仅 asset_list
	AssetID          string                   `json:"assetId,omitempty"`          // 升档快照身份，或撤回的平台级身份
	Assets           []promotableAsset        `json:"assets,omitempty"`           // 可升档厂级元数据，不含正文
	Snapshot         *service.AssetSnapshot   `json:"snapshot,omitempty"`         // 升档快照，含正文
	Closure          *service.ClosureSnapshot  `json:"closure,omitempty"`          // 平台级闭包（可用或停用修订）
	Template         *service.TemplateSnapshot `json:"template,omitempty"`         // 当前内容模版
	Error            string                    `json:"error,omitempty"`            // 英文业务错误
}

type promotableAsset struct {
	ID       string `json:"id"`       // 厂级稳定身份
	Kind     string `json:"kind"`     // process / project
	Name     string `json:"name"`     // 显示名
	Revision int64  `json:"revision"` // 当前修订
	Digest   []byte `json:"digest"`   // 内容摘要
	Status   string `json:"status"`   // 须为 available
	Copyable bool   `json:"copyable"` // 须为可复制
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

func (h *Handler) enrollThenServe(ctx context.Context, c *websocket.Conn, enrollmentCode string) {
	writeErr := func(err error) {
		_ = wsjson.Write(ctx, c, channelMsg{Typ: "error", Error: err.Error()})
	}
	offer, err := h.svc.OfferEnroll(ctx, enrollmentCode)
	if err != nil {
		writeErr(err)
		return
	}
	if err := wsjson.Write(ctx, c, channelMsg{
		Typ:        "enrolled",
		FactoryID:  offer.FactoryID.String(),
		Name:       offer.Name,
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

func (h *Handler) helloThenServe(ctx context.Context, c *websocket.Conn, factoryID string) {
	writeErr := func(err error) {
		_ = wsjson.Write(ctx, c, channelMsg{Typ: "error", Error: err.Error()})
	}
	fid, err := uuid.Parse(factoryID)
	if err != nil {
		writeErr(domain.ErrUnauthorized)
		return
	}
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

func (h *Handler) pinFactory(ctx context.Context, c *websocket.Conn, factoryID uuid.UUID) {
	gen := h.live.acquire(factoryID)
	h.live.attach(factoryID, gen, c)
	if err := h.svc.MarkChannelOnline(ctx, factoryID); err != nil {
		_ = wsjson.Write(ctx, c, channelMsg{Typ: "error", Error: err.Error()})
		return
	}
	h.replayClients(ctx, c, factoryID)
	h.replayClosures(ctx, c, factoryID)
	h.replayTemplates(ctx, c, factoryID)
	h.replayRetractions(ctx, c)
	defer func() {
		if h.live.release(factoryID, gen) {
			_ = h.svc.MarkChannelOffline(context.Background(), factoryID)
		}
	}()
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
			_ = h.svc.TouchChannel(ctx, factoryID)
			if err := wsjson.Write(ctx, c, channelMsg{Typ: "pong"}); err != nil {
				return
			}
		case "asset_list_ok", "asset_snapshot_ok", "error":
			h.live.deliver(factoryID, msg)
		}
	}
}

func (h *Handler) replayClosures(ctx context.Context, c *websocket.Conn, factoryID uuid.UUID) {
	snaps, err := h.svc.PackAvailableForFactory(ctx, factoryID)
	if err != nil {
		return
	}
	for i := range snaps {
		if err := wsjson.Write(ctx, c, channelMsg{Typ: "platform_closure", Closure: &snaps[i]}); err != nil {
			return
		}
	}
}

func (h *Handler) fanoutAvailable(ctx context.Context) {
	facs, err := h.svc.Store().ListFactories(ctx)
	if err != nil {
		return
	}
	for _, fac := range facs {
		if fac.Status != service.FactoryActive {
			continue
		}
		snaps, err := h.svc.PackAvailableForFactory(ctx, fac.ID)
		if err != nil {
			continue
		}
		for i := range snaps {
			_ = h.live.push(ctx, fac.ID, channelMsg{Typ: "platform_closure", Closure: &snaps[i]})
		}
	}
}

func (h *Handler) replayTemplates(ctx context.Context, c *websocket.Conn, factoryID uuid.UUID) {
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

func (h *Handler) replayRetractions(ctx context.Context, c *websocket.Conn) { // 回连补送已删平台级身份
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

func (h *Handler) fanoutRetract(ctx context.Context, assetID uuid.UUID) { // 在线厂立刻收到删除撤回
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

func (h *Handler) replayClients(ctx context.Context, c *websocket.Conn, factoryID uuid.UUID) {
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

func (h *Handler) lifecycleMsg(ctx context.Context, factoryID uuid.UUID, typ string) channelMsg {
	msg := channelMsg{Typ: typ, FactoryID: factoryID.String(), Status: service.FactoryActive}
	fac, err := h.svc.Store().FactoryByID(ctx, factoryID)
	if err != nil {
		return msg
	}
	msg.Status = fac.Status
	msg.Revision = fac.LifecycleRevision
	return msg
}

func (h *Handler) pushLifecycle(factoryID uuid.UUID) {
	msg := h.lifecycleMsg(context.Background(), factoryID, "factory_state")
	msg.Typ = "factory_state"
	_ = h.live.push(context.Background(), factoryID, msg)
}

func clientBindMsg(row service.Client) channelMsg {
	return channelMsg{
		Typ:             "client_bind",
		ClientID:        row.ID.String(),
		ClientName:      row.Name,
		ClientPublicKey: row.PublicKey,
		BindingRevision: row.BindingRevision,
	}
}

func clientVoidMsg(row service.Client) channelMsg {
	return channelMsg{
		Typ:             "client_void",
		ClientID:        row.ID.String(),
		BindingRevision: row.BindingRevision,
	}
}

func (h *Handler) pushClientToFactory(row service.Client, oldFactory *uuid.UUID) {
	ctx := context.Background()
	if oldFactory != nil && (row.FactoryID == nil || *oldFactory != *row.FactoryID) {
		_ = h.live.push(ctx, *oldFactory, clientVoidMsg(row))
	}
	if row.FactoryID != nil {
		_ = h.live.push(ctx, *row.FactoryID, clientBindMsg(row))
	}
}
