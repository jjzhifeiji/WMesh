package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/contentcrypt"
	"wmesh/global/internal/platform/domain"
)

type contentLeaseRow struct {
	FactoryID uuid.UUID `gorm:"type:uuid;primaryKey"` // 这家厂
	LeaseKey  []byte    `gorm:"type:bytea;not null"`   // 租约钥 L，不进审计
	NotAfter  time.Time `gorm:"not null"`              // WAN 钟到期
	RenewedAt time.Time `gorm:"not null"`              // 最近签发或续期
}

func (contentLeaseRow) TableName() string { return "content_leases" }

// ContentLease 是发给厂端的当前租约；Key 只走通道，不进审计。
type ContentLease struct {
	Key      []byte    // 32 字节 L
	NotAfter time.Time // WAN 钟
}

// IssueContentLease 签发或续期：同一把 L，窗口重新算 24 小时。
func (s *Store) IssueContentLease(ctx context.Context, factoryID uuid.UUID) (ContentLease, error) {
	if _, err := s.FactoryByID(ctx, factoryID); err != nil {
		return ContentLease{}, err
	}
	now := time.Now().UTC()
	notAfter := now.Add(contentcrypt.MaxLease)
	var row contentLeaseRow
	err := s.db.WithContext(ctx).First(&row, "factory_id = ?", factoryID).Error
	switch {
	case err == nil:
		// 续期不换 L，否则厂端解不开盘上 MK。
		row.NotAfter = notAfter
		row.RenewedAt = now
		if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
			return ContentLease{}, err
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		// 这家厂第一次要租约。
		key, err := contentcrypt.RandomKey()
		if err != nil {
			return ContentLease{}, err
		}
		row = contentLeaseRow{FactoryID: factoryID, LeaseKey: key, NotAfter: notAfter, RenewedAt: now}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return ContentLease{}, err
		}
	default:
		return ContentLease{}, err
	}
	return ContentLease{Key: append([]byte(nil), row.LeaseKey...), NotAfter: row.NotAfter}, nil
}

// LeaseKey 取出当前 L 的副本，用于过站封/解；没有租约则未找到。
func (s *Store) LeaseKey(ctx context.Context, factoryID uuid.UUID) ([]byte, error) {
	var row contentLeaseRow
	if err := s.db.WithContext(ctx).First(&row, "factory_id = ?", factoryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return append([]byte(nil), row.LeaseKey...), nil
}
