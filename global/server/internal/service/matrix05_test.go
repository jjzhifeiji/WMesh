// 阶段3第5圈：WAN 不得代建厂级、不得读厂内正文（5.2、7.2）。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
)

// 验云端不得代建厂级，也不得读厂内正文。
func testWANAssetAuthorship(t *testing.T, run func(string, func(*testing.T))) {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 登记工厂「厂A」，建不成后面没有厂可授权。
	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 5.2：代建厂级工艺「焊接」应被拒为越权，放行或错类都算没拦住。
	run("5.2", func(t *testing.T) {
		// 代建厂级工艺「焊接」应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.CreateFactoryProcess(ctx, tok, fac.Factory.ID, "焊接", []byte("x")); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：代建厂级工艺「焊接」应被拒为越权。
			t.Fatalf("got %v", err)
		}
	})
	// 7.2：读厂内正文应被拒为越权，放行或错类都算没拦住。
	run("7.2", func(t *testing.T) {
		// 读厂内正文应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.ReadFactoryAssetContent(ctx, tok, fac.Factory.ID, id.New()); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：读厂内正文应被拒为越权。
			t.Fatalf("got %v", err)
		}
	})
}
