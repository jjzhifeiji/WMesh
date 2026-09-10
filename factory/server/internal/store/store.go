// Package store 只读写本厂库：人员、组织、角色、会话、归属桩和本厂 Client 凭证。
// 不判定允许/拒绝，也不回调应用服务。
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
)

// Store 只打开这一家工厂的库，按工厂稳定身份选库，不搞单库多厂。
type Store struct {
	db        *gorm.DB
	factoryID uuid.UUID // 本厂稳定身份，写入事实时带上
}

// Open 打开这一家工厂的库；factoryID 是选库用的稳定身份，不从名称推导。
func Open(db *gorm.DB, factoryID uuid.UUID) *Store {
	return &Store{db: db, factoryID: factoryID}
}

func (s *Store) FactoryID() uuid.UUID { return s.factoryID }

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

func (s *Store) CreateOrgUnit(ctx context.Context, name string, parentID *uuid.UUID) (OrgUnit, error) {
	if parentID != nil {
		if err := s.assertUnitActive(ctx, *parentID); err != nil {
			return OrgUnit{}, err
		}
	}
	row := OrgUnit{
		ID:        id.New(),
		ParentID:  parentID,
		Name:      name,
		Status:    StatusActive,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsCheckViolation(err) {
			return OrgUnit{}, domain.ErrCycle
		}
		return OrgUnit{}, err
	}
	return row, nil
}

func (s *Store) RenameOrgUnit(ctx context.Context, unitID uuid.UUID, name string) error {
	res := s.db.WithContext(ctx).Model(&OrgUnit{}).Where("id = ?", unitID).Update("name", name)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ReparentOrgUnit 改挂前检查不能挂到自己或后代，避免把树打成环。
func (s *Store) ReparentOrgUnit(ctx context.Context, unitID uuid.UUID, parentID *uuid.UUID) error {
	if parentID != nil && *parentID == unitID {
		return domain.ErrCycle
	}
	if parentID != nil {
		if err := s.assertUnitActive(ctx, *parentID); err != nil {
			return err
		}
		cycle, err := s.wouldCycle(ctx, unitID, *parentID)
		if err != nil {
			return err
		}
		if cycle {
			return domain.ErrCycle
		}
	}
	res := s.db.WithContext(ctx).Model(&OrgUnit{}).Where("id = ?", unitID).Update("parent_id", parentID)
	if res.Error != nil {
		if domain.IsCheckViolation(res.Error) {
			return domain.ErrCycle
		}
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) wouldCycle(ctx context.Context, nodeID, newParent uuid.UUID) (bool, error) {
	current := &newParent
	seen := map[uuid.UUID]struct{}{}
	for current != nil {
		if *current == nodeID {
			return true, nil
		}
		if _, ok := seen[*current]; ok {
			return true, nil
		}
		seen[*current] = struct{}{}
		var u OrgUnit
		if err := s.db.WithContext(ctx).Select("parent_id").First(&u, "id = ?", *current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, domain.ErrNotFound
			}
			return false, err
		}
		current = u.ParentID
	}
	return false, nil
}

func (s *Store) Assign(ctx context.Context, personID, unitID uuid.UUID) (Assignment, error) {
	if err := s.assertUnitActive(ctx, unitID); err != nil {
		return Assignment{}, err
	}
	if err := s.assertPersonExists(ctx, personID); err != nil {
		return Assignment{}, err
	}
	row := Assignment{
		ID:        id.New(),
		PersonID:  personID,
		OrgUnitID: unitID,
		Status:    StatusActive,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return Assignment{}, domain.ErrDuplicateAssignment
		}
		return Assignment{}, err
	}
	return row, nil
}

