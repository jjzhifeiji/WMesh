package service

import (
	"context"
	"strings"
	"unicode/utf8"

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
	if err := s.can(ctx, acc, permManageAccount); err != nil {
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

// normalizeLoginName 去掉首尾空白后须 1～64 字；建档、改名和登录都按规范后的名字认人。
func normalizeLoginName(name string) (string, error) {
	// 去掉首尾空白，表格复制带的空格不进库
	name = strings.TrimSpace(name)
	// 按字数而不是字节数限长
	n := utf8.RuneCountInString(name)
	// 名字空了或超过六十四字，拒绝保存
	if n < 1 || n > 64 {
		return "", domain.ErrInvalidName
	}
	return name, nil
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
	if err := s.can(ctx, acc, permManageAccount); err != nil {
		// 不是工厂超管，记下建账号被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, &loginName, "create_person", loginName, audit.Deny)
		return Account{}, err
	}
	// 先去首尾空白再限长，默认密码也按规范后的名字拼
	name, err := normalizeLoginName(loginName)
	// 名字空了或太长，拒绝建档
	if err != nil {
		// 记下建账号被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, &loginName, "create_person", loginName, audit.Deny)
		return Account{}, err
	}
	// 往下审计、建档、拼默认密码都用规范后的名字
	loginName = name
	// 默认日常密码只存哈希，建档即有效。
	hash, err := secret.HashPassword(secret.DefaultPersonPassword(loginName))
	// 口令摘要失败，中止
	if err != nil {
		return Account{}, err
	}
	// 写下本厂新账号，口令原文不进审计
	p, err := s.store.CreatePerson(ctx, loginName, displayName, hash, false)
	// 账号写不进去，不留下半截人员
	if err != nil {
		// 记下建账号被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, &loginName, "create_person", loginName, audit.Deny)
		return Account{}, err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &acc.ID, &loginName, "create_person", p.ID.String(), audit.Allow); err != nil {
		return Account{}, err
	}
	// 收成不含口令的对外账号，再交回调用方
	return accountOf(p), nil
}

// CreateOrgUnit 工厂超管挂本厂组织节点。无父节点表示直挂工厂。
func (s *Org) CreateOrgUnit(ctx context.Context, token, name string, parentID *uuid.UUID) (OrgUnit, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return OrgUnit{}, err
	}
	// 只有工厂超管能建组织。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount); err != nil {
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

// ReparentOrgUnit 改挂父节点；成环或父节点不在本厂都拒绝。
func (s *Org) ReparentOrgUnit(ctx context.Context, token string, unitID uuid.UUID, parentID *uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 只有工厂超管能改组织结构。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount); err != nil {
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

// SetPersonOrg 工厂超管改人员的所属组织；unitID 空表示厂直属。只影响按组织统计，不给权限。
func (s *Org) SetPersonOrg(ctx context.Context, token string, personID uuid.UUID, unitID *uuid.UUID) (Account, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return Account{}, err
	}
	// 只有工厂超管能改。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount); err != nil {
		// 不是工厂超管，记下改所属组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "set_person_org", personID.String(), audit.Deny)
		return Account{}, err
	}
	// 节点须仍有效，人须存在
	if err := s.store.SetPersonOrg(ctx, personID, unitID); err != nil {
		// 记下改所属组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "set_person_org", personID.String(), audit.Deny)
		return Account{}, err
	}
	// 按稳定身份读人员，交回改后的样子
	p, err := s.store.PersonByID(ctx, personID)
	// 人员不存在，拒绝这次变更
	if err != nil {
		return Account{}, err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &acc.ID, &p.LoginName, "set_person_org", p.ID.String(), audit.Allow); err != nil {
		return Account{}, err
	}
	// 收成不含口令的对外账号，再交回调用方
	return accountOf(p), nil
}

// SetPersonDisplayName 工厂超管改别人的显示名；登录名和稳定身份不变。
func (s *Org) SetPersonDisplayName(ctx context.Context, token string, personID uuid.UUID, displayName string) (Account, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return Account{}, err
	}
	// 只有工厂超管能改别人的。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount); err != nil {
		// 不是工厂超管，记下改显示名被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "rename", personID.String(), audit.Deny)
		return Account{}, err
	}
	// 落库并交回改后的账号
	return s.renameDisplay(ctx, acc, personID, displayName)
}

