package service

import (
	"context"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
)

// perm 是厂内受保护动作。没有明确角色作用域就默认拒绝。
type perm int

const (
	permManageOrg perm = iota // 组织节点：超管或子树内组织管理员
	permManageAccount         // 建停账号：仅工厂超管
	permAssign                    // 人员分配：超管或子树内组织管理员
	permGrant                     // 占位，实际走 canGrant
	permView                      // 只读查看
	permOperate                   // 节点上的业务操作，不含超管自动经营权
)

func (s *Service) grantsOf(ctx context.Context, personID uuid.UUID) ([]RoleGrant, error) {
	return s.store.ActiveGrants(ctx, personID)
}

func isFactorySA(grants []RoleGrant) bool {
	for _, g := range grants {
		if g.Role == RoleFactorySuperAdmin && g.ScopeKind == ScopeFactory {
			return true
		}
	}
	return false
}

func (s *Service) covers(ctx context.Context, grants []RoleGrant, unit *uuid.UUID) (bool, error) {
	for _, g := range grants {
		// Factory 作用域覆盖本厂当时全部节点。
		if g.ScopeKind == ScopeFactory {
			return true, nil
		}
		if unit != nil && g.OrgUnitID != nil {
			ok, err := s.store.InSubtree(ctx, *g.OrgUnitID, *unit)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
	}
	return false, nil
}

func withRoles(grants []RoleGrant, roles ...string) []RoleGrant {
	allow := map[string]struct{}{}
	for _, r := range roles {
		allow[r] = struct{}{}
	}
	out := make([]RoleGrant, 0, len(grants))
	for _, g := range grants {
		if _, ok := allow[g.Role]; ok {
			out = append(out, g)
		}
	}
	return out
}

// can 按角色并集判断；组织管理员不能管父节点或兄弟分支。
func (s *Service) can(ctx context.Context, acc Account, p perm, unit *uuid.UUID) error {
	grants, err := s.grantsOf(ctx, acc.ID)
	if err != nil {
		return err
	}
	switch p {
	case permManageAccount:
		if isFactorySA(grants) {
			return nil
		}
		return domain.ErrForbidden
	case permManageOrg, permAssign:
		if isFactorySA(grants) {
			if unit == nil {
				return nil
			}
			if _, err := s.store.Unit(ctx, *unit); err != nil {
				return err
			}
			return nil
		}
		ok, err := s.covers(ctx, withRoles(grants, RoleOrgAdmin), unit)
		if err != nil {
			return err
		}
		if ok && unit != nil {
			return nil
		}
		return domain.ErrForbidden
	case permView:
		if isFactorySA(grants) {
			return nil
		}
		ok, err := s.covers(ctx, withRoles(grants, RoleOrgAdmin, RoleOrgLead, RoleAuditor), unit)
		if err != nil {
			return err
		}
		if ok && unit != nil {
			return nil
		}
		return domain.ErrForbidden
	case permOperate:
		if unit == nil {
			return domain.ErrForbidden
		}
		ok, err := s.covers(ctx, withRoles(grants, RoleOperator, RoleProcessEngineer), unit)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		return domain.ErrForbidden
	case permGrant:
		return domain.ErrForbidden
	default:
		return domain.ErrForbidden
	}
}

// canGrant：超管可授本厂全部固定角色；组织管理员只能在当前子树内授非超管角色。
func (s *Service) canGrant(ctx context.Context, acc Account, role, scopeKind string, orgUnitID *uuid.UUID) error {
	if !validScope(role, scopeKind, orgUnitID) {
		return domain.ErrInvalidRoleScope
	}
	grants, err := s.grantsOf(ctx, acc.ID)
	if err != nil {
		return err
	}
	if isFactorySA(grants) {
		return nil
	}
	// 组织管理员不能把工厂超管授出去，也不能授到自己子树之外。
	if role == RoleFactorySuperAdmin || scopeKind != ScopeOrgUnit || orgUnitID == nil {
		return domain.ErrForbidden
	}
	ok, err := s.covers(ctx, withRoles(grants, RoleOrgAdmin), orgUnitID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrForbidden
	}
	return nil
}

// guardLastAdminOnDisable 在激活后生效；待启用超管不计入有效管理入口。
func (s *Service) guardLastAdminOnDisable(ctx context.Context, personID uuid.UUID) error {
	p, err := s.store.PersonByID(ctx, personID)
	if err != nil {
		return err
	}
	if p.Status != StatusActive {
		return nil
	}
	isSA, err := s.store.PersonIsActiveFactorySA(ctx, personID)
	if err != nil {
		return err
	}
	if !isSA {
		return nil
	}
	n, err := s.store.ActiveFactorySuperAdminCount(ctx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return domain.ErrLastAdmin
	}
	return nil
}

func (s *Service) guardLastAdminOnRevoke(ctx context.Context, g RoleGrant) error {
	if g.Status != StatusActive || g.Role != RoleFactorySuperAdmin || g.ScopeKind != ScopeFactory {
		return nil
	}
	p, err := s.store.PersonByID(ctx, g.PersonID)
	if err != nil {
		return err
	}
	if p.Status != StatusActive {
		return nil
	}
	n, err := s.store.ActiveFactorySuperAdminCount(ctx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return domain.ErrLastAdmin
	}
	return nil
}
