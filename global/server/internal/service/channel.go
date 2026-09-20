package service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/secret"
)

// Channel 管厂出站认领、MQTT 在线和 HTTPS 拉正文。不判厂内业务对错。
type Channel struct{ *kernel }

// OfferEnroll 用建厂码换出待认领身份；码不对或已用完则拒绝。
func (s *Channel) OfferEnroll(ctx context.Context, enrollmentCode string) (EnrollmentOffer, error) {
	// 用建厂码哈希换待认领身份。码不对、已用完或工厂不可认领都记拒绝。
	offer, err := s.store.LookupEnrollment(ctx, secret.TokenHash(enrollmentCode))
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "enroll_factory", "channel", audit.Deny)
		if errors.Is(err, domain.ErrInvalidEnrollment) {
			return EnrollmentOffer{}, domain.ErrInvalidEnrollment
		}
		return EnrollmentOffer{}, err
	}
	if err := s.guardEnrollable(ctx, offer.FactoryID); err != nil {
		_ = s.audit(ctx, nil, nil, &offer.FactoryID, "enroll_factory", offer.FactoryID.String(), audit.Deny)
		return EnrollmentOffer{}, err
	}
	// 出示成功才记允许。
	if err := s.audit(ctx, nil, nil, &offer.FactoryID, "enroll_factory", offer.FactoryID.String(), audit.Allow); err != nil {
		return EnrollmentOffer{}, err
	}
	return offer, nil
}

// ConfirmEnroll 厂端本地落库成功后登记签发公钥并作废建厂码。
func (s *Channel) ConfirmEnroll(ctx context.Context, factoryID uuid.UUID, publicKey []byte) error {
	// 停用、注销或落库失败都记拒绝。
	if err := s.guardEnrollable(ctx, factoryID); err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "confirm_enroll", factoryID.String(), audit.Deny)
		return err
	}
	// 登记签发公钥并作废建厂码。
	if err := s.store.ConfirmEnrollment(ctx, factoryID, publicKey); err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "confirm_enroll", factoryID.String(), audit.Deny)
		return err
	}
	// 认领成功才记允许。
	return s.audit(ctx, nil, nil, &factoryID, "confirm_enroll", factoryID.String(), audit.Allow)
}

// RequireFactoryKey 已认领且未注销；名录里没了或已注销都当注销。
func (s *Channel) RequireFactoryKey(ctx context.Context, factoryID uuid.UUID) error {
	// 已认领工厂才算有效；名录没了或已注销都拒绝。
	fac, err := s.store.FactoryByID(ctx, factoryID)
	if errors.Is(err, domain.ErrNotFound) {
		_ = s.audit(ctx, nil, nil, &factoryID, "hello_factory", factoryID.String(), audit.Deny)
		return domain.ErrFactoryRetired
	}
	if err != nil {
		return err
	}
	// 停用仍算已认领，才能再推启用；注销则拒绝。
	if fac.Status == FactoryRetired {
		_ = s.audit(ctx, nil, nil, &factoryID, "hello_factory", factoryID.String(), audit.Deny)
		return domain.ErrFactoryRetired
	}
	// 必须已有签发公钥。
	_, err = s.store.FactoryPublicKey(ctx, factoryID)
	if errors.Is(err, domain.ErrNotFound) {
		_ = s.audit(ctx, nil, nil, &factoryID, "hello_factory", factoryID.String(), audit.Deny)
		return domain.ErrUnauthorized
	}
	if err != nil {
		return err
	}
	return nil
}

// guardEnrollable 停用或注销的工厂不能再认领。
func (s *Channel) guardEnrollable(ctx context.Context, factoryID uuid.UUID) error {
	fac, err := s.store.FactoryByID(ctx, factoryID)
	if err != nil {
		return err
	}
	switch fac.Status {
	case FactoryDisabled:
		return domain.ErrFactoryDisabled
	case FactoryRetired:
		return domain.ErrFactoryRetired
	default:
		return nil
	}
}

// MarkChannelOnline 名录标在线；心跳不另记审计。
func (s *Channel) MarkChannelOnline(ctx context.Context, factoryID uuid.UUID) error {
	// 名录标在线。
	if err := s.store.MarkChannelOnline(ctx, factoryID); err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "channel_up", factoryID.String(), audit.Deny)
		return err
	}
	// 上线成功才记允许。
	return s.audit(ctx, nil, nil, &factoryID, "channel_up", factoryID.String(), audit.Allow)
}

// TouchChannel 刷新最近心跳；已离线则忽略。
func (s *Channel) TouchChannel(ctx context.Context, factoryID uuid.UUID) error {
	return s.store.TouchChannel(ctx, factoryID)
}

// MarkChannelOffline 名录标离线；只在仍显示在线时写断开时间。
func (s *Channel) MarkChannelOffline(ctx context.Context, factoryID uuid.UUID) error {
	// 名录标离线。
	if err := s.store.MarkChannelOffline(ctx, factoryID); err != nil {
		return err
	}
	// 断开成功才记允许。
	return s.audit(ctx, nil, nil, &factoryID, "channel_down", factoryID.String(), audit.Allow)
}

// ResetChannelPresence WAN 进程起来时清掉上一轮残留的在线标记。
func (s *Channel) ResetChannelPresence(ctx context.Context) error {
	return s.store.ResetChannelPresence(ctx)
}

// ReportFactoryRelease 记下该厂自报的前端和服务版本；心跳不上审计。
func (s *Channel) ReportFactoryRelease(ctx context.Context, factoryID uuid.UUID, webCode int64, webName string, svcCode int64, svcName string) error {
	if webCode < 1 || svcCode < 1 || strings.TrimSpace(webName) == "" || strings.TrimSpace(svcName) == "" {
		return domain.ErrInvalidName
	}
	// 把厂端自报版本写入名录，离线展示时再清掉。
	return s.store.PutFactoryRelease(ctx, factoryID, webCode, strings.TrimSpace(webName), svcCode, strings.TrimSpace(svcName))
}

// IssueContentLease 给该厂签发或续期内容租约；同一把 L，窗口最多 24 小时。
func (s *Channel) IssueContentLease(ctx context.Context, factoryID uuid.UUID) (ContentLease, error) {
	// 同一把 L 续期，窗口重新算 24 小时。
	lease, err := s.store.IssueContentLease(ctx, factoryID)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "content_lease", factoryID.String(), audit.Deny)
		return ContentLease{}, err
	}
	// 租约成功才记允许。
	if err := s.audit(ctx, nil, nil, &factoryID, "content_lease", factoryID.String(), audit.Allow); err != nil {
		return ContentLease{}, err
	}
	return lease, nil
}
