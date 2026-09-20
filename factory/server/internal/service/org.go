package service

import (
	"context"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/secret"
)

// CreatePerson 由工厂超管创建本厂账号，默认日常密码为登录名+123456，直接有效；不发激活码。
func (s *Org) CreatePerson(ctx context.Context, token, loginName, displayName string) (Account, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Account{}, err
	}
	// 只有工厂超管能建本厂账号。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, &loginName, "create_person", loginName, audit.Deny)
		return Account{}, err
	}
	p, err := s.store.CreatePerson(ctx, loginName, displayName, false)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, &loginName, "create_person", loginName, audit.Deny)
		return Account{}, err
	}
	// 默认日常密码只存哈希，当场转有效。
	hash, err := secret.HashPassword(secret.DefaultPersonPassword(loginName))
	if err != nil {
		return Account{}, err
	}
	if err := s.store.ActivatePerson(ctx, p.ID, hash); err != nil {
		return Account{}, err
	}
	p, err = s.store.PersonByID(ctx, p.ID)
	if err != nil {
		return Account{}, err
	}
	if err := s.audit(ctx, &acc.ID, &loginName, "create_person", p.ID.String(), audit.Allow); err != nil {
		return Account{}, err
	}
	return accountOf(p), nil
}

// CreateOrgUnit 挂本厂组织节点。无父节点表示直挂工厂，须整厂管理权。
func (s *Org) CreateOrgUnit(ctx context.Context, token, name string, parentID *uuid.UUID) (OrgUnit, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return OrgUnit{}, err
	}
	// 挂到父节点须对该父有组织管理权；无父须整厂管理权。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageOrg, parentID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "create_org_unit", name, audit.Deny)
		return OrgUnit{}, err
	}
	// 无父表示直挂工厂；成环由库拒绝。
	row, err := s.store.CreateOrgUnit(ctx, name, parentID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "create_org_unit", name, audit.Deny)
		return OrgUnit{}, err
	}
	return row, s.audit(ctx, &acc.ID, nil, "create_org_unit", row.ID.String(), audit.Allow)
}

// ReparentOrgUnit 改挂父节点；跨厂、成环、或组织管理员越出自己子树都拒绝。
func (s *Org) ReparentOrgUnit(ctx context.Context, token string, unitID uuid.UUID, parentID *uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	// 原节点和新父都要在作用域内。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageOrg, &unitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "reparent", unitID.String(), audit.Deny)
		return err
	}
	if err := s.can(ctx, acc, permManageOrg, parentID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "reparent", unitID.String(), audit.Deny)
		return err
	}
	// 改挂父节点；成环由库拒绝。
	if err := s.store.ReparentOrgUnit(ctx, unitID, parentID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "reparent", unitID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "reparent", unitID.String(), audit.Allow)
}

// AddParent 拒绝给节点加第二个父节点，树必须保持单父。
func (s *Org) AddParent(ctx context.Context, token string, unitID, parentID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	_ = s.audit(ctx, &acc.ID, nil, "add_parent", unitID.String(), audit.Deny)
	return domain.ErrMultiParent
}

// Assign 把人员放到本厂恰好一个有效节点；已有有效分配再分则拒绝。分配不等于授权。
func (s *Org) Assign(ctx context.Context, token string, personID, unitID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	// 对该节点有分配权才能放人。失败一律记拒绝。
	if err := s.can(ctx, acc, permAssign, &unitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "assign", personID.String()+" "+unitID.String(), audit.Deny)
		return err
	}
	// 恰好一个有效分配；再分则拒绝。分配不等于授权。
	if _, err := s.store.Assign(ctx, personID, unitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "assign", personID.String()+" "+unitID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "assign", personID.String()+" "+unitID.String(), audit.Allow)
}

// Unassign 取消当前分配，只影响之后的工作上下文，不改账号归属或历史事实。
func (s *Org) Unassign(ctx context.Context, token string, personID, unitID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	if err := s.can(ctx, acc, permAssign, &unitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "unassign", personID.String(), audit.Deny)
		return err
	}
	// 取消当前分配，不改历史事实。
	if err := s.store.Unassign(ctx, personID, unitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "unassign", personID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "unassign", personID.String(), audit.Allow)
}

