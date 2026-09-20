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

// OrgUnit 是本厂一棵树上的节点，至多一个父节点，不能跨厂、不能成环。
type OrgUnit struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"` // 节点稳定身份
	ParentID  *uuid.UUID `gorm:"type:uuid" json:"parentId"`      // 空表示直接挂在工厂下
	Name      string     `gorm:"not null" json:"name"`           // 显示名，改名不改历史快照
	Status    string     `gorm:"not null" json:"status"`         // 组织状态：active / disabled；停用后不能再当新工作上下文
	CreatedAt time.Time  `gorm:"not null" json:"createdAt"`      // 创建时间
}

func (OrgUnit) TableName() string { return "org_units" }

// Assignment 是人员到组织节点的关系；每人最多一条有效分配，取消只把状态标成已结束，不删行。
type Assignment struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`      // 分配关系稳定身份
	PersonID  uuid.UUID  `gorm:"type:uuid;not null" json:"personId"`  // 本厂人员
	OrgUnitID uuid.UUID  `gorm:"type:uuid;not null" json:"orgUnitId"` // 分配到的组织节点
	Status    string     `gorm:"not null" json:"status"`              // active 或 ended；取消不删行
	CreatedAt time.Time  `gorm:"not null" json:"createdAt"`           // 分配开始时间
	EndedAt   *time.Time `json:"endedAt"`                             // 取消分配的时间；有效分配必须为空
}

func (Assignment) TableName() string { return "assignments" }

// RoleGrant 是带明确作用域的一条角色；分配组织不会自动产生本行。
type RoleGrant struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`     // 授予记录稳定身份
	PersonID  uuid.UUID  `gorm:"type:uuid;not null" json:"personId"` // 被授予的本厂人员
	Role      string     `gorm:"not null" json:"role"`               // 六种固定角色之一
	ScopeKind string     `gorm:"not null" json:"scopeKind"`          // factory 或 org_unit
	OrgUnitID *uuid.UUID `gorm:"type:uuid" json:"orgUnitId"`         // Factory 作用域必须为空
	Status    string     `gorm:"not null" json:"status"`             // active 或 revoked
	CreatedAt time.Time  `gorm:"not null" json:"createdAt"`          // 授予时间
	RevokedAt *time.Time `json:"revokedAt"`                          // 收回时间；有效授予必须为空
}

func (RoleGrant) TableName() string { return "role_grants" }

// CreateOrgUnit 在本厂树上新建节点；父节点必须有效。
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
		// 库约束挡住自挂为父。
		if domain.IsCheckViolation(err) {
			return OrgUnit{}, domain.ErrCycle
		}
		return OrgUnit{}, err
	}
	return row, nil
}

// RenameOrgUnit 只改显示名，不改已落库的路径快照。
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

// wouldCycle 沿新父上走，碰到自己或已见过的节点就是环。
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

// Assign 把人员放到本厂恰好一个有效节点；已有有效分配再分到另一节点则拒绝。
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

// Unassign 取消有效分配：只改状态不删行，留给历史。
func (s *Store) Unassign(ctx context.Context, personID, unitID uuid.UUID) error {
	now := time.Now().UTC()
	// 取消只改状态不删行，留给历史。
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

// RevokeRole 收回有效授予：只改状态不删行。
func (s *Store) RevokeRole(ctx context.Context, grantID uuid.UUID) error {
	now := time.Now().UTC()
	// 收回只改状态不删行。
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
			{&governedAssetRow{}, "org_unit_id = ?", []any{unitID}},
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

// DisableOrgUnit 停用节点；还有有效下级则拒绝。
func (s *Store) DisableOrgUnit(ctx context.Context, unitID uuid.UUID) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&OrgUnit{}).
		Where("parent_id = ? AND status = ?", unitID, StatusActive).
		Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		// 还有有效下级时不能停用。
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

// ValidScope：工厂超管只能挂厂；组织负责人只能挂节点；管理员与其余三角色两种作用域都可以。
func ValidScope(role, scopeKind string, orgUnitID *uuid.UUID) bool {
	switch role {
	case RoleFactorySuperAdmin:
		return scopeKind == ScopeFactory && orgUnitID == nil
	case RoleOrgLead:
		return scopeKind == ScopeOrgUnit && orgUnitID != nil
	case RoleOrgAdmin, RoleProcessEngineer, RoleOperator, RoleAuditor:
		if scopeKind == ScopeFactory {
			return orgUnitID == nil
		}
		return scopeKind == ScopeOrgUnit && orgUnitID != nil
	default:
		return false
	}
}

// RoleGrantCount 数角色授予行，含已收回。
func (s *Store) RoleGrantCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&RoleGrant{}).Count(&n).Error
	return n, err
}

