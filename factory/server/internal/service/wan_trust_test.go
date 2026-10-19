// 本厂钉 WAN 下行指令验签公钥：首钉、同钥幂等、换钥拒绝、坏钥拒绝，审计不含钥原文。
package service_test

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
)

// 钉钥矩阵：没钉读出空；首钉成功、同钥幂等；换钥和坏长度拒绝；审计记允许和拒绝。
func TestPinWANKey(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面。
	ctx := context.Background()
	// 起一套测试库和厂服务。
	h := New(t)
	// 建厂，失败则本例无法开始。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停。
	if err != nil {
		// 带上原因停掉本例。
		t.Fatal(err)
	}
	// 还没钉时读出空，不算错。
	if key, err := fac.Node.WANKey(ctx); err != nil || key != nil {
		// 没钉却读出钥或报错就失败。
		t.Fatalf("empty %x %v", key, err)
	}
	// 生成 WAN 公钥。
	pub, _, err := nodekey.Generate()
	// 生不出钥就停。
	if err != nil {
		// 带上原因停掉本例。
		t.Fatal(err)
	}
	// 另一把，用作换钥。
	pub2, _, err := nodekey.Generate()
	// 生不出钥就停。
	if err != nil {
		// 带上原因停掉本例。
		t.Fatal(err)
	}
	// 首次钉住要成功。
	if err := fac.Node.PinWANKey(ctx, pub); err != nil {
		// 首钉失败就停。
		t.Fatalf("pin %v", err)
	}
	// 同一把再钉一次幂等。
	if err := fac.Node.PinWANKey(ctx, pub); err != nil {
		// 同钥被拒就停。
		t.Fatalf("pin again %v", err)
	}
	// 换一把要拒绝。
	if err := fac.Node.PinWANKey(ctx, pub2); !errors.Is(err, domain.ErrIntegrity) {
		// 换钥被接受就停。
		t.Fatalf("pin other %v", err)
	}
	// 长度不对的钥要拒绝。
	if err := fac.Node.PinWANKey(ctx, []byte("short")); !errors.Is(err, domain.ErrInvalidKey) {
		// 坏钥被接受就停。
		t.Fatalf("pin short %v", err)
	}
	// 读回的应是首钉那把。
	if key, err := fac.Node.WANKey(ctx); err != nil || string(key) != string(pub) {
		// 钉钥被换掉就停。
		t.Fatalf("read %x %v", key, err)
	}
	// 读审计，核对允许和拒绝。
	rows, err := fac.ListAudit(ctx)
	// 读不出审计就停。
	if err != nil {
		// 带上原因停掉本例。
		t.Fatal(err)
	}
	// 数钉钥的允许和拒绝条数。
	allow, deny := 0, 0
	// 逐条看审计。
	for _, r := range rows {
		// 只看钉钥这一类。
		if r.Action != "pin_wan_key" {
			continue
		}
		// 目标只能是本厂身份，不含钥原文。
		if r.Target != seed.ID.String() || strings.Contains(r.Target, hex.EncodeToString(pub)) {
			// 目标不对就停。
			t.Fatalf("target %q", r.Target)
		}
		// 按结果分别计数。
		switch r.Result {
		// 允许记一条。
		case audit.Allow:
			// 多记一条允许。
			allow++
		// 拒绝记一条。
		case audit.Deny:
			// 多记一条拒绝。
			deny++
		}
	}
	// 首钉和同钥各一条允许，换钥一条拒绝。
	if allow != 2 || deny != 1 {
		// 条数不对就停。
		t.Fatalf("audit allow %d deny %d", allow, deny)
	}
}
