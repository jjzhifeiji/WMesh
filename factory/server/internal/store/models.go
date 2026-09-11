package store

import (
	"time"

	"github.com/google/uuid"
)

const (
	StatusPending  = "pending"  // 待启用：可预写角色，但不能登录
	StatusActive   = "active"   // 有效：认证后按角色作用域操作
	StatusDisabled = "disabled" // 已停用：不能新开会话或新开受保护操作
	StatusEnded    = "ended"    // 分配已取消，行留下做历史
	StatusRevoked  = "revoked"  // 角色已收回，行留下做历史

	ClientStatusBound = "bound" // 本厂有效绑定，可以签发
	ClientStatusVoid  = "void"  // 已作废，不得再签发

	RoleFactorySuperAdmin = "factory_super_admin" // 工厂超级管理员，只能挂 Factory 作用域
	RoleOrgAdmin          = "org_admin"           // 组织管理员，只管本节点及当前子树
	RoleOrgLead           = "org_lead"            // 组织负责人，子树只读
	RoleProcessEngineer   = "process_engineer"    // 工艺工程师，可 Factory 或节点作用域
	RoleOperator          = "operator"            // 操作员，可产生运行事实
	RoleAuditor           = "auditor"             // 审计员，只读，不能改业务

	ScopeFactory = "factory"  // 覆盖本厂及当时全部组织节点
	ScopeOrgUnit = "org_unit" // 只覆盖该节点及当时子树

	KindProcess = "process" // 可复用工艺
	KindProject = "project" // 一次作业工程

	AssetLevelFactory  = "factory"  // 本厂厂级
	AssetLevelPersonal = "personal" // 本厂个人级
	AssetLevelPlatform = "platform" // 已下发到本厂的平台级副本

	AssetDraft     = "draft"     // 草稿：不可依赖、不可升档
	AssetAvailable = "available" // 可用
	AssetDisabled  = "disabled"  // 停用后不得改内容或升档
)

// Person 是本厂一个自然人账号，固定只属于本厂库，不属于任何组织节点。
type Person struct {
	ID                  uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`      // 稳定身份，改名也不变
	LoginName           string    `gorm:"not null" json:"loginName"`           // 本厂内唯一登录名，不是身份
	DisplayName         string    `gorm:"not null" json:"displayName"`         // 显示名，可改
	Status              string    `gorm:"not null" json:"status"`              // pending / active / disabled
	PasswordHash        *string   `json:"-"`                                   // 日常口令哈希，只存在本厂；激活前为空
	ActivationTokenHash *string   `json:"-"`                                   // 一次性激活口令哈希，激活后清空
	IsInitialSuperAdmin bool      `gorm:"not null" json:"isInitialSuperAdmin"` // 本厂唯一的 WAN 下发初始超管
	CreatedAt           time.Time `gorm:"not null" json:"createdAt"`           // 账号创建时间
}

func (Person) TableName() string { return "people" }

// OrgUnit 是本厂一棵树上的节点，至多一个父节点，不能跨厂、不能成环。
type OrgUnit struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"` // 节点稳定身份
	ParentID  *uuid.UUID `gorm:"type:uuid" json:"parentId"`      // 空表示直接挂在工厂下
	Name      string     `gorm:"not null" json:"name"`           // 显示名，改名不改历史快照
	Status    string     `gorm:"not null" json:"status"`         // 停用后不能再当新工作上下文
	CreatedAt time.Time  `gorm:"not null" json:"createdAt"`      // 创建时间
}

func (OrgUnit) TableName() string { return "org_units" }

// Assignment 是人员到组织节点的关系；取消只把状态标成已结束，不删行。
type Assignment struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`      // 分配关系稳定身份
	PersonID  uuid.UUID  `gorm:"type:uuid;not null" json:"personId"`  // 本厂人员
	OrgUnitID uuid.UUID  `gorm:"type:uuid;not null" json:"orgUnitId"` // 分配到的组织节点
	Status    string     `gorm:"not null" json:"status"`              // active 或 ended；取消不删行
	CreatedAt time.Time  `gorm:"not null" json:"createdAt"`           // 分配开始时间
	EndedAt   *time.Time `json:"endedAt"`                             // 取消分配的时间；有效分配必须为空
}

func (Assignment) TableName() string { return "assignments" }

// RoleGrant 是带明确作用域的一条角色；分配组织不会自动产生本行。
type RoleGrant struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`     // 授予记录稳定身份
	PersonID  uuid.UUID  `gorm:"type:uuid;not null" json:"personId"` // 被授予的本厂人员
	Role      string     `gorm:"not null" json:"role"`               // 六种固定角色之一
	ScopeKind string     `gorm:"not null" json:"scopeKind"`          // factory 或 org_unit
	OrgUnitID *uuid.UUID `gorm:"type:uuid" json:"orgUnitId"`         // Factory 作用域必须为空
	Status    string     `gorm:"not null" json:"status"`             // active 或 revoked
	CreatedAt time.Time  `gorm:"not null" json:"createdAt"`          // 授予时间
	RevokedAt *time.Time `json:"revokedAt"`                          // 收回时间；有效授予必须为空
}

