// Package wanchannel 厂出站连 WAN 的 WSS 认领与心跳；不 import WAN 包，信封字段两边对齐。
package wanchannel

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
)

// 与 WAN 通道帧对齐的信封，不 import WAN 包。
type envelope struct {
	Typ              string          `json:"typ"`                        // enroll / enrolled / claimed / welcome / hello / challenge / hello_ack / ready / ping / pong / content_lease / content_lease_renew / factory_state / client_bind / client_void / platform_closure / platform_closure_body / platform_retract / content_template / asset_list / asset_snapshot / asset_list_ok / asset_snapshot_ok / error
	EnrollmentCode   string          `json:"enrollmentCode,omitempty"`   // 一次性建厂码，仅 enroll
	FactoryPublicKey []byte          `json:"factoryPublicKey,omitempty"` // 本厂签发公钥，仅 claimed
	FactoryID        string          `json:"factoryId,omitempty"`        // 工厂稳定身份
	Name             string          `json:"name,omitempty"`             // 工厂显示名
	SAPersonID       string          `json:"saPersonId,omitempty"`       // 约定的初始超管身份
	SALogin          string          `json:"saLogin,omitempty"`          // 初始超管登录名
	SADisplay        string          `json:"saDisplay,omitempty"`        // 初始超管显示名
	Nonce            []byte          `json:"nonce,omitempty"`            // hello 挑战随机数
	Signature        []byte          `json:"signature,omitempty"`        // 厂钥对 nonce 的签名
	Status           string          `json:"status,omitempty"`           // 工厂治理状态
	Revision         int64           `json:"revision,omitempty"`         // 治理修订，厂端只向前
	ClientID         string          `json:"clientId,omitempty"`         // 现场设备固定识别号
	ClientName       string          `json:"clientName,omitempty"`       // 给人看的设备名
	ClientShortCode  string          `json:"clientShortCode,omitempty"`  // Client 短码，随绑定下发
	FactoryShortCode string          `json:"factoryShortCode,omitempty"` // 本厂短码，认领或握手补齐
	ClientPublicKey  []byte          `json:"clientPublicKey,omitempty"`  // 本机公钥，可空
	BindingRevision  int64           `json:"bindingRevision,omitempty"`  // 绑定修订，厂端只向前
	ReqID            string          `json:"reqId,omitempty"`            // 问询与回执配对
	Kind             string          `json:"kind,omitempty"`             // process / project，仅 asset_list
	AssetID          string          `json:"assetId,omitempty"`          // 升档快照身份，或撤回的平台级身份
	Assets           json.RawMessage `json:"assets,omitempty"`           // 厂端升档清单元数据，不含正文
	Snapshot         json.RawMessage `json:"snapshot,omitempty"`         // 升档快照，含正文
	Closure          json.RawMessage `json:"closure,omitempty"`          // 平台级闭包（可用或停用修订）
	Template         json.RawMessage `json:"template,omitempty"`         // 当前内容模版
	Lease            []byte          `json:"lease,omitempty"`            // 内容租约钥 L，仅 content_lease
	NotAfter         string          `json:"notAfter,omitempty"`         // 租约到期 RFC3339，WAN 钟
	Error            string          `json:"error,omitempty"`            // 英文业务错误
}

// Offer 是 WAN 给出的待认领身份，不含建厂码。
type Offer struct {
	FactoryID  uuid.UUID // 工厂稳定身份
	Name       string    // 工厂显示名
	ShortCode  string    // 本厂短码
	SAPersonID uuid.UUID // 约定的初始超管身份
	SALogin    string    // 初始超管登录名
	SADisplay  string    // 初始超管显示名
}

// Session 一次认领或日常通道：认领确认后关掉；日常由 Hold 自己管。
type Session struct {
	conn *websocket.Conn
}

var pingEvery = 15 * time.Second // 心跳间隔；测试可改短
var leaseEvery = time.Hour       // 内容租约续期间隔

