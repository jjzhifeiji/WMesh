// Package service 是 WAN 侧入口：唯一管理员、工厂名录、Client 绑定和初始化交付。
// 不代建厂内普通账号、组织或角色，也不直连 SQL。
// 按域分类型：Auth / Factories / Clients / Assets / Closure；本文件只组装。
package service

import (
	"context"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
)

// FactoryBootstrap 在目标厂库写入待启用初始超管，并把激活口令只交给交付夹具。
type FactoryBootstrap interface {
	Bootstrap(ctx context.Context, factoryID uuid.UUID, saLogin, saDisplay string) (personID uuid.UUID, activationToken string, err error)
}

// kernel 是各域共用的 WAN 库、建厂引导和审计。
type kernel struct {
	store *Store
	boot  FactoryBootstrap
}

// Auth 管 WAN 管理员登录与会话。
type Auth struct{ *kernel }

// Factories 管工厂名录、建厂和明确拒绝的代管入口。
type Factories struct{ *kernel }

// Clients 管现场节点绑定。
type Clients struct{ *kernel }

// Assets 管平台级工艺/工程。
type Assets struct{ *kernel }

// Closure 管平台级组包与下发授权。
type Closure struct{ *kernel }

// Service 组装各域；HTTP 调 svc.Auth / svc.Factories，验收仍可走提升方法。
type Service struct {
	*kernel
	*Auth
	*Factories
	*Clients
	*Assets
	*Closure
}

// NewService 组装 WAN 应用服务；boot 只在目标厂库写初始超管。
func NewService(st *Store, boot FactoryBootstrap) *Service {
	k := &kernel{store: st, boot: boot}
	return &Service{
		kernel:    k,
		Auth:      &Auth{k},
		Factories: &Factories{k},
		Clients:   &Clients{k},
		Assets:    &Assets{k},
		Closure:   &Closure{k},
	}
}

func (k *kernel) Store() *Store { return k.store }

// Ping 只确认 WAN 库可达，给探活用。
func (k *kernel) Ping(ctx context.Context) error {
	return k.store.Ping(ctx)
}

// AdminExists 给进程启动判断是否还需引导，避免每次重启都留一条拒绝审计。
func (k *kernel) AdminExists(ctx context.Context) (bool, error) {
	n, err := k.store.AdminCount(ctx)
	return n > 0, err
}

func (k *kernel) ListAudit(ctx context.Context) ([]audit.Row, error) {
	return k.store.ListAudit(ctx)
}

func (k *kernel) HasTable(ctx context.Context, name string) (bool, error) {
	return k.store.HasTable(ctx, name)
}

func (k *kernel) audit(ctx context.Context, actor *uuid.UUID, claimed *string, factoryID *uuid.UUID, action, target, result string) error {
	return k.store.AppendAudit(ctx, audit.Event{
		ActorID:      actor,
		ClaimedLogin: claimed,
		FactoryID:    factoryID,
		Action:       action,
		Target:       target,
		Result:       result,
		TimeSource:   audit.Server,
	})
}
