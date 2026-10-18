// WAN 登录失败总闸：只在失败时扣，关着时不查账号、探不出登录名。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
)

// 连错二十次后，第二十一次即使口令对也被总闸挡下，并留审计。
func TestLoginFailGate(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，每例新建，总闸互不影响。
	h := New(t)
	// 立唯一管理员，后面才有账号可错。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 立管理员失败就停，后面没有账号可登。
		t.Fatalf("bootstrap: %v", err)
	}
	// 连错二十次，正好用光总闸名额。
	for i := range 20 {
		// 错口令应照常回凭证不对。
		if _, err := h.WAN.Login(ctx, "w", "bad"); !errors.Is(err, domain.ErrInvalidCredentials) {
			// 名额未尽就被挡或错类，带上次数停测。
			t.Fatalf("fail %d: %v", i+1, err)
		}
	}
	// 第二十一次即使口令对也应被挡下。
	if _, err := h.WAN.Login(ctx, "w", "wan-secret"); !errors.Is(err, domain.ErrTooManyAttempts) {
		// 总闸没生效，带上错误停测。
		t.Fatalf("21st: %v", err)
	}
	// 读审计，核对被总闸挡下也留了痕。
	rows, err := h.WAN.ListAudit(ctx)
	// 读审计失败就停，读不到就无法核对。
	if err != nil {
		// 不符即停：读审计失败。
		t.Fatal(err)
	}
	// 记下是否找到总闸拒绝。
	found := false
	// 逐行找总闸挡下的登录拒绝。
	for _, row := range rows {
		// 动作、对象、结果都对上才算。
		if row.Action == "login" && row.Target == "wan throttled" && row.Result == audit.Deny {
			// 找到了总闸拒绝审计。
			found = true
		}
	}
	// 没有总闸拒绝审计就停测，认证失败要留痕。
	if !found {
		// 不符即停：总闸拒绝没落审计。
		t.Fatal("throttled login audit missing")
	}
}

// 用不存在的登录名错二十次，同样关上总闸，借它探不出登录名。
func TestLoginFailGateUnknownName(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，每例新建，总闸互不影响。
	h := New(t)
	// 立唯一管理员，最后用它试一次。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 立管理员失败就停，后面没有账号可登。
		t.Fatalf("bootstrap: %v", err)
	}
	// 用不存在的登录名连错二十次。
	for i := range 20 {
		// 不存在也当凭证不对，不泄露账号有无。
		if _, err := h.WAN.Login(ctx, "ghost", "bad"); !errors.Is(err, domain.ErrInvalidCredentials) {
			// 名额未尽就被挡或错类，带上次数停测。
			t.Fatalf("ghost %d: %v", i+1, err)
		}
	}
	// 真账号此时也应被挡，回的与不存在时相同。
	if _, err := h.WAN.Login(ctx, "w", "wan-secret"); !errors.Is(err, domain.ErrTooManyAttempts) {
		// 总闸没对所有登录名一致生效，就停测。
		t.Fatalf("real after ghost: %v", err)
	}
	// 不存在的登录名也应被挡，回同一个错。
	if _, err := h.WAN.Login(ctx, "ghost", "bad"); !errors.Is(err, domain.ErrTooManyAttempts) {
		// 存在与否回的不一样，能被拿来探测。
		t.Fatalf("ghost after gate: %v", err)
	}
}

// 成功登录不扣总闸；错十九次后口令对仍能登录。
func TestLoginSuccessNotCounted(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，每例新建，总闸互不影响。
	h := New(t)
	// 立唯一管理员。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 立管理员失败就停，后面没有账号可登。
		t.Fatalf("bootstrap: %v", err)
	}
	// 连续成功二十五次，超过总闸名额。
	for i := range 25 {
		// 成功不该被总闸挡。
		if _, err := h.WAN.Login(ctx, "w", "wan-secret"); err != nil {
			// 成功也被计次，带上次数停测。
			t.Fatalf("success %d: %v", i+1, err)
		}
	}
	// 再错十九次，总闸仍留一个名额。
	for i := range 19 {
		// 错口令应照常回凭证不对。
		if _, err := h.WAN.Login(ctx, "w", "bad"); !errors.Is(err, domain.ErrInvalidCredentials) {
			// 名额未尽就被挡或错类，带上次数停测。
			t.Fatalf("fail %d: %v", i+1, err)
		}
	}
	// 口令对应能登录，没到总闸上限。
	if _, err := h.WAN.Login(ctx, "w", "wan-secret"); err != nil {
		// 未到上限就被挡，带上错误停测。
		t.Fatalf("after 19 fails: %v", err)
	}
}
