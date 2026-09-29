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

	"wmesh/global/internal/platform/domain"
)

const (
	// MaxLease 厂侧解包钥最长有效期；续期也不得超过。
	MaxLease = 24 * time.Hour

	magic     = "WM2"        // 信封魔数，用来认出密文
	version   = 1            // 当前信封版本，对不上就拒绝
	keySize   = 32           // 密钥固定三十二字节
	nonceSize = 12           // 每次加密用的随机数长度
	kekIDSize = 8            // 信封上只留密钥指纹前八字节
	wrapSize  = keySize + 16 // 包起来的密钥再加认证标签
	// 信封最短：魔数+版本+kek 提示+两个 nonce+wrap+GCM tag。
	minEnvelope = 3 + 1 + kekIDSize + nonceSize + wrapSize + nonceSize + 16
)

// KeySize 是 L / MK / DEK 的字节数。
const KeySize = keySize

// Seal 用 KEK 包一把新 DEK，再加密正文；每次信封都不同。
func Seal(kek, plaintext, aad []byte) ([]byte, error) {
	// 密钥长度不对就当损坏，不封半套。
	if len(kek) != keySize {
		return nil, domain.ErrIntegrity
	}
	// 每次信封换一把新的数据钥。
	dek, err := RandomKey()
	// 发不出数据钥就不能封。
	if err != nil {
		return nil, err
	}
	// 用完就清掉数据钥，少留明文副本。
	defer Zero(dek)
	// 用主密钥包住数据钥。
	nW, wrap, err := gcmSeal(kek, dek, extraAAD(aad, "dek"))
	// 包钥失败则整封不作。
	if err != nil {
		return nil, err
	}
	// 再用数据钥封正文。
	nC, body, err := gcmSeal(dek, nonempty(plaintext), extraAAD(aad, "body"))
	// 封正文失败则整封不作。
	if err != nil {
		return nil, err
	}
	// 按各段长度预留信封缓冲。
	out := make([]byte, 0, 3+1+kekIDSize+nonceSize+wrapSize+nonceSize+len(body))
	// 先写魔数，好认出这是信封。
	out = append(out, magic...)
	// 再写版本，旧解包能拒绝新格式。
	out = append(out, version)
	// 写上主密钥指纹，只作提示。
	out = append(out, kekHint(kek)...)
	// 接上包钥用的随机数。
	out = append(out, nW...)
	// 接上被包住的数据钥。
	out = append(out, wrap...)
	// 接上封正文用的随机数。
	out = append(out, nC...)
	// 最后接上密文正文。
	out = append(out, body...)
	return out, nil
}

// Open 解开 WM2；无前缀当明文（旧行）。KEK 不对或 AAD 不对 → 完整性失败。
func Open(kek, blob, aad []byte) ([]byte, error) {
	// 没有信封头就当旧明文，原样拷回。
	if !IsEnvelope(blob) {
		// 拷一份交回，避免调用方改到入参。
		return append([]byte(nil), nonempty(blob)...), nil
	}
	// 密钥长度或信封太短都当损坏。
	if len(kek) != keySize || len(blob) < minEnvelope {
		return nil, domain.ErrIntegrity
	}
	// 版本对不上就拒绝，避免误解开。
	if blob[3] != version {
		return nil, domain.ErrIntegrity
	}
	// 跳过魔数、版本和密钥指纹。
	off := 4 + kekIDSize
	// 取出包钥用的随机数。
	nW := blob[off : off+nonceSize]
	// 随机数读完，指针移到被包的钥。
	off += nonceSize
	// 取出被包住的数据钥。
	wrap := blob[off : off+wrapSize]
	// 包钥段读完，指针移到正文随机数。
	off += wrapSize
	// 取出封正文用的随机数。
	nC := blob[off : off+nonceSize]
	// 再往后就是密文正文。
	off += nonceSize
	// 余下整段当密文正文。
	body := blob[off:]
	// 用主密钥解开数据钥。
	dek, err := gcmOpen(kek, nW, wrap, extraAAD(aad, "dek"))
	// 钥或附加数据不对就当损坏，不提密钥。
	if err != nil {
		return nil, domain.ErrIntegrity
	}
	// 用完就清掉数据钥。
	defer Zero(dek)
	// 用数据钥解开正文。
	plain, err := gcmOpen(dek, nC, body, extraAAD(aad, "body"))
	// 正文段对不上也当损坏。
	if err != nil {
		return nil, domain.ErrIntegrity
	}
	return plain, nil
}

