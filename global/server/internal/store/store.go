// Package store 只碰 WAN 库：管理员、工厂名录、初始超管对账、现场设备名录、平台级资产、内容模版、下发授权、软件发布、通道在线、内容租约、焊汇总和审计。
// 不判定允许/拒绝，也不回调应用服务；不见厂内人员、组织。
// 文件按域拆：account / directory / channel / client / asset / fs / template / closure / lease / update / stats。
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
	db *gorm.DB // 已迁移的云端库连接，判断不在这一层做
}

// Open 打开已迁移的 WAN 库；调用方保证这是 WAN 库而不是厂库。
func Open(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Ping 只确认 WAN 库连接可用。
func (s *Store) Ping(ctx context.Context) error {
	// 发一条空查询，只确认库还连得上。
	return s.db.WithContext(ctx).Exec("SELECT 1").Error
}

// DatabaseSize 当前库占用的字节，给超管看存储。
func (s *Store) DatabaseSize(ctx context.Context) (int64, error) {
	// 准备接住行数，不能事先把零当成没有。
	var n int64
	// 问当前库占了多少字节，给超管看存储。
	err := s.db.WithContext(ctx).Raw("SELECT pg_database_size(current_database())").Scan(&n).Error
	return n, err
}

// AppendAudit 把一条审计写入 WAN 库；缺身份则现场发号。
func (s *Store) AppendAudit(ctx context.Context, e audit.Event) error {
	// 没带身份就现场发号，避免空主键进审计。
	if e.ID == uuid.Nil {
		// 补上审计身份，后面才能写入。
		e.ID = id.New()
	}
	// 把这一条审计写入云端库。
	return s.db.WithContext(ctx).Create(audit.RowFrom(e)).Error
}

// ListAudit 按发生时间从新到旧列出审计行。
func (s *Store) ListAudit(ctx context.Context) ([]audit.Row, error) {
	// 准备接住查出来的列表，空的也要能交回。
	var rows []audit.Row
	// 按发生时间从新到旧把审计读出来。
	err := s.db.WithContext(ctx).Order("occurred_at DESC").Find(&rows).Error
	return rows, err
}

// HasTable 看 public 下是否已有该表，给迁移夹具用。
func (s *Store) HasTable(ctx context.Context, name string) (bool, error) {
	// 准备接住在不在，零值先不当成没有。
	var exists bool
	// 问公共模式里有没有这张表，给迁移夹具用。
	err := s.db.WithContext(ctx).
		Raw("SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = ?)", name).
		Scan(&exists).Error
	return exists, err
}
