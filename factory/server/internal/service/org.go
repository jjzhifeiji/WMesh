package service

import (
	"context"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/secret"
)

// SetKeepPouch 工厂超管按人设置退出后是否保留示教器库文件；没设过的人默认留。
func (s *Org) SetKeepPouch(ctx context.Context, token string, personID uuid.UUID, keep bool) (Account, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return Account{}, err
	}
	// 只有工厂超管能改这项。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 不是工厂超管，记下设置退出是否留库被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "set_keep_pouch", personID.String(), audit.Deny)
		return Account{}, err
	}
	// 人员不存在，拒绝这次变更
	if _, err := s.store.PersonByID(ctx, personID); err != nil {
		// 找不到这个人，记下设置退出是否留库被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "set_keep_pouch", personID.String(), audit.Deny)
		return Account{}, err
	}
	// 只改留库标记，不碰密码和会话。
	if err := s.store.SetKeepPouch(ctx, personID, keep); err != nil {
		// 记下设置退出是否留库被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "set_keep_pouch", personID.String(), audit.Deny)
		return Account{}, err
	}
	// 按稳定身份读人员，改名也不变
	p, err := s.store.PersonByID(ctx, personID)
	// 人员不存在，拒绝这次变更
	if err != nil {
		return Account{}, err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &acc.ID, &p.LoginName, "set_keep_pouch", p.ID.String(), audit.Allow); err != nil {
		return Account{}, err
	}
	// 收成不含口令的对外账号，再交回调用方
	return accountOf(p), nil
}

// CreatePerson 由工厂超管创建本厂账号，默认日常密码为登录名+123456，直接有效；不发激活码。
func (s *Org) CreatePerson(ctx context.Context, token, loginName, displayName string) (Account, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return Account{}, err
	}
	// 只有工厂超管能建本厂账号。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 不是工厂超管，记下建账号被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, &loginName, "create_person", loginName, audit.Deny)
		return Account{}, err
	}
	// 写下本厂新账号，口令原文不进审计
	p, err := s.store.CreatePerson(ctx, loginName, displayName, false)
	// 账号写不进去，不留下半截人员
	if err != nil {
		// 记下建账号被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, &loginName, "create_person", loginName, audit.Deny)
		return Account{}, err
	}
	// 默认日常密码只存哈希，当场转有效。
	hash, err := secret.HashPassword(secret.DefaultPersonPassword(loginName))
	// 重置口令拼不出来，中止
	if err != nil {
		return Account{}, err
	}
	// 激活落库失败，账号仍待启用
	if err := s.store.ActivatePerson(ctx, p.ID, hash); err != nil {
		return Account{}, err
	}
	// 按稳定身份读人员，改名也不变
	p, err = s.store.PersonByID(ctx, p.ID)
	// 人员不存在，拒绝这次变更
	if err != nil {
		return Account{}, err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &acc.ID, &loginName, "create_person", p.ID.String(), audit.Allow); err != nil {
		return Account{}, err
	}
	// 收成不含口令的对外账号，再交回调用方
	return accountOf(p), nil
}

// CreateOrgUnit 挂本厂组织节点。无父节点表示直挂工厂，须整厂管理权。
func (s *Org) CreateOrgUnit(ctx context.Context, token, name string, parentID *uuid.UUID) (OrgUnit, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return OrgUnit{}, err
	}
	// 挂到父节点须对该父有组织管理权；无父须整厂管理权。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageOrg, parentID); err != nil {
		// 管不到这个组织节点，记下建组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "create_org_unit", name, audit.Deny)
		return OrgUnit{}, err
	}
	// 无父表示直挂工厂；成环由库拒绝。
	row, err := s.store.CreateOrgUnit(ctx, name, parentID)
	// 写入失败则停住，避免留下半截状态
	if err != nil {
		// 记下建组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "create_org_unit", name, audit.Deny)
		return OrgUnit{}, err
	}
	// 建组织成功后记审计，再把结果交回
	return row, s.audit(ctx, &acc.ID, nil, "create_org_unit", row.ID.String(), audit.Allow)
}

