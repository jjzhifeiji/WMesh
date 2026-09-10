package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
)

func (s *Store) AdminByLogin(ctx context.Context, loginName string) (Admin, error) {
	var row Admin
	if err := s.db.WithContext(ctx).First(&row, "login_name = ?", loginName).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Admin{}, domain.ErrNotFound
		}
		return Admin{}, err
	}
	return row, nil
}

func (s *Store) AdminByID(ctx context.Context, id uuid.UUID) (Admin, error) {
	var row Admin
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Admin{}, domain.ErrNotFound
		}
		return Admin{}, err
	}
	return row, nil
}

func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	var row Session
	if err := s.db.WithContext(ctx).First(&row, "token_hash = ?", tokenHash).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Session{}, domain.ErrNotFound
		}
		return Session{}, err
	}
	if !row.ExpiresAt.After(time.Now().UTC()) {
		return Session{}, domain.ErrSessionExpired
	}
	return row, nil
}

func (s *Store) DeleteSessionByTokenHash(ctx context.Context, tokenHash string) error {
	res := s.db.WithContext(ctx).Where("token_hash = ?", tokenHash).Delete(&Session{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
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

func (s *Store) ListAudit(ctx context.Context) ([]audit.Row, error) {
	var rows []audit.Row
	err := s.db.WithContext(ctx).Order("occurred_at").Find(&rows).Error
	return rows, err
}
