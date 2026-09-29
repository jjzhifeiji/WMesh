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

// 一家厂当前的内容租约，钥不进审计。
type contentLeaseRow struct {
	FactoryID uuid.UUID `gorm:"type:uuid;primaryKey"` // 这家厂
	LeaseKey  []byte    `gorm:"type:bytea;not null"`  // 租约钥 L，不进审计
	NotAfter  time.Time `gorm:"not null"`             // WAN 钟到期
	RenewedAt time.Time `gorm:"not null"`             // 最近签发或续期
}

// 内容租约落这张表，钥不进审计。
func (contentLeaseRow) TableName() string { return "content_leases" }

// ContentLease 是发给厂端的当前租约；Key 只走通道，不进审计。
type ContentLease struct {
	Key      []byte    // 32 字节 L
	NotAfter time.Time // WAN 钟
}

// IssueContentLease 签发或续期：同一把 L，窗口重新算 24 小时。
func (s *Store) IssueContentLease(ctx context.Context, factoryID uuid.UUID) (ContentLease, error) {
	// 厂不在名录就不发租约，避免给空厂配钥。
	if _, err := s.FactoryByID(ctx, factoryID); err != nil {
		return ContentLease{}, err
	}
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 从现在起算租约窗口，续期也按这一下重算。
	notAfter := now.Add(contentcrypt.MaxLease)
	// 准备接住库里的那一行，没有再另作处理。
	var row contentLeaseRow
	// 按厂取当前租约，没有则下面新发。
	err := s.db.WithContext(ctx).First(&row, "factory_id = ?", factoryID).Error
	// 有租约就续窗口，没有就新发一把钥。
	switch {
	// 已经有租约，只延长窗口不换钥。
	case err == nil:
		// 续期不换 L，否则厂端解不开盘上 MK。
		row.NotAfter = notAfter
		// 记下这次续期的时刻。
		row.RenewedAt = now
		// 写回失败就停，刚才改过的字段不算数。
		if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
			return ContentLease{}, err
		}
	// 这家厂还没有租约，下面新发一把。
	case errors.Is(err, gorm.ErrRecordNotFound):
		// 这家厂第一次要租约。
		key, err := contentcrypt.RandomKey()
		// 钥生成失败就停，不能写下空租约。
		if err != nil {
			return ContentLease{}, err
		}
		// 这家厂第一次配租约，钥和窗口一起写下。
		row = contentLeaseRow{FactoryID: factoryID, LeaseKey: key, NotAfter: notAfter, RenewedAt: now}
		// 这一行没写进去就停，不能当成已经落库。
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return ContentLease{}, err
		}
	// 读租约失败就停，不当成还没有。
	default:
		return ContentLease{}, err
	}
	// 交回钥的副本和到期时刻，不把库行交出去。
	return ContentLease{Key: append([]byte(nil), row.LeaseKey...), NotAfter: row.NotAfter}, nil
}

// LeaseKey 取出当前 L 的副本，用于过站封/解；没有租约则未找到。
func (s *Store) LeaseKey(ctx context.Context, factoryID uuid.UUID) ([]byte, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row contentLeaseRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "factory_id = ?", factoryID).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	// 交回钥的副本，调用方改不到库里那一份。
	return append([]byte(nil), row.LeaseKey...), nil
}
