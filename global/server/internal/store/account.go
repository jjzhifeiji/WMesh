package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
)

// Admin 是 WAN 唯一管理员；表上有单行约束，不能再邀请第二人。
type Admin struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`        // WAN 管理员稳定身份
	LoginName    string    `gorm:"not null;uniqueIndex" json:"loginName"` // WAN 登录名，全库唯一
	PasswordHash string    `gorm:"not null" json:"-"`                     // 日常口令哈希，只存在 WAN 库
	CreatedAt    time.Time `gorm:"not null" json:"createdAt"`             // 账号创建时间
}

func (Admin) TableName() string { return "wan_admins" }

// Session 是 WAN 管理员会话；库里只存令牌哈希。
type Session struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`    // 会话稳定身份
	AdminID   uuid.UUID `gorm:"type:uuid;not null" json:"adminId"` // 持有该会话的 WAN 管理员
	TokenHash string    `gorm:"not null;uniqueIndex" json:"-"`     // 会话令牌哈希，不存原文
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`         // 会话建立时间
	ExpiresAt time.Time `gorm:"not null" json:"expiresAt"`         // 过期后此会话立刻无效
}

func (Session) TableName() string { return "sessions" }

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

func (s *Store) AdminCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Admin{}).Count(&n).Error
	return n, err
}

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
