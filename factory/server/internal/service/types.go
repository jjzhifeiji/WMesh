package service

import (
	"github.com/google/uuid"

	"wmesh/factory/internal/store"
)

const (
	StatusPending  = store.StatusPending  // 待启用：可预写角色，但不能登录
	StatusActive   = store.StatusActive   // 有效：认证后按角色作用域操作
	StatusDisabled = store.StatusDisabled // 已停用：不能新开会话或新开受保护操作
	StatusEnded    = store.StatusEnded    // 分配已取消，行留下做历史
	StatusRevoked  = store.StatusRevoked  // 角色已收回，行留下做历史

	RoleFactorySuperAdmin = store.RoleFactorySuperAdmin // 工厂超管，只能挂本厂作用域
	RoleOrgAdmin          = store.RoleOrgAdmin          // 组织管理员，只管本节点及当前子树
	RoleOrgLead           = store.RoleOrgLead           // 组织负责人，子树只读
	RoleProcessEngineer   = store.RoleProcessEngineer   // 工艺工程师，下发仍按作用域
	RoleOperator          = store.RoleOperator          // 操作员，可产生运行事实
	RoleAuditor           = store.RoleAuditor           // 审计员，只读，不能改业务

	ScopeFactory = store.ScopeFactory // 覆盖本厂当时全部组织节点
	ScopeOrgUnit = store.ScopeOrgUnit // 只覆盖该节点及当时子树

	ClientStatusBound = store.ClientStatusBound // 本厂有效绑定，可以签发
	ClientStatusVoid  = store.ClientStatusVoid  // 已作废，不得再签发

	FactoryActive   = store.FactoryActive   // 本厂有效，可登录
	FactoryDisabled = store.FactoryDisabled // WAN 停用，拒绝新登录
	FactoryRetired  = store.FactoryRetired  // 已注销，立即拒绝登录

	KindProcess = store.KindProcess // 可复用工艺
	KindProject = store.KindProject // 一次作业工程

	AssetLevelFactory  = store.AssetLevelFactory  // 本厂厂级
	AssetLevelPersonal = store.AssetLevelPersonal // 本厂个人级
	AssetLevelPlatform = store.AssetLevelPlatform // 已下发到本厂的平台级副本
	AssetDraft         = store.AssetDraft         // 草稿：不可依赖、不可升档
	AssetAvailable     = store.AssetAvailable     // 可用
	AssetDisabled      = store.AssetDisabled      // 停用后不得改内容或升档

	UploadPointCloud = store.UploadPointCloud // 点云上传
	UploadImage      = store.UploadImage      // 图片上传
)

// 授角色时与落库用同一条作用域规则。
func validScope(role, scopeKind string, orgUnitID *uuid.UUID) bool {
	return store.ValidScope(role, scopeKind, orgUnitID)
}

// 与 store 同源的表模型，按域给应用服务和验收当入口类型用。
type (
	Store   = store.Store
	Person  = store.Person
	Session = store.Session

	Lifecycle = store.Lifecycle

	OrgUnit    = store.OrgUnit
	Assignment = store.Assignment
	RoleGrant  = store.RoleGrant

	PathNode      = store.PathNode
	WorkContext   = store.WorkContext
	FactStub      = store.FactStub
	PersonalAsset = store.PersonalAsset

	Client       = store.Client
	SigningKey   = store.SigningKey
	RuntimeGrant = store.RuntimeGrant

	Asset         = store.Asset
	AssetDep      = store.AssetDep
	AssetSnapshot = store.AssetSnapshot

	ContentTemplate  = store.ContentTemplate
	TemplateSnapshot = store.TemplateSnapshot

	AssetReplica    = store.AssetReplica
	ClosureMember   = store.ClosureMember
	ClosureSnapshot = store.ClosureSnapshot

	UploadRecord = store.UploadRecord

	SoftwareReplica = store.SoftwareReplica
	WANTrust        = store.WANTrust
)
