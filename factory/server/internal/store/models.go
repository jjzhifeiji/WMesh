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

	RoleFactorySuperAdmin = "factory_super_admin" // 工厂超级管理员，只能挂 Factory 作用域
	RoleOrgAdmin          = "org_admin"           // 组织管理员，只管本节点及当前子树
	RoleOrgLead           = "org_lead"            // 组织负责人，子树只读
	RoleProcessEngineer   = "process_engineer"    // 工艺工程师，可 Factory 或节点作用域
	RoleOperator          = "operator"            // 操作员，可产生运行事实
	RoleAuditor           = "auditor"             // 审计员，只读，不能改业务

	ScopeFactory = "factory"  // 覆盖本厂及当时全部组织节点
	ScopeOrgUnit = "org_unit" // 只覆盖该节点及当时子树
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
