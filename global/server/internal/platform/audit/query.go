package audit

import (
	"fmt"
	"strings"
)

// Dump 把审计行收成可检索文本，给验收断言用；不含密码原文。
func Dump(rows []Row) string {
	// 拼成一段文本，方便验收里检索。
	var b strings.Builder
	// 每行按固定栏位展开，空身份也留位。
	for _, r := range rows {
		// 栏位用竖线分开，路径快照放最后。
		fmt.Fprintf(&b, "%s|%v|%v|%v|%v|%s|%s|%s|%s|%s\n",
			r.ID, r.ActorID, r.ClaimedLogin, r.FactoryID, r.OrgUnitID,
			r.Action, r.Target, r.Result, r.TimeSource, string(r.OrgPath))
	}
	// 交回整段，供断言搜索。
	return b.String()
}

// ContainsAny 检查审计文本是否漏出秘密原文。
func ContainsAny(text string, secrets ...string) bool {
	// 逐条看秘密有没有漏进审计文本。
	for _, s := range secrets {
		// 空串不算泄漏，避免误报。
		if s != "" && strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// HasResult 是否有服务端钟记下的指定动作与结果。
func HasResult(rows []Row, action, result string) bool {
	// 只要有一条对上动作、结果和服务器钟即可。
	for _, r := range rows {
		// 本机钟的同名记录不算这一条。
		if r.Action == action && r.Result == result && r.TimeSource == Server {
			return true
		}
	}
	return false
}

// Incomplete 找出缺身份、动作、对象、结果、时间来源或发生时间的行。
func Incomplete(rows []Row) []Row {
	// 攒下缺字段的行，一次交回。
	var bad []Row
	// 逐行看身份、动作、对象、结果、来源和时间。
	for _, r := range rows {
		// 缺任一项或结果、来源不在枚举里就记下。
		if r.ID.String() == "" || r.Action == "" || r.Target == "" ||
			(r.Result != Allow && r.Result != Deny) || (r.TimeSource != Server && r.TimeSource != Local) || r.OccurredAt.IsZero() {
			// 残缺行留给验收指出。
			bad = append(bad, r)
		}
	}
	return bad
}

// LastLoginDeny 取发生时间最近的一次登录拒绝，不依赖列表先后。
func LastLoginDeny(rows []Row) (Row, bool) {
	// 准备接时间最近的那条拒绝。
	var best Row
	// 还没见到登录拒绝。
	ok := false
	// 不依赖列表先后，自己比发生时间。
	for _, r := range rows {
		// 不是登录拒绝就跳过。
		if r.Action != "login" || r.Result != Deny {
			continue
		}
		// 第一条，或比已见到的更晚，就换上。
		if !ok || r.OccurredAt.After(best.OccurredAt) {
			// 记下这条更晚的拒绝。
			best = r
			// 之后可以拿它跟后面的比。
			ok = true
		}
	}
	return best, ok
}