func (RoleGrant) TableName() string { return "role_grants" }

// Session 是厂内在线会话；库里只存令牌哈希，每次操作要重查账号状态和角色。
type Session struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`     // 会话稳定身份
	PersonID  uuid.UUID `gorm:"type:uuid;not null" json:"personId"` // 持有该会话的本厂人员
	TokenHash string    `gorm:"not null;uniqueIndex" json:"-"`      // 会话令牌哈希，不存原文
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`          // 会话建立时间
	ExpiresAt time.Time `gorm:"not null" json:"expiresAt"`          // 过期后立刻无效
}

func (Session) TableName() string { return "sessions" }

// PathNode 是事实发生时路径上的一截：当时的身份和名称。
type PathNode struct {
	ID     uuid.UUID  `json:"id"`               // 节点稳定身份
	TypeID *uuid.UUID `json:"typeId,omitempty"` // 旧快照可能带类型身份；新写入不再写
	Name   string     `json:"name"`             // 当时的显示名，之后改名也不改这里
}

// WorkContext 是产生事实或个人资产时必须明确选的一个上下文。
type WorkContext struct {
	Direct    bool       // true 表示 Factory 直属，路径为空
	OrgUnitID *uuid.UUID // 与 Direct 互斥；必须是本人当前分配的有效节点
}

// FactStub 是最小运行事实：只记创建人和发生时的组织路径，供统计口径验收。
type FactStub struct {
	ID        uuid.UUID  // 事实桩稳定身份
	CreatorID uuid.UUID  // 创建账号稳定身份
	FactoryID uuid.UUID  // 所属工厂
	OrgUnitID *uuid.UUID // 发生节点；直属工厂时为空
	OrgPath   []PathNode // 当时从工厂到该节点的祖先快照；直属为空
	CreatedAt time.Time  // 发生时间；路径快照此后不得改写
}

// PersonalAsset 是最小个人级资产桩，不是真实工艺/工程；内容不因超管身份打开。
type PersonalAsset struct {
	ID        uuid.UUID  // 个人资产桩稳定身份
	CreatorID uuid.UUID  // 创建人；仅本人可读
	FactoryID uuid.UUID  // 所属工厂
	OrgUnitID *uuid.UUID // 创建时节点；直属时为空
	OrgPath   []PathNode // 创建时路径，改分配不改写
	CreatedAt time.Time  // 创建时间
}

type factRow struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey"` // 事实桩稳定身份
	CreatorID uuid.UUID  `gorm:"type:uuid;not null"`   // 创建账号稳定身份
	FactoryID uuid.UUID  `gorm:"type:uuid;not null"`   // 所属工厂
	OrgUnitID *uuid.UUID `gorm:"type:uuid"`            // 发生节点；直属工厂时为空
	OrgPath   []byte     `gorm:"type:jsonb;not null"`  // 快照原文，迁移不得改写
	CreatedAt time.Time  `gorm:"not null"`             // 发生时间
}

func (factRow) TableName() string { return "fact_stubs" }

// SigningKey 是本厂签发密钥；全表一行，私钥不进审计。
type SigningKey struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`       // 本厂签发密钥行身份
	PublicKey  []byte    `gorm:"type:bytea;not null" json:"publicKey"` // Ed25519 公钥 32 字节
	PrivateKey []byte    `gorm:"type:bytea;not null" json:"-"`         // 本厂签发私钥，不是 Client 私钥
	CreatedAt  time.Time `gorm:"not null" json:"createdAt"`            // 写入时间
}

func (SigningKey) TableName() string { return "signing_keys" }

// Client 是本厂已接受的一台现场节点绑定。
type Client struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`       // 与 WAN 相同的 Client 稳定身份
	PublicKey       []byte     `gorm:"type:bytea;not null" json:"publicKey"` // 本机公钥；无私钥
	BindingRevision int64      `gorm:"not null" json:"bindingRevision"`      // 已接受的绑定修订，只向前
	Status          string     `gorm:"not null" json:"status"`               // bound / void
	BoundAt         time.Time  `gorm:"not null" json:"boundAt"`              // 最近一次接受为 bound 的时间
	VoidedAt        *time.Time `json:"voidedAt"`                             // 作废时间；bound 必须为空
}

