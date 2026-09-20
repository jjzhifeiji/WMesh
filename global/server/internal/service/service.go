// Package service 是 WAN 侧入口：唯一管理员、工厂名录、现场设备分配和初始化交付。
// 不代建厂内普通账号、组织或角色，也不直连 SQL。
// 按域分类型：Auth / Factories / Channel / Clients / Assets / Templates / Closure / Updates / Stats；本文件只组装。
package service

import (
	"context"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/blob"
)

// kernel 是各域共用的 WAN 库、对象存根和审计。
type kernel struct {
	store         *Store
	blobs         blob.Store    // 软件包字节；不进库、不进审计
	bus           CmdBus        // 厂端 MQTT 指令；测试可不挂
	applyOutcome  ApplyOutcome  // 确认后是否立刻记已装；测试可改
	pendingSink   PendingSink   // 确认后落盘；空则只走夹具
	applyReporter ApplyReporter // 读 updater 进度；测试可空
	imageJanitor  ImageJanitor  // 请 updater 清无用镜像；测试可空
}

// ImageJanitor 把清镜像请求交给 updater，并读本机占用；不在业务进程里碰 docker。
type ImageJanitor interface {
	RequestPrune(ref string) error
	PruneResult() (reclaimed string, ok bool, present bool, err error)
	ImagesJSON() ([]byte, error)
}

// PendingSink 把已确认的 app 包落到更新目录，供 updater 来换。
type PendingSink interface {
	Stage(kind string, version int64, digest, body []byte) error
}

// SetPendingSink 生产写入更新目录；测试可空。
func (s *Updates) SetPendingSink(sink PendingSink) { s.pendingSink = sink }

// ApplyReporter 读本机待切换与 updater 结果，不换容器。
type ApplyReporter interface {
	Progress() (phase, kind string, version int64, errMsg string, err error)
}

// SetApplyReporter 生产读更新目录；测试可空。
func (s *Updates) SetApplyReporter(r ApplyReporter) { s.applyReporter = r }

// SetImageJanitor 生产写清镜像请求；测试可空。
func (s *Updates) SetImageJanitor(j ImageJanitor) { s.imageJanitor = j }

// SetBlobs 生产改用对象存储；测试保持内存。
func (k *kernel) SetBlobs(store blob.Store) {
	if store == nil {
		return
	}
	k.blobs = store
}

// Auth 管 WAN 管理员登录、会话和改密码。
type Auth struct{ *kernel }

// Factories 管工厂名录、建厂和明确拒绝的代管入口。
type Factories struct{ *kernel }

// Channel 在 channel.go。

// Clients 管现场设备名录与分厂。
type Clients struct{ *kernel }

// Assets 管平台级工艺/工程。
type Assets struct{ *kernel }

// Templates 管工艺/工程字段模版。
type Templates struct{ *kernel }

// Closure 管平台级组包与下发授权。
type Closure struct{ *kernel }

// Stats 在 stats.go。

// Service 组装各域；HTTP 调 svc.Auth / svc.Factories，验收仍可走提升方法。
type Service struct {
	*kernel
	*Auth
	*Factories
	*Channel
	*Clients
	*Assets
	*Templates
	*Closure
	*Updates
	*Stats
}

// NewService 组装 WAN 应用服务。建厂不再反打厂内网。
func NewService(st *Store) *Service {
	k := &kernel{store: st, blobs: blob.NewMemory()}
	return &Service{
		kernel:    k,
		Auth:      &Auth{k},
		Factories: &Factories{k},
		Channel:   &Channel{k},
		Clients:   &Clients{k},
		Assets:    &Assets{k},
		Templates: &Templates{k},
		Closure:   &Closure{k},
		Updates:   &Updates{k},
		Stats:     &Stats{k},
	}
}

// Store 取出本侧库连接，给验收夹具用。
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

// ListAudit 读 WAN 审计行，不含秘密原文。
func (k *kernel) ListAudit(ctx context.Context) ([]audit.Row, error) {
	return k.store.ListAudit(ctx)
}

// HasTable 查迁移是否已建该表，给夹具用。
func (k *kernel) HasTable(ctx context.Context, name string) (bool, error) {
	return k.store.HasTable(ctx, name)
}

// audit 写审计行，不含秘密原文。
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
