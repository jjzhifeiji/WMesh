// 默认日常密码与一次性 8 位激活码。
package secret_test

import (
	"sync"
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

// 并发校验口令时排队派生，结果仍然各自正确。
func TestVerifyPasswordConcurrent(t *testing.T) {
	// 先算出一份可存储的哈希，后面都拿它比。
	hash, err := secret.HashPassword("pass-1")
	// 哈希算不出来就停住本用例。
	if err != nil {
		// 没有哈希就没法往下比。
		t.Fatal(err)
	}
	// 收集各协程的结果，最后统一核对。
	var wg sync.WaitGroup
	// 每个协程一格，对错分开记。
	got := make([]bool, 16)
	// 起十六个协程，超出名额的要排队。
	for i := range got {
		// 记下又起了一个协程。
		wg.Add(1)
		// 单数用错口令，双数用对口令。
		go func(i int) {
			// 做完就报到，主协程才能收尾。
			defer wg.Done()
			// 按单双选口令，核对排队后结果不乱。
			pass := "pass-1"
			// 单数换成错口令。
			if i%2 == 1 {
				// 错口令应当比不上。
				pass = "wrong"
			}
			// 校验口令，结果放进自己那一格。
			got[i] = secret.VerifyPassword(hash, pass)
		}(i)
	}
	// 等所有协程都比完。
	wg.Wait()
	// 逐格核对，对口令通过、错口令不通过。
	for i, ok := range got {
		// 结果和单双不符说明排队把结果弄乱了。
		if ok != (i%2 == 0) {
			// 结果和预期不符就停住。
			t.Fatalf("slot %d got %v", i, ok)
		}
	}
}
