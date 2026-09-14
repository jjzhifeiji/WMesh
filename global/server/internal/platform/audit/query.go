package audit

import (
	"fmt"
	"strings"
)

// Dump 把审计行收成可检索文本，给验收断言用；不含密码原文。
func Dump(rows []Row) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%s|%v|%v|%v|%v|%s|%s|%s|%s|%s\n",
			r.ID, r.ActorID, r.ClaimedLogin, r.FactoryID, r.OrgUnitID,
			r.Action, r.Target, r.Result, r.TimeSource, string(r.OrgPath))
	}
	return b.String()
}

// ContainsAny 检查审计文本是否漏出秘密原文。
func ContainsAny(text string, secrets ...string) bool {
	for _, s := range secrets {
		if s != "" && strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// HasResult 是否有服务端钟记下的指定动作与结果。
func HasResult(rows []Row, action, result string) bool {
	for _, r := range rows {
		if r.Action == action && r.Result == result && r.TimeSource == Server {
			return true
		}
	}
	return false
}

// Incomplete 找出缺身份、动作、对象、结果、时间来源或发生时间的行。
func Incomplete(rows []Row) []Row {
	var bad []Row
	for _, r := range rows {
		if r.ID.String() == "" || r.Action == "" || r.Target == "" ||
			(r.Result != Allow && r.Result != Deny) || (r.TimeSource != Server && r.TimeSource != Local) || r.OccurredAt.IsZero() {
			bad = append(bad, r)
		}
	}
	return bad
}

// LastLoginDeny 取发生时间最近的一次登录拒绝，不依赖列表先后。
func LastLoginDeny(rows []Row) (Row, bool) {
	var best Row
	ok := false
	for _, r := range rows {
		if r.Action != "login" || r.Result != Deny {
			continue
		}
		if !ok || r.OccurredAt.After(best.OccurredAt) {
			best = r
			ok = true
		}
	}
	return best, ok
}
