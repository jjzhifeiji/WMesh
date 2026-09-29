// 默认日常密码与一次性 8 位激活码。
package secret_test

import (
	"testing"
	"unicode"

	"wmesh/factory/internal/platform/secret"
)

// 默认日常密码应是登录名接上固定后缀。
func TestDefaultPersonPassword(t *testing.T) {
	// 结果和预期不符就进入失败。
	if got := secret.DefaultPersonPassword("op1"); got != "op1123456" {
		// 结果和预期不符就停住。
		t.Fatalf("got %q", got)
	}
}

// 激活码应是八位数字，方便当面念。
func TestActivationCodeIsEightDigits(t *testing.T) {
	// 用空表记下见过的，防止重复。
	seen := map[string]bool{}
	// 按下标扫过去，直到越界或提前结束。
	for i := 0; i < 32; i++ {
		// 抽一个方便当面念的八位数字。
		code, err := secret.ActivationCode()
		// 没能抽一个方便当面念的八位数字就停住本用例。
		if err != nil {
			// 没能抽一个方便当面念的八位数字就停住本用例。
			t.Fatal(err)
		}
		// 结果和预期不符就进入失败。
		if len(code) != 8 {
			// 长度不对就停住，再继续处理。
			t.Fatalf("len %d code %q", len(code), code)
		}
		// 逐项处理，空的就不进入循环。
		for _, r := range code {
			// 结果和预期不符就进入失败。
			if r < '0' || r > '9' || !unicode.IsDigit(r) {
				// 不是纯数字却被收成了数字。
				t.Fatalf("non-digit %q", code)
			}
		}
		// 定下已经见过，再交给后面。
		seen[code] = true
	}
	// 结果和预期不符就进入失败。
	if len(seen) < 2 {
		// 两次结果不该完全一样。
		t.Fatalf("not random: %v", seen)
	}
}
