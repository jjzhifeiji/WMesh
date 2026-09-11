package store

import (
	"time"

	"github.com/google/uuid"
)

// Factory 是 WAN 名录里的一家工厂，不含厂内组织或普通账号。
type Factory struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"` // 工厂稳定身份，也用来选厂库
	Name      string    `gorm:"not null" json:"name"`           // 工厂显示名，不当身份
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`      // 名录入库时间
}

func (Factory) TableName() string { return "factories" }

// Admin 是 WAN 唯一管理员；表上有单行约束，不能再邀请第二人。
type Admin struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`        // WAN 管理员稳定身份
	LoginName    string    `gorm:"not null;uniqueIndex" json:"loginName"` // WAN 登录名，全库唯一
	PasswordHash string    `gorm:"not null" json:"-"`                     // 日常口令哈希，只存在 WAN 库
	CreatedAt    time.Time `gorm:"not null" json:"createdAt"`             // 账号创建时间
}

func (Admin) TableName() string { return "wan_admins" }

// InitialSuperAdmin 是交付对账用的初始超管身份，不含日常口令。
type InitialSuperAdmin struct {
	FactoryID uuid.UUID `gorm:"type:uuid;primaryKey" json:"factoryId"` // 一厂只能绑一名初始超管
	PersonID  uuid.UUID `gorm:"type:uuid;not null" json:"personId"`    // 落在目标厂库里的账号身份
	LoginName string    `gorm:"not null" json:"loginName"`             // 交付时的登录名，不是秘密
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`             // 对账记录写入时间
}

func (InitialSuperAdmin) TableName() string { return "initial_super_admins" }

// Session 是 WAN 管理员会话；库里只存令牌哈希。
type Session struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`    // 会话稳定身份
	AdminID   uuid.UUID `gorm:"type:uuid;not null" json:"adminId"` // 持有该会话的 WAN 管理员
	TokenHash string    `gorm:"not null;uniqueIndex" json:"-"`     // 会话令牌哈希，不存原文
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`         // 会话建立时间
	ExpiresAt time.Time `gorm:"not null" json:"expiresAt"`         // 过期后此会话立刻无效
}

func (Session) TableName() string { return "sessions" }

// FactoryPublicKey 是某厂签发用的公钥，WAN 不存对应私钥。
type FactoryPublicKey struct {
	FactoryID uuid.UUID `gorm:"type:uuid;primaryKey" json:"factoryId"` // 该厂签发公钥，一对一
	PublicKey []byte    `gorm:"type:bytea;not null" json:"publicKey"`  // Ed25519 公钥 32 字节，无私钥
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`             // 登记时间
}

func (FactoryPublicKey) TableName() string { return "factory_public_keys" }

// Client 是一台现场节点：公钥在此登记，同一时刻只属一个工厂。
type Client struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`       // Client 稳定身份，不由名称生成
	PublicKey       []byte     `gorm:"type:bytea;not null" json:"publicKey"` // 本机公钥，绑定时登记；无私钥
	FactoryID       *uuid.UUID `gorm:"type:uuid" json:"factoryId"`           // 当前所属工厂；空表示未绑定
	BindingRevision int64      `gorm:"not null" json:"bindingRevision"`      // 绑定修订；未绑定为 0，改绑必须升高
	BoundAt         *time.Time `json:"boundAt"`                              // 当前这次绑定生效时间；未绑定为空
	CreatedAt       time.Time  `gorm:"not null" json:"createdAt"`            // 身份登记时间
}

func (Client) TableName() string { return "clients" }

const (
	KindProcess = "process" // 可复用工艺
	KindProject = "project" // 一次作业工程

	AssetLevelPlatform = "platform" // 平台级，只在 WAN

	AssetDraft     = "draft"     // 草稿
	AssetAvailable = "available" // 可用
	AssetDisabled  = "disabled"  // 停用
)

// AssetDep 是工程钉死的一条工艺依赖。
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
	Deps            []AssetDep `json:"deps"`            // 源依赖
}

// Asset 是一条平台级工艺或工程的当前行。
type Asset struct {
	ID              uuid.UUID  `json:"id"`              // 稳定身份
	Kind            string     `json:"kind"`            // process / project
	Level           string     `json:"level"`           // 固定 platform
	Name            string     `json:"name"`            // 显示名
	Status          string     `json:"status"`          // draft / available / disabled
	Copyable        bool       `json:"copyable"`        // 平台级必须为否
	Revision        int64      `json:"revision"`        // 当前修订
	Content         []byte     `json:"-"`               // 正文；不进列表/元数据
	Digest          []byte     `json:"digest"`          // SHA-256 32 字节
	CreatorID       uuid.UUID  `json:"creatorId"`       // WAN 管理员
	SourceID        *uuid.UUID `json:"sourceId"`        // 升档源厂级身份
	SourceRevision  *int64     `json:"sourceRevision"`  // 升档源修订
	SourceFactoryID *uuid.UUID `json:"sourceFactoryId"` // 升档源厂
	Deps            []AssetDep `json:"deps"`            // 工艺必须空
	CreatedAt       time.Time  `json:"createdAt"`       // 创建时间
	UpdatedAt       time.Time  `json:"updatedAt"`       // 最近升高修订的时间
}

// AssetWrite 是一次改名/改内容/改状态/改依赖的写入；可复制在 WAN 保持为否。
type AssetWrite struct {
	Name    string     // 显示名
	Content []byte     // 正文
	Digest  []byte     // 与正文对应的摘要
	Status  string     // 状态
	Deps    []AssetDep // 工程依赖；工艺必须空
}

type assetRow struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey"`   // 稳定身份
	Kind            string     `gorm:"not null"`               // process / project
	Level           string     `gorm:"not null"`               // 固定 platform
	Name            string     `gorm:"not null"`               // 显示名
	Status          string     `gorm:"not null"`               // draft / available / disabled
	Copyable        bool       `gorm:"not null"`               // 必须为否
	Revision        int64      `gorm:"not null"`               // 当前修订
	Content         []byte     `gorm:"type:bytea;not null"`    // 正文
	Digest          []byte     `gorm:"type:bytea;not null"`    // SHA-256
	CreatorID       uuid.UUID  `gorm:"type:uuid;not null"`     // WAN 管理员
	SourceID        *uuid.UUID `gorm:"type:uuid"`              // 升档源厂级身份
	SourceRevision  *int64     `gorm:"column:source_revision"` // 升档源修订
	SourceFactoryID *uuid.UUID `gorm:"type:uuid"`              // 升档源厂
	Deps            []byte     `gorm:"type:jsonb;not null"`    // 依赖 JSON
	CreatedAt       time.Time  `gorm:"not null"`               // 创建时间
	UpdatedAt       time.Time  `gorm:"not null"`               // 最近升高修订的时间
}

