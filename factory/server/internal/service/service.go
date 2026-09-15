// Package service 是厂内应用入口：认证、允许/拒绝和审计。
// 不管 WAN 管理员，也不直连 SQL，不把本厂密码送到 WAN。
// 按域分类型：Auth / Org / Attr / Node / Assets / Templates / Closure / Sync / Updates；本文件只组装。
package service

import (
	"context"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/blob"
)

// kernel 是各域共用的本厂库与审计；不对外当业务入口。
type kernel struct {
	store     *Store
	blobs     blob.Store       // 上传正文与软件包；不进库、不进审计
	installer FactoryInstaller // 厂服务确认后安装；测试可注入失败
}

// Auth 管登录、激活、会话、停用和密码重置。
type Auth struct{ *kernel }

// Org 管人员、组织、角色和名册。
type Org struct{ *kernel }

// Attr 管事实桩、个人资产桩和工作上下文。
type Attr struct{ *kernel }

// Node 管本厂已分配的设备和运行凭证。
type Node struct{ *kernel }

// Assets 管本厂工艺/工程治理。
type Assets struct{ *kernel }

// Templates 管已收的工艺/工程字段模版。
type Templates struct{ *kernel }

// Closure 管组包、下发、缓存和平台级副本。
type Closure struct{ *kernel }

// Sync 管上传入队与回连汇聚。
type Sync struct{ *kernel }

// Service 组装各域；HTTP 调 svc.Auth / svc.Org，验收仍可走提升方法。
type Service struct {
	*kernel
	*Auth
	*Org
	*Attr
	*Node
	*Assets
	*Templates
	*Closure
	*Sync
	*Updates
}

// NewService 组装厂内应用服务；调用方先打开这一家厂库。
func NewService(st *Store) *Service {
	k := &kernel{store: st, blobs: blob.NewMemory(), installer: okInstaller{}}
	return &Service{
		kernel:    k,
		Auth:      &Auth{k},
		Org:       &Org{k},
		Attr:      &Attr{k},
		Node:      &Node{k},
		Assets:    &Assets{k},
		Templates: &Templates{k},
		Closure:   &Closure{k},
		Sync:      &Sync{k},
		Updates:   &Updates{k},
	}
}

// Store 取出本厂库连接，给验收夹具用。
func (k *kernel) Store() *Store { return k.store }

// Account 是对外可见的账号视图，不含密码或激活码。
type Account struct {
	ID          uuid.UUID `json:"id"`          // 稳定身份，改名也不变
	LoginName   string    `json:"loginName"`   // 厂内登录名，一厂唯一
	DisplayName string    `json:"displayName"` // 显示名，可改
	Status      string    `json:"status"`      // 人员状态：pending / active / disabled
}

// 对外账号视图，不含密码。
func accountOf(p Person) Account {
	return Account{ID: p.ID, LoginName: p.LoginName, DisplayName: p.DisplayName, Status: p.Status}
}

// ListAudit 列出本厂审计行，给验收用。
func (k *kernel) ListAudit(ctx context.Context) ([]audit.Row, error) {
	return k.store.ListAudit(ctx)
}

// audit 记下允许或拒绝；令牌和密码不进原文。
func (k *kernel) audit(ctx context.Context, actor *uuid.UUID, claimed *string, action, target, result string) error {
	fid := k.store.FactoryID()
	return k.store.AppendAudit(ctx, audit.Event{
		ActorID:      actor,
		ClaimedLogin: claimed,
		FactoryID:    &fid,
		Action:       action,
		Target:       target,
		Result:       result,
		TimeSource:   audit.Server,
	})
}