func (Client) TableName() string { return "clients" }

// RuntimeGrant 是签给某 Client 的一版节点运行凭证。
type RuntimeGrant struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`       // 节点运行凭证稳定身份
	ClientID  uuid.UUID `gorm:"type:uuid;not null" json:"clientId"`   // 签给本厂这台 Client
	Revision  int64     `gorm:"not null" json:"revision"`             // 该 Client 的节点授权修订，只向前
	CanRun    bool      `gorm:"not null" json:"canRun"`               // 本修订是否允许运行
	NotBefore time.Time `gorm:"not null" json:"notBefore"`            // 生效时间
	NotAfter  time.Time `gorm:"not null" json:"notAfter"`             // 失效时间
	Payload   []byte    `gorm:"type:bytea;not null" json:"payload"`   // 被签名的声明原文
	Signature []byte    `gorm:"type:bytea;not null" json:"signature"` // Ed25519 签名 64 字节
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`            // 写入时间
}

func (RuntimeGrant) TableName() string { return "client_runtime_grants" }

// OrgOption 是人员离线授权里当时可选的一个组织节点及其祖先路径。
type OrgOption struct {
	OrgUnitID uuid.UUID  `json:"orgUnitId"` // 当时可选节点
	Path      []PathNode `json:"path"`      // 当时从工厂到该节点的祖先快照
}

// RoleSnapshot 是签发时一条角色与作用域，离线不再查厂库。
type RoleSnapshot struct {
	Role      string     `json:"role"`                // 六种固定角色之一
	ScopeKind string     `json:"scopeKind"`           // factory 或 org_unit
	OrgUnitID *uuid.UUID `json:"orgUnitId,omitempty"` // Factory 作用域为空
}

// PersonOfflineGrant 是签给本厂账号、绑定特定 Client 的人员离线授权快照。
type PersonOfflineGrant struct {
	ID            uuid.UUID      // 人员离线授权稳定身份
	PersonID      uuid.UUID      // 本厂账号稳定身份
	ClientID      uuid.UUID      // 绑定到的本厂 Client
	Revision      int64          // 该账号在该 Client 上的授权修订，只向前
	LoginName     string         // 签发时登录名，离线对照用，不是身份
	PasswordHash  string         // 该人口令验证材料副本；不进 JSON
	AllowDirect   bool           // 是否允许 Factory 直属
	OrgSnapshot   []OrgOption    // 当时可选 OrgUnit 及祖先路径
	RolesSnapshot []RoleSnapshot // 当时固定角色与作用域
	NotBefore     time.Time      // 生效时间
	NotAfter      time.Time      // 失效时间
	Payload       []byte         // 被签名的声明原文
	Signature     []byte         // Ed25519 签名 64 字节
	CreatedAt     time.Time      // 写入时间
}

type personOfflineGrantRow struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey"` // 人员离线授权稳定身份
	PersonID      uuid.UUID `gorm:"type:uuid;not null"`   // 本厂账号稳定身份
	ClientID      uuid.UUID `gorm:"type:uuid;not null"`   // 绑定到的本厂 Client
	Revision      int64     `gorm:"not null"`             // 该账号在该 Client 上的授权修订，只向前
	LoginName     string    `gorm:"not null"`             // 签发时登录名，离线对照用，不是身份
	PasswordHash  string    `gorm:"not null"`             // 该人口令验证材料副本，不是全厂账号库
	AllowDirect   bool      `gorm:"not null"`             // 是否允许 Factory 直属
	OrgSnapshot   []byte    `gorm:"type:jsonb;not null"`  // 当时可选 OrgUnit 及祖先路径
	RolesSnapshot []byte    `gorm:"type:jsonb;not null"`  // 当时固定角色与作用域
	NotBefore     time.Time `gorm:"not null"`             // 生效时间
	NotAfter      time.Time `gorm:"not null"`             // 失效时间
	Payload       []byte    `gorm:"type:bytea;not null"`  // 被签名的声明原文
	Signature     []byte    `gorm:"type:bytea;not null"`  // Ed25519 签名 64 字节
	CreatedAt     time.Time `gorm:"not null"`             // 写入时间
}

