package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/platform/secret"
)

// Channel 管厂出站认领、验签握手和在线心跳。不判厂内业务对错。
type Channel struct{ *kernel }

// OfferEnroll 用建厂码换出待认领身份；码不对或已用完则拒绝。
func (s *Channel) OfferEnroll(ctx context.Context, enrollmentCode string) (EnrollmentOffer, error) {
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
	if err := s.audit(ctx, nil, nil, &offer.FactoryID, "enroll_factory", offer.FactoryID.String(), audit.Allow); err != nil {
		return EnrollmentOffer{}, err
	}
	return offer, nil
}

// ConfirmEnroll 厂端本地落库成功后登记签发公钥并作废建厂码。
func (s *Channel) ConfirmEnroll(ctx context.Context, factoryID uuid.UUID, publicKey []byte) error {
	if err := s.guardEnrollable(ctx, factoryID); err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "confirm_enroll", factoryID.String(), audit.Deny)
		return err
	}
	if err := s.store.ConfirmEnrollment(ctx, factoryID, publicKey); err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "confirm_enroll", factoryID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, nil, nil, &factoryID, "confirm_enroll", factoryID.String(), audit.Allow)
}

// RequireFactoryKey 已认领工厂才能做日常握手；名录里没了或已注销都当注销。
func (s *Channel) RequireFactoryKey(ctx context.Context, factoryID uuid.UUID) error {
	fac, err := s.store.FactoryByID(ctx, factoryID)
	if errors.Is(err, domain.ErrNotFound) {
		_ = s.audit(ctx, nil, nil, &factoryID, "hello_factory", factoryID.String(), audit.Deny)
		return domain.ErrFactoryRetired
	}
	if err != nil {
		return err
	}
	// 停用仍允许握手，才能再推启用；注销则拒绝。
	if fac.Status == FactoryRetired {
		_ = s.audit(ctx, nil, nil, &factoryID, "hello_factory", factoryID.String(), audit.Deny)
		return domain.ErrFactoryRetired
	}
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

// AcceptHello 用已登记厂钥验签 nonce；通过后才允许钉死这条连接。
func (s *Channel) AcceptHello(ctx context.Context, factoryID uuid.UUID, nonce, signature []byte) error {
	if len(nonce) < 16 {
		_ = s.audit(ctx, nil, nil, &factoryID, "hello_factory", factoryID.String(), audit.Deny)
		return domain.ErrUnauthorized
	}
	key, err := s.store.FactoryPublicKey(ctx, factoryID)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "hello_factory", factoryID.String(), audit.Deny)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrUnauthorized
		}
		return err
	}
	if !nodekey.Verify(key.PublicKey, channelHelloPayload(factoryID, nonce), signature) {
		_ = s.audit(ctx, nil, nil, &factoryID, "hello_factory", factoryID.String(), audit.Deny)
		return domain.ErrUnauthorized
	}
	return s.audit(ctx, nil, nil, &factoryID, "hello_factory", factoryID.String(), audit.Allow)
}

// MarkChannelOnline 名录标在线；心跳不另记审计。
func (s *Channel) MarkChannelOnline(ctx context.Context, factoryID uuid.UUID) error {
	if err := s.store.MarkChannelOnline(ctx, factoryID); err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "channel_up", factoryID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, nil, nil, &factoryID, "channel_up", factoryID.String(), audit.Allow)
}

// TouchChannel 刷新最近心跳；已离线则忽略。
func (s *Channel) TouchChannel(ctx context.Context, factoryID uuid.UUID) error {
	return s.store.TouchChannel(ctx, factoryID)
}

// MarkChannelOffline 名录标离线；只在仍显示在线时写断开时间。
func (s *Channel) MarkChannelOffline(ctx context.Context, factoryID uuid.UUID) error {
	if err := s.store.MarkChannelOffline(ctx, factoryID); err != nil {
		return err
	}
	return s.audit(ctx, nil, nil, &factoryID, "channel_down", factoryID.String(), audit.Allow)
}

// ResetChannelPresence WAN 进程起来时清掉上一轮残留的在线标记。
func (s *Channel) ResetChannelPresence(ctx context.Context) error {
	return s.store.ResetChannelPresence(ctx)
}

// channelHelloPayload 与厂端 wanchannel 签名原文必须字节一致。
func channelHelloPayload(factoryID uuid.UUID, nonce []byte) []byte {
	b := make([]byte, 0, 18+16+len(nonce))
	b = append(b, "wmesh-wan-hello-v1"...)
	b = append(b, factoryID[:]...)
	b = append(b, nonce...)
	return b
}
