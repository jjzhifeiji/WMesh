// 默认日常密码与一次性 8 位激活码。
package secret_test

import (
	"testing"
	"unicode"

	"wmesh/factory/internal/platform/secret"
)

func TestDefaultPersonPassword(t *testing.T) {
	if got := secret.DefaultPersonPassword("op1"); got != "op1123456" {
		t.Fatalf("got %q", got)
	}
}

func TestActivationCodeIsEightDigits(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		code, err := secret.ActivationCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 8 {
			t.Fatalf("len %d code %q", len(code), code)
		}
		for _, r := range code {
			if r < '0' || r > '9' || !unicode.IsDigit(r) {
				t.Fatalf("non-digit %q", code)
			}
		}
		seen[code] = true
	}
	if len(seen) < 2 {
		t.Fatalf("not random: %v", seen)
	}
}
