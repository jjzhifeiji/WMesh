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

type envelope struct {
	Typ              string          `json:"typ"`
	EnrollmentCode   string          `json:"enrollmentCode,omitempty"`
	FactoryPublicKey []byte          `json:"factoryPublicKey,omitempty"`
	FactoryID        string          `json:"factoryId,omitempty"`
	Name             string          `json:"name,omitempty"`
	SAPersonID       string          `json:"saPersonId,omitempty"`
	SALogin          string          `json:"saLogin,omitempty"`
	SADisplay        string          `json:"saDisplay,omitempty"`
	Nonce            []byte          `json:"nonce,omitempty"`
	Signature        []byte          `json:"signature,omitempty"`
	Status           string          `json:"status,omitempty"`
	Revision         int64           `json:"revision,omitempty"`
	ClientID         string          `json:"clientId,omitempty"`
	ClientName       string          `json:"clientName,omitempty"`
	ClientPublicKey  []byte          `json:"clientPublicKey,omitempty"`
	BindingRevision  int64           `json:"bindingRevision,omitempty"`
	ReqID            string          `json:"reqId,omitempty"`
	Kind             string          `json:"kind,omitempty"`
	AssetID          string          `json:"assetId,omitempty"`
	Assets           json.RawMessage `json:"assets,omitempty"`
	Snapshot         json.RawMessage `json:"snapshot,omitempty"`
	Closure          json.RawMessage `json:"closure,omitempty"`
	Template         json.RawMessage `json:"template,omitempty"`
	Lease            []byte          `json:"lease,omitempty"`
	NotAfter         string          `json:"notAfter,omitempty"`
	Error            string          `json:"error,omitempty"`
}

// Offer 是 WAN 给出的待认领身份，不含建厂码。
type Offer struct {
	FactoryID  uuid.UUID
	Name       string
	SAPersonID uuid.UUID
	SALogin    string
	SADisplay  string
}

// Session 一次认领或日常通道：认领确认后关掉；日常由 Hold 自己管。
type Session struct {
	conn *websocket.Conn
}

var pingEvery = 15 * time.Second  // 心跳间隔；测试可改短
var leaseEvery = time.Hour      // 内容租约续期间隔

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
	return sess, Offer{FactoryID: fid, Name: msg.Name, SAPersonID: pid, SALogin: msg.SALogin, SADisplay: msg.SADisplay}, nil
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
	Status   string
	Revision int64
}

// ClientIntent 是 WAN 推来的设备分配或作废。
type ClientIntent struct {
	Typ       string    // client_bind / client_void
	ClientID  uuid.UUID // 固定识别号
	Name      string    // 给人看的设备名
	PublicKey []byte    // 本机公钥，可空
	Revision  int64     // 绑定修订
}

// RequestHandler 回答 WAN 经通道问本厂的升档问询；失败不拆连接。
type RequestHandler func(typ, reqID, kind, assetID string) (assets, snapshot json.RawMessage, err error)

// ClosureHandler 落地 WAN 推来的平台级闭包（可用或停用修订）；失败不拆连接。
type ClosureHandler func(raw json.RawMessage) error

// Lease 是 WAN 下发的内容解包钥，只进内存。
type Lease struct {
	Key      []byte
	NotAfter time.Time
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
		return domain.ErrNotFound
	}
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
		if err := apply(State{Status: msg.Status, Revision: msg.Revision}); err != nil {
			return err
		}
	}
	return holdPings(ctx, c, apply, applyClient, applyClosure, applyTemplate, applyRetract, applyLease, onRequest)
}

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
				if err := apply(State{Status: msg.Status, Revision: msg.Revision}); err != nil {
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

func parseClientIntent(msg envelope) (ClientIntent, error) {
	cid, err := uuid.Parse(msg.ClientID)
	if err != nil {
		return ClientIntent{}, domain.ErrNotFound
	}
	return ClientIntent{
		Typ:       msg.Typ,
		ClientID:  cid,
		Name:      msg.ClientName,
		PublicKey: msg.ClientPublicKey,
		Revision:  msg.BindingRevision,
	}, nil
}

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

func helloPayload(factoryID uuid.UUID, nonce []byte) []byte {
	b := make([]byte, 0, 18+16+len(nonce))
	b = append(b, "wmesh-wan-hello-v1"...)
	b = append(b, factoryID[:]...)
	b = append(b, nonce...)
	return b
}

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
