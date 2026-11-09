package wanchannel

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Ticket 是 WAN 给现场的申请凭据，密钥原文只回一次。
type Ticket struct {
	RequestID  string    `json:"requestId"`  // 申请稳定身份
	Secret     string    `json:"secret"`     // 申请密钥原文
	VerifyCode string    `json:"verifyCode"` // 六位核对码
	ExpiresAt  time.Time `json:"expiresAt"`  // 申请到期时间
}

// Progress 是现场申请的进度。
type Progress struct {
	Status      string `json:"status"`      // pending / approved / rejected / expired
	FactoryName string `json:"factoryName"` // 工厂显示名，以批准时的为准
}

// 现场申请接入时交给 WAN 的内容，不含密码。
type applyReq struct {
	FactoryName        string `json:"factoryName"`        // 工厂显示名
	SALogin            string `json:"saLogin"`            // 初始超管登录名
	SADisplay          string `json:"saDisplay"`          // 初始超管显示名
	ServiceVersionName string `json:"serviceVersionName"` // 厂端服务版本名
}

// 只带申请密钥的请求。
type secretReq struct {
	Secret string `json:"secret"` // 申请密钥原文
}

// 申请找回时报的版本。
type saResetReq struct {
	ServiceVersionName string `json:"serviceVersionName"` // 厂端服务版本名
}

// ApplyOnboarding 向 WAN 提交新厂接入申请；密码不出厂。
func ApplyOnboarding(ctx context.Context, wanURL, name, saLogin, saDisplay, version string) (Ticket, error) {
	// 预备装 WAN 回的凭据。
	var out Ticket
	// 提交申请，失败不把零值当成功。
	if err := postJSON(ctx, wanURL, "/v1/channel/onboarding", applyReq{FactoryName: name, SALogin: saLogin, SADisplay: saDisplay, ServiceVersionName: version}, &out); err != nil {
		return Ticket{}, err
	}
	return out, nil
}

// OnboardingStatus 凭申请密钥查进度。
func OnboardingStatus(ctx context.Context, wanURL, secret string) (Progress, error) {
	// 预备装 WAN 回的进度。
	var out Progress
	// 查进度，失败不把零值当成功。
	if err := postJSON(ctx, wanURL, "/v1/channel/onboarding/status", secretReq{Secret: secret}, &out); err != nil {
		return Progress{}, err
	}
	return out, nil
}

// RequestSAReset 用厂钥签名向 WAN 申请找回初始超管密码。
func RequestSAReset(ctx context.Context, wanURL string, factoryID uuid.UUID, priv []byte, version string) (Ticket, error) {
	// 预备装 WAN 回的凭据。
	var out Ticket
	// 带厂钥签名提交，WAN 据此认厂。
	if err := postSigned(ctx, wanURL, "/v1/channel/sa-reset", saResetReq{ServiceVersionName: version}, &out, factoryID, priv); err != nil {
		return Ticket{}, err
	}
	return out, nil
}

// ConsumeSAReset 用厂钥签名用掉已批准的找回单，只成功一次。
func ConsumeSAReset(ctx context.Context, wanURL string, factoryID uuid.UUID, priv []byte, secret string) error {
	// 带厂钥签名用掉找回单。
	return postSigned(ctx, wanURL, "/v1/channel/sa-reset/consume", secretReq{Secret: secret}, nil, factoryID, priv)
}

// 带厂钥签名的 JSON POST。
func postSigned(ctx context.Context, wanURL, path string, body, dst any, factoryID uuid.UUID, priv []byte) error {
	// 签名头和拉取共用一套。
	p := &puller{factoryID: factoryID, priv: priv}
	// 发送前写上签名头。
	return postJSONWith(ctx, wanURL, path, body, dst, func(req *http.Request) { p.sign(req) })
}
