// Package audit 规定审计形态：谁、对哪厂/哪节点、做什么、允许还是拒绝、时间来源。
// 不得包含口令、激活口令或完整令牌。
package audit

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	Allow  = "allow"  // 允许并落审计
	Deny   = "deny"   // 拒绝并落审计
	Server = "server" // 时间来源固定为服务端时钟
)

// Event 是一条审计。认不出稳定账号时 ActorID 为空，改记 ClaimedLogin。
type Event struct {
	ID           uuid.UUID       // 审计条目稳定身份
	ActorID      *uuid.UUID      // 操作者稳定身份；认证失败且无法识别则为空
	ClaimedLogin *string         // 被声明的登录标识
	FactoryID    *uuid.UUID      // 相关工厂；无则空
	OrgUnitID    *uuid.UUID      // 相关组织节点；WAN 侧通常为空
	OrgPath      json.RawMessage // 当时组织路径快照；无则空
	Action       string          // 做了什么操作
	Target       string          // 作用对象
	Result       string          // allow 或 deny
	TimeSource   string          // 时间来源，本阶段固定 server
	OccurredAt   time.Time       // 服务端记录时间
}
