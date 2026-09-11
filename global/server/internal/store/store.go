// Package store 只碰 WAN 库：管理员、工厂名录、初始超管对账、Client 绑定、平台级资产和审计。
// 不判定允许/拒绝，也不回调应用服务；不见厂内人员、组织或人员离线授权。
package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
)

// Store 只碰 WAN 库：管理员、工厂名录、Client 绑定、平台级资产和 WAN 审计。
type Store struct {
	db *gorm.DB
}

// Open 打开已迁移的 WAN 库；调用方保证这是 WAN 库而不是厂库。
func Open(db *gorm.DB) *Store {
	return &Store{db: db}
}

// CreateAdmin 写入 WAN 管理员；单行唯一索引保证不能有第二个。
func (s *Store) CreateAdmin(ctx context.Context, loginName, passwordHash string) (Admin, error) {
	row := Admin{
		ID:           id.New(),
		LoginName:    loginName,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return Admin{}, domain.ErrWANAdminExists
		}
		return Admin{}, err
	}
	return row, nil
}

// Ping 只确认 WAN 库连接可用。
func (s *Store) Ping(ctx context.Context) error {
	return s.db.WithContext(ctx).Exec("SELECT 1").Error
}

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

func (s *Store) CreateSession(ctx context.Context, adminID uuid.UUID, tokenHash string, expiresAt time.Time) (Session, error) {
	row := Session{
		ID:        id.New(),
		AdminID:   adminID,
		TokenHash: tokenHash,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return Session{}, domain.ErrDuplicateSession
		}
		return Session{}, err
	}
	return row, nil
}

func (s *Store) AppendAudit(ctx context.Context, e audit.Event) error {
	if e.ID == uuid.Nil {
		e.ID = id.New()
	}
	return s.db.WithContext(ctx).Create(audit.RowFrom(e)).Error
}

func (s *Store) HasTable(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := s.db.WithContext(ctx).
		Raw("SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = ?)", name).
		Scan(&exists).Error
	return exists, err
}

func (s *Store) AdminCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Admin{}).Count(&n).Error
	return n, err
}
