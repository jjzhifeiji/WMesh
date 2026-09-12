package service

import (
	"github.com/google/uuid"

	"wmesh/factory/internal/store"
)

const (
	StatusPending  = store.StatusPending
	StatusActive   = store.StatusActive
	StatusDisabled = store.StatusDisabled
	StatusEnded    = store.StatusEnded
	StatusRevoked  = store.StatusRevoked

	RoleFactorySuperAdmin = store.RoleFactorySuperAdmin
	RoleOrgAdmin          = store.RoleOrgAdmin
	RoleOrgLead           = store.RoleOrgLead
	RoleProcessEngineer   = store.RoleProcessEngineer
	RoleOperator          = store.RoleOperator
	RoleAuditor           = store.RoleAuditor

	ScopeFactory = store.ScopeFactory
	ScopeOrgUnit = store.ScopeOrgUnit

	ClientStatusBound = store.ClientStatusBound
	ClientStatusVoid  = store.ClientStatusVoid

	FactoryActive   = store.FactoryActive
	FactoryDisabled = store.FactoryDisabled
	FactoryRetired  = store.FactoryRetired

	KindProcess = store.KindProcess
	KindProject = store.KindProject

	AssetLevelFactory  = store.AssetLevelFactory
	AssetLevelPersonal = store.AssetLevelPersonal
	AssetLevelPlatform = store.AssetLevelPlatform
	AssetDraft         = store.AssetDraft
	AssetAvailable     = store.AssetAvailable
	AssetDisabled      = store.AssetDisabled

	UploadPointCloud = store.UploadPointCloud
	UploadImage      = store.UploadImage
)

func validScope(role, scopeKind string, orgUnitID *uuid.UUID) bool {
	return store.ValidScope(role, scopeKind, orgUnitID) // 授角色时与落库用同一条作用域规则
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
)
