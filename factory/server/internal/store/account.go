package store

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
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
	RoleOrgAdmin          = "org_admin"           // 组织管理员，可挂整厂或某个节点及当前子树
	RoleOrgLead           = "org_lead"            // 组织负责人，子树只读
	RoleProcessEngineer   = "process_engineer"    // 历史角色，新授予不再提供；制作不依赖它
	RoleOperator          = "operator"            // 操作员，可产生运行事实
	RoleAuditor           = "auditor"             // 审计员，只读，不能改业务

	ScopeFactory = "factory"  // 覆盖本厂及当时全部组织节点
	ScopeOrgUnit = "org_unit" // 只覆盖该节点及当时子树

	SessionKindWeb = "web" // 管理端登录，不计入 APP 在线
	SessionKindApp = "app" // 示教器登录；名册在线按未过期会话计
)

// Person 是本厂一个自然人账号，固定只属于本厂库，不属于任何组织节点。
type Person struct {
	ID                  uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`      // 稳定身份，改名也不变
	LoginName           string    `gorm:"not null" json:"loginName"`           // 本厂内唯一登录名，不是身份
	DisplayName         string    `gorm:"not null" json:"displayName"`         // 显示名，可改
	Status              string    `gorm:"not null" json:"status"`              // pending / active / disabled
	PasswordHash        *string   `json:"-"`                                   // 日常密码哈希，只存在本厂；激活前为空
	ActivationTokenHash *string   `json:"-"`                                   // 一次性 8 位激活码哈希，激活后清空
	IsInitialSuperAdmin bool       `gorm:"not null" json:"isInitialSuperAdmin"` // 本厂唯一的 WAN 下发初始超管
	CreatedAt           time.Time  `gorm:"not null" json:"createdAt"`           // 账号创建时间
	UnwrapKey           []byte     `json:"-"`                                  // 登录人解封钥；焊机不持钥
	AppLastSeenAt       *time.Time `json:"appLastSeenAt,omitempty"`            // 最近一次示教器登录或 MQTT 见到
	AppVersion          int64      `json:"appVersion"`                         // 示教器自报 versionCode；0 表示还没报到
	AppVersionName      string     `json:"appVersionName"`                     // 示教器自报 versionName
}

func (Person) TableName() string { return "people" }

// Session 是厂内在线会话；库里只存令牌哈希，每次操作要重查账号状态和角色。
type Session struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`     // 会话稳定身份
	PersonID  uuid.UUID `gorm:"type:uuid;not null" json:"personId"` // 持有该会话的本厂人员
	TokenHash string    `gorm:"not null;uniqueIndex" json:"-"`      // 会话令牌哈希，不存原文
	Kind      string    `gorm:"not null" json:"kind"`               // web / app；名册 APP 在线只认 app
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

// CreatePersonAt 用 WAN 约定的身份写入待启用账号；身份冲突且已是同一初始超管则返回已有行。
func (s *Store) CreatePersonAt(ctx context.Context, personID uuid.UUID, loginName, displayName string, initial bool) (Person, error) {
	row := Person{
		ID:                  personID,
		LoginName:           loginName,
		DisplayName:         displayName,
		Status:              StatusPending,
		IsInitialSuperAdmin: initial,
		CreatedAt:           time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			existing, getErr := s.PersonByID(ctx, personID)
			if getErr == nil && existing.IsInitialSuperAdmin && existing.LoginName == loginName {
				return existing, nil
			}
			if initial {
				return Person{}, domain.ErrInitialSAExists
			}
			return Person{}, domain.ErrLoginNameTaken
		}
		return Person{}, err
	}
	return row, nil
}

// RenamePerson 改显示名和登录名；登录名本厂唯一。
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

