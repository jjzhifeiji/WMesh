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

// 认领第一步只带建厂码，确认前仍可重试。
type enrollReq struct {
	EnrollmentCode string `json:"enrollmentCode"` // 一次性建厂码
}

// 平台回的待认领身份，不含建厂码本身。
type enrollResp struct {
	FactoryID        string `json:"factoryId"`        // 工厂稳定身份
	Name             string `json:"name"`             // 工厂显示名
	FactoryShortCode string `json:"factoryShortCode"` // 本厂短码
	SAPersonID       string `json:"saPersonId"`       // 约定的初始超管身份
	SALogin          string `json:"saLogin"`          // 初始超管登录名
	SADisplay        string `json:"saDisplay"`        // 初始超管显示名
}

// 确认认领时交公钥，建厂码随之作废。
type claimReq struct {
	EnrollmentCode   string `json:"enrollmentCode"`   // 一次性建厂码
	FactoryPublicKey []byte `json:"factoryPublicKey"` // 本厂签发公钥
}

// Enroll 用建厂码向 WAN 要身份；确认前建厂码仍可重试。
func Enroll(ctx context.Context, wanURL, enrollmentCode string) (Offer, error) {
	// 预备装平台回的身份，失败时不把零值当成功。
	var out enrollResp
	// 建厂码换不到身份就交回，确认前仍可重试。
	if err := postJSON(ctx, wanURL, "/v1/channel/enroll", enrollReq{EnrollmentCode: enrollmentCode}, &out); err != nil {
		return Offer{}, err
	}
	// 工厂身份不合法就当建厂码无效。
	fid, err := uuid.Parse(out.FactoryID)
	// 坏的工厂编号拒绝，避免落错厂。
	if err != nil {
		return Offer{}, domain.ErrInvalidEnrollment
	}
	// 超管身份不合法就当建厂码无效。
	pid, err := uuid.Parse(out.SAPersonID)
	// 坏的超管编号拒绝，避免落错账号。
	if err != nil {
		return Offer{}, domain.ErrInvalidEnrollment
	}
	// 把平台回的身份交回，确认前建厂码仍可重试。
	return Offer{FactoryID: fid, Name: out.Name, ShortCode: out.FactoryShortCode, SAPersonID: pid, SALogin: out.SALogin, SADisplay: out.SADisplay}, nil
}

// Confirm 把本厂签发公钥交给 WAN，作废建厂码。
func Confirm(ctx context.Context, wanURL, enrollmentCode string, publicKey []byte) error {
	// 交公钥并作废建厂码，失败则认领未完成。
	return postJSON(ctx, wanURL, "/v1/channel/claim", claimReq{EnrollmentCode: enrollmentCode, FactoryPublicKey: publicKey}, nil)
}

// State 是 WAN 下发的工厂治理状态。
type State struct {
	Status    string // 工厂治理状态：active / disabled / retired
	Revision  int64  // 治理修订，厂端只向前
	ShortCode string // 本厂短码
	Name      string // 本厂显示名
}

// ClientIntent 是 WAN 推来的设备分配或作废。
type ClientIntent struct {
	Typ          string    // client_bind / client_void
	ClientID     uuid.UUID // 固定识别号
	Name         string    // 给人看的设备名
	ShortCode    string    // Client 短码
	DeviceSerial string    // 机械臂识别号；未填为空
	PublicKey    []byte    // 本机公钥，可空
	Revision     int64     // 绑定修订
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
	// 先规范化平台根地址，空的或非网页就拒绝。
	root, err := wanRoot(wanURL)
	// 地址不可用就交回，这次认领不做。
	if err != nil {
		return err
	}
	// 编出正文，编不出就当平台不可达。
	raw, err := json.Marshal(body)
	// 编不出就拒绝，避免发出残缺认领包。
	if err != nil {
		return domain.ErrWANUnreachable
	}
	// 带上取消来组请求，超时才能停掉。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, root+path, bytes.NewReader(raw))
	// 请求组不出来就当不可达，不发出去。
	if err != nil {
		return domain.ErrWANUnreachable
	}
	// 标明是结构化正文，平台才按字段解析。
	req.Header.Set("Content-Type", "application/json")
	// 送出认领请求，网络失败就当平台不可达。
	res, err := enrollClient.Do(req)
	// 送不出去就当不可达，调用方可以再试。
	if err != nil {
		return domain.ErrWANUnreachable
	}
	// 读完就关掉响应，连接才能复用。
	defer res.Body.Close()
	// 只读到上限，异常页面不能灌满内存。
	payload, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	// 响应读不完就当不可达，避免用半截正文。
	if err != nil {
		return domain.ErrWANUnreachable
	}
	// 平台拒绝就把状态收成业务错误。
	if res.StatusCode >= 300 {
		// 按状态和英文原因交回，调用方不要当成功。
		return mapHTTPStatus(res.StatusCode, payload)
	}
	// 调用方不要正文就到此为止。
	if dst == nil {
		return nil
	}
	// 正文解不开就当不可达，避免用残缺身份。
	if err := json.Unmarshal(payload, dst); err != nil {
		return domain.ErrWANUnreachable
	}
	return nil
}

// 认领只走 HTTP(S) 根地址，不改写路径。
func wanRoot(raw string) (string, error) {
	// 去掉首尾空白和斜杠，避免拼出双斜杠。
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	// 空地址当不可达，避免去拨意外端口。
	if raw == "" {
		return "", domain.ErrWANUnreachable
	}
	// 解析地址，没有主机就拒绝。
	u, err := url.Parse(raw)
	// 解析失败或没有主机就当不可达。
	if err != nil || u.Host == "" {
		return "", domain.ErrWANUnreachable
	}
	// 只接受网页地址，别的协议当不可达。
	switch u.Scheme {
	// 网页协议才允许认领。
	case "http", "https":
	// 别的协议拒绝，避免认领走错通道。
	default:
		return "", domain.ErrWANUnreachable
	}
	// 丢掉查询参数，认领只走根地址。
	u.RawQuery = ""
	// 丢掉片段，避免把锚点拼进请求。
	u.Fragment = ""
	// 交回规范化的根地址。
	return strings.TrimRight(u.String(), "/"), nil
}

// 把 WAN 英文错误收回本侧业务错误。
func mapChannelError(msg string) error {
	// 把平台英文错误收成业务错误，调用方不用猜字符串。
	switch msg {
	// 建厂码无效收成认领错误，可以换码再试。
	case domain.ErrInvalidEnrollment.Error():
		return domain.ErrInvalidEnrollment
	// 厂已停用就拒绝认领，不要当成网络故障。
	case domain.ErrFactoryDisabled.Error():
		return domain.ErrFactoryDisabled
	// 厂已注销就拒绝，通道不应再重连。
	case domain.ErrFactoryRetired.Error():
		return domain.ErrFactoryRetired
	// 公钥已登记就当钥已存在，不能覆盖。
	case "factory public key already registered":
		return domain.ErrSigningKeyExists
	// 钥不合法就拒绝，避免装上坏钥。
	case domain.ErrInvalidKey.Error():
		return domain.ErrInvalidKey
	// 其余原因单独处理，空的当不可达。
	default:
		// 没有英文原因就当平台不可达。
		if msg == "" {
			return domain.ErrWANUnreachable
		}
		// 其余英文原因原样交回，便于对照平台。
		return errors.New(msg)
	}
}
