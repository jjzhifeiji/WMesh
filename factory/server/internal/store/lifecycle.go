package store

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"wmesh/factory/internal/platform/assetcode"
	"wmesh/factory/internal/platform/domain"
)

const (
	FactoryActive   = "active"   // 有效：可登录
	FactoryDisabled = "disabled" // WAN 停用
	FactoryRetired  = "retired"  // 已注销
)

// Lifecycle 是本厂治理状态，由 WAN 通道下发。
type Lifecycle struct {
	ID        int16     `gorm:"primaryKey" json:"-"`       // 全表一行
	Status    string    `gorm:"not null" json:"status"`    // active / disabled / retired
	Revision  int64     `gorm:"not null" json:"revision"`  // 已接受的 WAN 修订，只向前
	ShortCode string    `json:"shortCode,omitempty"`      // 本厂短码，认领后写入
	UpdatedAt time.Time `gorm:"not null" json:"updatedAt"` // 最近一次落地
}

func (Lifecycle) TableName() string { return "factory_lifecycle" }

// Lifecycle 读取本厂当前治理状态；没有行当作有效。
func (s *Store) Lifecycle(ctx context.Context) (Lifecycle, error) {
	var row Lifecycle
	if err := s.db.WithContext(ctx).First(&row, "id = ?", 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Lifecycle{Status: FactoryActive}, nil
		}
		return Lifecycle{}, err
	}
	return row, nil
}

// ApplyLifecycle 只接受更高修订；同修订幂等。
func (s *Store) ApplyLifecycle(ctx context.Context, status string, revision int64) (Lifecycle, error) {
	now := time.Now().UTC()
	var out Lifecycle
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&out, "id = ?", 1).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			out = Lifecycle{ID: 1, Status: FactoryActive}
		}
		// 低修订或同修订忽略，避免迟到的通道帧把连接打掉。
		if revision <= out.Revision {
			return nil
		}
		out = Lifecycle{ID: 1, Status: status, Revision: revision, ShortCode: out.ShortCode, UpdatedAt: now}
		if out.ShortCode == "" {
			return tx.Omit("ShortCode").Save(&out).Error
		}
		return tx.Save(&out).Error
	})
	if err != nil {
		return Lifecycle{}, err
	}
	return out, nil
}

// PutFactoryShortCode 写入本厂短码；已有则必须相同。
func (s *Store) PutFactoryShortCode(ctx context.Context, code string) error {
	if !assetcode.ValidFactoryOrigin(code) {
		return domain.ErrAssetCodeConflict
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row Lifecycle
		if err := tx.First(&row, "id = ?", 1).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			row = Lifecycle{ID: 1, Status: FactoryActive}
		}
		if row.ShortCode != "" && row.ShortCode != code {
			return domain.ErrAssetCodeConflict
		}
		row.ShortCode = code
		if row.UpdatedAt.IsZero() {
			row.UpdatedAt = time.Now().UTC()
		}
		return tx.Save(&row).Error
	})
}

// FactoryShortCode 取本厂短码；未齐则不得发号。
func (s *Store) FactoryShortCode(ctx context.Context) (string, error) {
	row, err := s.Lifecycle(ctx)
	if err != nil {
		return "", err
	}
	if row.ShortCode == "" {
		return "", domain.ErrAssetCodeMissing
	}
	return row.ShortCode, nil
}