func (personOfflineGrantRow) TableName() string { return "person_offline_grants" }

type assetRow struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey"` // 个人资产桩稳定身份
	CreatorID uuid.UUID  `gorm:"type:uuid;not null"`   // 创建人；仅本人可读
	FactoryID uuid.UUID  `gorm:"type:uuid;not null"`   // 所属工厂
	OrgUnitID *uuid.UUID `gorm:"type:uuid"`            // 创建时节点；直属时为空
	OrgPath   []byte     `gorm:"type:jsonb;not null"`  // 创建时路径，改分配不改写
	Content   string     `gorm:"not null"`             // 内容正文；不进审计
	CreatedAt time.Time  `gorm:"not null"`             // 创建时间
}

func (assetRow) TableName() string { return "personal_asset_stubs" }

// AssetDep 是工程钉死的一条工艺依赖：身份、修订和当时摘要。
type AssetDep struct {
	ID       uuid.UUID `json:"id"`       // 被依赖工艺稳定身份
	Revision int64     `json:"revision"` // 钉死的工艺修订
	Digest   []byte    `json:"digest"`   // 当时该修订的 SHA-256 摘要
}

// AssetSnapshot 是厂级升平台用的内存快照，不测协议。
type AssetSnapshot struct {
	SourceID        uuid.UUID  `json:"sourceId"`        // 源厂级身份
	SourceRevision  int64      `json:"sourceRevision"`  // 源修订
	SourceFactoryID uuid.UUID  `json:"sourceFactoryId"` // 源厂
	Kind            string     `json:"kind"`            // process / project
	Name            string     `json:"name"`            // 显示名
	Content         []byte     `json:"content"`         // 正文
	Digest          []byte     `json:"digest"`          // 摘要
	Copyable        bool       `json:"copyable"`        // 源是否可复制
	Status          string     `json:"status"`          // 源状态
	Deps            []AssetDep `json:"deps"`            // 源依赖（身份+修订+摘要）
}

// Asset 是本厂一条工艺或工程的当前行，不含历史正文。
type Asset struct {
	ID             uuid.UUID  `json:"id"`             // 稳定身份
	Kind           string     `json:"kind"`           // process / project
	Level          string     `json:"level"`          // factory / personal
	Name           string     `json:"name"`           // 显示名，不当身份
	Status         string     `json:"status"`         // draft / available / disabled
	Copyable       bool       `json:"copyable"`       // 可否升档
	Revision       int64      `json:"revision"`       // 当前修订
	Content        []byte     `json:"-"`              // 不透明正文；不进列表/元数据
	Digest         []byte     `json:"digest"`         // SHA-256 32 字节
	CreatorID      uuid.UUID  `json:"creatorId"`      // 创建人
	FactoryID      uuid.UUID  `json:"factoryId"`      // 所属本厂
	OrgUnitID      *uuid.UUID `json:"orgUnitId"`      // 创建时节点；直属为空
	OrgPath        []PathNode `json:"orgPath"`        // 创建时路径
	SourceID       *uuid.UUID `json:"sourceId"`       // 升档源身份
	SourceRevision *int64     `json:"sourceRevision"` // 升档源修订
	Deps           []AssetDep `json:"deps"`           // 工艺必须空
	CreatedAt      time.Time  `json:"createdAt"`      // 创建时间
	UpdatedAt      time.Time  `json:"updatedAt"`      // 最近升高修订的时间
}

// AssetWrite 是一次改名/改内容/改可复制/改状态/改依赖的写入。
type AssetWrite struct {
	Name     string     // 显示名
	Content  []byte     // 正文
	Digest   []byte     // 与正文对应的摘要
	Copyable bool       // 可复制
	Status   string     // 状态
	Deps     []AssetDep // 工程依赖；工艺必须空
}

