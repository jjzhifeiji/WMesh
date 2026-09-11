package service

import "wmesh/global/internal/store"

const (
	KindProcess        = store.KindProcess
	KindProject        = store.KindProject
	AssetLevelPlatform = store.AssetLevelPlatform
	AssetDraft         = store.AssetDraft
	AssetAvailable     = store.AssetAvailable
	AssetDisabled      = store.AssetDisabled
)

// 与 store 同源的表模型，给 WAN 应用服务和验收当入口类型用。
type (
	Store              = store.Store
	Admin              = store.Admin
	Factory            = store.Factory
	InitialSuperAdmin  = store.InitialSuperAdmin
	Session            = store.Session
	Client             = store.Client
	Asset              = store.Asset
	AssetDep           = store.AssetDep
	AssetSnapshot      = store.AssetSnapshot
	ClosureMember      = store.ClosureMember
	ClosureSnapshot    = store.ClosureSnapshot
	DistributionGrant  = store.DistributionGrant
	DistributionRecord = store.DistributionRecord
)
