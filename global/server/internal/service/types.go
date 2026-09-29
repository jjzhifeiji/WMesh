package service

import "wmesh/global/internal/store"

const (
	KindProcess            = store.KindProcess            // 工艺
	KindProject            = store.KindProject            // 工程
	AssetLevelPlatform     = store.AssetLevelPlatform     // 平台级，只在 WAN
	AssetDraft             = store.AssetDraft             // 草稿
	AssetAvailable         = store.AssetAvailable         // 可用
	AssetDisabled          = store.AssetDisabled          // 停用
	FactoryActive          = store.FactoryActive          // 有效：可认领、可登录
	FactoryDisabled        = store.FactoryDisabled        // 停用：可再启用
	FactoryRetired         = store.FactoryRetired         // 已注销：不能再启用
	SoftwareWANService     = store.SoftwareWANService     // 云端服务包
	SoftwareFactoryService = store.SoftwareFactoryService // 厂端服务包
	SoftwareClientAPK      = store.SoftwareClientAPK      // 客户端 APK
)

// 与 store 同源的表模型，按域给应用服务和验收当入口类型用。
type (
	// 本侧库连接，应用服务经它读写。
	Store = store.Store
	// 唯一的云端管理员账号。
	Admin = store.Admin
	// 管理员在线会话，只存令牌哈希。
	Session = store.Session
	// 工厂名录里的一家厂。
	Factory = store.Factory
	// 建厂时交给该厂的初始超管身份。
	InitialSuperAdmin = store.InitialSuperAdmin
	// 分到某厂的一台现场设备。
	Client = store.Client
	// 平台级的一条工艺或工程。
	Asset = store.Asset
	// 工程钉住的一条工艺依赖。
	AssetDep = store.AssetDep
	// 升档或下发时带上的资产快照。
	AssetSnapshot = store.AssetSnapshot
	// 平台目录里的一个节点。
	FSNode = store.FSNode
	// 目录上挂的文件夹提示。
	FSFolderHint = store.FSFolderHint
	// 某一类资产的整棵平台目录。
	PlatformFSLayout = store.PlatformFSLayout
	// 封闭包里的一个成员。
	ClosureMember = store.ClosureMember
	// 一次组包得到的封闭包。
	ClosureSnapshot = store.ClosureSnapshot
	// 工艺或工程的一份字段模版。
	ContentTemplate = store.ContentTemplate
	// 下发给厂的一份模版快照。
	TemplateSnapshot = store.TemplateSnapshot
	// 允许某厂领取某资产的授权。
	DistributionGrant = store.DistributionGrant
	// 已经下发给某厂的一条记录。
	DistributionRecord = store.DistributionRecord
	// 尚未用完的一次建厂认领。
	EnrollmentOffer = store.EnrollmentOffer
	// 发给该厂、用来拆包的租约。
	ContentLease = store.ContentLease
	// 云端用来签名的那一把钥。
	WANSigningKey = store.WANSigningKey
	// 已发布的一个服务包或客户端包。
	SoftwareRelease = store.SoftwareRelease
	// 软件包发到某厂的记录。
	SoftwareDistribution = store.SoftwareDistribution
	// 某厂上送的一粒焊汇总。
	WeldSummary = store.WeldSummary
)
