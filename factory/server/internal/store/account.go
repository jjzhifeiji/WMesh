package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
)

const (
	StatusPending  = "pending"  // 待启用：可预写角色，但不能登录
	StatusActive   = "active"   // 有效：认证后按角色作用域操作
	StatusDisabled = "disabled" // 已停用：不能新开会话或新开受保护操作
	StatusEnded    = "ended"    // 分配已取消，行留下做历史
	StatusRevoked  = "revoked"  // 角色已收回，行留下做历史

	RoleFactorySuperAdmin = "factory_super_admin" // 工厂超级管理员，只能挂 Factory 作用域
	RoleOrgAdmin          = "org_admin"           // 组织管理员，只管本节点及当前子树
	RoleOrgLead           = "org_lead"            // 组织负责人，子树只读
	RoleProcessEngineer   = "process_engineer"    // 工艺工程师，可 Factory 或节点作用域
	RoleOperator          = "operator"            // 操作员，可产生运行事实
	RoleAuditor           = "auditor"             // 审计员，只读，不能改业务

	ScopeFactory = "factory"  // 覆盖本厂及当时全部组织节点
	ScopeOrgUnit = "org_unit" // 只覆盖该节点及当时子树
)

// Person 是本厂一个自然人账号，固定只属于本厂库，不属于任何组织节点。
type Person struct {
	ID                  uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`      // 稳定身份，改名也不变
	LoginName           string    `gorm:"not null" json:"loginName"`           // 本厂内唯一登录名，不是身份
	DisplayName         string    `gorm:"not null" json:"displayName"`         // 显示名，可改
	Status              string    `gorm:"not null" json:"status"`              // pending / active / disabled
	PasswordHash        *string   `json:"-"`                                   // 日常口令哈希，只存在本厂；激活前为空
	ActivationTokenHash *string   `json:"-"`                                   // 一次性激活口令哈希，激活后清空
	IsInitialSuperAdmin bool      `gorm:"not null" json:"isInitialSuperAdmin"` // 本厂唯一的 WAN 下发初始超管
	CreatedAt           time.Time `gorm:"not null" json:"createdAt"`           // 账号创建时间
}

func (Person) TableName() string { return "people" }

// Session 是厂内在线会话；库里只存令牌哈希，每次操作要重查账号状态和角色。
type Session struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`     // 会话稳定身份
	PersonID  uuid.UUID `gorm:"type:uuid;not null" json:"personId"` // 持有该会话的本厂人员
	TokenHash string    `gorm:"not null;uniqueIndex" json:"-"`      // 会话令牌哈希，不存原文
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`          // 会话建立时间
	ExpiresAt time.Time `gorm:"not null" json:"expiresAt"`          // 过期后立刻无效
}

func (Session) TableName() string { return "sessions" }

// CreatePerson 写入待启用账号；本厂只能有一名初始超管，登录名本厂唯一。
func (s *Store) CreatePerson(ctx context.Context, loginName, displayName string, initial bool) (Person, error) {
	row := Person{
		ID:                  id.New(),
		LoginName:           loginName,
		DisplayName:         displayName,
		Status:              StatusPending,
		IsInitialSuperAdmin: initial,
		CreatedAt:           time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			if initial {
				return Person{}, domain.ErrInitialSAExists
			}
			return Person{}, domain.ErrLoginNameTaken
		}
		return Person{}, err
	}
	return row, nil
}

func (s *Store) RenamePerson(ctx context.Context, personID uuid.UUID, displayName, loginName string) error {
	res := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID).Updates(map[string]any{
		"display_name": displayName,
		"login_name":   loginName,
	})
	if res.Error != nil {
		if domain.IsUniqueViolation(res.Error) {
			return domain.ErrLoginNameTaken
		}
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) CreateSession(ctx context.Context, personID uuid.UUID, tokenHash string, expiresAt time.Time) (Session, error) {
	if err := s.assertPersonExists(ctx, personID); err != nil {
		return Session{}, err
	}
	row := Session{
		ID:        id.New(),
		PersonID:  personID,
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

func (s *Store) PersonByLogin(ctx context.Context, loginName string) (Person, error) {
	var row Person
	if err := s.db.WithContext(ctx).First(&row, "login_name = ?", loginName).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Person{}, domain.ErrNotFound
		}
		return Person{}, err
	}
	return row, nil
}

func (s *Store) PersonByID(ctx context.Context, personID uuid.UUID) (Person, error) {
	var row Person
	if err := s.db.WithContext(ctx).First(&row, "id = ?", personID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Person{}, domain.ErrNotFound
		}
		return Person{}, err
	}
	return row, nil
}

func (s *Store) ActivatePerson(ctx context.Context, personID uuid.UUID, passwordHash string) error {
	res := s.db.WithContext(ctx).Model(&Person{}).
		Where("id = ? AND status = ?", personID, StatusPending).
		Updates(map[string]any{
			"password_hash":         passwordHash,
			"activation_token_hash": nil,
			"status":                StatusActive,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrAlreadyActivated
	}
	return nil
}

func (s *Store) SetActivationHash(ctx context.Context, personID uuid.UUID, hash string) error {
	res := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID).Update("activation_token_hash", hash)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) SetPasswordHash(ctx context.Context, personID uuid.UUID, passwordHash string) error {
	res := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID).Update("password_hash", passwordHash)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) SetPersonStatus(ctx context.Context, personID uuid.UUID, status string) error {
	res := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID).Update("status", status)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
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

func (s *Store) PersonCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Person{}).Count(&n).Error
	return n, err
}

// ListPeople 列出本厂全部人员行；调用方不得把口令哈希交给前端。
func (s *Store) ListPeople(ctx context.Context) ([]Person, error) {
	var rows []Person
	err := s.db.WithContext(ctx).Order("created_at").Find(&rows).Error
	return rows, err
}
