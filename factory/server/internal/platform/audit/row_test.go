// 审计落库前截断调用方传来的登录名和作用对象。
package audit_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"wmesh/factory/internal/platform/audit"
)

// 超长登录名和作用对象按字截断，末尾带省略号。
func TestRowFromClipsLongStrings(t *testing.T) {
	// 两百个字母的登录名，超过一百二十八字上限。
	login := strings.Repeat("a", 200)
	// 记下原值，核对调用方指向的值没被改。
	orig := login
	// 六百个汉字的作用对象，超过五百一十二字上限。
	target := strings.Repeat("焊", 600)
	// 收成落库行，截断在这里发生。
	row := audit.RowFrom(audit.Event{ClaimedLogin: &login, Action: "login", Target: target, Result: audit.Deny})
	// 登录名应是一百二十八字加省略号。
	if got := *row.ClaimedLogin; utf8.RuneCountInString(got) != 129 || !strings.HasSuffix(got, "…") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("claimed %d %q", utf8.RuneCountInString(got), got)
	}
	// 作用对象应是五百一十二字加省略号。
	if got := row.Target; utf8.RuneCountInString(got) != 513 || !strings.HasSuffix(got, "…") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("target %d", utf8.RuneCountInString(got))
	}
	// 汉字截断后仍应是合法文字，不能切坏。
	if !utf8.ValidString(row.Target) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("target not valid utf8")
	}
	// 调用方的登录名不应被改。
	if login != orig {
		// 原值被改说明截断动了调用方的指针。
		t.Fatalf("caller value changed")
	}
}

// 短字符串原样保留，没有登录名时仍为空。
func TestRowFromKeepsShortStrings(t *testing.T) {
	// 普通长度的登录名，不该被动。
	login := "sa"
	// 收成落库行，短值应原样留下。
	row := audit.RowFrom(audit.Event{ClaimedLogin: &login, Action: "login", Target: "焊工A", Result: audit.Allow})
	// 登录名或作用对象变了说明误截。
	if *row.ClaimedLogin != "sa" || row.Target != "焊工A" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("short changed %q %q", *row.ClaimedLogin, row.Target)
	}
	// 没给登录名时收成落库行。
	row = audit.RowFrom(audit.Event{Action: "logout", Target: "f", Result: audit.Allow})
	// 空登录名应保持为空，不能凭空造出来。
	if row.ClaimedLogin != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("nil claimed became %q", *row.ClaimedLogin)
	}
	// 恰好五百一十二字的作用对象不该加省略号。
	exact := strings.Repeat("焊", 512)
	// 收成落库行，刚好到上限的不截。
	row = audit.RowFrom(audit.Event{Action: "x", Target: exact, Result: audit.Allow})
	// 被截说明边界算错了一位。
	if row.Target != exact {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("exact target clipped")
	}
}
