// Package contentcrypt 把正文封成 WM2 信封：每写一把 DEK，KEK 只包 DEK。
// 不管密码、不管节点签发；解包失败当损坏，不提密钥。
package contentcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
)

const (
	// MaxLease 厂侧解包钥最长有效期；续期也不得超过。
	MaxLease = 24 * time.Hour

	magic     = "WM2"
	version   = 1
	keySize   = 32
	nonceSize = 12
	kekIDSize = 8
	wrapSize  = keySize + 16
	// 信封最短：魔数+版本+kek 提示+两个 nonce+wrap+GCM tag。
	minEnvelope = 3 + 1 + kekIDSize + nonceSize + wrapSize + nonceSize + 16
)

// KeySize 是 L / MK / DEK 的字节数。
const KeySize = keySize

// Seal 用 KEK 包一把新 DEK，再加密正文；每次信封都不同。
func Seal(kek, plaintext, aad []byte) ([]byte, error) {
	if len(kek) != keySize {
		return nil, domain.ErrIntegrity
	}
	dek, err := RandomKey()
	if err != nil {
		return nil, err
	}
	defer Zero(dek)
	nW, wrap, err := gcmSeal(kek, dek, extraAAD(aad, "dek"))
	if err != nil {
		return nil, err
	}
	nC, body, err := gcmSeal(dek, nonempty(plaintext), extraAAD(aad, "body"))
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 3+1+kekIDSize+nonceSize+wrapSize+nonceSize+len(body))
	out = append(out, magic...)
	out = append(out, version)
	out = append(out, kekHint(kek)...)
	out = append(out, nW...)
	out = append(out, wrap...)
	out = append(out, nC...)
	out = append(out, body...)
	return out, nil
}

// Open 解开 WM2；无前缀当明文（旧行）。KEK 不对或 AAD 不对 → 完整性失败。
func Open(kek, blob, aad []byte) ([]byte, error) {
	if !IsEnvelope(blob) {
		return append([]byte(nil), nonempty(blob)...), nil
	}
	if len(kek) != keySize || len(blob) < minEnvelope {
		return nil, domain.ErrIntegrity
	}
	if blob[3] != version {
		return nil, domain.ErrIntegrity
	}
	off := 4 + kekIDSize
	nW := blob[off : off+nonceSize]
	off += nonceSize
	wrap := blob[off : off+wrapSize]
	off += wrapSize
	nC := blob[off : off+nonceSize]
	off += nonceSize
	body := blob[off:]
	dek, err := gcmOpen(kek, nW, wrap, extraAAD(aad, "dek"))
	if err != nil {
		return nil, domain.ErrIntegrity
	}
	defer Zero(dek)
	plain, err := gcmOpen(dek, nC, body, extraAAD(aad, "body"))
	if err != nil {
		return nil, domain.ErrIntegrity
	}
	return plain, nil
}

// IsEnvelope 判断是否已是 WM2 密文：魔数、版本和最短长度都要齐。
func IsEnvelope(blob []byte) bool {
	return len(blob) >= minEnvelope && string(blob[:3]) == magic && blob[3] == version
}

// RandomKey 生成 32 字节随机钥。
func RandomKey() ([]byte, error) {
	b := make([]byte, keySize)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// Zero 尽量清掉钥或明文副本。
func Zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// AssetAAD 把信封绑到这一行，避免把别人的密文贴过来。
func AssetAAD(id uuid.UUID, rev int64, table string) []byte {
	var revb [8]byte
	binary.BigEndian.PutUint64(revb[:], uint64(rev))
	out := make([]byte, 0, 16+8+len(table))
	out = append(out, id[:]...)
	out = append(out, revb[:]...)
	out = append(out, table...)
	return out
}

// TransitAAD 过站信封绑接收厂与该成员身份。
func TransitAAD(factoryID, assetID uuid.UUID, rev int64) []byte {
	var revb [8]byte
	binary.BigEndian.PutUint64(revb[:], uint64(rev))
	out := make([]byte, 0, 16+16+8+7)
	out = append(out, factoryID[:]...)
	out = append(out, assetID[:]...)
	out = append(out, revb[:]...)
	out = append(out, "transit"...)
	return out
}

// kekHint 信封上的 KEK 指纹，解包时不靠它验钥。
func kekHint(kek []byte) []byte {
	sum := sha256.Sum256(kek)
	return sum[:kekIDSize]
}

// extraAAD 把 DEK/正文段名拼进 AAD，避免两段互用。
func extraAAD(aad []byte, part string) []byte {
	out := make([]byte, 0, len(aad)+len(part))
	out = append(out, aad...)
	out = append(out, part...)
	return out
}

// nonempty 空指针收成空切片，空正文也能封。
func nonempty(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}

// gcmSeal AES-GCM 封一段，每次新 nonce。
func gcmSeal(key, plain, aad []byte) (nonce, ct []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, aead.Seal(nil, nonce, plain, aad), nil
}

// gcmOpen 解开一段；失败只回笼统错误。
func gcmOpen(key, nonce, ct, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, errors.New("open")
	}
	return plain, nil
}
