package audit

import (
	"fmt"
	"strings"
)

// Dump 把审计行收成可检索文本，给验收断言用；不含密码原文。
func Dump(rows []Row) string {
	// 准备拼文本，避免来回分配。
	var b strings.Builder
	// 逐行看审计，空列表就什么都不做。
	for _, r := range rows {
		// 做完这一步，再交给后面。
		fmt.Fprintf(&b, "%s|%v|%v|%v|%v|%s|%s|%s|%s|%s\n",
			r.ID, r.ActorID, r.ClaimedLogin, r.FactoryID, r.OrgUnitID,
			r.Action, r.Target, r.Result, r.TimeSource, string(r.OrgPath))
	}
	// 交回收成普通文本再拿去比较或拼接的结果。
	return b.String()
}

// ContainsAny 检查审计文本是否漏出秘密原文。
func ContainsAny(text string, secrets ...string) bool {
	// 逐段核对有没有把原文漏出去。
	for _, s := range secrets {
		// 非空秘密出现在文本里就算泄露。
		if s != "" && strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// HasResult 是否有服务端钟记下的指定动作与结果。
func HasResult(rows []Row, action, result string) bool {
	// 逐行看审计，空列表就什么都不做。
	for _, r := range rows {
		// 对上了才走这一路，其余分开处理。
		if r.Action == action && r.Result == result && r.TimeSource == Server {
			return true
		}
	}
	return false
}

// Incomplete 找出缺身份、动作、对象、结果、时间来源或发生时间的行。
func Incomplete(rows []Row) []Row {
	// 准备收集不合格的审计行。
	var bad []Row
	// 逐行看审计，空列表就什么都不做。
	for _, r := range rows {
		// 是空的就改用默认，或按没有处理。
		if r.ID.String() == "" || r.Action == "" || r.Target == "" ||
			(r.Result != Allow && r.Result != Deny) || (r.TimeSource != Server && r.TimeSource != Local) || r.OccurredAt.IsZero() {
			// 把这一段接进结果，顺序要保持住。
			bad = append(bad, r)
		}
	}
	return bad
}

// LastLoginDeny 取发生时间最近的一次登录拒绝，不依赖列表先后。
func LastLoginDeny(rows []Row) (Row, bool) {
	// 准备记下目前最晚的那一条。
	var best Row
	// 先当还没找到，找到再改成命中。
	ok := false
	// 逐行看审计，空列表就什么都不做。
	for _, r := range rows {
		// 不是拒绝就不算这次要找的登录失败。
		if r.Action != "login" || r.Result != Deny {
			continue
		}
		// 没有命中就走另一路，不用零值冒充有值。
		if !ok || r.OccurredAt.After(best.OccurredAt) {
			// 定下目前最晚的一条。
			best = r
			// 先当还没找到，找到再改成命中。
			ok = true
		}
	}
	return best, ok
}
