// 工厂停用、启用、删除：WAN 权威，厂端按修订落地。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/service"
)

// 验停用、启用和注销，注销后不能再认领。
func TestFactoryLifecycle(t *testing.T) {
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
	a, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 厂A状态不是在用就停，这一步不能算通过。
	if a.Factory.Status != service.FactoryActive {
		// 不符即停：厂A状态不是在用。
		t.Fatalf("new status %s", a.Factory.Status)
	}
	// 停用工厂，停用失败后面没有可启用的厂。
	disabled, err := h.WAN.DisableFactory(ctx, tok, a.Factory.ID)
	// 停用工厂失败就停，停用失败后面没有可启用的厂。
	if err != nil {
		// 不符即停：停用工厂失败。
		t.Fatal(err)
	}
	// 停用后的厂状态不是停用就停，这一步不能算通过。
	if disabled.Status != service.FactoryDisabled || disabled.LifecycleRevision != 1 {
		// 不符即停：停用后的厂状态不是停用。
		t.Fatalf("disable %+v", disabled)
	}
	// 用建厂码认领应被拒为厂已停用，放行或错类都算没拦住。
	if _, err := h.WAN.OfferEnroll(ctx, a.EnrollmentToken); !errors.Is(err, domain.ErrFactoryDisabled) {
		// 不符即停：用建厂码认领应被拒为厂已停用。
		t.Fatalf("enroll while disabled: %v", err)
	}
	// 启用工厂，启用失败状态就还停着。
	enabled, err := h.WAN.EnableFactory(ctx, tok, a.Factory.ID)
	// 启用工厂失败就停，启用失败状态就还停着。
	if err != nil {
		// 不符即停：启用工厂失败。
		t.Fatal(err)
	}
	// 启用后的厂状态不是在用就停，这一步不能算通过。
	if enabled.Status != service.FactoryActive || enabled.LifecycleRevision != 2 {
		// 不符即停：启用后的厂状态不是在用。
		t.Fatalf("enable %+v", enabled)
	}
	// 用建厂码认领，认领失败厂就没接上云。
	offer, err := h.WAN.OfferEnroll(ctx, a.EnrollmentToken)
	// 用建厂码认领失败就停，认领失败厂就没接上云。
	if err != nil {
		// 不符即停：用建厂码认领失败。
		t.Fatal(err)
	}
	// 生成一对密钥，认领和改绑都用这把公钥。
	pub, _, err := nodekey.Generate()
	// 生成一对密钥失败就停，没有公钥认领就绑不成。
	if err != nil {
		// 不符即停：生成一对密钥失败。
		t.Fatal(err)
	}
	// 确认厂钥失败就停，厂钥确认失败就不能绑死。
	if err := h.WAN.ConfirmEnroll(ctx, offer.FactoryID, pub); err != nil {
		// 不符即停：确认厂钥失败。
		t.Fatal(err)
	}
	// 核对该厂公钥失败就停，公钥核对失败通道状态不明。
	if err := h.WAN.RequireFactoryKey(ctx, a.Factory.ID); err != nil {
		// 不符即停：核对该厂公钥失败。
		t.Fatal(err)
	}
	// 停用工厂失败就停，停用失败后面没有可启用的厂。
	if _, err := h.WAN.DisableFactory(ctx, tok, a.Factory.ID); err != nil {
		// 不符即停：停用工厂失败。
		t.Fatal(err)
	}
	// 核对该厂公钥失败就停，公钥核对失败通道状态不明。
	if err := h.WAN.RequireFactoryKey(ctx, a.Factory.ID); err != nil {
		// 不符即停：核对该厂公钥失败。
		t.Fatalf("key while disabled: %v", err)
	}
	// 注销工厂，注销失败名录里还会看见。
	retired, err := h.WAN.DeleteFactory(ctx, tok, a.Factory.ID)
	// 注销工厂失败或不该是空的就停，不能当通过。
	if err != nil || retired == nil || retired.Status != service.FactoryRetired {
		// 不符即停：注销工厂失败或不该是空的。
		t.Fatalf("retire enrolled: %+v %v", retired, err)
	}
	// 启用工厂应被拒为厂已注销，放行或错类都算没拦住。
	if _, err := h.WAN.EnableFactory(ctx, tok, a.Factory.ID); !errors.Is(err, domain.ErrFactoryRetired) {
		// 不符即停：启用工厂应被拒为厂已注销。
		t.Fatalf("enable retired: %v", err)
	}
	// 核对该厂公钥应被拒为厂已注销，放行或错类都算没拦住。
	if err := h.WAN.RequireFactoryKey(ctx, a.Factory.ID); !errors.Is(err, domain.ErrFactoryRetired) {
		// 不符即停：核对该厂公钥应被拒为厂已注销。
		t.Fatalf("key retired: %v", err)
	}
	// 读工厂名录，名录读不到就无法对厂。
	dir, err := h.WAN.Directory(ctx, tok)
	// 读工厂名录失败就停，名录读不到就无法对厂。
	if err != nil {
		// 不符即停：读工厂名录失败。
		t.Fatal(err)
	}
	// 先当没找到，循环里命中再改成真。
	found := false
	// 逐条查看结果，漏看一条会把结论判错。
	for _, f := range dir.Factories {
		// 条件成立才做这一支，漏进来会算错样本。
		if f.ID == a.Factory.ID {
			// 标成已命中，最后仍为假说明名单里没有。
			found = true
			// 这里状态不是已注销就停，这一步不能算通过。
			if f.Status != service.FactoryRetired {
				// 不符即停：这里状态不是已注销。
				t.Fatalf("listed status %s", f.Status)
			}
		}
	}
	// 查找结果没有出现就停，名单或种子丢了。
	if !found {
		// 不符即停：查找结果没有出现。
		t.Fatal("retired factory missing from directory")
	}

	// 登记工厂「厂B」，建不成后面没有厂可授权。
	b, err := h.WAN.CreateFactory(ctx, tok, "厂B", "sa-b", "超管B")
	// 登记工厂「厂B」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂B」失败。
		t.Fatal(err)
	}
	// 注销工厂，注销失败名录里还会看见。
	gone, err := h.WAN.DeleteFactory(ctx, tok, b.Factory.ID)
	// 注销工厂失败就停，注销失败名录里还会看见。
	if err != nil || gone != nil {
		// 不符即停：注销工厂失败。
		t.Fatalf("delete unclaimed: %+v %v", gone, err)
	}
	// 读工厂名录，名录读不到就无法对厂。
	dir, err = h.WAN.Directory(ctx, tok)
	// 读工厂名录失败就停，名录读不到就无法对厂。
	if err != nil {
		// 不符即停：读工厂名录失败。
		t.Fatal(err)
	}
	// 逐条查看结果，漏看一条会把结论判错。
	for _, f := range dir.Factories {
		// 这里身份没有分开就停，说明没有分成新的一份。
		if f.ID == b.Factory.ID {
			// 不符即停：这里身份没有分开。
			t.Fatal("unclaimed factory still listed")
		}
	}
	// 核对该厂公钥应被拒为厂已注销，放行或错类都算没拦住。
	if err := h.WAN.RequireFactoryKey(ctx, b.Factory.ID); !errors.Is(err, domain.ErrFactoryRetired) {
		// 不符即停：核对该厂公钥应被拒为厂已注销。
		t.Fatalf("key after delete: %v", err)
	}
}
