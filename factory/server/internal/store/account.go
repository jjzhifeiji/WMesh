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
	RoleOperator          = "operator"            // 操作员，可产生运行事实、作用域内下发
	RoleAuditor           = "auditor"             // 审计员，只读，不能改业务

	ScopeFactory = "factory"  // 覆盖本厂及当时全部组织节点
	ScopeOrgUnit = "org_unit" // 只覆盖该节点及当时子树

	SessionKindWeb = "web" // 管理端登录，不计入 APP 在线
	SessionKindApp = "app" // 示教器登录；名册在线按未过期会话计
)

// Person 是本厂一个自然人账号，固定只属于本厂库，不属于任何组织节点。
type Person struct {
	ID                  uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`         // 稳定身份，改名也不变
	LoginName           string     `gorm:"not null" json:"loginName"`              // 本厂内唯一登录名，不是身份
	DisplayName         string     `gorm:"not null" json:"displayName"`            // 显示名，可改
	Status              string     `gorm:"not null" json:"status"`                 // pending / active / disabled
	PasswordHash        *string    `json:"-"`                                      // 日常密码哈希，只存在本厂；激活前为空
	ActivationTokenHash *string    `json:"-"`                                      // 一次性 8 位激活码哈希，激活后清空
	IsInitialSuperAdmin bool       `gorm:"not null" json:"isInitialSuperAdmin"`    // 本厂唯一的 WAN 下发初始超管
	CreatedAt           time.Time  `gorm:"not null" json:"createdAt"`              // 账号创建时间
	UnwrapKey           []byte     `json:"-"`                                      // 登录人解封钥；焊机不持钥
	AppLastSeenAt       *time.Time `json:"appLastSeenAt,omitempty"`                // 最近一次示教器登录或 MQTT 见到
	AppVersion          int64      `json:"appVersion"`                             // 示教器自报 versionCode；0 表示还没报到
	AppVersionName      string     `json:"appVersionName"`                         // 示教器自报 versionName
	KeepPouch           bool       `gorm:"not null;default:true" json:"keepPouch"` // 退出后是否保留示教器库文件；默认留
}

// 指定落库表名，避免查询时按类型名去猜。
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

// 指定落库表名，避免查询时按类型名去猜。
func (Session) TableName() string { return "sessions" }

// CreatePerson 写入待启用账号；本厂只能有一名初始超管，登录名本厂唯一。
func (s *Store) CreatePerson(ctx context.Context, loginName, displayName string, initial bool) (Person, error) {
	// 组装待启用账号，激活前不放密码。
	row := Person{
		ID:                  id.New(),
		LoginName:           loginName,
		DisplayName:         displayName,
		Status:              StatusPending,
		IsInitialSuperAdmin: initial,
		CreatedAt:           time.Now().UTC(),
		KeepPouch:           true,
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 唯一约束撞了就改成业务冲突，不抛库原文。
		if domain.IsUniqueViolation(err) {
			// 初始超管只能有一名，撞了就拒绝再建。
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
	// 组装待启用账号，激活前不放密码。
	row := Person{
		ID:                  personID,
		LoginName:           loginName,
		DisplayName:         displayName,
		Status:              StatusPending,
		IsInitialSuperAdmin: initial,
		CreatedAt:           time.Now().UTC(),
		KeepPouch:           true,
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 唯一约束撞了就改成业务冲突，不抛库原文。
		if domain.IsUniqueViolation(err) {
			// 冲突后再按身份读已有行，看是不是同一个人。
			existing, getErr := s.PersonByID(ctx, personID)
			// 身份和登录名都对上初始超管，就交回已有行。
			if getErr == nil && existing.IsInitialSuperAdmin && existing.LoginName == loginName {
				return existing, nil
			}
			// 初始超管只能有一名，撞了就拒绝再建。
			if initial {
				return Person{}, domain.ErrInitialSAExists
			}
			return Person{}, domain.ErrLoginNameTaken
		}
		return Person{}, err
	}
	return row, nil
}

// SetKeepPouch 记下这个人退出后是否保留示教器库文件。
func (s *Store) SetKeepPouch(ctx context.Context, personID uuid.UUID, keep bool) error {
	// 只改点名的列，避免把别的字段写成零值。
	res := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID).Update("keep_pouch", keep)
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// RenamePerson 改显示名和登录名；登录名本厂唯一。
func (s *Store) RenamePerson(ctx context.Context, personID uuid.UUID, displayName, loginName string) error {
	// 只改点名的列，避免把别的字段写成零值。
	res := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID).Updates(map[string]any{
		"display_name": displayName,
		"login_name":   loginName,
	})
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		// 唯一约束撞了就改成业务冲突，不抛库原文。
		if domain.IsUniqueViolation(res.Error) {
			return domain.ErrLoginNameTaken
		}
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// CreateSession 写入厂内会话；库里只存令牌哈希。
func (s *Store) CreateSession(ctx context.Context, personID uuid.UUID, tokenHash string, expiresAt time.Time) (Session, error) {
	// 人必须已经在本厂，否则不继续写。
	if err := s.assertPersonExists(ctx, personID); err != nil {
		return Session{}, err
	}
	// 组装管理端会话，库里只留令牌哈希。
	row := Session{
		ID:        id.New(),
		PersonID:  personID,
		TokenHash: tokenHash,
		Kind:      SessionKindWeb,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: expiresAt,
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 唯一约束撞了就改成业务冲突，不抛库原文。
		if domain.IsUniqueViolation(err) {
			return Session{}, domain.ErrDuplicateSession
		}
		return Session{}, err
	}
	return row, nil
}

// CreateAppSession 写入示教器会话，给 12 小时令牌用；名册在线看 MQTT。
func (s *Store) CreateAppSession(ctx context.Context, personID uuid.UUID, tokenHash string, expiresAt time.Time) (Session, error) {
	// 人必须已经在本厂，否则不继续写。
	if err := s.assertPersonExists(ctx, personID); err != nil {
		return Session{}, err
	}
	// 组装示教器会话，在线名册按这种算。
	row := Session{
		ID:        id.New(),
		PersonID:  personID,
		TokenHash: tokenHash,
		Kind:      SessionKindApp,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: expiresAt,
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 唯一约束撞了就改成业务冲突，不抛库原文。
		if domain.IsUniqueViolation(err) {
			return Session{}, domain.ErrDuplicateSession
		}
		return Session{}, err
	}
	return row, nil
}

// NotePersonApp 刷新示教器最近见到；有版本才覆盖上次自报。
func (s *Store) NotePersonApp(ctx context.Context, personID uuid.UUID, version int64, versionName string) error {
	// 取当前时刻，时间列和租约用同一个时钟。
	now := time.Now().UTC()
	// 只收集这次要改的列，避免整行覆盖。
	updates := map[string]any{"app_last_seen_at": now}
	// 去掉首尾空白，纯空白不当有效内容。
	versionName = strings.TrimSpace(versionName)
	// 超过字数上限就截断，避免超出列宽。
	if utf8Len := len([]rune(versionName)); utf8Len > 80 {
		// 按字截到上限，避免超长内容进库。
		versionName = string([]rune(versionName)[:80])
	}
	// 带了版本号或版本名才覆盖，空的别冲掉上次。
	if version > 0 || versionName != "" {
		// 把这一列放进本次更新。
		updates["app_version"] = version
		// 把这一列放进本次更新。
		updates["app_version_name"] = versionName
	}
	// 只改点名的列，避免把别的字段写成零值。
	res := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID).Updates(updates)
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// PersonByLogin 按本厂登录名取账号。
func (s *Store) PersonByLogin(ctx context.Context, loginName string) (Person, error) {
	// 准备承接查到的账号。
	var row Person
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "login_name = ?", loginName).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Person{}, domain.ErrNotFound
		}
		return Person{}, err
	}
	return row, nil
}

// PeopleByIDs 按稳定身份批量取本厂账号，给列表拼显示名。
func (s *Store) PeopleByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Person, error) {
	// 准备要交回的结果，后面逐条填。
	out := map[uuid.UUID]Person{}
	// 没有要查的身份就交回空结果。
	if len(ids) == 0 {
		return out, nil
	}
	// 准备承接查到的多条账号。
	var rows []Person
	// 按条件取多行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, p := range rows {
		// 按身份放进映射，调用方好查找。
		out[p.ID] = p
	}
	return out, nil
}

// PersonByID 按稳定身份取本厂账号。
func (s *Store) PersonByID(ctx context.Context, personID uuid.UUID) (Person, error) {
	// 准备承接查到的账号。
	var row Person
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", personID).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Person{}, domain.ErrNotFound
		}
		return Person{}, err
	}
	return row, nil
}

// EnsurePersonUnwrapKey 登录人若还没有解封钥就补一把；焊机不持钥。
func (s *Store) EnsurePersonUnwrapKey(ctx context.Context, personID uuid.UUID) (Person, error) {
	// 准备承接查到的账号。
	var out Person
	// 放进同一事务，中途失败就整单回滚。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 准备承接查到的账号。
		var row Person
		// 按条件取一行失败就停，避免带着错误继续。
		if err := tx.First(&row, "id = ?", personID).Error; err != nil {
			// 没有这一行就当成不存在。
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		// 解封钥已经齐了就直接用，不再重发。
		if len(row.UnwrapKey) == 32 {
			// 把查到的结果交给事务外的调用方。
			out = row
			return nil
		}
		// 先留出三十二字节，再填随机数。
		key := make([]byte, 32)
		// 读随机字节失败就停，避免带着错误继续。
		if _, err := rand.Read(key); err != nil {
			return err
		}
		// 更新指定列失败就停，避免带着错误继续。
		if err := tx.Model(&Person{}).Where("id = ?", personID).Update("unwrap_key", key).Error; err != nil {
			return err
		}
		// 按条件取一行失败就停，避免带着错误继续。
		if err := tx.First(&row, "id = ?", personID).Error; err != nil {
			return err
		}
		// 把查到的结果交给事务外的调用方。
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
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrAlreadyActivated
	}
	return nil
}

// SetActivationHash 写入一次性激活码哈希，不存原文。
func (s *Store) SetActivationHash(ctx context.Context, personID uuid.UUID, hash string) error {
	// 只改点名的列，避免把别的字段写成零值。
	res := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID).Update("activation_token_hash", hash)
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SetPasswordHash 只改日常密码哈希，不改登录名。
func (s *Store) SetPasswordHash(ctx context.Context, personID uuid.UUID, passwordHash string) error {
	// 只改点名的列，避免把别的字段写成零值。
	res := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID).Update("password_hash", passwordHash)
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ApplyPersonPassword 写入日常密码哈希并清激活码；status 空则不改状态。
func (s *Store) ApplyPersonPassword(ctx context.Context, personID uuid.UUID, passwordHash, status string) error {
	// 只收集这次要改的列，避免整行覆盖。
	updates := map[string]any{
		"password_hash":         passwordHash,
		"activation_token_hash": nil,
	}
	// 给了新状态才改状态，空着就保持原样。
	if status != "" {
		// 把这一列放进本次更新。
		updates["status"] = status
	}
	// 补上筛选或限定列，避免动到不该动的字段。
	q := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID)
	// 要改状态时连状态列一起写，避免零值覆盖。
	if status != "" {
		// 补上筛选或限定列，避免动到不该动的字段。
		q = q.Select("password_hash", "activation_token_hash", "status")
		// 不改状态时只更新密码和激活码。
	} else {
		// 补上筛选或限定列，避免动到不该动的字段。
		q = q.Select("password_hash", "activation_token_hash")
	}
	// 只改点名的列，避免把别的字段写成零值。
	res := q.Updates(updates)
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SetPersonStatus 只改人员状态，不改密码。
func (s *Store) SetPersonStatus(ctx context.Context, personID uuid.UUID, status string) error {
	// 只改点名的列，避免把别的字段写成零值。
	res := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID).Update("status", status)
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SessionByTokenHash 按令牌哈希取会话；过期立刻无效。
func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	// 准备承接查到的会话。
	var row Session
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "token_hash = ?", tokenHash).Error; err != nil {
		// 没有这一行就当成不存在。
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
	// 删掉匹配行，没有行交给后面判断。
	res := s.db.WithContext(ctx).Where("token_hash = ?", tokenHash).Delete(&Session{})
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteSessionsForPerson 作废该人全部在线会话；没有会话不算错。
func (s *Store) DeleteSessionsForPerson(ctx context.Context, personID uuid.UUID) error {
	// 删掉匹配行，库错误原样交回。
	return s.db.WithContext(ctx).Where("person_id = ?", personID).Delete(&Session{}).Error
}

// PersonCount 数本厂账号行。
func (s *Store) PersonCount(ctx context.Context) (int64, error) {
	// 准备承接计数，用来判断有没有匹配行。
	var n int64
	// 数匹配行，用来判断还有没有引用。
	err := s.db.WithContext(ctx).Model(&Person{}).Count(&n).Error
	return n, err
}

// ListPeople 列出本厂全部人员行，按创建时间从新到旧；调用方不得把密码哈希交给前端。
func (s *Store) ListPeople(ctx context.Context) ([]Person, error) {
	// 准备承接查到的多条账号。
	var rows []Person
	// 按条件去读，没有行交给后面的分支。
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// InitialPerson 本厂唯一的 WAN 下发初始超管；没有则当未认领。
func (s *Store) InitialPerson(ctx context.Context) (Person, error) {
	// 准备承接查到的账号。
	var row Person
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "is_initial_super_admin = ?", true).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Person{}, domain.ErrNotFound
		}
		return Person{}, err
	}
	return row, nil
}