func (assetRow) TableName() string { return "assets" }

// ClosureMember 是闭包里的一条资产快照，含正文。
type ClosureMember struct {
	ID       uuid.UUID  `json:"id"`       // 稳定身份
	Kind     string     `json:"kind"`     // process / project
	Level    string     `json:"level"`    // 固定 platform
	Name     string     `json:"name"`     // 显示名
	Status   string     `json:"status"`   // 组包时状态
	Copyable bool       `json:"copyable"` // 与源相同
	Revision int64      `json:"revision"` // 钉死修订
	Content  []byte     `json:"content"`  // 正文
	Digest   []byte     `json:"digest"`   // 内容 SHA-256
	Deps     []AssetDep `json:"deps"`     // 工艺必须空
}

// ClosureSnapshot 是一份平台级工程或工艺的完整快照，不是新身份。
type ClosureSnapshot struct {
	Kind            string          `json:"kind"`            // process / project
	AssetID         uuid.UUID       `json:"assetId"`         // 根资产身份
	Revision        int64           `json:"revision"`        // 根修订
	Level           string          `json:"level"`           // 固定 platform
	Copyable        bool            `json:"copyable"`        // 必须为否
	Status          string          `json:"status"`          // 组包时状态
	TargetFactoryID *uuid.UUID      `json:"targetFactoryId"` // 目标工厂
	TargetClientID  *uuid.UUID      `json:"targetClientId"`  // WAN→厂为空
	Members         []ClosureMember `json:"members"`         // 根在前，其余按 deps 顺序
	Digest          []byte          `json:"digest"`          // 整包 SHA-256
}

// DistributionGrant 是某平台级资产可否下发到某厂。
type DistributionGrant struct {
	ID        uuid.UUID `json:"id"`        // 授权记录身份
	AssetID   uuid.UUID `json:"assetId"`   // 平台级资产
	FactoryID uuid.UUID `json:"factoryId"` // 目标工厂
	Active    bool      `json:"active"`    // 是否仍有效
	CreatedAt time.Time `json:"createdAt"` // 授权时间
	UpdatedAt time.Time `json:"updatedAt"` // 最近变更
}

// DistributionRecord 是向某厂下发过的一份修订，不含正文。
type DistributionRecord struct {
	ID            uuid.UUID  `json:"id"`            // 记录身份
	AssetID       uuid.UUID  `json:"assetId"`       // 根资产
	Revision      int64      `json:"revision"`      // 下发修订
	FactoryID     uuid.UUID  `json:"factoryId"`     // 目标工厂
	Kind          string     `json:"kind"`          // process / project
	ClosureDigest []byte     `json:"closureDigest"` // 整包摘要
	Members       []AssetDep `json:"members"`       // 成员身份+修订+摘要
	CreatedAt     time.Time  `json:"createdAt"`     // 首次下发时间
}

type distGrantRow struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"` // 授权身份
	AssetID   uuid.UUID `gorm:"type:uuid;not null"`   // 平台级资产
	FactoryID uuid.UUID `gorm:"type:uuid;not null"`   // 工厂
	Active    bool      `gorm:"not null"`             // 是否有效
	CreatedAt time.Time `gorm:"not null"`             // 授权时间
	UpdatedAt time.Time `gorm:"not null"`             // 最近变更
}

func (distGrantRow) TableName() string { return "distribution_grants" }

type distRecordRow struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey"` // 记录身份
	AssetID       uuid.UUID `gorm:"type:uuid;not null"`   // 根资产
	Revision      int64     `gorm:"not null"`             // 修订
	FactoryID     uuid.UUID `gorm:"type:uuid;not null"`   // 工厂
	Kind          string    `gorm:"not null"`             // process / project
	ClosureDigest []byte    `gorm:"type:bytea;not null"`  // 整包摘要
	Members       []byte    `gorm:"type:jsonb;not null"`  // 成员 JSON
	CreatedAt     time.Time `gorm:"not null"`             // 首次下发
}

func (distRecordRow) TableName() string { return "distribution_records" }