type governedAssetRow struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey"`   // 稳定身份
	Kind           string     `gorm:"not null"`               // process / project
	Level          string     `gorm:"not null"`               // factory / personal
	Name           string     `gorm:"not null"`               // 显示名
	Status         string     `gorm:"not null"`               // draft / available / disabled
	Copyable       bool       `gorm:"not null"`               // 可否升档
	Revision       int64      `gorm:"not null"`               // 当前修订
	Content        []byte     `gorm:"type:bytea;not null"`    // 正文
	Digest         []byte     `gorm:"type:bytea;not null"`    // SHA-256
	CreatorID      uuid.UUID  `gorm:"type:uuid;not null"`     // 创建人
	FactoryID      uuid.UUID  `gorm:"type:uuid;not null"`     // 所属本厂
	OrgUnitID      *uuid.UUID `gorm:"type:uuid"`              // 创建时节点
	OrgPath        []byte     `gorm:"type:jsonb;not null"`    // 路径快照
	SourceID       *uuid.UUID `gorm:"type:uuid"`              // 升档源
	SourceRevision *int64     `gorm:"column:source_revision"` // 升档源修订
	Deps           []byte     `gorm:"type:jsonb;not null"`    // 依赖 JSON
	CreatedAt      time.Time  `gorm:"not null"`               // 创建时间
	UpdatedAt      time.Time  `gorm:"not null"`               // 最近升高修订的时间
}

func (governedAssetRow) TableName() string { return "assets" }

// ClosureMember 是闭包里的一条资产快照，含正文。
type ClosureMember struct {
	ID        uuid.UUID  `json:"id"`        // 稳定身份
	Kind      string     `json:"kind"`      // process / project
	Level     string     `json:"level"`     // platform / factory / personal
	Name      string     `json:"name"`      // 显示名
	Status    string     `json:"status"`    // 送达时状态
	Copyable  bool       `json:"copyable"`  // 与源相同
	Revision  int64      `json:"revision"`  // 钉死修订
	Content   []byte     `json:"content"`   // 正文
	Digest    []byte     `json:"digest"`    // 内容 SHA-256
	Deps      []AssetDep `json:"deps"`      // 工艺必须空
	CreatorID *uuid.UUID `json:"creatorId"` // 个人级创建人；其余可空
}

// ClosureSnapshot 是一份工程或单条工艺的完整快照，不是新身份。
type ClosureSnapshot struct {
	Kind            string          `json:"kind"`            // process / project
	AssetID         uuid.UUID       `json:"assetId"`         // 根资产身份
	Revision        int64           `json:"revision"`        // 根修订
	Level           string          `json:"level"`           // 与源相同
	Copyable        bool            `json:"copyable"`        // 与源相同
	Status          string          `json:"status"`          // 与源相同
	TargetFactoryID *uuid.UUID      `json:"targetFactoryId"` // WAN→厂时必填
	TargetClientID  *uuid.UUID      `json:"targetClientId"`  // 厂→Client 或个人级装袋时必填
	Members         []ClosureMember `json:"members"`         // 根在前，其余按 deps 顺序
	Digest          []byte          `json:"digest"`          // 整包 SHA-256
}

// AssetReplica 是已送达本厂的一条平台级（身份, 修订）只读副本。
type AssetReplica struct {
	ID         uuid.UUID  `json:"id"`         // 平台级稳定身份
	Revision   int64      `json:"revision"`   // 送达修订
	Kind       string     `json:"kind"`       // process / project
	Level      string     `json:"level"`      // 固定 platform
	Name       string     `json:"name"`       // 显示名
	Status     string     `json:"status"`     // 送达时状态
	Copyable   bool       `json:"copyable"`   // 必须为否
	Content    []byte     `json:"content"`    // 正文
	Digest     []byte     `json:"digest"`     // SHA-256
	Deps       []AssetDep `json:"deps"`       // 工艺必须空
	ReceivedAt time.Time  `json:"receivedAt"` // 本厂收到时间
}

// FactorySettings 是本厂一份设置，目前只有 Client 工程缓存上限。
type FactorySettings struct {
	ID                int16 `json:"id"`                // 固定 1
	MaxCachedProjects int   `json:"maxCachedProjects"` // 每 Client 工程份上限，≥1
}

// ClientDistributionGrant 是某工程可否下发到某 Client。
type ClientDistributionGrant struct {
	ID        uuid.UUID `json:"id"`        // 授权记录身份
	ProjectID uuid.UUID `json:"projectId"` // 工程身份
	ClientID  uuid.UUID `json:"clientId"`  // 目标 Client
	Active    bool      `json:"active"`    // 是否仍有效
	GrantedBy uuid.UUID `json:"grantedBy"` // 授权超管
	CreatedAt time.Time `json:"createdAt"` // 授权时间
	UpdatedAt time.Time `json:"updatedAt"` // 最近变更
}

