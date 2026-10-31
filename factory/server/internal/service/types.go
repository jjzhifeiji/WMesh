package service

import (
	"wmesh/factory/internal/store"
)

const (
	StatusActive   = store.StatusActive   // 有效：认证后按角色操作
	StatusDisabled = store.StatusDisabled // 已停用：不能新开会话或新开受保护操作
	StatusRevoked  = store.StatusRevoked  // 角色已收回，行留下做历史

	RoleFactorySuperAdmin = store.RoleFactorySuperAdmin // 工厂超管，拥有本厂全部权限
	RoleOrgAdmin          = store.RoleOrgAdmin          // 管理员：厂级工艺工程、报表与审计
	RoleOperator          = store.RoleOperator          // 操作员：示教器作业、产生焊事实

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

	LoginKindPad    = store.LoginKindPad    // 厂网示教器登录
	LoginKindClient = store.LoginKindClient // 已钉设备号的本机登录
	LoginKindMQTT   = store.LoginKindMQTT   // 本厂 MQTT 回连补现场快照
)

// 与 store 同源的表模型，按域给应用服务和验收当入口类型用。
type (
	Store   = store.Store   // 本厂库连接，业务只经它读写
	Person  = store.Person  // 厂内人员，含登录名与启停状态
	Session = store.Session // 登录会话，过期后新操作拒绝

	Lifecycle = store.Lifecycle // 本厂启停注销以及当前修订

	OrgUnit   = store.OrgUnit   // 组织节点，停用后不能再收人
	RoleGrant = store.RoleGrant // 角色授予，都覆盖整厂

	PathNode    = store.PathNode    // 事实发生时路径上的一节
	WorkContext = store.WorkContext // 这次操作选定的工作上下文
	FactStub    = store.FactStub    // 运行事实桩，带冻结的路径

	Client         = store.Client         // 已分配到本厂的设备
	SigningKey     = store.SigningKey     // 本厂签发运行凭证的钥
	RuntimeGrant   = store.RuntimeGrant   // 允许或撤销设备运行的凭证
	LoginSnap      = store.LoginSnap      // 一次登录的现场快照
	PersonLoginLog = store.PersonLoginLog // 人员登录历史，供名册回看

	Asset            = store.Asset            // 工艺或工程的治理记录
	AssetDep         = store.AssetDep         // 资产依赖，作业类型必须一致
	AssetSnapshot    = store.AssetSnapshot    // 钉死修订的资产快照
	FSNode           = store.FSNode           // 文件树节点，不承载工艺正文
	FSFolderHint     = store.FSFolderHint     // 目录提示，用来摆放资产
	PlatformFSLayout = store.PlatformFSLayout // 平台下发时的目录布局

	ContentTemplate  = store.ContentTemplate  // 已收的工艺或工程字段模版
	TemplateSnapshot = store.TemplateSnapshot // 模版送达时的修订快照

	AssetReplica    = store.AssetReplica    // 下发到本厂的平台级副本
	ClosureMember   = store.ClosureMember   // 闭包成员，正文可以是信封
	ClosureSnapshot = store.ClosureSnapshot // 一次组包钉死的闭包快照
	ClientPolicy    = store.ClientPolicy    // 对本厂全部本机生效的策略

	UploadRecord = store.UploadRecord // 点云或图片的上传记录
	WeldFact     = store.WeldFact     // 一次焊接起停的事实
	WeldTotals   = store.WeldTotals   // 焊长和时长的合计

	SoftwareReplica = store.SoftwareReplica // 厂端收到的软件包副本
	WANTrust        = store.WANTrust        // 本厂信任广域侧的材料
)
