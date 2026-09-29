package audit

import (
	"time"

	"github.com/google/uuid"
)

// Row 是 audit_events 落库行，字段与 Event 同义。
type Row struct {
	ID           uuid.UUID  `gorm:"type:uuid;primaryKey"` // 审计条目稳定身份
	ActorID      *uuid.UUID `gorm:"type:uuid"`            // 操作者稳定身份；认不出则为空
	ClaimedLogin *string    // 被声明的登录标识
	FactoryID    *uuid.UUID `gorm:"type:uuid"`  // 所属工厂；本厂库写入时即本厂
	OrgUnitID    *uuid.UUID `gorm:"type:uuid"`  // 相关组织节点；无则空
	OrgPath      []byte     `gorm:"type:jsonb"` // 当时组织路径快照；无则空
	Action       string     `gorm:"not null"`   // 做了什么操作
	Target       string     `gorm:"not null"`   // 作用对象
	Result       string     `gorm:"not null"`   // allow 或 deny
	TimeSource   string     `gorm:"not null"`   // 时间来源：server 或 local
	OccurredAt   time.Time  `gorm:"not null"`   // 服务端记录时间
}

// 固定落在审计表，不跟类型名字走。
func (Row) TableName() string { return "audit_events" }

// RowFrom 把审计事件收成落库行，缺省时间来源和服务端时间。
func RowFrom(e Event) Row {
	// 没写时间来源就记成服务端。
	if e.TimeSource == "" {
		// 把这个值定下来，后面的判断才有依据。
		e.TimeSource = Server
	}
	// 没写发生时间就用现在这一刻。
	if e.OccurredAt.IsZero() {
		// 换成世界时，避免时区不一致。
		e.OccurredAt = time.Now().UTC()
	}
	return Row{
		ID:           e.ID,
		ActorID:      e.ActorID,
		ClaimedLogin: e.ClaimedLogin,
		FactoryID:    e.FactoryID,
		OrgUnitID:    e.OrgUnitID,
		OrgPath:      e.OrgPath,
		Action:       e.Action,
		Target:       e.Target,
		Result:       e.Result,
		TimeSource:   e.TimeSource,
		OccurredAt:   e.OccurredAt,
	}
}
