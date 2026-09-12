// Package hub 按工厂身份打开对应厂库并组装应用服务。
// 不认 WAN 名录，不把本厂密码送到 WAN。
package hub

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
	"wmesh/factory/internal/platform/provision"
	"wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
	"wmesh/factory/internal/wanchannel"
)

type tenant struct {
	db  *gorm.DB
	svc *service.Service
}

// Hub 按工厂稳定身份选库；没有库就当作工厂还不存在。
type Hub struct {
	mu         sync.Mutex
	presenceMu sync.Mutex
	adminDSN   string
	admin      *gorm.DB
	tenants    map[uuid.UUID]*tenant
	presence   map[uuid.UUID]context.CancelFunc // 每厂一条出站通道
	wanURL     string                           // WAN 根地址，空则不连
	run        context.Context                  // 进程生命周期，给通道重连用
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
	return &Hub{adminDSN: adminDSN, admin: admin, tenants: map[uuid.UUID]*tenant{}, presence: map[uuid.UUID]context.CancelFunc{}}, nil
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

// Bootstrap 为目标厂建库并写入待启用初始超管；激活码只返回给调用方。
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

// SiteFactory 是本机已认领的一家工厂，给登录页免填 UUID。
type SiteFactory struct {
	ID      uuid.UUID `json:"id"`      // 工厂稳定身份
	SALogin string    `json:"saLogin"` // 初始超管登录名，方便认领后登录
	Status  string    `json:"status"`  // 本厂治理状态：active / disabled / retired
}

// ListSite 列出本机已有厂库；没有 WAN 名录。
func (h *Hub) ListSite(ctx context.Context) ([]SiteFactory, error) {
	ids, err := provision.ListFactoryIDs(h.admin)
	if err != nil {
		return nil, err
	}
	out := make([]SiteFactory, 0, len(ids))
	for _, id := range ids {
		svc, err := h.Service(ctx, id)
		if err != nil {
			continue
		}
		p, err := svc.Store().InitialPerson(ctx)
		login := ""
		if err == nil {
			login = p.LoginName
		}
		status := store.FactoryActive
		// 读本厂已落地的治理状态，给登录页提示停用/注销。
		if lc, err := svc.Store().Lifecycle(ctx); err == nil {
			status = lc.Status
		}
		out = append(out, SiteFactory{ID: id, SALogin: login, Status: status})
	}
	return out, nil
}

// Claim 用建厂码向 WAN 认领本厂：落库初始超管并当场激活，再登记签发公钥。
func (h *Hub) Claim(ctx context.Context, wanURL, enrollmentCode, password string) (SiteFactory, error) {
	if wanURL == "" {
		return SiteFactory{}, domain.ErrWANUnreachable
	}
	sess, offer, err := wanchannel.Enroll(ctx, wanURL, enrollmentCode)
	if err != nil {
		return SiteFactory{}, err
	}
	defer sess.Close()

	svc, err := h.ensure(offer.FactoryID)
	if err != nil {
		return SiteFactory{}, err
	}
	if _, err := svc.Auth.AcceptEnrollment(ctx, offer.SAPersonID, offer.SALogin, offer.SADisplay, password); err != nil {
		return SiteFactory{}, err
	}
	pub, _, err := h.signingMaterial(ctx, svc)
	if err != nil {
		return SiteFactory{}, err
	}
	if err := sess.Confirm(ctx, pub); err != nil {
		return SiteFactory{}, err
	}
	h.ensureChannel(offer.FactoryID)
	return SiteFactory{ID: offer.FactoryID, SALogin: offer.SALogin, Status: store.FactoryActive}, nil
}

func (h *Hub) signingMaterial(ctx context.Context, svc *service.Service) (pub, priv []byte, err error) {
	k, err := svc.Store().SigningKey(ctx)
	if err == nil {
		return k.PublicKey, k.PrivateKey, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, nil, err
	}
	pub, priv, err = nodekey.Generate()
	if err != nil {
		return nil, nil, err
	}
	if err := svc.Node.InstallSigningKey(ctx, pub, priv); err != nil {
		return nil, nil, err
	}
	return pub, priv, nil
}

// Drop 关掉该厂连接并删库；仅测试回收。
func (h *Hub) Drop(factoryID uuid.UUID) error {
	h.stopChannel(factoryID)
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
	h.presenceMu.Lock()
	for id, cancel := range h.presence {
		cancel()
		delete(h.presence, id)
	}
	h.presenceMu.Unlock()
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
