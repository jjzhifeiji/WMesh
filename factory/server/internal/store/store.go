// Package store 只读写本厂库：人员、组织、角色、会话、归属桩、焊事实、本厂 Client 凭证、本厂工艺/工程、已收平台级副本、内容模版副本、内容主钥包装、软件副本和上传记录。
// 不判定允许/拒绝，也不回调应用服务。
// 文件按域拆：account / org / attr / node / asset / fs / template / closure / policy / sync / lifecycle / crypt / update / stats。
package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
)

// Store 只打开这一家工厂的库，按工厂稳定身份选库，不搞单库多厂。
type Store struct {
	db        *gorm.DB  // 本厂库连接，读写都从这里走
	factoryID uuid.UUID // 本厂稳定身份，写入事实时带上
	crypt     cryptBox  // 内容租约与 MK，只在内存
}

// Open 打开这一家工厂的库；factoryID 是选库用的稳定身份，不从名称推导。
func Open(db *gorm.DB, factoryID uuid.UUID) *Store {
	return &Store{db: db, factoryID: factoryID}
}

// FactoryID 返回本厂选库用的稳定身份。
func (s *Store) FactoryID() uuid.UUID { return s.factoryID }

// DatabaseSize 当前厂库占用的字节，给超管看存储。
func (s *Store) DatabaseSize(ctx context.Context) (int64, error) {
	// 准备承接计数，用来判断有没有匹配行。
	var n int64
	// 把查询结果扫进来，失败就不要当空表。
	err := s.db.WithContext(ctx).Raw("SELECT pg_database_size(current_database())").Scan(&n).Error
	return n, err
}

// AppendAudit 写入本厂审计；缺身份则现场发号，并带上本厂。
func (s *Store) AppendAudit(ctx context.Context, e audit.Event) error {
	// 审计没有身份就现发一个，避免空主键。
	if e.ID == uuid.Nil {
		// 现发一个新身份，不沿用空值。
		e.ID = id.New()
	}
	// 审计没带工厂就补上本厂，避免没有归属。
	if e.FactoryID == nil {
		// 先取出本厂身份，才能取地址写进事件。
		fid := s.factoryID
		// 补上本厂归属，这条审计才能按厂追溯。
		e.FactoryID = &fid
	}
	// 库行收成对外结果再交回。
	return s.db.WithContext(ctx).Create(audit.RowFrom(e)).Error
}

// ListAudit 按发生时间从新到旧列出审计行。
func (s *Store) ListAudit(ctx context.Context) ([]audit.Row, error) {
	// 准备承接查到的多条审计行。
	var rows []audit.Row
	// 按条件去读，没有行交给后面的分支。
	err := s.db.WithContext(ctx).Order("occurred_at DESC").Find(&rows).Error
	return rows, err
}

// AuditDeniedSince 看 since 动作对 sinceTarget 最近一次允许之后，action 是否已有对象以 prefix 开头的拒绝。
// 从没允许过就看全部；只查不判定，去重口径由调用方定。
func (s *Store) AuditDeniedSince(ctx context.Context, action, prefix, since, sinceTarget string) (bool, error) {
	// 准备承接查到的有无，没有匹配行就是否。
	var found bool
	// 先取最近一次允许的时刻，再看它之后有没有同前缀的拒绝。
	err := s.db.WithContext(ctx).Raw(`SELECT EXISTS (
		SELECT 1 FROM audit_events
		WHERE action = ? AND result = ? AND starts_with(target, ?)
		AND occurred_at >= COALESCE((
			SELECT MAX(occurred_at) FROM audit_events
			WHERE action = ? AND result = ? AND target = ?
		), '-infinity'::timestamptz)
	)`, action, audit.Deny, prefix, since, audit.Allow, sinceTarget).Scan(&found).Error
	return found, err
}

// hasRows 判断是否还有匹配行，用来挡物理删除。
func hasRows(db *gorm.DB, model any, query string, args ...any) (bool, error) {
	// 准备承接计数，用来判断有没有匹配行。
	var n int64
	// 数匹配行，用来判断还有没有引用。
	err := db.Model(model).Where(query, args...).Count(&n).Error
	return n > 0, err
}

// setStatus 只改状态列，找不到行就当不存在。
func (s *Store) setStatus(ctx context.Context, model any, id uuid.UUID, status string) error {
	// 只改点名的列，避免把别的字段写成零值。
	res := s.db.WithContext(ctx).Model(model).Where("id = ?", id).Update("status", status)
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

// getUnit 按身份取组织节点；没有则当不存在。
func (s *Store) getUnit(ctx context.Context, unitID uuid.UUID) (OrgUnit, error) {
	// 准备承接查到的组织节点。
	var u OrgUnit
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&u, "id = ?", unitID).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return OrgUnit{}, domain.ErrNotFound
		}
		return OrgUnit{}, err
	}
	return u, nil
}

// assertUnitActive 停用节点不能再当父节点或工作上下文。
func (s *Store) assertUnitActive(ctx context.Context, unitID uuid.UUID) error {
	// 先拿到这一步的结果，后面断言还要用。
	u, err := s.getUnit(ctx, unitID)
	// 读取组织节点失败就停，避免带着错误继续。
	if err != nil {
		return err
	}
	// 停用的节点不能再当父级或工作上下文。
	if u.Status != StatusActive {
		return domain.ErrDisabledOrgUnit
	}
	return nil
}

// assertPersonExists 确认本厂还有这个人，外键前先挡。
func (s *Store) assertPersonExists(ctx context.Context, personID uuid.UUID) error {
	// 准备承接计数，用来判断有没有匹配行。
	var n int64
	// 计数失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID).Count(&n).Error; err != nil {
		return err
	}
	// 一个人都没有就当不存在。
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
