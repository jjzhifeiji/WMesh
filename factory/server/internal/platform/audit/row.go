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

// 来自调用方的字符串落库前截断，免得超长正文灌进审计表。
const (
	maxClaimedLogin = 128 // 被声明登录名最多留的字数
	maxTarget       = 512 // 作用对象最多留的字数
)

// clip 按字截到 n 个，超长末尾加省略号；不切坏多字节字符。
func clip(s string, n int) string {
	// 数到第几个字，按字而不是按字节。
	count := 0
	// 逐字往后走，走满 n 个就在这里切。
	for i := range s {
		// 已经留够 n 个字，后面的换成省略号。
		if count == n {
			// 在字的边界切开，多字节字符不会坏。
			return s[:i] + "…"
		}
		// 再数一个字。
		count++
	}
	return s
}

// RowFrom 把审计事件收成落库行，缺省时间来源和服务端时间。
// 被声明登录名和作用对象按字截断，调用方的原值不改。
func RowFrom(e Event) Row {
	// 被声明登录名另存一份再截，不改调用方指向的值。
	if e.ClaimedLogin != nil {
		// 截到上限，超长末尾带省略号。
		claimed := clip(*e.ClaimedLogin, maxClaimedLogin)
		// 换成新指针，原值保持不动。
		e.ClaimedLogin = &claimed
	}
	// 作用对象也可能来自调用方，同样截断。
	e.Target = clip(e.Target, maxTarget)
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