func (s *Store) Unassign(ctx context.Context, personID, unitID uuid.UUID) error {
	now := time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&Assignment{}).
		Where("person_id = ? AND org_unit_id = ? AND status = ?", personID, unitID, StatusActive).
		Updates(map[string]any{"status": StatusEnded, "ended_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// GrantRole 写入一条带作用域的角色；作用域不合法由库约束与 ValidScope 双检。
func (s *Store) GrantRole(ctx context.Context, personID uuid.UUID, role, scopeKind string, orgUnitID *uuid.UUID) (RoleGrant, error) {
	if !ValidScope(role, scopeKind, orgUnitID) {
		return RoleGrant{}, domain.ErrInvalidRoleScope
	}
	if err := s.assertPersonExists(ctx, personID); err != nil {
		return RoleGrant{}, err
	}
	if orgUnitID != nil {
		if _, err := s.getUnit(ctx, *orgUnitID); err != nil {
			return RoleGrant{}, err
		}
	}
	row := RoleGrant{
		ID:        id.New(),
		PersonID:  personID,
		Role:      role,
		ScopeKind: scopeKind,
		OrgUnitID: orgUnitID,
		Status:    StatusActive,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return RoleGrant{}, domain.ErrDuplicateRoleGrant
		}
		if domain.IsCheckViolation(err) {
			return RoleGrant{}, domain.ErrInvalidRoleScope
		}
		return RoleGrant{}, err
	}
	return row, nil
}

func (s *Store) RevokeRole(ctx context.Context, grantID uuid.UUID) error {
	now := time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&RoleGrant{}).
		Where("id = ? AND status = ?", grantID, StatusActive).
		Updates(map[string]any{"status": StatusRevoked, "revoked_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func hasRows(db *gorm.DB, model any, query string, args ...any) (bool, error) {
	var n int64
	err := db.Model(model).Where(query, args...).Count(&n).Error
	return n > 0, err
}

// pathMentions 看事实/资产路径快照里是否出现过该身份。
func pathMentions(db *gorm.DB, key string, id uuid.UUID) (bool, error) {
	payload := fmt.Sprintf(`[{"%s":"%s"}]`, key, id)
	for _, table := range []string{"fact_stubs", "personal_asset_stubs"} {
		var n int64
		err := db.Raw("SELECT COUNT(*) FROM "+table+" WHERE org_path @> ?::jsonb", payload).Scan(&n).Error
		if err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

// DeleteOrgUnit 没有下级、当前人员、有效角色、事实或资产才物理删除。
func (s *Store) DeleteOrgUnit(ctx context.Context, unitID uuid.UUID) error {
	if _, err := s.getUnit(ctx, unitID); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		checks := []struct {
			model any
			query string
			args  []any
		}{
			{&OrgUnit{}, "parent_id = ?", []any{unitID}},
			{&Assignment{}, "org_unit_id = ? AND status = ?", []any{unitID, StatusActive}},
			{&RoleGrant{}, "org_unit_id = ? AND status = ?", []any{unitID, StatusActive}},
			{&factRow{}, "org_unit_id = ?", []any{unitID}},
			{&assetRow{}, "org_unit_id = ?", []any{unitID}},
		}
		for _, c := range checks {
			ok, err := hasRows(tx, c.model, c.query, c.args...)
			if err != nil {
				return err
			}
			if ok {
				return domain.ErrReferenced
			}
		}
		ok, err := pathMentions(tx, "id", unitID)
		if err != nil {
			return err
		}
		if ok {
			return domain.ErrReferenced
		}
		// 已取消的分配、已收回的角色不再挡删除，但外键还在，先清掉。
		if err := tx.Where("org_unit_id = ?", unitID).Delete(&Assignment{}).Error; err != nil {
			return err
		}
		if err := tx.Where("org_unit_id = ?", unitID).Delete(&RoleGrant{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", unitID).Delete(&OrgUnit{})
		if res.Error != nil {
			if domain.IsForeignKeyViolation(res.Error) {
				return domain.ErrReferenced
			}
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

func (s *Store) DisableOrgUnit(ctx context.Context, unitID uuid.UUID) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&OrgUnit{}).
		Where("parent_id = ? AND status = ?", unitID, StatusActive).
		Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return domain.ErrHasActiveChildren
	}
	res := s.db.WithContext(ctx).Model(&OrgUnit{}).Where("id = ?", unitID).Update("status", StatusDisabled)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// setStatus 只改状态列，找不到行就当不存在。
func (s *Store) setStatus(ctx context.Context, model any, id uuid.UUID, status string) error {
	res := s.db.WithContext(ctx).Model(model).Where("id = ?", id).Update("status", status)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// EnableOrgUnit 重新启用节点；上级必须已经有效。
func (s *Store) EnableOrgUnit(ctx context.Context, unitID uuid.UUID) error {
	u, err := s.getUnit(ctx, unitID)
	if err != nil {
		return err
	}
	if u.Status == StatusActive {
		return nil
	}
	if u.ParentID != nil {
		if err := s.assertUnitActive(ctx, *u.ParentID); err != nil {
			return err
		}
	}
	return s.setStatus(ctx, &OrgUnit{}, unitID, StatusActive)
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

func (s *Store) AppendAudit(ctx context.Context, e audit.Event) error {
	if e.ID == uuid.Nil {
		e.ID = id.New()
	}
	if e.FactoryID == nil {
		fid := s.factoryID
		e.FactoryID = &fid
	}
	return s.db.WithContext(ctx).Create(audit.RowFrom(e)).Error
}

func (s *Store) getUnit(ctx context.Context, unitID uuid.UUID) (OrgUnit, error) {
	var u OrgUnit
	if err := s.db.WithContext(ctx).First(&u, "id = ?", unitID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return OrgUnit{}, domain.ErrNotFound
		}
		return OrgUnit{}, err
	}
	return u, nil
}

func (s *Store) assertUnitActive(ctx context.Context, unitID uuid.UUID) error {
	u, err := s.getUnit(ctx, unitID)
	if err != nil {
		return err
	}
	if u.Status != StatusActive {
		return domain.ErrDisabledOrgUnit
	}
	return nil
}

func (s *Store) assertPersonExists(ctx context.Context, personID uuid.UUID) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ValidScope：工厂超管只能挂厂；组织管理员/负责人只能挂节点；其余三角色两种作用域都可以。
func ValidScope(role, scopeKind string, orgUnitID *uuid.UUID) bool {
	switch role {
	case RoleFactorySuperAdmin:
		return scopeKind == ScopeFactory && orgUnitID == nil
	case RoleOrgAdmin, RoleOrgLead:
		return scopeKind == ScopeOrgUnit && orgUnitID != nil
	case RoleProcessEngineer, RoleOperator, RoleAuditor:
		if scopeKind == ScopeFactory {
			return orgUnitID == nil
		}
		return scopeKind == ScopeOrgUnit && orgUnitID != nil
	default:
		return false
	}
}
