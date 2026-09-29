package service

import (
	"context"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
)

// perm 是厂内受保护动作。没有明确角色作用域就默认拒绝。
type perm int

const (
	permManageOrg     perm = iota // 组织节点：超管或覆盖该处的组织管理员
	permManageAccount             // 建停账号：仅工厂超管
	permAssign                    // 人员分配：超管或覆盖该处的组织管理员
	permGrant                     // 占位，实际走 canGrant
	permView                      // 只读查看
	permOperate                   // 节点上的业务操作，不含超管自动经营权
)

// grantsOf 只取当前有效角色；已收回的不参与判定。
func (s *kernel) grantsOf(ctx context.Context, personID uuid.UUID) ([]RoleGrant, error) {
	// 只取仍有效的角色，再交回调用方
	return s.store.ActiveGrants(ctx, personID)
}

// isFactorySA 是否持有本厂作用域的有效超管角色。
func isFactorySA(grants []RoleGrant) bool {
	// 逐条有效授权看是否盖住目标
	for _, g := range grants {
		// 整厂作用域盖住全部节点
		if g.Role == RoleFactorySuperAdmin && g.ScopeKind == ScopeFactory {
			return true
		}
	}
	return false
}

// covers 厂级覆盖全厂；组织作用域只覆盖当时子树。
func (s *kernel) covers(ctx context.Context, grants []RoleGrant, unit *uuid.UUID) (bool, error) {
	// 逐条有效授权看是否盖住目标
	for _, g := range grants {
		// Factory 作用域覆盖本厂当时全部节点。
		if g.ScopeKind == ScopeFactory {
			return true, nil
		}
		// 分配到了节点就按该节点钉路径
		if unit != nil && g.OrgUnitID != nil {
			// 子树覆盖才算有权。
			ok, err := s.store.InSubtree(ctx, *g.OrgUnitID, *unit)
			// 子树判断失败则拒绝，不猜是否覆盖
			if err != nil {
				return false, err
			}
			// 取到了才继续，没取到则拒绝或跳过
			if ok {
				return true, nil
			}
		}
	}
	return false, nil
}

// withRoles 只留下指定角色的授予，用来收窄判定面。
func withRoles(grants []RoleGrant, roles ...string) []RoleGrant {
	// 收成允许的角色集合，用来收窄判定
	allow := map[string]struct{}{}
	// 逐条处理，某一条失败不把整批悄悄算成功
	for _, r := range roles {
		// 把这个角色放进允许集合
		allow[r] = struct{}{}
	}
	// 按条数决定是空、超限还是继续
	out := make([]RoleGrant, 0, len(grants))
	// 逐条有效授权看是否盖住目标
	for _, g := range grants {
		// 这条授权的角色在收窄范围内才参与判定
		if _, ok := allow[g.Role]; ok {
			// 把这一条收进结果，漏了清单就不齐
			out = append(out, g)
		}
	}
	return out
}

// can 按角色并集判断；组织管理员不能管父节点或兄弟分支。
func (s *kernel) can(ctx context.Context, acc Account, p perm, unit *uuid.UUID) error {
	// 只取当前有效角色，收回的不参与
	grants, err := s.grantsOf(ctx, acc.ID)
	// 角色读失败就无法判定，拒绝放行
	if err != nil {
		return err
	}
	// 按动作种类收窄角色，没有明确许可就拒绝
	switch p {
	// 管账号只给工厂超管，其他人拒绝
	case permManageAccount:
		// 持有厂级超管则盖住全厂，不必再查子树
		if isFactorySA(grants) {
			return nil
		}
		return domain.ErrForbidden
	// 管组织或分配：超管，或覆盖该节点的组织管理员
	case permManageOrg, permAssign:
		// 持有厂级超管则盖住全厂，不必再查子树
		if isFactorySA(grants) {
			// 没指定节点时只有整厂权限才能过
			if unit == nil {
				return nil
			}
			// 超管也要目标节点仍在本厂。
			if _, err := s.store.Unit(ctx, *unit); err != nil {
				return err
			}
			return nil
		}
		// 只留下指定角色，收窄判定面
		ok, err := s.covers(ctx, withRoles(grants, RoleOrgAdmin), unit)
		// 角色筛完仍为空则后面会拒绝
		if err != nil {
			return err
		}
		// 整厂管理员 covers(nil) 为真，可建厂直属根节点；节点管理员 covers(nil) 仍假。
		if ok {
			return nil
		}
		return domain.ErrForbidden
	// 只读看超管，或负责人、审计员是否盖住
	case permView:
		// 持有厂级超管则盖住全厂，不必再查子树
		if isFactorySA(grants) {
			return nil
		}
		// 只留下指定角色，收窄判定面
		ok, err := s.covers(ctx, withRoles(grants, RoleOrgAdmin, RoleOrgLead, RoleAuditor), unit)
		// 角色筛完仍为空则后面会拒绝
		if err != nil {
			return err
		}
		// 取到了才继续，没取到则拒绝或跳过
		if ok {
			return nil
		}
		return domain.ErrForbidden
	// 操作必须有覆盖该节点的操作员角色
	case permOperate:
		// 没指定节点时只有整厂权限才能过
		if unit == nil {
			return domain.ErrForbidden
		}
		// 只留下指定角色，收窄判定面
		ok, err := s.covers(ctx, withRoles(grants, RoleOperator), unit)
		// 角色筛完仍为空则后面会拒绝
		if err != nil {
			return err
		}
		// 取到了才继续，没取到则拒绝或跳过
		if ok {
			return nil
		}
		return domain.ErrForbidden
	// 授权不在这里放行，走单独的授角规则
	case permGrant:
		return domain.ErrForbidden
	// 没列出的动作一律拒绝，避免漏放行
	default:
		// 未列明的动作一律拒绝。
		return domain.ErrForbidden
	}
}

