// 修订升高只组这一条，不把该厂当前全部可用条再组一遍。
package service_test

import (
	"context"
	"testing"
)

// 修订升高只组这一份，不把该厂可用的全组一遍。
func TestPackAssetForFactoryOnlyThatAsset(t *testing.T) {
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
	created, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 新建平台工艺「焊A」，建不成后面没有工艺号可对。
	a, err := h.WAN.CreatePlatformProcess(ctx, tok, "焊A", []byte("body-a"))
	// 新建平台工艺「焊A」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「焊A」失败。
		t.Fatal(err)
	}
	// 新建平台工艺「焊B」，建不成后面没有工艺号可对。
	b, err := h.WAN.CreatePlatformProcess(ctx, tok, "焊B", []byte("body-b"))
	// 新建平台工艺「焊B」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「焊B」失败。
		t.Fatal(err)
	}
	// 发布资产失败就停，发不出去后面不能当可用。
	if a, err = h.WAN.PublishPlatformAsset(ctx, tok, a.ID, a.Revision); err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 发布资产失败就停，发不出去后面不能当可用。
	if b, err = h.WAN.PublishPlatformAsset(ctx, tok, b.ID, b.Revision); err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 组该厂全部可用，整厂组包失败范围就说不清。
	all, err := h.WAN.PackAvailableForFactory(ctx, created.Factory.ID)
	// 组该厂全部可用失败就停，整厂组包失败范围就说不清。
	if err != nil {
		// 不符即停：组该厂全部可用失败。
		t.Fatal(err)
	}
	// 这里条数不是2就停，这一步不能算通过。
	if len(all) != 2 {
		// 不符即停：这里条数不是2。
		t.Fatalf("pack all %d", len(all))
	}
	// 按这份资产组包，组不出包下发就没有内容。
	snap, err := h.WAN.PackAssetForFactory(ctx, a.ID, created.Factory.ID)
	// 按这份资产组包失败就停，组不出包下发就没有内容。
	if err != nil {
		// 不符即停：按这份资产组包失败。
		t.Fatal(err)
	}
	// 组包修订没有跟上就停，这一步不能算通过。
	if snap.AssetID != a.ID || snap.Revision != a.Revision {
		// 不符即停：组包修订没有跟上。
		t.Fatalf("pack one: %+v", snap)
	}
	// 改名「焊A2」，改名失败就证明不了号还在。
	renamed, err := h.WAN.RenamePlatformAsset(ctx, tok, a.ID, a.Revision, "焊A2")
	// 改名「焊A2」失败就停，改名失败就证明不了号还在。
	if err != nil {
		// 不符即停：改名「焊A2」失败。
		t.Fatal(err)
	}
	// 按这份资产组包，组不出包下发就没有内容。
	snap, err = h.WAN.PackAssetForFactory(ctx, a.ID, created.Factory.ID)
	// 按这份资产组包失败就停，组不出包下发就没有内容。
	if err != nil {
		// 不符即停：按这份资产组包失败。
		t.Fatal(err)
	}
	// 组包修订没有跟上或名称不是焊A2就停，不能当通过。
	if snap.AssetID != a.ID || snap.Revision != renamed.Revision || snap.Members[0].Name != "焊A2" {
		// 不符即停：组包修订没有跟上或名称不是焊A2。
		t.Fatalf("renamed pack: %+v", snap)
	}
	// 查是否已下发，查不到是否下发就分不清。
	hasB, err := h.WAN.HasDistributedTo(ctx, b.ID, created.Factory.ID)
	// 查是否已下发失败就停，查不到是否下发就分不清。
	if err != nil || !hasB {
		// 不符即停：查是否已下发失败。
		t.Fatalf("other still granted: %v", err)
	}
}