// Enroll 用建厂码向 WAN 要身份；确认前建厂码仍可重试。
func Enroll(ctx context.Context, wanURL, enrollmentCode string) (*Session, Offer, error) {
	u, err := channelURL(wanURL)
	if err != nil {
		return nil, Offer{}, err
	}
	c, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		return nil, Offer{}, domain.ErrWANUnreachable
	}
	sess := &Session{conn: c}
	if err := wsjson.Write(ctx, c, envelope{Typ: "enroll", EnrollmentCode: enrollmentCode}); err != nil {
		sess.Close()
		return nil, Offer{}, domain.ErrWANUnreachable
	}
	var msg envelope
	if err := wsjson.Read(ctx, c, &msg); err != nil {
		sess.Close()
		return nil, Offer{}, domain.ErrWANUnreachable
	}
	if msg.Typ == "error" {
		sess.Close()
		return nil, Offer{}, mapChannelError(msg.Error)
	}
	if msg.Typ != "enrolled" {
		sess.Close()
		return nil, Offer{}, domain.ErrWANUnreachable
	}
	fid, err := uuid.Parse(msg.FactoryID)
	if err != nil {
		sess.Close()
		return nil, Offer{}, domain.ErrInvalidEnrollment
	}
	pid, err := uuid.Parse(msg.SAPersonID)
	if err != nil {
		sess.Close()
		return nil, Offer{}, domain.ErrInvalidEnrollment
	}
	return sess, Offer{FactoryID: fid, Name: msg.Name, ShortCode: msg.FactoryShortCode, SAPersonID: pid, SALogin: msg.SALogin, SADisplay: msg.SADisplay}, nil
}

// Confirm 把本厂签发公钥交给 WAN，作废建厂码。
func (s *Session) Confirm(ctx context.Context, publicKey []byte) error {
	if s == nil || s.conn == nil {
		return domain.ErrWANUnreachable
	}
	if err := wsjson.Write(ctx, s.conn, envelope{Typ: "claimed", FactoryPublicKey: publicKey}); err != nil {
		return domain.ErrWANUnreachable
	}
	var msg envelope
	if err := wsjson.Read(ctx, s.conn, &msg); err != nil {
		return domain.ErrWANUnreachable
	}
	if msg.Typ == "error" {
		return mapChannelError(msg.Error)
	}
	if msg.Typ != "welcome" {
		return domain.ErrWANUnreachable
	}
	return nil
}

// State 是 WAN 下发的工厂治理状态。
type State struct {
	Status    string // 工厂治理状态：active / disabled / retired
	Revision  int64  // 治理修订，厂端只向前
	ShortCode string // 本厂短码，已认领厂握手补齐
}

// ClientIntent 是 WAN 推来的设备分配或作废。
type ClientIntent struct {
	Typ       string    // client_bind / client_void
	ClientID  uuid.UUID // 固定识别号
	Name      string    // 给人看的设备名
	ShortCode string    // Client 短码
	PublicKey []byte    // 本机公钥，可空
	Revision  int64     // 绑定修订
}

// RequestHandler 回答 WAN 经通道问本厂的升档问询；失败不拆连接。
type RequestHandler func(typ, reqID, kind, assetID string) (assets, snapshot json.RawMessage, err error)

// ClosureHandler 落地 WAN 推来的平台级闭包（可用或停用修订）；失败不拆连接。
type ClosureHandler func(raw json.RawMessage) error

// Lease 是 WAN 下发的内容解包钥，只进内存。
type Lease struct {
	Key      []byte    // 内容租约钥 L
	NotAfter time.Time // 到期时刻，按 WAN 钟
}

// LeaseHandler 把租约交给本厂进程；失败不拆连接。
type LeaseHandler func(Lease) error

// RetractHandler 落地 WAN 推来的平台级删除撤回；失败不拆连接。
type RetractHandler func(assetID uuid.UUID) error

