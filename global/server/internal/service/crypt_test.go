// WAN 库平台级仍明文；同一把租约钥续期；过站信封到站解开后明文入库。
package service_test

import (
	"bytes"
	"context"
	"testing"

	"wmesh/global/internal/platform/contentcrypt"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

// 验云端正文仍是明文，同一租约钥可以续。
func TestContentLeaseAndWANPlaintext(t *testing.T) {
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
	// 准备正文样本，后面入库和比对都用它。
	body := []byte("platform-weld-secret")
	// 新建平台工艺「焊接」，建不成后面没有工艺号可对。
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "焊接", body)
	// 新建平台工艺「焊接」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「焊接」失败。
		t.Fatal(err)
	}
	// 按身份读库中正文，库中正文读不到就分不清明密文。
	row, err := h.WAN.Store().AssetByID(ctx, proc.ID)
	// 按身份读库中正文失败或不该仍是密文就停，不能当通过。
	if err != nil || contentcrypt.IsEnvelope(row.Content) {
		// 不符即停：按身份读库中正文失败或不该仍是密文。
		t.Fatalf("wan must store plaintext: %v", err)
	}

	// 登记工厂「厂A」，建不成后面没有厂可授权。
	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 签发正文租约，租约签不出就解不开信封。
	lease, err := h.WAN.IssueContentLease(ctx, fac.Factory.ID)
	// 签发正文租约失败或租约钥长度不对就停，不能当通过。
	if err != nil || len(lease.Key) != contentcrypt.KeySize {
		// 不符即停：签发正文租约失败或租约钥长度不对。
		t.Fatalf("lease %v", err)
	}
	// 签发正文租约，租约签不出就解不开信封。
	again, err := h.WAN.IssueContentLease(ctx, fac.Factory.ID)
	// 签发正文租约失败或正文不一致就停，不能当通过。
	if err != nil || !bytes.Equal(lease.Key, again.Key) {
		// 不符即停：签发正文租约失败或正文不一致。
		t.Fatalf("lease rotated")
	}

	// 新造一个身份，后面用来区分是不是同一份。
	srcID := id.New()
	// 准备正文样本，后面入库和比对都用它。
	plain := []byte("promote-plain")
	// 封成过站信封，封不上就没法验到站解开。
	env, err := contentcrypt.Seal(lease.Key, plain, contentcrypt.TransitAAD(fac.Factory.ID, srcID, 1))
	// 封成过站信封失败就停，没有结果不能继续验。
	if err != nil {
		// 不符即停：封成过站信封失败。
		t.Fatal(err)
	}
	// 升档，升不上去档位就断了。
	promoted, err := h.WAN.PromoteFromSnapshot(ctx, tok, global.AssetSnapshot{
		SourceID: srcID, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
		Kind: global.KindProcess, Name: "升档焊", Content: env, Digest: digest.Sum(plain),
		Copyable: true, Status: global.AssetAvailable,
	})
	// 升档失败就停，升不上去档位就断了。
	if err != nil {
		// 不符即停：升档失败。
		t.Fatal(err)
	}
	// 读平台正文，读不到就无法比对。
	got, err := h.WAN.ReadPlatformAssetContent(ctx, tok, promoted.ID)
	// 读平台正文失败或正文不一致就停，不能当通过。
	if err != nil || !bytes.Equal(got, plain) || contentcrypt.IsEnvelope(got) {
		// 不符即停：读平台正文失败或正文不一致。
		t.Fatalf("promoted %q %v", got, err)
	}
}
