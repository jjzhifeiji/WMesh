// Package store 只碰 WAN 库：管理员、工厂名录、初始超管对账和审计。
// 不判定允许/拒绝，也不回调应用服务。
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

// Store 只碰 WAN 库：管理员、工厂名录、初始超管对账和 WAN 审计。
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

func (s *Store) CreateFactory(ctx context.Context, name string) (Factory, error) {
	row := Factory{
		ID:        id.New(),
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return Factory{}, err
	}
	return row, nil
}

// BindInitialSuperAdmin 记下该厂初始超管身份与登录名，不存口令。
func (s *Store) BindInitialSuperAdmin(ctx context.Context, factoryID, personID uuid.UUID, loginName string) error {
	row := InitialSuperAdmin{
		FactoryID: factoryID,
		PersonID:  personID,
		LoginName: loginName,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
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
