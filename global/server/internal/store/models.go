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