// renameDisplay 规范显示名后落库并记审计。
func (s *kernel) renameDisplay(ctx context.Context, acc Account, personID uuid.UUID, displayName string) (Account, error) {
	// 去首尾空白再限长，和登录名同一口径
	name, err := normalizeLoginName(displayName)
	// 名字空了或太长，拒绝改名
	if err != nil {
		// 记下改显示名被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "rename", personID.String(), audit.Deny)
		return Account{}, err
	}
	// 只改显示名
	if err := s.store.SetDisplayName(ctx, personID, name); err != nil {
		// 记下改显示名被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "rename", personID.String(), audit.Deny)
		return Account{}, err
	}
	// 按稳定身份读人员，交回改后的样子
	p, err := s.store.PersonByID(ctx, personID)
	// 人员不存在，拒绝这次变更
	if err != nil {
		return Account{}, err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &acc.ID, &p.LoginName, "rename", p.ID.String(), audit.Allow); err != nil {
		return Account{}, err
	}
	// 收成不含口令的对外账号，再交回调用方
	return accountOf(p), nil
}

// GrantRole 工厂超管授予固定角色；角色都覆盖整厂。
func (s *Org) GrantRole(ctx context.Context, token string, personID uuid.UUID, role string) (RoleGrant, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return RoleGrant{}, err
	}
	// 只有超管能授，角色须认识。失败一律记拒绝。
	if err := s.canGrant(ctx, acc, role); err != nil {
		// 没有这份许可，记下授角色被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "grant_role", role, audit.Deny)
		return RoleGrant{}, err
	}
	// 写下角色
	row, err := s.store.GrantRole(ctx, personID, role)
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
	// 只有超管能收
	if err := s.canGrant(ctx, acc, g.Role); err != nil {
		// 没有这份许可，记下收回角色被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "revoke_role", grantID.String(), audit.Deny)
		return err
	}
	// 会变成没有超管入口，拒绝收回；这里先快速预检
	if err := s.guardLastAdminOnRevoke(ctx, g); err != nil {
		// 会去掉最后一名有效超管，记下收回角色被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "revoke_role", grantID.String(), audit.Deny)
		return err
	}
	// 拿入口锁重数后收回，并发互收超管也只成一个；收回后新操作立刻按新角色判定。
	if err := s.store.RevokeRoleKeepAdmin(ctx, grantID); err != nil {
		// 收回失败或会去掉最后一名超管，记下收回角色被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "revoke_role", grantID.String(), audit.Deny)
		return err
	}
	// 收回角色成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "revoke_role", grantID.String(), audit.Allow)
}

// DeleteOrgUnit 没有下级节点、也没有人归在这里才删；否则拒绝。
func (s *Org) DeleteOrgUnit(ctx context.Context, token string, unitID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 管不到这个组织节点，拒绝改结构
	if err := s.can(ctx, acc, permManageAccount); err != nil {
		// 管不到这个组织节点，记下删除组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "delete_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 还有下级或有人归在这里就拒绝。
	if err := s.store.DeleteOrgUnit(ctx, unitID); err != nil {
		// 记下删除组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "delete_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 删除组织成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "delete_org_unit", unitID.String(), audit.Allow)
}

// DisableOrgUnit 停用组织节点；还有有效子节点时拒绝。已归在这里的人不动，但不能再往里收人。
func (s *Org) DisableOrgUnit(ctx context.Context, token string, unitID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 管不到这个组织节点，拒绝改结构
	if err := s.can(ctx, acc, permManageAccount); err != nil {
		// 管不到这个组织节点，记下停用组织被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "disable_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 有有效子节点时拒绝。
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
	if err := s.can(ctx, acc, permManageAccount); err != nil {
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

// RenameOrgUnit 改显示名，不改稳定身份。
func (s *Org) RenameOrgUnit(ctx context.Context, token string, unitID uuid.UUID, name string) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 管不到这个组织节点，拒绝改结构
	if err := s.can(ctx, acc, permManageAccount); err != nil {
		// 管不到这个组织节点，记下组织改名被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "rename_org_unit", unitID.String(), audit.Deny)
		return err
	}
	// 只改显示名。
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
	if err := s.can(ctx, acc, permManageAccount); err != nil {
		return nil, err
	}
	// 给能管账号的人看登录现场，再交回调用方
	return s.store.ListPersonLogins(ctx, personID)
}
