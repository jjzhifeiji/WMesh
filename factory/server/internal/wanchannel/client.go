// Package wanchannel 厂出站连 WAN：HTTPS 认领交钥，日常 MQTT 指令加 HTTPS 拉正文。
package wanchannel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
)

var enrollClient = &http.Client{Timeout: 30 * time.Second} // 认领两步都短，不跟拉包共用长超时

// Offer 是 WAN 给出的待认领身份，不含建厂码。
type Offer struct {
	FactoryID  uuid.UUID // 工厂稳定身份
	Name       string    // 工厂显示名
	ShortCode  string    // 本厂短码
	SAPersonID uuid.UUID // 约定的初始超管身份
	SALogin    string    // 初始超管登录名
	SADisplay  string    // 初始超管显示名
}

type enrollReq struct {
	EnrollmentCode string `json:"enrollmentCode"` // 一次性建厂码
}

type enrollResp struct {
	FactoryID        string `json:"factoryId"`        // 工厂稳定身份
	Name             string `json:"name"`             // 工厂显示名
	FactoryShortCode string `json:"factoryShortCode"` // 本厂短码
	SAPersonID       string `json:"saPersonId"`       // 约定的初始超管身份
	SALogin          string `json:"saLogin"`          // 初始超管登录名
	SADisplay        string `json:"saDisplay"`        // 初始超管显示名
}

type claimReq struct {
	EnrollmentCode   string `json:"enrollmentCode"`   // 一次性建厂码
	FactoryPublicKey []byte `json:"factoryPublicKey"` // 本厂签发公钥
}

// Enroll 用建厂码向 WAN 要身份；确认前建厂码仍可重试。
func Enroll(ctx context.Context, wanURL, enrollmentCode string) (Offer, error) {
	var out enrollResp
	if err := postJSON(ctx, wanURL, "/v1/channel/enroll", enrollReq{EnrollmentCode: enrollmentCode}, &out); err != nil {
		return Offer{}, err
	}
	fid, err := uuid.Parse(out.FactoryID)
	if err != nil {
		return Offer{}, domain.ErrInvalidEnrollment
	}
	pid, err := uuid.Parse(out.SAPersonID)
	if err != nil {
		return Offer{}, domain.ErrInvalidEnrollment
	}
	return Offer{FactoryID: fid, Name: out.Name, ShortCode: out.FactoryShortCode, SAPersonID: pid, SALogin: out.SALogin, SADisplay: out.SADisplay}, nil
}

// Confirm 把本厂签发公钥交给 WAN，作废建厂码。
func Confirm(ctx context.Context, wanURL, enrollmentCode string, publicKey []byte) error {
	return postJSON(ctx, wanURL, "/v1/channel/claim", claimReq{EnrollmentCode: enrollmentCode, FactoryPublicKey: publicKey}, nil)
}

// State 是 WAN 下发的工厂治理状态。
type State struct {
	Status    string // 工厂治理状态：active / disabled / retired
	Revision  int64  // 治理修订，厂端只向前
	ShortCode string // 本厂短码
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

// SyncRequest 是厂端向 WAN 要当前快照的控制面请求。
type SyncRequest struct {
	Typ  string // sync_closures / sync_templates
	Kind string // process / project；空则该类型全量补送
}

// 无厂钥签名的 JSON POST，只给认领两步用。
func postJSON(ctx context.Context, wanURL, path string, body, dst any) error {
	root, err := wanRoot(wanURL)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return domain.ErrWANUnreachable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, root+path, bytes.NewReader(raw))
	if err != nil {
		return domain.ErrWANUnreachable
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := enrollClient.Do(req)
	if err != nil {
		return domain.ErrWANUnreachable
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return domain.ErrWANUnreachable
	}
	if res.StatusCode >= 300 {
		return mapHTTPStatus(res.StatusCode, payload)
	}
	if dst == nil {
		return nil
	}
	if err := json.Unmarshal(payload, dst); err != nil {
		return domain.ErrWANUnreachable
	}
	return nil
}

// 认领只走 HTTP(S) 根地址，不改写路径。
func wanRoot(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", domain.ErrWANUnreachable
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", domain.ErrWANUnreachable
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return "", domain.ErrWANUnreachable
	}
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
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
