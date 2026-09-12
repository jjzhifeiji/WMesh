package store

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/domain"
)

// EnrollmentOffer 是建厂码对应的待认领身份，不含建厂码原文。
type EnrollmentOffer struct {
	FactoryID  uuid.UUID // 工厂稳定身份
	Name       string    // 工厂显示名
	SAPersonID uuid.UUID // 约定的初始超管身份，厂库必须用这个
	SALogin    string    // 初始超管登录名
	SADisplay  string    // 初始超管显示名
}

// LookupEnrollment 按建厂码哈希找出尚未认领的工厂；对不上或已认领都当无效。
func (s *Store) LookupEnrollment(ctx context.Context, tokenHash string) (EnrollmentOffer, error) {
	if tokenHash == "" {
		return EnrollmentOffer{}, domain.ErrInvalidEnrollment
	}
	var fac Factory
	if err := s.db.WithContext(ctx).First(&fac, "enrollment_token_hash = ?", tokenHash).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return EnrollmentOffer{}, domain.ErrInvalidEnrollment
		}
		return EnrollmentOffer{}, err
	}
	sa, err := s.InitialSuperAdmin(ctx, fac.ID)
	if err != nil {
		return EnrollmentOffer{}, err
	}
	return EnrollmentOffer{
		FactoryID:  fac.ID,
		Name:       fac.Name,
		SAPersonID: sa.PersonID,
		SALogin:    sa.LoginName,
		SADisplay:  sa.DisplayName,
	}, nil
}

// ConfirmEnrollment 登记厂签发公钥并作废建厂码；同钥再确认视为幂等。
func (s *Store) ConfirmEnrollment(ctx context.Context, factoryID uuid.UUID, publicKey []byte) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var fac Factory
		if err := tx.First(&fac, "id = ?", factoryID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		var existing FactoryPublicKey
		err := tx.First(&existing, "factory_id = ?", factoryID).Error
		switch {
		case err == nil:
			if !bytes.Equal(existing.PublicKey, publicKey) {
				return domain.ErrFactoryKeyExists
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
			row := FactoryPublicKey{FactoryID: factoryID, PublicKey: publicKey, CreatedAt: now}
			if err := tx.Create(&row).Error; err != nil {
				if domain.IsCheckViolation(err) {
					return domain.ErrInvalidKey
				}
				return err
			}
		default:
			return err
		}
		res := tx.Model(&Factory{}).Where("id = ?", factoryID).Updates(map[string]any{
			"enrollment_token_hash": nil,
			"enrolled_at":           now,
		})
		return res.Error
	})
}

// MarkChannelOnline 记下这条厂端通道刚连上；上一根的离线时间清掉。
func (s *Store) MarkChannelOnline(ctx context.Context, factoryID uuid.UUID) error {
	now := time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&Factory{}).Where("id = ?", factoryID).Updates(map[string]any{
		"channel_connected_at":    now,
		"channel_last_seen_at":    now,
		"channel_disconnected_at": nil,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// TouchChannel 刷新最近心跳；已经离线的行不改。
func (s *Store) TouchChannel(ctx context.Context, factoryID uuid.UUID) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Model(&Factory{}).
		Where("id = ? AND channel_connected_at IS NOT NULL", factoryID).
		Update("channel_last_seen_at", now).Error
}

// MarkChannelOffline 只在仍标记为在线时记下断开时间。
func (s *Store) MarkChannelOffline(ctx context.Context, factoryID uuid.UUID) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Model(&Factory{}).
		Where("id = ? AND channel_connected_at IS NOT NULL", factoryID).
		Updates(map[string]any{
			"channel_connected_at":    nil,
			"channel_disconnected_at": now,
			"channel_last_seen_at":    now,
		}).Error
}

// ResetChannelPresence WAN 重启时把残留的在线标成离线，避免幽灵在线。
func (s *Store) ResetChannelPresence(ctx context.Context) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Model(&Factory{}).
		Where("channel_connected_at IS NOT NULL").
		Updates(map[string]any{
			"channel_connected_at":    nil,
			"channel_disconnected_at": now,
		}).Error
}