// ReparentOrgUnit 改挂父节点；跨厂、成环、或组织管理员越出自己子树都拒绝。
func (s *Org) ReparentOrgUnit(ctx context.Context, token string, unitID uuid.UUID, parentID *uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 原节点和新父都要在作用域内。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageOrg, &unitID); err != nil {
		// 管不到这个组织节点，记下改挂组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "reparent", unitID.String(), audit.Deny)
		return err
	}
	// 管不到这个组织节点，拒绝改结构
	if err := s.can(ctx, acc, permManageOrg, parentID); err != nil {
		// 管不到这个组织节点，记下改挂组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "reparent", unitID.String(), audit.Deny)
		return err
	}
	// 改挂父节点；成环由库拒绝。
	if err := s.store.ReparentOrgUnit(ctx, unitID, parentID); err != nil {
		// 记下改挂组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "reparent", unitID.String(), audit.Deny)
		return err
	}
	// 改挂组织成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "reparent", unitID.String(), audit.Allow)
}

// AddParent 拒绝给节点加第二个父节点，树必须保持单父。
func (s *Org) AddParent(ctx context.Context, token string, unitID, parentID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 记下加第二个父被拒绝，写失败不改变结果
	_ = s.audit(ctx, &acc.ID, nil, "add_parent", unitID.String(), audit.Deny)
	return domain.ErrMultiParent
}

// Assign 把人员放到本厂恰好一个有效节点；已有有效分配再分则拒绝。分配不等于授权。
func (s *Org) Assign(ctx context.Context, token string, personID, unitID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 对该节点有分配权才能放人。失败一律记拒绝。
	if err := s.can(ctx, acc, permAssign, &unitID); err != nil {
		// 没有分配权，记下分配人员被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "assign", personID.String()+" "+unitID.String(), audit.Deny)
		return err
	}
	// 恰好一个有效分配；再分则拒绝。分配不等于授权。
	if _, err := s.store.Assign(ctx, personID, unitID); err != nil {
		// 记下分配人员被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "assign", personID.String()+" "+unitID.String(), audit.Deny)
		return err
	}
	// 分配人员成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "assign", personID.String()+" "+unitID.String(), audit.Allow)
}

// Unassign 取消当前分配，只影响之后的工作上下文，不改账号归属或历史事实。
func (s *Org) Unassign(ctx context.Context, token string, personID, unitID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 没有分配权，拒绝把人挂到该节点
	if err := s.can(ctx, acc, permAssign, &unitID); err != nil {
		// 没有分配权，记下取消分配被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "unassign", personID.String(), audit.Deny)
		return err
	}
	// 取消当前分配，不改历史事实。
	if err := s.store.Unassign(ctx, personID, unitID); err != nil {
		// 记下取消分配被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "unassign", personID.String(), audit.Deny)
		return err
	}
	// 取消分配成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "unassign", personID.String(), audit.Allow)
}

// GrantRole 授予带作用域的固定角色；组织管理员不能授工厂超管，也不能授到作用域外。
func (s *Org) GrantRole(ctx context.Context, token string, personID uuid.UUID, role, scopeKind string, orgUnitID *uuid.UUID) (RoleGrant, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return RoleGrant{}, err
	}
	// 授角色须覆盖目标作用域。失败一律记拒绝。
	if err := s.canGrant(ctx, acc, role, scopeKind, orgUnitID); err != nil {
		// 没有这份许可，记下授角色被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "grant_role", role, audit.Deny)
		return RoleGrant{}, err
	}
	// 写下带作用域的角色，待启用先不生效
	row, err := s.store.GrantRole(ctx, personID, role, scopeKind, orgUnitID)
	// 授权写不进去，不留下半截授予
	if err != nil {
		// 记下授角色被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "grant_role", role, audit.Deny)
		return RoleGrant{}, err
	}
	// 授角色成功后记审计，再把结果交回
	return row, s.audit(ctx, &acc.ID, nil, "grant_role", row.ID.String(), audit.Allow)
}

// RevokeRole 收回角色；收回后旧会话上的新操作立刻按新角色判定，最后一名厂级超管不能收。
func (s *Org) RevokeRole(ctx context.Context, token string, grantID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 按身份读这一条角色授予
	g, err := s.store.GrantByID(ctx, grantID)
	// 授予不存在，拒绝收回
	if err != nil {
		return err
	}
	// 超出可授范围，拒绝这次授权
	if err := s.canGrant(ctx, acc, g.Role, g.ScopeKind, g.OrgUnitID); err != nil {
		// 没有这份许可，记下收回角色被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "revoke_role", grantID.String(), audit.Deny)
		return err
	}
	// 会变成没有超管入口，拒绝收回
	if err := s.guardLastAdminOnRevoke(ctx, g); err != nil {
		// 会去掉最后一名有效超管，记下收回角色被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "revoke_role", grantID.String(), audit.Deny)
		return err
	}
	// 收回后新操作立刻按新角色判定。
	if err := s.store.RevokeRole(ctx, grantID); err != nil {
		// 记下收回角色被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "revoke_role", grantID.String(), audit.Deny)
		return err
	}
	// 收回角色成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "revoke_role", grantID.String(), audit.Allow)
}

