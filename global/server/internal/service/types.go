package service

import "wmesh/global/internal/store"

const (
	KindProcess        = store.KindProcess
	KindProject        = store.KindProject
	AssetLevelPlatform = store.AssetLevelPlatform
	AssetDraft         = store.AssetDraft
	AssetAvailable     = store.AssetAvailable
	AssetDisabled      = store.AssetDisabled
	FactoryActive      = store.FactoryActive
	FactoryDisabled    = store.FactoryDisabled
	FactoryRetired     = store.FactoryRetired
)

// 与 store 同源的表模型，按域给应用服务和验收当入口类型用。
type (
	Store              = store.Store
	Admin              = store.Admin
	Session            = store.Session
	Factory            = store.Factory
	InitialSuperAdmin  = store.InitialSuperAdmin
	Client             = store.Client
	Asset              = store.Asset
	AssetDep           = store.AssetDep
	AssetSnapshot      = store.AssetSnapshot
	ClosureMember      = store.ClosureMember
	ClosureSnapshot    = store.ClosureSnapshot
	ContentTemplate    = store.ContentTemplate
	TemplateSnapshot   = store.TemplateSnapshot
	DistributionGrant  = store.DistributionGrant
	DistributionRecord = store.DistributionRecord
	EnrollmentOffer    = store.EnrollmentOffer
	ContentLease       = store.ContentLease
)
