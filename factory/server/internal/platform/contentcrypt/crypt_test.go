// 正文信封：同一把钥可解开，错钥或错附加数据拒绝；明文不当信封。
package contentcrypt_test

import (
	"bytes"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/domain"
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
	env2, err := contentcrypt.Seal(kek, plain, aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(env, env2) {
		t.Fatal("same DEK reused")
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
	if _, err := contentcrypt.Open(kek, env, contentcrypt.AssetAAD(id, 3, "assets")); err != domain.ErrIntegrity {
		t.Fatalf("wrong aad: %v", err)
	}
	legacy, err := contentcrypt.Open(kek, plain, aad)
	if err != nil || !bytes.Equal(legacy, plain) {
		t.Fatalf("legacy %q %v", legacy, err)
	}
	if contentcrypt.IsEnvelope(plain) || contentcrypt.IsEnvelope([]byte("WM2")) {
		t.Fatal("short json must not look like envelope")
	}
}
