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

// 指定落库表名，避免查询时按类型名去猜。
func (OrgUnit) TableName() string { return "org_units" }

// RoleGrant 是带明确作用域的一条角色；分配组织不会自动产生本行。
type RoleGrant struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`     // 授予记录稳定身份
	PersonID  uuid.UUID  `gorm:"type:uuid;not null" json:"personId"` // 被授予的本厂人员
	Role      string     `gorm:"not null" json:"role"`               // 固定角色：超管/管理员/操作员，都覆盖整厂
	Status    string     `gorm:"not null" json:"status"`             // active 或 revoked
	CreatedAt time.Time  `gorm:"not null" json:"createdAt"`          // 授予时间
	RevokedAt *time.Time `json:"revokedAt"`                          // 收回时间；有效授予必须为空
}

// 指定落库表名，避免查询时按类型名去猜。
func (RoleGrant) TableName() string { return "role_grants" }

// CreateOrgUnit 在本厂树上新建节点；父节点必须有效。
func (s *Store) CreateOrgUnit(ctx context.Context, name string, parentID *uuid.UUID) (OrgUnit, error) {
	// 指定了父级就挂过去，否则留在根上。
	if parentID != nil {
		// 节点必须仍有效，停用的不能再当上下文。
		if err := s.assertUnitActive(ctx, *parentID); err != nil {
			return OrgUnit{}, err
		}
	}
	// 按入参组装要写入的行。
	row := OrgUnit{
		ID:        id.New(),
		ParentID:  parentID,
		Name:      name,
		Status:    StatusActive,
		CreatedAt: time.Now().UTC(),
	}
	// 写入一行失败就停，避免带着错误继续。
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
	// 只改点名的列，避免把别的字段写成零值。
	res := s.db.WithContext(ctx).Model(&OrgUnit{}).Where("id = ?", unitID).Update("name", name)
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

// ReparentOrgUnit 改挂前检查不能挂到自己或后代，避免把树打成环。
func (s *Store) ReparentOrgUnit(ctx context.Context, unitID uuid.UUID, parentID *uuid.UUID) error {
	// 指定了父级就挂过去，否则留在根上。
	if parentID != nil && *parentID == unitID {
		return domain.ErrCycle
	}
	// 指定了父级就挂过去，否则留在根上。
	if parentID != nil {
		// 节点必须仍有效，停用的不能再当上下文。
		if err := s.assertUnitActive(ctx, *parentID); err != nil {
			return err
		}
		// 先检查会不会成环，结果留给紧跟着的判断。
		cycle, err := s.wouldCycle(ctx, unitID, *parentID)
		// 检查会不会成环失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 会成环就拒绝改挂。
		if cycle {
			return domain.ErrCycle
		}
	}
	// 只改点名的列，避免把别的字段写成零值。
	res := s.db.WithContext(ctx).Model(&OrgUnit{}).Where("id = ?", unitID).Update("parent_id", parentID)
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		// 检查约束不通过就改成业务拒绝。
		if domain.IsCheckViolation(res.Error) {
			return domain.ErrCycle
		}
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// wouldCycle 沿新父上走，碰到自己或已见过的节点就是环。
func (s *Store) wouldCycle(ctx context.Context, nodeID, newParent uuid.UUID) (bool, error) {
	// 从当前节点往上收集，直到厂根。
	current := &newParent
	// 准备去重或保留集合，避免重复和误删。
	seen := map[uuid.UUID]struct{}{}
	// 按这个范围推进，避免漏项或死循环。
	for current != nil {
		// 往上走到了自己，说明形成了环。
		if *current == nodeID {
			return true, nil
		}
		// 这个节点走过就停，避免组织环死循环。
		if _, ok := seen[*current]; ok {
			return true, nil
		}
		// 记下走过的节点，下一圈用来认环。
		seen[*current] = struct{}{}
		// 准备承接查到的组织节点。
		var u OrgUnit
		// 按条件取一行失败就停，避免带着错误继续。
		if err := s.db.WithContext(ctx).Select("parent_id").First(&u, "id = ?", *current).Error; err != nil {
			// 没有这一行就当成不存在。
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, domain.ErrNotFound
			}
			return false, err
		}
		// 继续走向父级，直到根或发现环。
		current = u.ParentID
	}
	return false, nil
}

// GrantRole 写入一条角色；角色都覆盖整厂，不认识的角色拒绝。
func (s *Store) GrantRole(ctx context.Context, personID uuid.UUID, role string) (RoleGrant, error) {
	// 不认识的角色不能授。
	if !ValidRole(role) {
		return RoleGrant{}, domain.ErrInvalidRoleScope
	}
	// 人必须已经在本厂，否则不继续写。
	if err := s.assertPersonExists(ctx, personID); err != nil {
		return RoleGrant{}, err
	}
	// 按入参组装要写入的行。
	row := RoleGrant{
		ID:        id.New(),
		PersonID:  personID,
		Role:      role,
		Status:    StatusActive,
		CreatedAt: time.Now().UTC(),
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 唯一约束撞了就改成业务冲突，不抛库原文。
		if domain.IsUniqueViolation(err) {
			return RoleGrant{}, domain.ErrDuplicateRoleGrant
		}
		// 检查约束不通过就改成业务拒绝。
		if domain.IsCheckViolation(err) {
			return RoleGrant{}, domain.ErrInvalidRoleScope
		}
		return RoleGrant{}, err
	}
	return row, nil
}

// RevokeRole 收回有效授予：只改状态不删行。
func (s *Store) RevokeRole(ctx context.Context, grantID uuid.UUID) error {
	// 不看超管入口，直接收回这一条。
	return revokeGrant(s.db.WithContext(ctx), grantID)
}

// RevokeRoleKeepAdmin 收回有效授予；拿入口锁后重读授予和持有人并重数有效超管。
// 收的是最后一名有效厂级超管就拒绝，并发互收也只成一个。
func (s *Store) RevokeRoleKeepAdmin(ctx context.Context, grantID uuid.UUID) error {
	// 放进同一事务，计数和收回之间不放锁。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先拿入口锁，计数和收回之间不让别的停用或收角色插进来。
		if err := lockAdminEntry(tx); err != nil {
			return err
		}
		// 准备承接拿锁后重读的授予。
		var g RoleGrant
		// 拿锁后重读授予，状态以此刻为准。
		if err := tx.First(&g, "id = ?", grantID).Error; err != nil {
			// 没有这一行就当成不存在。
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		// 只有仍有效的厂级超管授予才关系到管理入口。
		if g.Status == StatusActive && g.Role == RoleFactorySuperAdmin {
			// 准备承接拿锁后重读的持有人。
			var p Person
			// 拿锁后重读持有人，待启用或停用的不算入口。
			if err := tx.First(&p, "id = ?", g.PersonID).Error; err != nil {
				// 没有这一行就当成不存在。
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return domain.ErrNotFound
				}
				return err
			}
			// 持有人有效才算入口，只剩这一名就拒绝收回。
			if p.Status == StatusActive {
				// 重数后只剩一名就拒绝。
				if err := keepLastAdmin(tx); err != nil {
					return err
				}
			}
		}
		// 守住入口后再在同一事务里收回。
		return revokeGrant(tx, grantID)
	})
}

// revokeGrant 在给定连接或事务上收回一条有效授予：只改状态不删行。
func revokeGrant(db *gorm.DB, grantID uuid.UUID) error {
	// 取当前时刻，时间列和租约用同一个时钟。
	now := time.Now().UTC()
	// 收回只改状态不删行。
	res := db.Model(&RoleGrant{}).
		Where("id = ? AND status = ?", grantID, StatusActive).
		Updates(map[string]any{"status": StatusRevoked, "revoked_at": now})
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

// DeleteOrgUnit 没有下级节点、也没有人员归在这里才物理删除。
func (s *Store) DeleteOrgUnit(ctx context.Context, unitID uuid.UUID) error {
	// 读取组织节点失败就停，避免带着错误继续。
	if _, err := s.getUnit(ctx, unitID); err != nil {
		return err
	}
	// 放进同一事务，中途失败就整单回滚。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先列出可能挡住删除的引用。
		checks := []struct {
			model any    // 要查是否仍被引用的表
			query string // 引用是否还在的条件
			args  []any  // 条件里的节点身份
		}{
			{&OrgUnit{}, "parent_id = ?", []any{unitID}},
			{&Person{}, "org_unit_id = ?", []any{unitID}},
			{&factRow{}, "org_unit_id = ?", []any{unitID}},
		}
		// 逐条处理，避免漏掉还要读或还要写的行。
		for _, c := range checks {
			// 先检查是否还有引用，结果留给紧跟着的判断。
			ok, err := hasRows(tx, c.model, c.query, c.args...)
			// 检查是否还有引用失败就停，避免带着错误继续。
			if err != nil {
				return err
			}
			// 还有引用就不能物理删除。
			if ok {
				return domain.ErrReferenced
			}
		}
		// 删掉匹配行，没有行交给后面判断。
		res := tx.Where("id = ?", unitID).Delete(&OrgUnit{})
		// 更新失败则交回库错误，不能当成已经改完。
		if res.Error != nil {
			// 外键对不上就当被指的对象不存在。
			if domain.IsForeignKeyViolation(res.Error) {
				return domain.ErrReferenced
			}
			return res.Error
		}
		// 没有改到行就当目标不存在。
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// DisableOrgUnit 停用节点；还有有效下级则拒绝。
func (s *Store) DisableOrgUnit(ctx context.Context, unitID uuid.UUID) error {
	// 准备承接计数，用来判断有没有匹配行。
	var n int64
	// 接上要操作的表失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Model(&OrgUnit{}).
		Where("parent_id = ? AND status = ?", unitID, StatusActive).
		Count(&n).Error; err != nil {
		return err
	}
	// 已经有匹配行就停，避免重复或误删。
	if n > 0 {
		// 还有有效下级时不能停用。
		return domain.ErrHasActiveChildren
	}
	// 只改点名的列，避免把别的字段写成零值。
	res := s.db.WithContext(ctx).Model(&OrgUnit{}).Where("id = ?", unitID).Update("status", StatusDisabled)
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

// EnableOrgUnit 重新启用节点；上级必须已经有效。
func (s *Store) EnableOrgUnit(ctx context.Context, unitID uuid.UUID) error {
	// 先拿到这一步的结果，后面断言还要用。
	u, err := s.getUnit(ctx, unitID)
	// 读取组织节点失败就停，避免带着错误继续。
	if err != nil {
		return err
	}
	// 已经是有效就不必再启用一次。
	if u.Status == StatusActive {
		return nil
	}
	// 父级还停着就不能启用子级。
	if u.ParentID != nil {
		// 节点必须仍有效，停用的不能再当上下文。
		if err := s.assertUnitActive(ctx, *u.ParentID); err != nil {
			return err
		}
	}
	// 把这一步的结果交回给调用方。
	return s.setStatus(ctx, &OrgUnit{}, unitID, StatusActive)
}

// ValidRole 只认三种固定角色。
func ValidRole(role string) bool {
	// 按角色看是否认识，避免授出不存在的角色。
	switch role {
	// 超管、管理员、操作员都可以授。
	case RoleFactorySuperAdmin, RoleOrgAdmin, RoleOperator:
		return true
	// 不认识的角色一律不能授权。
	default:
		return false
	}
}

// RoleGrantCount 数角色授予行，含已收回。
func (s *Store) RoleGrantCount(ctx context.Context) (int64, error) {
	// 准备承接计数，用来判断有没有匹配行。
	var n int64
	// 数匹配行，用来判断还有没有引用。
	err := s.db.WithContext(ctx).Model(&RoleGrant{}).Count(&n).Error
	return n, err
}

// ActiveGrants 列出某人当前有效角色。
func (s *Store) ActiveGrants(ctx context.Context, personID uuid.UUID) ([]RoleGrant, error) {
	// 准备承接查到的多条角色授予。
	var rows []RoleGrant
	// 按条件去读，没有行交给后面的分支。
	err := s.db.WithContext(ctx).Where("person_id = ? AND status = ?", personID, StatusActive).Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// GrantByID 按身份取一条授予，含已收回。
func (s *Store) GrantByID(ctx context.Context, grantID uuid.UUID) (RoleGrant, error) {
	// 准备承接查到的角色授予。
	var row RoleGrant
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", grantID).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return RoleGrant{}, domain.ErrNotFound
		}
		return RoleGrant{}, err
	}
	return row, nil
}

// ActiveFactorySuperAdminCount 只数「有效账号 + 有效厂级超管角色」，停用的不计。
func (s *Store) ActiveFactorySuperAdminCount(ctx context.Context) (int64, error) {
	// 走库连接数，口径与事务内重数一致。
	return activeFactorySACount(s.db.WithContext(ctx))
}

// activeFactorySACount 在给定连接或事务上数「有效账号 + 有效厂级超管角色」。
func activeFactorySACount(db *gorm.DB) (int64, error) {
	// 准备承接计数，用来判断有没有匹配行。
	var n int64
	// 先走手写查询，结果留给紧跟着的判断。
	err := db.Raw(`
		SELECT COUNT(*) FROM people p
		INNER JOIN role_grants g ON g.person_id = p.id
		WHERE p.status = ? AND g.status = ? AND g.role = ?`,
		StatusActive, StatusActive, RoleFactorySuperAdmin,
	).Scan(&n).Error
	return n, err
}

// PersonIsActiveFactorySA 是否仍握着有效厂级超管角色。
func (s *Store) PersonIsActiveFactorySA(ctx context.Context, personID uuid.UUID) (bool, error) {
	// 走库连接查，口径与事务内重查一致。
	return personIsActiveFactorySA(s.db.WithContext(ctx), personID)
}

// personIsActiveFactorySA 在给定连接或事务上看此人是否仍握着有效厂级超管角色。
func personIsActiveFactorySA(db *gorm.DB, personID uuid.UUID) (bool, error) {
	// 准备承接计数，用来判断有没有匹配行。
	var n int64
	// 先接上要操作的表，结果留给紧跟着的判断。
	err := db.Model(&RoleGrant{}).
		Where("person_id = ? AND status = ? AND role = ?", personID, StatusActive, RoleFactorySuperAdmin).
		Count(&n).Error
	return n > 0, err
}

// Unit 按稳定身份取组织节点。
func (s *Store) Unit(ctx context.Context, unitID uuid.UUID) (OrgUnit, error) {
	// 做完读取组织节点后把结果交回。
	return s.getUnit(ctx, unitID)
}

// InSubtree 沿当前父指针上走，判断 node 是否在 root 的当前子树里（含自身）。
func (s *Store) InSubtree(ctx context.Context, root, node uuid.UUID) (bool, error) {
	// 自己就在这棵子树里。
	if root == node {
		return true, nil
	}
	// 从当前节点往上收集，直到厂根。
	current := node
	// 准备去重或保留集合，避免重复和误删。
	seen := map[uuid.UUID]struct{}{}
	// 一直往上走，遇到环或到根就停。
	for {
		// 这个节点走过就停，避免组织环死循环。
		if _, ok := seen[current]; ok {
			return false, domain.ErrCycle
		}
		// 记下走过的节点，下一圈用来认环。
		seen[current] = struct{}{}
		// 先拿到这一步的结果，后面断言还要用。
		u, err := s.getUnit(ctx, current)
		// 读取组织节点失败就停，避免带着错误继续。
		if err != nil {
			return false, err
		}
		// 已经是根就停，根不能改父级或删除。
		if u.ParentID == nil {
			return false, nil
		}
		// 父级就是子树的根，说明节点在里面。
		if *u.ParentID == root {
			return true, nil
		}
		// 继续走向父级，直到根或发现环。
		current = *u.ParentID
	}
}

// 递归求子树时承接一个节点身份。
type orgIDScan struct {
	ID uuid.UUID `gorm:"column:id"` // 子树里的节点身份
}

// OrgSubtreeIDs 递归求这些节点及其当前子树的全部节点；不按状态裁，UNION 去重防组织环。
func (s *Store) OrgSubtreeIDs(ctx context.Context, roots []uuid.UUID) ([]uuid.UUID, error) {
	// 没有根就没有子树，不必查库。
	if len(roots) == 0 {
		return []uuid.UUID{}, nil
	}
	// 准备承接子树里的节点。
	var rows []orgIDScan
	// 沿父指针往下递归，UNION 遇到走过的节点就不再展开。
	err := s.db.WithContext(ctx).Raw(`
		WITH RECURSIVE sub(id) AS (
			SELECT id FROM org_units WHERE id IN ?
			UNION
			SELECT u.id FROM org_units u JOIN sub ON u.parent_id = sub.id
		)
		SELECT id FROM sub`, roots).Scan(&rows).Error
	// 查库失败就停，不交残缺的子树。
	if err != nil {
		return nil, err
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]uuid.UUID, 0, len(rows))
	// 逐个收下节点身份。
	for _, r := range rows {
		// 收进结果，顺序不影响可见判定。
		out = append(out, r.ID)
	}
	return out, nil
}

// TryDeleteOrgUnit 仅用于验收「被引用不得物理删除」；业务路径不提供删除。
func (s *Store) TryDeleteOrgUnit(ctx context.Context, unitID uuid.UUID) error {
	// 把库操作的错误原样交回，好让调用方处理。
	return s.db.WithContext(ctx).Exec("DELETE FROM org_units WHERE id = ?", unitID).Error
}

// ListOrgUnits 列出本厂组织节点，按创建时间从新到旧。
func (s *Store) ListOrgUnits(ctx context.Context) ([]OrgUnit, error) {
	// 准备承接查到的多条组织节点。
	var rows []OrgUnit
	// 按条件去读，没有行交给后面的分支。
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// ListRoleGrants 列出当前有效角色授予。
func (s *Store) ListRoleGrants(ctx context.Context) ([]RoleGrant, error) {
	// 准备承接查到的多条角色授予。
	var rows []RoleGrant
	// 按条件去读，没有行交给后面的分支。
	err := s.db.WithContext(ctx).Where("status = ?", StatusActive).Order("created_at DESC").Find(&rows).Error
	return rows, err
}