// canGrant：超管可授本厂全部固定角色；组织管理员只能在自己作用域内授非超管角色。
func (s *kernel) canGrant(ctx context.Context, acc Account, role, scopeKind string, orgUnitID *uuid.UUID) error {
	// 条件不成立则拒绝或跳过，不往下改数据
	if !validScope(role, scopeKind, orgUnitID) {
		return domain.ErrInvalidRoleScope
	}
	// 只取当前有效角色，收回的不参与
	grants, err := s.grantsOf(ctx, acc.ID)
	// 角色读失败就无法判定，拒绝放行
	if err != nil {
		return err
	}
	// 持有厂级超管则盖住全厂，不必再查子树
	if isFactorySA(grants) {
		return nil
	}
	// 不能把工厂超管授出去；整厂角色须自己也是整厂管理员，节点角色不能越出当前子树。
	if role == RoleFactorySuperAdmin {
		return domain.ErrForbidden
	}
	// 只留下指定角色，收窄判定面
	ok, err := s.covers(ctx, withRoles(grants, RoleOrgAdmin), orgUnitID)
	// 角色筛完仍为空则后面会拒绝
	if err != nil {
		return err
	}
	// 没有取到则按缺失拒绝或留空
	if !ok {
		return domain.ErrForbidden
	}
	return nil
}

// guardLastAdminOnDisable 在激活后生效；待启用超管不计入有效管理入口。
func (s *kernel) guardLastAdminOnDisable(ctx context.Context, personID uuid.UUID) error {
	// 按稳定身份读人员，改名也不变
	p, err := s.store.PersonByID(ctx, personID)
	// 人员不存在，拒绝这次变更
	if err != nil {
		return err
	}
	// 不是有效状态则拒绝，待启用和停用都不能做
	if p.Status != StatusActive {
		return nil
	}
	// 数还剩几名有效厂级超管
	isSA, err := s.store.PersonIsActiveFactorySA(ctx, personID)
	// 超管计数失败，为防锁死而拒绝
	if err != nil {
		return err
	}
	// 条件不成立则拒绝或跳过，不往下改数据
	if !isSA {
		return nil
	}
	// 有效超管入口还剩一个就不能停。
	n, err := s.store.ActiveFactorySuperAdminCount(ctx)
	// 计数失败则拒绝，以免关掉最后入口
	if err != nil {
		return err
	}
	// 条数超过上限就停，避免把袋或报表撑满
	if n <= 1 {
		return domain.ErrLastAdmin
	}
	return nil
}

// guardLastAdminOnRevoke 收回厂级超管时，至少留一名有效超管入口。
func (s *kernel) guardLastAdminOnRevoke(ctx context.Context, g RoleGrant) error {
	// 不是有效状态则拒绝，待启用和停用都不能做
	if g.Status != StatusActive || g.Role != RoleFactorySuperAdmin || g.ScopeKind != ScopeFactory {
		return nil
	}
	// 按稳定身份读人员，改名也不变
	p, err := s.store.PersonByID(ctx, g.PersonID)
	// 人员不存在，拒绝这次变更
	if err != nil {
		return err
	}
	// 不是有效状态则拒绝，待启用和停用都不能做
	if p.Status != StatusActive {
		return nil
	}
	// 有效超管入口还剩一个就不能收。
	n, err := s.store.ActiveFactorySuperAdminCount(ctx)
	// 计数失败则拒绝，以免关掉最后入口
	if err != nil {
		return err
	}
	// 条数超过上限就停，避免把袋或报表撑满
	if n <= 1 {
		return domain.ErrLastAdmin
	}
	return nil
}