// CreateSession 写入厂内会话；库里只存令牌哈希。
func (s *Store) CreateSession(ctx context.Context, personID uuid.UUID, tokenHash string, expiresAt time.Time) (Session, error) {
	if err := s.assertPersonExists(ctx, personID); err != nil {
		return Session{}, err
	}
	row := Session{
		ID:        id.New(),
		PersonID:  personID,
		TokenHash: tokenHash,
		Kind:      SessionKindWeb,
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

// CreateAppSession 写入示教器会话，给 12 小时令牌用；名册在线看 MQTT。
func (s *Store) CreateAppSession(ctx context.Context, personID uuid.UUID, tokenHash string, expiresAt time.Time) (Session, error) {
	if err := s.assertPersonExists(ctx, personID); err != nil {
		return Session{}, err
	}
	row := Session{
		ID:        id.New(),
		PersonID:  personID,
		TokenHash: tokenHash,
		Kind:      SessionKindApp,
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

// NotePersonApp 刷新示教器最近见到；有版本才覆盖上次自报。
func (s *Store) NotePersonApp(ctx context.Context, personID uuid.UUID, version int64, versionName string) error {
	now := time.Now().UTC()
	updates := map[string]any{"app_last_seen_at": now}
	versionName = strings.TrimSpace(versionName)
	if utf8Len := len([]rune(versionName)); utf8Len > 80 {
		versionName = string([]rune(versionName)[:80])
	}
	if version > 0 || versionName != "" {
		updates["app_version"] = version
		updates["app_version_name"] = versionName
	}
	res := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID).Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// PersonByLogin 按本厂登录名取账号。
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

// PeopleByIDs 按稳定身份批量取本厂账号，给列表拼显示名。
func (s *Store) PeopleByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Person, error) {
	out := map[uuid.UUID]Person{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []Person
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, p := range rows {
		out[p.ID] = p
	}
	return out, nil
}

// PersonByID 按稳定身份取本厂账号。
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

// EnsurePersonUnwrapKey 登录人若还没有解封钥就补一把；焊机不持钥。
func (s *Store) EnsurePersonUnwrapKey(ctx context.Context, personID uuid.UUID) (Person, error) {
	var out Person
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row Person
		if err := tx.First(&row, "id = ?", personID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		if len(row.UnwrapKey) == 32 {
			out = row
			return nil
		}
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return err
		}
		if err := tx.Model(&Person{}).Where("id = ?", personID).Update("unwrap_key", key).Error; err != nil {
			return err
		}
		if err := tx.First(&row, "id = ?", personID).Error; err != nil {
			return err
		}
		out = row
		return nil
	})
	return out, err
}

// ActivatePerson 把待启用改成有效并写入日常密码；已激活拒绝。
func (s *Store) ActivatePerson(ctx context.Context, personID uuid.UUID, passwordHash string) error {
	// 只把待启用改成有效；已激活或停用不走这条。
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

// SetActivationHash 写入一次性激活码哈希，不存原文。
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

// SetPasswordHash 只改日常密码哈希，不改登录名。
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

// ApplyPersonPassword 写入日常密码哈希并清激活码；status 空则不改状态。
func (s *Store) ApplyPersonPassword(ctx context.Context, personID uuid.UUID, passwordHash, status string) error {
	updates := map[string]any{
		"password_hash":         passwordHash,
		"activation_token_hash": nil,
	}
	if status != "" {
		updates["status"] = status
	}
	q := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID)
	if status != "" {
		q = q.Select("password_hash", "activation_token_hash", "status")
	} else {
		q = q.Select("password_hash", "activation_token_hash")
	}
	res := q.Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SetPersonStatus 只改人员状态，不改密码。
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

// SessionByTokenHash 按令牌哈希取会话；过期立刻无效。
func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	var row Session
	if err := s.db.WithContext(ctx).First(&row, "token_hash = ?", tokenHash).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Session{}, domain.ErrNotFound
		}
		return Session{}, err
	}
	// 过期会话立刻无效，不当权限缓存。
	if !row.ExpiresAt.After(time.Now().UTC()) {
		return Session{}, domain.ErrSessionExpired
	}
	return row, nil
}

// DeleteSessionByTokenHash 作废这一条会话；找不到算不存在。
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

// DeleteSessionsForPerson 作废该人全部在线会话；没有会话不算错。
func (s *Store) DeleteSessionsForPerson(ctx context.Context, personID uuid.UUID) error {
	return s.db.WithContext(ctx).Where("person_id = ?", personID).Delete(&Session{}).Error
}

// PersonCount 数本厂账号行。
func (s *Store) PersonCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Person{}).Count(&n).Error
	return n, err
}

// ListPeople 列出本厂全部人员行，按创建时间从新到旧；调用方不得把密码哈希交给前端。
func (s *Store) ListPeople(ctx context.Context) ([]Person, error) {
	var rows []Person
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// InitialPerson 本厂唯一的 WAN 下发初始超管；没有则当未认领。
func (s *Store) InitialPerson(ctx context.Context) (Person, error) {
	var row Person
	if err := s.db.WithContext(ctx).First(&row, "is_initial_super_admin = ?", true).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Person{}, domain.ErrNotFound
		}
		return Person{}, err
	}
	return row, nil
}
