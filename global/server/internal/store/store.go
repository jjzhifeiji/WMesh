// Package store 只碰 WAN 库：管理员、工厂名录、初始超管对账、现场设备名录、平台级资产、内容模版、下发授权、通道在线、内容租约和审计。
// 不判定允许/拒绝，也不回调应用服务；不见厂内人员、组织。
// 文件按域拆：account / directory / channel / client / asset / template / closure / lease。
package store

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/id"
)

// Store 只碰 WAN 库：管理员、工厂名录、Client 绑定、平台级资产、下发授权和 WAN 审计。
type Store struct {
	db *gorm.DB
}

// Open 打开已迁移的 WAN 库；调用方保证这是 WAN 库而不是厂库。
func Open(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Ping 只确认 WAN 库连接可用。
func (s *Store) Ping(ctx context.Context) error {
	return s.db.WithContext(ctx).Exec("SELECT 1").Error
}

func (s *Store) AppendAudit(ctx context.Context, e audit.Event) error {
	if e.ID == uuid.Nil {
		e.ID = id.New()
	}
	return s.db.WithContext(ctx).Create(audit.RowFrom(e)).Error
}

// ListAudit 按发生时间从新到旧列出审计行。
func (s *Store) ListAudit(ctx context.Context) ([]audit.Row, error) {
	var rows []audit.Row
	err := s.db.WithContext(ctx).Order("occurred_at DESC").Find(&rows).Error
	return rows, err
}

func (s *Store) HasTable(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := s.db.WithContext(ctx).
		Raw("SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = ?)", name).
		Scan(&exists).Error
	return exists, err
}
