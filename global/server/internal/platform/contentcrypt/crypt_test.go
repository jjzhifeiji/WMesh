// WAN 侧正文信封：同一把钥可解开，错钥拒绝；明文不当信封。
package contentcrypt_test

import (
	"bytes"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/contentcrypt"
	"wmesh/global/internal/platform/domain"
)

func TestSealRoundtrip(t *testing.T) {
	kek, err := contentcrypt.RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	aad := contentcrypt.AssetAAD(id, 2, "assets")
	plain := []byte(`{"current":200}`)
	env, err := contentcrypt.Seal(kek, plain, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !contentcrypt.IsEnvelope(env) || bytes.Contains(env, plain) {
		t.Fatalf("envelope looks like plaintext")
	}
	got, err := contentcrypt.Open(kek, env, aad)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open %q %v", got, err)
	}
	other, err := contentcrypt.RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contentcrypt.Open(other, env, aad); err != domain.ErrIntegrity {
		t.Fatalf("wrong kek: %v", err)
	}
	if contentcrypt.IsEnvelope([]byte(`{"current":200}`)) || contentcrypt.IsEnvelope([]byte("WM2")) {
		t.Fatal("json must not look like envelope")
	}
}
