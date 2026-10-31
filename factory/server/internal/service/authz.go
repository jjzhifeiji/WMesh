package service

import (
	"context"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// perm 是厂内受保护动作。角色都覆盖整厂，没有对应角色就默认拒绝；超管拥有全部权限。
type perm int

const (
	permManageAccount perm = iota // 人员、组织、角色、设备、策略、更新：仅工厂超管
	permManageAssets              // 厂级工艺工程的制作和治理：超管或管理员
	permView                      // 报表与审计只读：超管或管理员
	permOperate                   // 示教器作业、产生焊事实：超管或操作员
)

// grantsOf 只取当前有效角色；已收回的不参与判定。
func (s *kernel) grantsOf(ctx context.Context, personID uuid.UUID) ([]RoleGrant, error) {
	// 只取仍有效的角色，再交回调用方
	return s.store.ActiveGrants(ctx, personID)
}

// isFactorySA 是否持有有效工厂超管角色。
func isFactorySA(grants []RoleGrant) bool {
	// 有一条超管就算
	return hasRole(grants, RoleFactorySuperAdmin)
}

// hasRole 是否持有其中任一角色。
func hasRole(grants []RoleGrant, roles ...string) bool {
	// 逐条有效授权看角色是否在列
	for _, g := range grants {
		// 逐个比对允许的角色
		for _, r := range roles {
			// 对上就算持有
			if g.Role == r {
				return true
			}
		}
	}
	return false
}

// permRoles 每个动作放行的角色；超管另外总放行。
func permRoles(p perm) []string {
	// 按动作种类给出放行角色，没列的动作只有超管能做
	switch p {
	// 厂级资产和报表审计给管理员
	case permManageAssets, permView:
		return []string{RoleOrgAdmin}
	// 示教器作业给操作员
	case permOperate:
		return []string{RoleOperator}
	// 其余只有超管
	default:
		return nil
	}
}

// can 按角色并集判断；超管拥有全部权限。
func (s *kernel) can(ctx context.Context, acc Account, p perm) error {
	// 只取当前有效角色，收回的不参与
	grants, err := s.grantsOf(ctx, acc.ID)
	// 角色读失败就无法判定，拒绝放行
	if err != nil {
		return err
	}
	// 超管拥有全部权限
	if isFactorySA(grants) {
		return nil
	}
	// 持有这个动作放行的角色才过
	if hasRole(grants, permRoles(p)...) {
		return nil
	}
	return domain.ErrForbidden
}

// canGrant 只有工厂超管能授、收角色；角色须是三种固定角色之一。
func (s *kernel) canGrant(ctx context.Context, acc Account, role string) error {
	// 不认识的角色不能授
	if !store.ValidRole(role) {
		return domain.ErrInvalidRoleScope
	}
	// 授角色归管账号，只给超管
	return s.can(ctx, acc, permManageAccount)
}

// guardLastAdminOnDisable 停用最后一名有效超管前拦住。
// 只是不拿锁的快速预检，并发下以 store.DisablePersonKeepAdmin 在锁内重数为准。
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
// 只是不拿锁的快速预检，并发下以 store.RevokeRoleKeepAdmin 在锁内重数为准。
func (s *kernel) guardLastAdminOnRevoke(ctx context.Context, g RoleGrant) error {
	// 不是有效状态则拒绝，待启用和停用都不能做
	if g.Status != StatusActive || g.Role != RoleFactorySuperAdmin {
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
