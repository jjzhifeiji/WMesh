// 正文信封：同一把钥可解开，错钥或错附加数据拒绝；明文不当信封。
package contentcrypt_test

import (
	"bytes"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/domain"
)

// 封上再解开应回到原文，钥不对要失败。
func TestSealRoundtrip(t *testing.T) {
	// 抽一把新的随机密钥。
	kek, err := contentcrypt.RandomKey()
	// 没能抽一把新的随机密钥就停住本用例。
	if err != nil {
		// 没能抽一把新的随机密钥就停住本用例。
		t.Fatal(err)
	}
	// 解析，再交给后面，再交给后面。
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	// 把信封绑到这一行的身份和修订。
	aad := contentcrypt.AssetAAD(id, 2, "assets")
	// 准备一份测试或签名用的字节。
	plain := []byte(`{"current":200}`)
	// 用封装钥把正文封进信封。
	env, err := contentcrypt.Seal(kek, plain, aad)
	// 没能用封装钥把正文封进信封就停住本用例。
	if err != nil {
		// 没能用封装钥把正文封进信封就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if !contentcrypt.IsEnvelope(env) || bytes.Contains(env, plain) {
		// 明文不该被看成信封。
		t.Fatalf("envelope looks like plaintext")
	}
	// 用封装钥把正文封进信封。
	env2, err := contentcrypt.Seal(kek, plain, aad)
	// 没能用封装钥把正文封进信封就停住本用例。
	if err != nil {
		// 没能用封装钥把正文封进信封就停住本用例。
		t.Fatal(err)
	}
	// 结果和预期不符就进入失败。
	if bytes.Equal(env, env2) {
		// 同一把数据钥被重复使用就停住。
		t.Fatal("same DEK reused")
	}
	// 打开这一份资源，再交给后面。
	got, err := contentcrypt.Open(kek, env, aad)
	// 出错或结果对不上就停住本用例。
	if err != nil || !bytes.Equal(got, plain) {
		// 打开失败就停住本用例。
		t.Fatalf("open %q %v", got, err)
	}
	// 抽一把新的随机密钥。
	other, err := contentcrypt.RandomKey()
	// 没能抽一把新的随机密钥就停住本用例。
	if err != nil {
		// 没能抽一把新的随机密钥就停住本用例。
		t.Fatal(err)
	}
	// 没能打开这一份资源就停住本用例。
	if _, err := contentcrypt.Open(other, env, aad); err != domain.ErrIntegrity {
		// 钥不对竟能解开就停住。
		t.Fatalf("wrong kek: %v", err)
	}
	// 没能把信封绑到这一行的身份和修订就停住本用例。
	if _, err := contentcrypt.Open(kek, env, contentcrypt.AssetAAD(id, 3, "assets")); err != domain.ErrIntegrity {
		// 绑定不对竟能解开就停住。
		t.Fatalf("wrong aad: %v", err)
	}
	// 打开这一份资源，再交给后面。
	legacy, err := contentcrypt.Open(kek, plain, aad)
	// 出错或结果对不上就停住本用例。
	if err != nil || !bytes.Equal(legacy, plain) {
		// 旧库基线的结果不对。
		t.Fatalf("legacy %q %v", legacy, err)
	}
	// 结果和预期不符就进入失败。
	if contentcrypt.IsEnvelope(plain) || contentcrypt.IsEnvelope([]byte("WM2")) {
		// 过短的内容不该像信封。
		t.Fatal("short json must not look like envelope")
	}
}