// IsEnvelope 判断是否已是 WM2 密文：魔数、版本和最短长度都要齐。
func IsEnvelope(blob []byte) bool {
	// 长度、魔数和版本都齐才当密文。
	return len(blob) >= minEnvelope && string(blob[:3]) == magic && blob[3] == version
}

// RandomKey 生成 32 字节随机钥。
func RandomKey() ([]byte, error) {
	// 准备三十二字节的随机缓冲。
	b := make([]byte, keySize)
	// 读不到随机数就不能当密钥。
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// Zero 尽量清掉钥或明文副本。
func Zero(b []byte) {
	// 逐字节清掉，少留钥或明文副本。
	for i := range b {
		// 把这一字节写成零清掉。
		b[i] = 0
	}
}

// AssetAAD 把信封绑到这一行，避免把别人的密文贴过来。
func AssetAAD(id uuid.UUID, rev int64, table string) []byte {
	// 修订号放进定长缓冲。
	var revb [8]byte
	// 修订按大端写入，两边字节一致。
	binary.BigEndian.PutUint64(revb[:], uint64(rev))
	// 按身份、修订和表名预留缓冲。
	out := make([]byte, 0, 16+8+len(table))
	// 先写这一行的稳定身份。
	out = append(out, id[:]...)
	// 再写修订，避免贴到别的版本。
	out = append(out, revb[:]...)
	// 最后写表名，避免跨表套用。
	out = append(out, table...)
	return out
}

// TransitAAD 过站信封绑接收厂与该成员身份。
func TransitAAD(factoryID, assetID uuid.UUID, rev int64) []byte {
	// 修订号放进定长缓冲。
	var revb [8]byte
	// 修订按大端写入，两边字节一致。
	binary.BigEndian.PutUint64(revb[:], uint64(rev))
	// 按厂、成员、修订和标记预留缓冲。
	out := make([]byte, 0, 16+16+8+7)
	// 先写接收厂，密文不能换厂。
	out = append(out, factoryID[:]...)
	// 接着写入成员身份。
	out = append(out, assetID[:]...)
	// 接着写入这一版修订。
	out = append(out, revb[:]...)
	// 最后写过站标记，和行内信封分开。
	out = append(out, "transit"...)
	return out
}

// kekHint 信封上的 KEK 指纹，解包时不靠它验钥。
func kekHint(kek []byte) []byte {
	// 对主密钥做摘要，只留指纹不当验证。
	sum := sha256.Sum256(kek)
	return sum[:kekIDSize]
}

// extraAAD 把 DEK/正文段名拼进 AAD，避免两段互用。
func extraAAD(aad []byte, part string) []byte {
	// 按原附加数据和段名预留缓冲。
	out := make([]byte, 0, len(aad)+len(part))
	// 先抄上原来的附加数据。
	out = append(out, aad...)
	// 再拼段名，两段就不能互用。
	out = append(out, part...)
	return out
}

// nonempty 空指针收成空切片，空正文也能封。
func nonempty(b []byte) []byte {
	// 空指针收成空切片，空正文也能封。
	if b == nil {
		return []byte{}
	}
	return b
}

// gcmSeal AES-GCM 封一段，每次新 nonce。
func gcmSeal(key, plain, aad []byte) (nonce, ct []byte, err error) {
	// 用这把钥建分组密码。
	block, err := aes.NewCipher(key)
	// 钥不合法就封不了这一段。
	if err != nil {
		return nil, nil, err
	}
	// 加上认证，篡改能被看出来。
	aead, err := cipher.NewGCM(block)
	// 认证模式建不起来就停。
	if err != nil {
		return nil, nil, err
	}
	// 每次加密换一段新随机数。
	nonce = make([]byte, nonceSize)
	// 随机数失败就不能封，避免重复使用。
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	// 交出随机数和密文。
	return nonce, aead.Seal(nil, nonce, plain, aad), nil
}

// gcmOpen 解开一段；失败只回笼统错误。
func gcmOpen(key, nonce, ct, aad []byte) ([]byte, error) {
	// 用这把钥建分组密码。
	block, err := aes.NewCipher(key)
	// 钥不合法就解不开。
	if err != nil {
		return nil, err
	}
	// 加上认证，才能核对有没有被改。
	aead, err := cipher.NewGCM(block)
	// 认证模式建不起来就停。
	if err != nil {
		return nil, err
	}
	// 解开这一段；附加数据不对也会失败。
	plain, err := aead.Open(nil, nonce, ct, aad)
	// 失败只回笼统错误，不透露密钥线索。
	if err != nil {
		// 笼统错误，避免把底层原因漏出去。
		return nil, errors.New("open")
	}
	return plain, nil
}