// GrantRole 授予带作用域的固定角色；组织管理员不能授工厂超管，也不能授到作用域外。
func (s *Org) GrantRole(ctx context.Context, token string, personID uuid.UUID, role, scopeKind string, orgUnitID *uuid.UUID) (RoleGrant, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return RoleGrant{}, err
	}
	// 授角色须覆盖目标作用域。失败一律记拒绝。
	if err := s.canGrant(ctx, acc, role, scopeKind, orgUnitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "grant_role", role, audit.Deny)
		return RoleGrant{}, err
	}
	row, err := s.store.GrantRole(ctx, personID, role, scopeKind, orgUnitID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "grant_role", role, audit.Deny)
		return RoleGrant{}, err
	}
	return row, s.audit(ctx, &acc.ID, nil, "grant_role", row.ID.String(), audit.Allow)
}

// RevokeRole 收回角色；收回后旧会话上的新操作立刻按新角色判定，最后一名厂级超管不能收。
func (s *Org) RevokeRole(ctx context.Context, token string, grantID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	g, err := s.store.GrantByID(ctx, grantID)
	if err != nil {
		return err
	}
	if err := s.canGrant(ctx, acc, g.Role, g.ScopeKind, g.OrgUnitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "revoke_role", grantID.String(), audit.Deny)
		return err
	}
	if err := s.guardLastAdminOnRevoke(ctx, g); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "revoke_role", grantID.String(), audit.Deny)
		return err
	}
	// 收回后新操作立刻按新角色判定。
	if err := s.store.RevokeRole(ctx, grantID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "revoke_role", grantID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "revoke_role", grantID.String(), audit.Allow)
}

// Operate 是节点上的受保护业务动作：必须有覆盖该节点的操作员或工程师角色，分配不够。
func (s *Org) Operate(ctx context.Context, token string, unitID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	if err := s.can(ctx, acc, permOperate, &unitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "operate", unitID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "operate", unitID.String(), audit.Allow)
}

// ViewOrg 只读查看作用域内组织；组织负责人、审计员可以，但不能改结构。
func (s *Org) ViewOrg(ctx context.Context, token string, unitID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	if err := s.can(ctx, acc, permView, &unitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "view_org", unitID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "view_org", unitID.String(), audit.Allow)
}

// DeleteOrgUnit 无引用才删；有下级、当前人员、有效角色或历史事实就拒绝。
func (s *Org) DeleteOrgUnit(ctx context.Context, token string, unitID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	if err := s.can(ctx, acc, permManageOrg, &unitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "delete_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 无引用才删；有下级或历史事实就拒绝。
	if err := s.store.DeleteOrgUnit(ctx, unitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "delete_org_unit", unitID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "delete_org_unit", unitID.String(), audit.Allow)
}

// DisableOrgUnit 停用组织节点；还有有效子节点时拒绝。旧分配留下，但不能再当新上下文。
func (s *Org) DisableOrgUnit(ctx context.Context, token string, unitID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	if err := s.can(ctx, acc, permManageOrg, &unitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "disable_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 有有效子节点时拒绝；旧分配留下，但不能再当新上下文。
	if err := s.store.DisableOrgUnit(ctx, unitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "disable_org_unit", unitID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "disable_org_unit", unitID.String(), audit.Allow)
}

// EnableOrgUnit 重新启用组织节点；上级仍停用时拒绝。
func (s *Org) EnableOrgUnit(ctx context.Context, token string, unitID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	if err := s.can(ctx, acc, permManageOrg, &unitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "enable_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 上级仍停用时拒绝。
	if err := s.store.EnableOrgUnit(ctx, unitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "enable_org_unit", unitID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "enable_org_unit", unitID.String(), audit.Allow)
}

// RenameOrgUnit 改显示名，不改稳定身份，也不改已经落下的历史路径。
func (s *Org) RenameOrgUnit(ctx context.Context, token string, unitID uuid.UUID, name string) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	if err := s.can(ctx, acc, permManageOrg, &unitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "rename_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 只改显示名，不改已落历史路径。
	if err := s.store.RenameOrgUnit(ctx, unitID, name); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "rename_org_unit", unitID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "rename_org_unit", unitID.String(), audit.Allow)
}

// ListPersonLogins 给能管账号的人看这个人的示教器登录现场。
func (s *Org) ListPersonLogins(ctx context.Context, token string, personID uuid.UUID) ([]PersonLoginLog, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		return nil, err
	}
	return s.store.ListPersonLogins(ctx, personID)
}
