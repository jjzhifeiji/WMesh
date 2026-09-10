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
