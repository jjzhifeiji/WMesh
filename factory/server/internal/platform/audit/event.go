// Package audit 规定审计形态：谁、对哪厂/哪节点、做什么、允许还是拒绝、时间来源。
// 不得包含口令、激活口令或完整令牌。
package audit

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	Allow  = "allow"
	Deny   = "deny"
	Server = "server"
)

// Event 是一条审计。认不出稳定账号时 ActorID 为空，改记 ClaimedLogin。
type Event struct {
	ID           uuid.UUID
	ActorID      *uuid.UUID // 操作者稳定身份；认证失败且无法识别则为空
	ClaimedLogin *string    // 被声明的登录标识
	FactoryID    *uuid.UUID
	OrgUnitID    *uuid.UUID
	OrgPath      json.RawMessage // 当时组织路径；无则空
	Action       string
	Target       string
	Result       string // allow 或 deny
	TimeSource   string // 本阶段固定 server
	OccurredAt   time.Time
}
