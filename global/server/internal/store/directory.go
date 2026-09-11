package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/domain"
)

// Factory 是 WAN 名录里的一家工厂，不含厂内组织或普通账号。
type Factory struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"` // 工厂稳定身份，也用来选厂库
	Name      string    `gorm:"not null" json:"name"`           // 工厂显示名，不当身份
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`      // 名录入库时间
}

func (Factory) TableName() string { return "factories" }

// InitialSuperAdmin 是交付对账用的初始超管身份，不含日常口令。
type InitialSuperAdmin struct {
	FactoryID uuid.UUID `gorm:"type:uuid;primaryKey" json:"factoryId"` // 一厂只能绑一名初始超管
	PersonID  uuid.UUID `gorm:"type:uuid;not null" json:"personId"`    // 落在目标厂库里的账号身份
	LoginName string    `gorm:"not null" json:"loginName"`             // 交付时的登录名，不是秘密
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`             // 对账记录写入时间
}

func (InitialSuperAdmin) TableName() string { return "initial_super_admins" }

// RegisterFactory 用调用方给的稳定身份写入名录，并在同一事务绑定初始超管，不存口令。
func (s *Store) RegisterFactory(ctx context.Context, factoryID uuid.UUID, name string, personID uuid.UUID, saLogin string) (Factory, error) {
	now := time.Now().UTC()
	fac := Factory{ID: factoryID, Name: name, CreatedAt: now}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&fac).Error; err != nil {
			return err
		}
		return bindInitialSuperAdmin(tx, factoryID, personID, saLogin, now)
	})
	if err != nil {
		return Factory{}, err
	}
	return fac, nil
}

// BindInitialSuperAdmin 记下该厂初始超管身份与登录名；一厂只能绑一名。
func (s *Store) BindInitialSuperAdmin(ctx context.Context, factoryID, personID uuid.UUID, loginName string) error {
	return bindInitialSuperAdmin(s.db.WithContext(ctx), factoryID, personID, loginName, time.Now().UTC())
}

func bindInitialSuperAdmin(tx *gorm.DB, factoryID, personID uuid.UUID, loginName string, at time.Time) error {
	row := InitialSuperAdmin{FactoryID: factoryID, PersonID: personID, LoginName: loginName, CreatedAt: at}
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
	return row, nil
}

func (s *Store) ListFactories(ctx context.Context) ([]Factory, error) {
	rows := []Factory{}
	err := s.db.WithContext(ctx).Order("created_at").Find(&rows).Error
	return rows, err
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

// ListInitialSuperAdmins 一次取全部工厂的初始超管对账行，不含口令。
func (s *Store) ListInitialSuperAdmins(ctx context.Context) ([]InitialSuperAdmin, error) {
	rows := []InitialSuperAdmin{}
	err := s.db.WithContext(ctx).Order("created_at").Find(&rows).Error
	return rows, err
}
