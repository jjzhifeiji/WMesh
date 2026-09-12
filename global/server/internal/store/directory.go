package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/domain"
)

const (
	FactoryActive   = "active"   // 有效：可认领、可登录
	FactoryDisabled = "disabled" // 停用：可再启用
	FactoryRetired  = "retired"  // 已注销：不能再启用
)

// Factory 是 WAN 名录里的一家工厂，不含厂内组织或普通账号。
type Factory struct {
	ID                    uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`    // 工厂稳定身份，也用来选厂库
	Name                  string     `gorm:"not null" json:"name"`              // 工厂显示名，不当身份
	Status                string     `gorm:"not null" json:"status"`            // 治理状态：active / disabled / retired
	LifecycleRevision     int64      `gorm:"not null" json:"lifecycleRevision"` // 厂端只接受更高修订
	StatusChangedAt       *time.Time `json:"statusChangedAt,omitempty"`         // 最近一次停用、启用或注销
	EnrollmentTokenHash   *string    `json:"-"`                                 // 一次性建厂码哈希，认领后清空
	EnrolledAt            *time.Time `json:"enrolledAt,omitempty"`              // 厂端认领成功时间；未认领为空
	ChannelConnectedAt    *time.Time `json:"channelConnectedAt,omitempty"`      // 当前这条 WSS 连上的时间；空表示离线
	ChannelLastSeenAt     *time.Time `json:"channelLastSeenAt,omitempty"`       // 最近一次心跳或握手
	ChannelDisconnectedAt *time.Time `json:"channelDisconnectedAt,omitempty"`   // 最近一次断开；在线时为空
	ChannelOnline         bool       `gorm:"-" json:"channelOnline"`            // 当前有没有钉死的厂端通道
	CreatedAt             time.Time  `gorm:"not null" json:"createdAt"`         // 名录入库时间
}

func (f *Factory) fillPresence() { // 有当前连接时间才算在线，不另存列
	f.ChannelOnline = f.ChannelConnectedAt != nil
}

func (Factory) TableName() string { return "factories" }

// InitialSuperAdmin 是交付对账用的初始超管身份，不含日常密码。
type InitialSuperAdmin struct {
	FactoryID   uuid.UUID `gorm:"type:uuid;primaryKey" json:"factoryId"` // 一厂只能绑一名初始超管
	PersonID    uuid.UUID `gorm:"type:uuid;not null" json:"personId"`    // 落在目标厂库里的账号身份
	LoginName   string    `gorm:"not null" json:"loginName"`             // 交付时的登录名，不是秘密
	DisplayName string    `gorm:"not null" json:"displayName"`           // 交付时显示名，不是秘密
	CreatedAt   time.Time `gorm:"not null" json:"createdAt"`             // 对账记录写入时间
}

func (InitialSuperAdmin) TableName() string { return "initial_super_admins" }

// RegisterFactory 用调用方给的稳定身份写入名录，并在同一事务绑定初始超管与建厂码哈希，不存密码。
func (s *Store) RegisterFactory(ctx context.Context, factoryID uuid.UUID, name string, personID uuid.UUID, saLogin, saDisplay, enrollHash string) (Factory, error) {
	now := time.Now().UTC()
	hash := enrollHash
	fac := Factory{ID: factoryID, Name: name, Status: FactoryActive, EnrollmentTokenHash: &hash, CreatedAt: now}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&fac).Error; err != nil {
			return err
		}
		return bindInitialSuperAdmin(tx, factoryID, personID, saLogin, saDisplay, now)
	})
	if err != nil {
		return Factory{}, err
	}
	return fac, nil
}

// BindInitialSuperAdmin 记下该厂初始超管身份与登录名；一厂只能绑一名。
func (s *Store) BindInitialSuperAdmin(ctx context.Context, factoryID, personID uuid.UUID, loginName, displayName string) error {
	return bindInitialSuperAdmin(s.db.WithContext(ctx), factoryID, personID, loginName, displayName, time.Now().UTC())
}

func bindInitialSuperAdmin(tx *gorm.DB, factoryID, personID uuid.UUID, loginName, displayName string, at time.Time) error {
	row := InitialSuperAdmin{FactoryID: factoryID, PersonID: personID, LoginName: loginName, DisplayName: displayName, CreatedAt: at}
	if err := tx.Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return domain.ErrInitialSAExists
		}
		return err
	}
	return nil
}

func (s *Store) FactoryByID(ctx context.Context, factoryID uuid.UUID) (Factory, error) {
	var row Factory
	if err := s.db.WithContext(ctx).First(&row, "id = ?", factoryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Factory{}, domain.ErrNotFound
		}
		return Factory{}, err
	}
	row.fillPresence()
	return row, nil
}

// ListFactories 列出全部工厂名录行，按创建时间从新到旧。
func (s *Store) ListFactories(ctx context.Context) ([]Factory, error) {
	rows := []Factory{}
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error
	for i := range rows {
		rows[i].fillPresence()
	}
	return rows, err
}

// SetFactoryStatus 写入治理状态并升高修订；相同状态不升修订。
func (s *Store) SetFactoryStatus(ctx context.Context, factoryID uuid.UUID, status string) (Factory, error) {
	now := time.Now().UTC()
	var out Factory
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&out, "id = ?", factoryID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		if out.Status == status {
			return nil
		}
		if out.Status == FactoryRetired && status != FactoryRetired {
			return domain.ErrFactoryRetired
		}
		res := tx.Model(&Factory{}).Where("id = ?", factoryID).Updates(map[string]any{
			"status":             status,
			"lifecycle_revision": out.LifecycleRevision + 1,
			"status_changed_at":  now,
		})
		if res.Error != nil {
			return res.Error
		}
		return tx.First(&out, "id = ?", factoryID).Error
	})
	if err != nil {
		return Factory{}, err
	}
	out.fillPresence()
	return out, nil
}

// DeleteUnclaimedFactory 尚未认领的工厂从名录拿掉；已认领的不能走这条。
func (s *Store) DeleteUnclaimedFactory(ctx context.Context, factoryID uuid.UUID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var fac Factory
		if err := tx.First(&fac, "id = ?", factoryID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		if fac.EnrolledAt != nil {
			return domain.ErrReferenced
		}
		if err := tx.Where("factory_id = ?", factoryID).Delete(&InitialSuperAdmin{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", factoryID).Delete(&Factory{})
		if res.Error != nil {
			if domain.IsForeignKeyViolation(res.Error) {
				return domain.ErrReferenced
			}
			return res.Error
		}
		return nil
	})
}

func (s *Store) InitialSuperAdmin(ctx context.Context, factoryID uuid.UUID) (InitialSuperAdmin, error) {
	var row InitialSuperAdmin
	if err := s.db.WithContext(ctx).First(&row, "factory_id = ?", factoryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return InitialSuperAdmin{}, domain.ErrNotFound
		}
		return InitialSuperAdmin{}, err
	}
	return row, nil
}

// ListInitialSuperAdmins 一次取全部工厂的初始超管对账行，不含密码。
func (s *Store) ListInitialSuperAdmins(ctx context.Context) ([]InitialSuperAdmin, error) {
	rows := []InitialSuperAdmin{}
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error
	return rows, err
}
