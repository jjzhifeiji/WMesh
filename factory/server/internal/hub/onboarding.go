package hub

import (
	"context"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/service"
	"wmesh/factory/internal/wanchannel"
)

// ApplyOnboarding 把现场的新厂接入申请转给 WAN；批准前本机不建厂库。
func (h *Hub) ApplyOnboarding(ctx context.Context, wanURL, name, saLogin, saDisplay, version string) (wanchannel.Ticket, error) {
	// 没配平台地址就申请不了。
	if wanURL == "" {
		return wanchannel.Ticket{}, domain.ErrWANUnreachable
	}
	// 转给 WAN，本机不留申请状态。
	return wanchannel.ApplyOnboarding(ctx, wanURL, name, saLogin, saDisplay, version)
}

// OnboardingStatus 凭申请密钥向 WAN 查进度。
func (h *Hub) OnboardingStatus(ctx context.Context, wanURL, secret string) (wanchannel.Progress, error) {
	// 没配平台地址就查不了。
	if wanURL == "" {
		return wanchannel.Progress{}, domain.ErrWANUnreachable
	}
	// 转给 WAN，进度以云端为准。
	return wanchannel.OnboardingStatus(ctx, wanURL, secret)
}

// RequestSAReset 用本厂签发钥向 WAN 申请找回初始超管密码。
func (h *Hub) RequestSAReset(ctx context.Context, wanURL string, factoryID uuid.UUID, version string) (wanchannel.Ticket, error) {
	// 没配平台地址就申请不了。
	if wanURL == "" {
		return wanchannel.Ticket{}, domain.ErrWANUnreachable
	}
	// 打开这家厂，不存在就交回。
	svc, err := h.Service(ctx, factoryID)
	// 厂库打不开就停。
	if err != nil {
		return wanchannel.Ticket{}, err
	}
	// 初始超管已停用就不发起，发起了也用不上。
	if err := svc.Auth.RequireInitialActive(ctx); err != nil {
		return wanchannel.Ticket{}, err
	}
	// 读本厂签发钥，WAN 靠它认厂。
	key, err := svc.Store().SigningKey(ctx)
	// 没有钥就发不出去。
	if err != nil {
		return wanchannel.Ticket{}, err
	}
	// 带厂钥签名提交。
	return wanchannel.RequestSAReset(ctx, wanURL, factoryID, key.PrivateKey, version)
}

// CompleteSAReset 用掉 WAN 批准的找回单并设新密码；交回初始超管登录名。
func (h *Hub) CompleteSAReset(ctx context.Context, wanURL string, factoryID uuid.UUID, secret, password string) (string, error) {
	// 没配平台地址就用不了找回单。
	if wanURL == "" {
		return "", domain.ErrWANUnreachable
	}
	// 口令太短先拒绝，找回单不白耗。
	if err := service.CheckPassword(password); err != nil {
		return "", err
	}
	// 打开这家厂，不存在就交回。
	svc, err := h.Service(ctx, factoryID)
	// 厂库打不开就停。
	if err != nil {
		return "", err
	}
	// 初始超管已停用就不用掉找回单。
	if err := svc.Auth.RequireInitialActive(ctx); err != nil {
		return "", err
	}
	// 读本厂签发钥，用单要厂钥签名。
	key, err := svc.Store().SigningKey(ctx)
	// 没有钥就发不出去。
	if err != nil {
		return "", err
	}
	// 先用掉找回单，WAN 保证只成功一次；没批准或已用过都在这里拒绝。
	if err := wanchannel.ConsumeSAReset(ctx, wanURL, factoryID, key.PrivateKey, secret); err != nil {
		return "", err
	}
	// 云端确认后才改本地密码。
	acc, err := svc.Auth.ResetInitialPassword(ctx, password)
	// 改不成就交回，现场需要重新申请。
	if err != nil {
		return "", err
	}
	return acc.LoginName, nil
}
