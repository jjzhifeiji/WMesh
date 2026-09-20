package service

import "wmesh/global/internal/store"

const (
	KindProcess        = store.KindProcess        // 工艺
	KindProject        = store.KindProject        // 工程
	AssetLevelPlatform = store.AssetLevelPlatform // 平台级，只在 WAN
	AssetDraft         = store.AssetDraft         // 草稿
	AssetAvailable     = store.AssetAvailable     // 可用
	AssetDisabled      = store.AssetDisabled      // 停用
	FactoryActive      = store.FactoryActive      // 有效：可认领、可登录
	FactoryDisabled    = store.FactoryDisabled    // 停用：可再启用
	FactoryRetired         = store.FactoryRetired         // 已注销：不能再启用
	SoftwareWANService     = store.SoftwareWANService     // 云端服务包
	SoftwareFactoryService = store.SoftwareFactoryService // 厂端服务包
	SoftwareClientAPK      = store.SoftwareClientAPK      // 客户端 APK
)

// 与 store 同源的表模型，按域给应用服务和验收当入口类型用。
type (
	Store                = store.Store
	Admin                = store.Admin
	Session              = store.Session
	Factory              = store.Factory
	InitialSuperAdmin    = store.InitialSuperAdmin
	Client               = store.Client
	Asset                = store.Asset
	AssetDep             = store.AssetDep
	AssetSnapshot        = store.AssetSnapshot
	ClosureMember        = store.ClosureMember
	ClosureSnapshot      = store.ClosureSnapshot
	ContentTemplate      = store.ContentTemplate
	TemplateSnapshot     = store.TemplateSnapshot
	DistributionGrant    = store.DistributionGrant
	DistributionRecord   = store.DistributionRecord
	EnrollmentOffer      = store.EnrollmentOffer
	ContentLease         = store.ContentLease
	WANSigningKey        = store.WANSigningKey
	SoftwareRelease      = store.SoftwareRelease
	SoftwareDistribution = store.SoftwareDistribution
	WeldSummary          = store.WeldSummary
)
