package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
)

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

func (s *Store) ListAudit(ctx context.Context) ([]audit.Row, error) {
	var rows []audit.Row
	err := s.db.WithContext(ctx).Order("occurred_at").Find(&rows).Error
	return rows, err
}

func (s *Store) PersonCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Person{}).Count(&n).Error
	return n, err
}

func (s *Store) RoleGrantCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&RoleGrant{}).Count(&n).Error
	return n, err
}

func (s *Store) ActiveGrants(ctx context.Context, personID uuid.UUID) ([]RoleGrant, error) {
	var rows []RoleGrant
	err := s.db.WithContext(ctx).Where("person_id = ? AND status = ?", personID, StatusActive).Find(&rows).Error
	return rows, err
}

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

func (s *Store) PersonIsActiveFactorySA(ctx context.Context, personID uuid.UUID) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&RoleGrant{}).
		Where("person_id = ? AND status = ? AND role = ? AND scope_kind = ?", personID, StatusActive, RoleFactorySuperAdmin, ScopeFactory).
		Count(&n).Error
	return n > 0, err
}

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

func (s *Store) AssignmentCount(ctx context.Context, personID uuid.UUID) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Assignment{}).
		Where("person_id = ? AND status = ?", personID, StatusActive).
		Count(&n).Error
	return n, err
}

// ListPeople 列出本厂全部人员行；调用方不得把口令哈希交给前端。
func (s *Store) ListPeople(ctx context.Context) ([]Person, error) {
	var rows []Person
	err := s.db.WithContext(ctx).Order("created_at").Find(&rows).Error
	return rows, err
}

// ListOrgUnits 列出本厂组织节点。
func (s *Store) ListOrgUnits(ctx context.Context) ([]OrgUnit, error) {
	var rows []OrgUnit
	err := s.db.WithContext(ctx).Order("created_at").Find(&rows).Error
	return rows, err
}

// ActiveAssignments 列出某人当前有效的组织分配。
func (s *Store) ActiveAssignments(ctx context.Context, personID uuid.UUID) ([]Assignment, error) {
	var rows []Assignment
	err := s.db.WithContext(ctx).Where("person_id = ? AND status = ?", personID, StatusActive).Order("created_at").Find(&rows).Error
	return rows, err
}

// ListAssignments 列出当前有效人员分配。
func (s *Store) ListAssignments(ctx context.Context) ([]Assignment, error) {
	var rows []Assignment
	err := s.db.WithContext(ctx).Where("status = ?", StatusActive).Order("created_at").Find(&rows).Error
	return rows, err
}

// ListRoleGrants 列出当前有效角色授予。
func (s *Store) ListRoleGrants(ctx context.Context) ([]RoleGrant, error) {
	var rows []RoleGrant
	err := s.db.WithContext(ctx).Where("status = ?", StatusActive).Order("created_at").Find(&rows).Error
	return rows, err
}