// ActiveGrants 列出某人当前有效角色。
func (s *Store) ActiveGrants(ctx context.Context, personID uuid.UUID) ([]RoleGrant, error) {
	var rows []RoleGrant
	err := s.db.WithContext(ctx).Where("person_id = ? AND status = ?", personID, StatusActive).Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// GrantByID 按身份取一条授予，含已收回。
func (s *Store) GrantByID(ctx context.Context, grantID uuid.UUID) (RoleGrant, error) {
	var row RoleGrant
	if err := s.db.WithContext(ctx).First(&row, "id = ?", grantID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return RoleGrant{}, domain.ErrNotFound
		}
		return RoleGrant{}, err
	}
	return row, nil
}

// ActiveFactorySuperAdminCount 只数「有效账号 + 有效厂级超管角色」，待启用不计。
func (s *Store) ActiveFactorySuperAdminCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Raw(`
		SELECT COUNT(*) FROM people p
		INNER JOIN role_grants g ON g.person_id = p.id
		WHERE p.status = ? AND g.status = ? AND g.role = ? AND g.scope_kind = ?`,
		StatusActive, StatusActive, RoleFactorySuperAdmin, ScopeFactory,
	).Scan(&n).Error
	return n, err
}

// PersonIsActiveFactorySA 是否仍握着有效厂级超管角色。
func (s *Store) PersonIsActiveFactorySA(ctx context.Context, personID uuid.UUID) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&RoleGrant{}).
		Where("person_id = ? AND status = ? AND role = ? AND scope_kind = ?", personID, StatusActive, RoleFactorySuperAdmin, ScopeFactory).
		Count(&n).Error
	return n > 0, err
}

// Unit 按稳定身份取组织节点。
func (s *Store) Unit(ctx context.Context, unitID uuid.UUID) (OrgUnit, error) {
	return s.getUnit(ctx, unitID)
}

// InSubtree 沿当前父指针上走，判断 node 是否在 root 的当前子树里（含自身）。
func (s *Store) InSubtree(ctx context.Context, root, node uuid.UUID) (bool, error) {
	if root == node {
		return true, nil
	}
	current := node
	seen := map[uuid.UUID]struct{}{}
	for {
		if _, ok := seen[current]; ok {
			return false, domain.ErrCycle
		}
		seen[current] = struct{}{}
		u, err := s.getUnit(ctx, current)
		if err != nil {
			return false, err
		}
		if u.ParentID == nil {
			return false, nil
		}
		if *u.ParentID == root {
			return true, nil
		}
		current = *u.ParentID
	}
}

// TryDeleteOrgUnit 仅用于验收「被引用不得物理删除」；业务路径不提供删除。
func (s *Store) TryDeleteOrgUnit(ctx context.Context, unitID uuid.UUID) error {
	return s.db.WithContext(ctx).Exec("DELETE FROM org_units WHERE id = ?", unitID).Error
}

// AssignmentCount 数某人当前有效分配。
func (s *Store) AssignmentCount(ctx context.Context, personID uuid.UUID) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Assignment{}).
		Where("person_id = ? AND status = ?", personID, StatusActive).
		Count(&n).Error
	return n, err
}

// ListOrgUnits 列出本厂组织节点，按创建时间从新到旧。
func (s *Store) ListOrgUnits(ctx context.Context) ([]OrgUnit, error) {
	var rows []OrgUnit
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// ActiveAssignments 列出某人当前有效的组织分配。
func (s *Store) ActiveAssignments(ctx context.Context, personID uuid.UUID) ([]Assignment, error) {
	var rows []Assignment
	err := s.db.WithContext(ctx).Where("person_id = ? AND status = ?", personID, StatusActive).Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// ListAssignments 列出当前有效人员分配。
func (s *Store) ListAssignments(ctx context.Context) ([]Assignment, error) {
	var rows []Assignment
	err := s.db.WithContext(ctx).Where("status = ?", StatusActive).Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// ListRoleGrants 列出当前有效角色授予。
func (s *Store) ListRoleGrants(ctx context.Context) ([]RoleGrant, error) {
	var rows []RoleGrant
	err := s.db.WithContext(ctx).Where("status = ?", StatusActive).Order("created_at DESC").Find(&rows).Error
	return rows, err
}