// Hold 验签连上后发心跳，并落地 WAN 推来的停用/启用/注销、设备分配、平台级闭包/撤回、内容模版、内容租约和升档问询。
func Hold(ctx context.Context, wanURL string, factoryID uuid.UUID, privateKey []byte, apply func(State) error, applyClient func(ClientIntent) error, applyClosure ClosureHandler, applyTemplate ClosureHandler, applyRetract RetractHandler, applyLease LeaseHandler, onRequest RequestHandler) error {
	u, err := channelURL(wanURL)
	if err != nil {
		return err
	}
	c, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		return domain.ErrWANUnreachable
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	// 已认领厂用稳定身份打招呼，等挑战。
	if err := wsjson.Write(ctx, c, envelope{Typ: "hello", FactoryID: factoryID.String()}); err != nil {
		return domain.ErrWANUnreachable
	}
	var msg envelope
	if err := wsjson.Read(ctx, c, &msg); err != nil {
		return domain.ErrWANUnreachable
	}
	if msg.Typ == "error" {
		return mapChannelError(msg.Error)
	}
	if msg.Typ != "challenge" || len(msg.Nonce) < 16 {
		return domain.ErrWANUnreachable
	}
	if len(privateKey) == 0 {
		return domain.ErrNotFound // 没有签发钥就无法证明本厂身份
	}
	// 用本厂签发钥签挑战，证明持有私钥。
	sig := nodekey.Sign(privateKey, helloPayload(factoryID, msg.Nonce))
	if err := wsjson.Write(ctx, c, envelope{Typ: "hello_ack", Signature: sig}); err != nil {
		return domain.ErrWANUnreachable
	}
	if err := wsjson.Read(ctx, c, &msg); err != nil {
		return domain.ErrWANUnreachable
	}
	if msg.Typ == "error" {
		return mapChannelError(msg.Error)
	}
	if msg.Typ != "ready" {
		return domain.ErrWANUnreachable
	}
	if apply != nil && msg.Status != "" {
		// 握手完成时带上当前治理状态，厂端只向前落地。
		if err := apply(State{Status: msg.Status, Revision: msg.Revision, ShortCode: msg.FactoryShortCode}); err != nil {
			return err
		}
	}
	return holdPings(ctx, c, apply, applyClient, applyClosure, applyTemplate, applyRetract, applyLease, onRequest)
}