// Operate 是节点上的受保护业务动作：必须有覆盖该节点的操作员或工程师角色，分配不够。
func (s *Org) Operate(ctx context.Context, token string, unitID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 没有覆盖该节点的操作员角色，拒绝操作
	if err := s.can(ctx, acc, permOperate, &unitID); err != nil {
		// 没有覆盖该节点的操作员角色，记下节点操作被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "operate", unitID.String(), audit.Deny)
		return err
	}
	// 节点操作成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "operate", unitID.String(), audit.Allow)
}

// ViewOrg 只读查看作用域内组织；组织负责人、审计员可以，但不能改结构。
func (s *Org) ViewOrg(ctx context.Context, token string, unitID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 没有只读许可，拒绝查看
	if err := s.can(ctx, acc, permView, &unitID); err != nil {
		// 没有只读许可，记下查看组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "view_org", unitID.String(), audit.Deny)
		return err
	}
	// 查看组织成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "view_org", unitID.String(), audit.Allow)
}

// DeleteOrgUnit 无引用才删；有下级、当前人员、有效角色或历史事实就拒绝。
func (s *Org) DeleteOrgUnit(ctx context.Context, token string, unitID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 管不到这个组织节点，拒绝改结构
	if err := s.can(ctx, acc, permManageOrg, &unitID); err != nil {
		// 管不到这个组织节点，记下删除组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "delete_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 无引用才删；有下级或历史事实就拒绝。
	if err := s.store.DeleteOrgUnit(ctx, unitID); err != nil {
		// 记下删除组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "delete_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 删除组织成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "delete_org_unit", unitID.String(), audit.Allow)
}

// DisableOrgUnit 停用组织节点；还有有效子节点时拒绝。旧分配留下，但不能再当新上下文。
func (s *Org) DisableOrgUnit(ctx context.Context, token string, unitID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 管不到这个组织节点，拒绝改结构
	if err := s.can(ctx, acc, permManageOrg, &unitID); err != nil {
		// 管不到这个组织节点，记下停用组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "disable_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 有有效子节点时拒绝；旧分配留下，但不能再当新上下文。
	if err := s.store.DisableOrgUnit(ctx, unitID); err != nil {
		// 记下停用组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "disable_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 停用组织成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "disable_org_unit", unitID.String(), audit.Allow)
}

// EnableOrgUnit 重新启用组织节点；上级仍停用时拒绝。
func (s *Org) EnableOrgUnit(ctx context.Context, token string, unitID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 管不到这个组织节点，拒绝改结构
	if err := s.can(ctx, acc, permManageOrg, &unitID); err != nil {
		// 管不到这个组织节点，记下启用组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "enable_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 上级仍停用时拒绝。
	if err := s.store.EnableOrgUnit(ctx, unitID); err != nil {
		// 记下启用组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "enable_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 启用组织成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "enable_org_unit", unitID.String(), audit.Allow)
}

// RenameOrgUnit 改显示名，不改稳定身份，也不改已经落下的历史路径。
func (s *Org) RenameOrgUnit(ctx context.Context, token string, unitID uuid.UUID, name string) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 管不到这个组织节点，拒绝改结构
	if err := s.can(ctx, acc, permManageOrg, &unitID); err != nil {
		// 管不到这个组织节点，记下组织改名被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "rename_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 只改显示名，不改已落历史路径。
	if err := s.store.RenameOrgUnit(ctx, unitID, name); err != nil {
		// 记下组织改名被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "rename_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 组织改名成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "rename_org_unit", unitID.String(), audit.Allow)
}

// ListPersonLogins 给能管账号的人看这个人的示教器登录现场。
func (s *Org) ListPersonLogins(ctx context.Context, token string, personID uuid.UUID) ([]PersonLoginLog, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return nil, err
	}
	// 不是工厂超管，拒绝这次账号或策略操作
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		return nil, err
	}
	// 给能管账号的人看登录现场，再交回调用方
	return s.store.ListPersonLogins(ctx, personID)
}
