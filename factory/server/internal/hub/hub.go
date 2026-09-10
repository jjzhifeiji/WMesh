// Package hub 按工厂身份打开对应厂库并组装应用服务。
// 不认 WAN 名录，不把本厂口令送到 WAN。
package hub

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/provision"
	"wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

type tenant struct {
	db  *gorm.DB
	svc *service.Service
}

// Hub 按工厂稳定身份选库；没有库就当作工厂还不存在。
type Hub struct {
	mu       sync.Mutex
	adminDSN string
	admin    *gorm.DB
	tenants  map[uuid.UUID]*tenant
}

// New 连维护库（用来建厂库），不预先打开任何厂库。
func New(adminDSN string) (*Hub, error) {
	admin, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	sqlDB, err := admin.DB()
	if err != nil {
		return nil, err
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, err
	}
	return &Hub{adminDSN: adminDSN, admin: admin, tenants: map[uuid.UUID]*tenant{}}, nil
}

// Ping 只确认维护库连接可用，给探活用；不打开任何厂库。
func (h *Hub) Ping(ctx context.Context) error {
	h.mu.Lock()
	admin := h.admin
	h.mu.Unlock()
	if admin == nil {
		return domain.ErrNotFound
	}
	return admin.WithContext(ctx).Exec("SELECT 1").Error
}

// Bootstrap 为目标厂建库并写入待启用初始超管；激活口令只返回给调用方。
func (h *Hub) Bootstrap(ctx context.Context, factoryID uuid.UUID, saLogin, saDisplay string) (uuid.UUID, string, error) {
	svc, err := h.ensure(factoryID)
	if err != nil {
		return uuid.Nil, "", err
	}
	acc, token, err := svc.BootstrapInitial(ctx, saLogin, saDisplay)
	if err != nil {
		return uuid.Nil, "", err
	}
	return acc.ID, token, nil
}

// Service 打开已存在的厂库；库还不在就当作工厂未初始化。
func (h *Hub) Service(ctx context.Context, factoryID uuid.UUID) (*service.Service, error) {
	h.mu.Lock()
	if t, ok := h.tenants[factoryID]; ok {
		h.mu.Unlock()
		return t.svc, nil
	}
	h.mu.Unlock()
	name := provision.DBName(factoryID)
	ok, err := provision.Exists(h.admin, name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, domain.ErrNotFound
	}
	return h.ensure(factoryID)
}

func (h *Hub) ensure(factoryID uuid.UUID) (*service.Service, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if t, ok := h.tenants[factoryID]; ok {
		return t.svc, nil
	}
	name := provision.DBName(factoryID)
	if err := provision.Ensure(h.admin, name); err != nil {
		return nil, err
	}
	dsn, err := provision.SwapDB(h.adminDSN, name)
	if err != nil {
		return nil, err
	}
	db, err := provision.OpenMigrated(dsn)
	if err != nil {
		return nil, err
	}
	svc := service.NewService(store.Open(db, factoryID))
	h.tenants[factoryID] = &tenant{db: db, svc: svc}
	return svc, nil
}

// Drop 关掉该厂连接并删库；仅测试回收。
func (h *Hub) Drop(factoryID uuid.UUID) error {
	h.mu.Lock()
	if t, ok := h.tenants[factoryID]; ok {
		if sqlDB, err := t.db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		delete(h.tenants, factoryID)
	}
	h.mu.Unlock()
	return provision.Drop(h.admin, provision.DBName(factoryID))
}

// Close 关掉已打开的厂库和维护库连接。
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, t := range h.tenants {
		if sqlDB, err := t.db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		delete(h.tenants, id)
	}
	if h.admin != nil {
		if sqlDB, err := h.admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
		h.admin = nil
	}
}