// 心跳与收帧：租约、治理、设备、闭包、模版、撤回、升档问询。
func holdPings(ctx context.Context, c *websocket.Conn, apply func(State) error, applyClient func(ClientIntent) error, applyClosure ClosureHandler, applyTemplate ClosureHandler, applyRetract RetractHandler, applyLease LeaseHandler, onRequest RequestHandler) error {
	ticker := time.NewTicker(pingEvery)
	defer ticker.Stop()
	renew := time.NewTicker(leaseEvery)
	defer renew.Stop()
	errCh := make(chan error, 1)
	go func() {
		for {
			var msg envelope
			if err := wsjson.Read(ctx, c, &msg); err != nil {
				errCh <- err
				return
			}
			if msg.Typ == "content_lease" && applyLease != nil && len(msg.Lease) > 0 {
				na, err := time.Parse(time.RFC3339Nano, msg.NotAfter)
				if err != nil {
					na, err = time.Parse(time.RFC3339, msg.NotAfter)
				}
				if err != nil {
					continue
				}
				if err := applyLease(Lease{Key: msg.Lease, NotAfter: na}); err != nil {
					// 失败不拆连接：错钥或厂钟快时通道仍要心跳。
					slog.Warn("apply content lease", "err", err)
					continue
				}
			}
			if msg.Typ == "factory_state" && apply != nil {
				if err := apply(State{Status: msg.Status, Revision: msg.Revision, ShortCode: msg.FactoryShortCode}); err != nil {
					errCh <- err
					return
				}
			}
			if (msg.Typ == "client_bind" || msg.Typ == "client_void") && applyClient != nil {
				in, err := parseClientIntent(msg)
				if err != nil {
					errCh <- err
					return
				}
				if err := applyClient(in); err != nil {
					errCh <- err
					return
				}
			}
			if (msg.Typ == "platform_closure" || msg.Typ == "platform_closure_body") && applyClosure != nil && len(msg.Closure) > 0 {
				if err := applyClosure(msg.Closure); err != nil {
					errCh <- err
					return
				}
			}
			if msg.Typ == "content_template" && applyTemplate != nil && len(msg.Template) > 0 {
				if err := applyTemplate(msg.Template); err != nil {
					errCh <- err
					return
				}
			}
			if msg.Typ == "platform_retract" && applyRetract != nil && msg.AssetID != "" {
				id, err := uuid.Parse(msg.AssetID)
				if err != nil {
					continue
				}
				if err := applyRetract(id); err != nil {
					errCh <- err
					return
				}
			}
			if msg.Typ == "asset_list" || msg.Typ == "asset_snapshot" {
				if err := answerRequest(ctx, c, msg, onRequest); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()
	if err := wsjson.Write(ctx, c, envelope{Typ: "ping"}); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case <-ticker.C:
			if err := wsjson.Write(ctx, c, envelope{Typ: "ping"}); err != nil {
				return err
			}
		case <-renew.C:
			if applyLease != nil {
				// 按间隔向 WAN 续内容租约。
				if err := wsjson.Write(ctx, c, envelope{Typ: "content_lease_renew"}); err != nil {
					return err
				}
			}
		}
	}
}

// Close 关掉这次通道。
func (s *Session) Close() {
	if s == nil || s.conn == nil {
		return
	}
	_ = s.conn.Close(websocket.StatusNormalClosure, "")
	s.conn = nil
}

// 把绑定或作废帧收成设备意图。
func parseClientIntent(msg envelope) (ClientIntent, error) {
	cid, err := uuid.Parse(msg.ClientID)
	if err != nil {
		return ClientIntent{}, domain.ErrNotFound
	}
	return ClientIntent{
		Typ:       msg.Typ,
		ClientID:  cid,
		Name:      msg.ClientName,
		ShortCode: msg.ClientShortCode,
		PublicKey: msg.ClientPublicKey,
		Revision:  msg.BindingRevision,
	}, nil
}

// 用同一问询号回列表或快照。
func answerRequest(ctx context.Context, c *websocket.Conn, msg envelope, onRequest RequestHandler) error {
	if onRequest == nil {
		return wsjson.Write(ctx, c, envelope{Typ: "error", ReqID: msg.ReqID, Error: domain.ErrNotFound.Error()})
	}
	assets, snap, err := onRequest(msg.Typ, msg.ReqID, msg.Kind, msg.AssetID)
	if err != nil {
		return wsjson.Write(ctx, c, envelope{Typ: "error", ReqID: msg.ReqID, Error: err.Error()})
	}
	reply := envelope{ReqID: msg.ReqID, Assets: assets, Snapshot: snap}
	if msg.Typ == "asset_snapshot" {
		reply.Typ = "asset_snapshot_ok"
	} else {
		reply.Typ = "asset_list_ok"
	}
	return wsjson.Write(ctx, c, reply)
}

// 把 HTTP(S) 根地址改成 /v1/channel 的 WS(S)。
func channelURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", domain.ErrWANUnreachable
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", domain.ErrWANUnreachable
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", domain.ErrWANUnreachable
	}
	u.Path = "/v1/channel"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// 厂钥要签的挑战原文：前缀、厂身份、nonce。
func helloPayload(factoryID uuid.UUID, nonce []byte) []byte {
	b := make([]byte, 0, 18+16+len(nonce))
	b = append(b, "wmesh-wan-hello-v1"...)
	b = append(b, factoryID[:]...)
	b = append(b, nonce...)
	return b
}

// 把 WAN 英文错误收回本侧业务错误。
func mapChannelError(msg string) error {
	switch msg {
	case domain.ErrInvalidEnrollment.Error():
		return domain.ErrInvalidEnrollment
	case domain.ErrFactoryDisabled.Error():
		return domain.ErrFactoryDisabled
	case domain.ErrFactoryRetired.Error():
		return domain.ErrFactoryRetired
	case "factory public key already registered":
		return domain.ErrSigningKeyExists
	case domain.ErrInvalidKey.Error():
		return domain.ErrInvalidKey
	default:
		if msg == "" {
			return domain.ErrWANUnreachable
		}
		return errors.New(msg)
	}
}
