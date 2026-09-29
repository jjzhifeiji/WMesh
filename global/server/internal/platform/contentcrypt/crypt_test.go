// WAN 侧正文信封：同一把钥可解开，错钥拒绝；明文不当信封。
package contentcrypt_test

import (
	"bytes"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/contentcrypt"
	"wmesh/global/internal/platform/domain"
)

// 同一把钥应能还原，错钥和明文都不得当成功信封。
func TestSealRoundtrip(t *testing.T) {
	// 先发一把用来封信封的主密钥。
	kek, err := contentcrypt.RandomKey()
	// 发不出钥则本用例不成立。
	if err != nil {
		// 发钥失败就中止本用例。
		t.Fatal(err)
	}
	// 固定一行身份，附加数据才稳定。
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	// 把信封绑到这一行的修订和表。
	aad := contentcrypt.AssetAAD(id, 2, "assets")
	// 用一小段正文当明文。
	plain := []byte(`{"current":200}`)
	// 把这段明文封成信封。
	env, err := contentcrypt.Seal(kek, plain, aad)
	// 封失败则后面无从核对。
	if err != nil {
		// 封装失败就中止本用例。
		t.Fatal(err)
	}
	// 必须像信封，且正文不得明文出现。
	if !contentcrypt.IsEnvelope(env) || bytes.Contains(env, plain) {
		// 不像密文就中止本用例。
		t.Fatalf("envelope looks like plaintext")
	}
	// 同一把钥应还原出原文。
	got, err := contentcrypt.Open(kek, env, aad)
	// 还原结果必须和明文一致。
	if err != nil || !bytes.Equal(got, plain) {
		// 对不上就中止并带上结果。
		t.Fatalf("open %q %v", got, err)
	}
	// 再发一把不同的钥。
	other, err := contentcrypt.RandomKey()
	// 发不出第二把则错钥测不了。
	if err != nil {
		// 发钥失败就中止本用例。
		t.Fatal(err)
	}
	// 错钥必须按损坏拒绝。
	if _, err := contentcrypt.Open(other, env, aad); err != domain.ErrIntegrity {
		// 没拒绝就中止并带上错误。
		t.Fatalf("wrong kek: %v", err)
	}
	// 纯正文和过短魔数都不得当信封。
	if contentcrypt.IsEnvelope([]byte(`{"current":200}`)) || contentcrypt.IsEnvelope([]byte("WM2")) {
		// 误认成信封就中止。
		t.Fatal("json must not look like envelope")
	}
}
