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

// 库里粗筛明文靠导出的头和最短长度，必须和信封判定一致。
func TestEnvelopeHeadMatchesIsEnvelope(t *testing.T) {
	// 抽一把新的随机密钥。
	kek, err := contentcrypt.RandomKey()
	// 没能抽一把新的随机密钥就停住本用例。
	if err != nil {
		// 没能抽一把新的随机密钥就停住本用例。
		t.Fatal(err)
	}
	// 封一份空正文，得到最短的真实信封。
	env, err := contentcrypt.Seal(kek, nil, []byte("assets"))
	// 没能用封装钥把正文封进信封就停住本用例。
	if err != nil {
		// 没能用封装钥把正文封进信封就停住本用例。
		t.Fatal(err)
	}
	// 头必须是四字节，且真实信封以它开头、长度够最短线。
	if len(contentcrypt.EnvelopeHead) != 4 || !bytes.HasPrefix(env, []byte(contentcrypt.EnvelopeHead)) || len(env) < contentcrypt.MinEnvelope {
		// 导出常量和信封格式对不上，库里粗筛会漏或多。
		t.Fatalf("head %q min %d env %d", contentcrypt.EnvelopeHead, contentcrypt.MinEnvelope, len(env))
	}
	// 拼一段恰好最短长度、带正确头的字节。
	edge := append([]byte(contentcrypt.EnvelopeHead), make([]byte, contentcrypt.MinEnvelope-len(contentcrypt.EnvelopeHead))...)
	// 恰好最短要算信封，少一字节就不算。
	if !contentcrypt.IsEnvelope(edge) || contentcrypt.IsEnvelope(edge[:len(edge)-1]) {
		// 最短线和信封判定不一致就停住。
		t.Fatalf("min envelope edge mismatch")
	}
	// 头换成别的版本字节就不能算信封。
	if contentcrypt.IsEnvelope(append([]byte("WM2\x02"), edge[4:]...)) {
		// 版本不对竟算信封就停住。
		t.Fatalf("wrong version must not look like envelope")
	}
}
