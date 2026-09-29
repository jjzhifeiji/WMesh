package store

import (
	"context"
	"errors"
	"strings"
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
	ShortCode string    `json:"shortCode,omitempty"`       // 本厂短码，认领后写入
	Name      string    `json:"name,omitempty"`            // 本厂显示名，认领或握手写入
	UpdatedAt time.Time `gorm:"not null" json:"updatedAt"` // 最近一次落地
}

// 指定落库表名，避免查询时按类型名去猜。
func (Lifecycle) TableName() string { return "factory_lifecycle" }

// Lifecycle 读取本厂当前治理状态；没有行当作有效。
func (s *Store) Lifecycle(ctx context.Context) (Lifecycle, error) {
	// 准备承接查到的治理状态。
	var row Lifecycle
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", 1).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Lifecycle{Status: FactoryActive}, nil
		}
		return Lifecycle{}, err
	}
	return row, nil
}

// ApplyLifecycle 只接受更高修订；同修订幂等。
func (s *Store) ApplyLifecycle(ctx context.Context, status string, revision int64) (Lifecycle, error) {
	// 取当前时刻，时间列和租约用同一个时钟。
	now := time.Now().UTC()
	// 准备承接查到的治理状态。
	var out Lifecycle
	// 放进同一事务，中途失败就整单回滚。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 按条件取一行失败就停，避免带着错误继续。
		if err := tx.First(&out, "id = ?", 1).Error; err != nil {
			// 不是缺行的库错误要原样交回。
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			// 拼上治理行，空着的名称和短码先留住。
			out = Lifecycle{ID: 1, Status: FactoryActive}
		}
		// 低修订或同修订忽略，避免迟到的通道帧把连接打掉。
		if revision <= out.Revision {
			return nil
		}
		// 拼上治理行，空着的名称和短码先留住。
		out = Lifecycle{ID: 1, Status: status, Revision: revision, ShortCode: out.ShortCode, Name: out.Name, UpdatedAt: now}
		// 按需要预留位置，避免后面反复扩容。
		omits := make([]string, 0, 2)
		// 短码还空着就先别写那一列，避免空串落库。
		if out.ShortCode == "" {
			// 收进结果，保持原来的先后顺序。
			omits = append(omits, "ShortCode")
		}
		// 名称还空着就先别写那一列。
		if out.Name == "" {
			// 收进结果，保持原来的先后顺序。
			omits = append(omits, "Name")
		}
		// 有列要跳过就排除它们再保存。
		if len(omits) > 0 {
			// 空着的列跳过再保存，避免写成空串。
			return tx.Omit(omits...).Save(&out).Error
		}
		// 整行保存，失败就让事务回滚。
		return tx.Save(&out).Error
	})
	// 出错就停，避免把失败当成已经完成。
	if err != nil {
		return Lifecycle{}, err
	}
	return out, nil
}

// PutFactoryShortCode 写入本厂短码；已有则必须相同。
func (s *Store) PutFactoryShortCode(ctx context.Context, code string) error {
	// 短码格式不对就拒绝，否则发出来的号对不上。
	if !assetcode.ValidFactoryOrigin(code) {
		return domain.ErrAssetCodeConflict
	}
	// 放进同一事务，中途失败就整单回滚。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 准备承接查到的治理状态。
		var row Lifecycle
		// 按条件取一行失败就停，避免带着错误继续。
		if err := tx.First(&row, "id = ?", 1).Error; err != nil {
			// 不是缺行的库错误要原样交回。
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			// 拼上治理行，空着的名称和短码先留住。
			row = Lifecycle{ID: 1, Status: FactoryActive}
		}
		// 已有不同短码就拒绝，一家厂只能留一个。
		if row.ShortCode != "" && row.ShortCode != code {
			return domain.ErrAssetCodeConflict
		}
		// 把改好的值放进内存行，随后随保存写入。
		row.ShortCode = code
		// 还没有更新时间就补上现在，避免零时间。
		if row.UpdatedAt.IsZero() {
			// 取当前时刻，时间列和租约用同一个时钟。
			row.UpdatedAt = time.Now().UTC()
		}
		// 整行保存，失败就让事务回滚。
		return tx.Save(&row).Error
	})
}

// FactoryShortCode 取本厂短码；未齐则不得发号。
func (s *Store) FactoryShortCode(ctx context.Context) (string, error) {
	// 先读本厂短码，结果留给紧跟着的判断。
	row, err := s.Lifecycle(ctx)
	// 出错就停，避免把失败当成已经完成。
	if err != nil {
		return "", err
	}
	// 短码还空着就先别写那一列，避免空串落库。
	if row.ShortCode == "" {
		return "", domain.ErrAssetCodeMissing
	}
	return row.ShortCode, nil
}

// PutFactoryName 写入本厂显示名；空串忽略，已有则覆盖。
func (s *Store) PutFactoryName(ctx context.Context, name string) error {
	// 去掉首尾空白，纯空白不当有效内容。
	name = strings.TrimSpace(name)
	// 空名字不落库，避免出现空白项。
	if name == "" {
		return nil
	}
	// 放进同一事务，中途失败就整单回滚。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 准备承接查到的治理状态。
		var row Lifecycle
		// 按条件取一行失败就停，避免带着错误继续。
		if err := tx.First(&row, "id = ?", 1).Error; err != nil {
			// 不是缺行的库错误要原样交回。
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			// 拼上治理行，空着的名称和短码先留住。
			row = Lifecycle{ID: 1, Status: FactoryActive}
		}
		// 把改好的值放进内存行，随后随保存写入。
		row.Name = name
		// 还没有更新时间就补上现在，避免零时间。
		if row.UpdatedAt.IsZero() {
			// 取当前时刻，时间列和租约用同一个时钟。
			row.UpdatedAt = time.Now().UTC()
		}
		// 短码还空着就先别写那一列，避免空串落库。
		if row.ShortCode == "" {
			// 空着的列跳过再保存，避免写成空串。
			return tx.Omit("ShortCode").Save(&row).Error
		}
		// 整行保存，失败就让事务回滚。
		return tx.Save(&row).Error
	})
}
