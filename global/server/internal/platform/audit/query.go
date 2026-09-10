package audit

import (
	"fmt"
	"strings"
)

func Dump(rows []Row) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%s|%v|%v|%v|%v|%s|%s|%s|%s|%s\n",
			r.ID, r.ActorID, r.ClaimedLogin, r.FactoryID, r.OrgUnitID,
			r.Action, r.Target, r.Result, r.TimeSource, string(r.OrgPath))
	}
	return b.String()
}

func ContainsAny(text string, secrets ...string) bool {
	for _, s := range secrets {
		if s != "" && strings.Contains(text, s) {
			return true
		}
	}
	return false
}

func HasResult(rows []Row, action, result string) bool {
	for _, r := range rows {
		if r.Action == action && r.Result == result && r.TimeSource == Server {
			return true
		}
	}
	return false
}

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

func LastLoginDeny(rows []Row) (Row, bool) {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Action == "login" && rows[i].Result == Deny {
			return rows[i], true
		}
	}
	return Row{}, false
}