// ClientDistributionRecord 是向 Client 下发过的一份工程修订，不含正文。
type ClientDistributionRecord struct {
	ID            uuid.UUID  `json:"id"`            // 记录身份
	ProjectID     uuid.UUID  `json:"projectId"`     // 工程身份
	Revision      int64      `json:"revision"`      // 下发修订
	ClientID      uuid.UUID  `json:"clientId"`      // 目标 Client
	ClosureDigest []byte     `json:"closureDigest"` // 整包摘要
	Members       []AssetDep `json:"members"`       // 成员身份+修订+摘要
	CreatedAt     time.Time  `json:"createdAt"`     // 首次下发时间
}

type replicaRow struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"` // 平台级身份
	Revision   int64     `gorm:"primaryKey"`           // 送达修订
	Kind       string    `gorm:"not null"`             // process / project
	Level      string    `gorm:"not null"`             // platform
	Name       string    `gorm:"not null"`             // 显示名
	Status     string    `gorm:"not null"`             // 送达时状态
	Copyable   bool      `gorm:"not null"`             // 必须为否
	Content    []byte    `gorm:"type:bytea;not null"`  // 正文
	Digest     []byte    `gorm:"type:bytea;not null"`  // SHA-256
	Deps       []byte    `gorm:"type:jsonb;not null"`  // 依赖 JSON
	ReceivedAt time.Time `gorm:"not null"`             // 收到时间
}

func (replicaRow) TableName() string { return "asset_replicas" }

type factorySettingsRow struct {
	ID                int16 `gorm:"primaryKey"` // 固定 1
	MaxCachedProjects int   `gorm:"not null"`   // 缓存上限
}

func (factorySettingsRow) TableName() string { return "factory_settings" }

type clientGrantRow struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"` // 授权身份
	ProjectID uuid.UUID `gorm:"type:uuid;not null"`   // 工程
	ClientID  uuid.UUID `gorm:"type:uuid;not null"`   // Client
	Active    bool      `gorm:"not null"`             // 是否有效
	GrantedBy uuid.UUID `gorm:"type:uuid;not null"`   // 超管
	CreatedAt time.Time `gorm:"not null"`             // 授权时间
	UpdatedAt time.Time `gorm:"not null"`             // 最近变更
}

func (clientGrantRow) TableName() string { return "client_distribution_grants" }

type clientRecordRow struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey"` // 记录身份
	ProjectID     uuid.UUID `gorm:"type:uuid;not null"`   // 工程
	Revision      int64     `gorm:"not null"`             // 修订
	ClientID      uuid.UUID `gorm:"type:uuid;not null"`   // Client
	ClosureDigest []byte    `gorm:"type:bytea;not null"`  // 整包摘要
	Members       []byte    `gorm:"type:jsonb;not null"`  // 成员 JSON
	CreatedAt     time.Time `gorm:"not null"`             // 首次下发
}

func (clientRecordRow) TableName() string { return "client_distribution_records" }

const (
	UploadPointCloud = "point_cloud" // 点云
	UploadImage      = "image"       // 图片
)

// UploadRecord 是 Client 主动上传点云/图片的元数据，不含正文。
type UploadRecord struct {
	ID        uuid.UUID // 产生端稳定身份，汇聚幂等键
	Kind      string    // point_cloud / image
	ObjectKey string    // 对象存储键
	Digest    []byte    // SHA-256 摘要 32 字节
	ByteSize  int64     // 正文字节数
	CreatorID uuid.UUID // 上传人
	ClientID  uuid.UUID // 来源 Client
	CreatedAt time.Time // 首次汇聚时间
}

type uploadRow struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"` // 产生端稳定身份
	Kind      string    `gorm:"not null"`              // point_cloud / image
	ObjectKey string    `gorm:"not null"`              // 对象键
	Digest    []byte    `gorm:"type:bytea;not null"`    // SHA-256
	ByteSize  int64     `gorm:"not null"`               // 字节数
	CreatorID uuid.UUID `gorm:"type:uuid;not null"`    // 上传人
	ClientID  uuid.UUID `gorm:"type:uuid;not null"`    // 来源 Client
	CreatedAt time.Time `gorm:"not null"`              // 首次汇聚时间
}

func (uploadRow) TableName() string { return "upload_records" }
