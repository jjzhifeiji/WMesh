// 审计落库前截断：超长按字切、带省略号，短的原样，调用方原值不改。
package audit

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// 超长的被声明登录名和作用对象截到上限，末尾带省略号。
func TestRowFromClipsLongStrings(t *testing.T) {
	// 登录名两百字，超过一百二十八。
	login := strings.Repeat("a", 200)
	// 作用对象一千字，超过五百一十二。
	target := strings.Repeat("b", 1000)
	// 收成落库行，应被截断。
	row := RowFrom(Event{ClaimedLogin: &login, Action: "login", Target: target, Result: Deny})
	// 登录名应留一百二十八字再加省略号。
	if *row.ClaimedLogin != strings.Repeat("a", maxClaimedLogin)+"…" {
		// 截断结果不对，带上长度停测。
		t.Fatalf("claimed len %d", utf8.RuneCountInString(*row.ClaimedLogin))
	}
	// 作用对象应留五百一十二字再加省略号。
	if row.Target != strings.Repeat("b", maxTarget)+"…" {
		// 截断结果不对，带上长度停测。
		t.Fatalf("target len %d", utf8.RuneCountInString(row.Target))
	}
	// 调用方的登录名不能被改。
	if login != strings.Repeat("a", 200) {
		// 原值被改了，调用方会拿到残缺值。
		t.Fatal("caller login mutated")
	}
}

// 汉字按字切，截完仍是合法 UTF-8；短字符串原样保留。
func TestRowFromClipsRunes(t *testing.T) {
	// 两百个汉字，每字三字节。
	login := strings.Repeat("焊", 200)
	// 收成落库行，应按字截断。
	row := RowFrom(Event{ClaimedLogin: &login, Action: "login", Target: strings.Repeat("缝", 600), Result: Deny})
	// 截完必须仍是合法 UTF-8，且字数是上限加省略号。
	if !utf8.ValidString(*row.ClaimedLogin) || utf8.RuneCountInString(*row.ClaimedLogin) != maxClaimedLogin+1 {
		// 多字节字符被切坏或字数不对，就停测。
		t.Fatalf("claimed %q", *row.ClaimedLogin)
	}
	// 作用对象同样按字截断。
	if !utf8.ValidString(row.Target) || utf8.RuneCountInString(row.Target) != maxTarget+1 {
		// 多字节字符被切坏或字数不对，就停测。
		t.Fatalf("target runes %d", utf8.RuneCountInString(row.Target))
	}
	// 正好一百二十八字的登录名不截。
	exact := strings.Repeat("焊", maxClaimedLogin)
	// 收成落库行，短字符串应原样。
	short := RowFrom(Event{ClaimedLogin: &exact, Action: "login", Target: "wan", Result: Allow})
	// 不超长却被改了就停测。
	if *short.ClaimedLogin != exact || short.Target != "wan" {
		// 短字符串被误截，就停测。
		t.Fatalf("short %q %q", *short.ClaimedLogin, short.Target)
	}
	// 没有被声明登录名时保持为空。
	if RowFrom(Event{Action: "logout", Target: "wan", Result: Deny}).ClaimedLogin != nil {
		// 空指针被换成了空串，就停测。
		t.Fatal("nil claimed became non-nil")
	}
}
