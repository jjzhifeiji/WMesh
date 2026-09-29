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

	magic     = "WM2" // 信封开头三个字母，用来认出格式。
	version   = 1
	keySize   = 32           // 密钥字节数，封装钥和数据钥一样长。
	nonceSize = 12           // 随机数字节数，每次封装都要换新的。
	kekIDSize = 8            // 密钥指纹占的字节，只作提示不算验证。
	wrapSize  = keySize + 16 // 包起来的数据钥长度，含认证标签。
	// 信封最短：魔数+版本+kek 提示+两个 nonce+wrap+GCM tag。
	minEnvelope = 3 + 1 + kekIDSize + nonceSize + wrapSize + nonceSize + 16
)

// KeySize 是 L / MK / DEK 的字节数。
const KeySize = keySize

// Seal 用 KEK 包一把新 DEK，再加密正文；每次信封都不同。
func Seal(kek, plaintext, aad []byte) ([]byte, error) {
	// 钥长不对就拒绝封装，避免封出坏信封。
	if len(kek) != keySize {
		return nil, domain.ErrIntegrity
	}
	// 抽一把新的随机密钥。
	dek, err := RandomKey()
	// 没能抽一把新的随机密钥就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	// 离开时做收尾，避免资源漏掉。
	defer Zero(dek)
	// 把段名拼进绑定材料，避免两段互用。
	nW, wrap, err := gcmSeal(kek, dek, extraAAD(aad, "dek"))
	// 没能把段名拼进绑定材料，避免两段互用就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	// 把段名拼进绑定材料，避免两段互用。
	nC, body, err := gcmSeal(dek, nonempty(plaintext), extraAAD(aad, "body"))
	// 没能把段名拼进绑定材料，避免两段互用就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	// 准备和摘要一样长的缓冲来接结果。
	out := make([]byte, 0, 3+1+kekIDSize+nonceSize+wrapSize+nonceSize+len(body))
	// 把这一段接进结果，顺序要保持住。
	out = append(out, magic...)
	// 把这一段接进结果，顺序要保持住。
	out = append(out, version)
	// 把这一段接进结果，顺序要保持住。
	out = append(out, kekHint(kek)...)
	// 把这一段接进结果，顺序要保持住。
	out = append(out, nW...)
	// 把这一段接进结果，顺序要保持住。
	out = append(out, wrap...)
	// 把这一段接进结果，顺序要保持住。
	out = append(out, nC...)
	// 把这一段接进结果，顺序要保持住。
	out = append(out, body...)
	return out, nil
}

// Open 解开 WM2；无前缀当明文（旧行）。KEK 不对或 AAD 不对 → 完整性失败。
func Open(kek, blob, aad []byte) ([]byte, error) {
	// 不满足就停住或跳过，避免做错下一步。
	if !IsEnvelope(blob) {
		// 交回空指针收成空切片，空正文也能封的结果。
		return append([]byte(nil), nonempty(blob)...), nil
	}
	// 钥长不对就拒绝封装，避免封出坏信封。
	if len(kek) != keySize || len(blob) < minEnvelope {
		return nil, domain.ErrIntegrity
	}
	// 对不上就换一路，避免把不符的当成通过。
	if blob[3] != version {
		return nil, domain.ErrIntegrity
	}
	// 先把这一步的结果放下，后面还要用。
	off := 4 + kekIDSize
	// 定下数量，再交给后面。
	nW := blob[off : off+nonceSize]
	// 把这个值定下来，后面的判断才有依据。
	off += nonceSize
	// 定下包装，再交给后面。
	wrap := blob[off : off+wrapSize]
	// 把这个值定下来，后面的判断才有依据。
	off += wrapSize
	// 定下数量，再交给后面。
	nC := blob[off : off+nonceSize]
	// 把这个值定下来，后面的判断才有依据。
	off += nonceSize
	// 定下正文，再交给后面。
	body := blob[off:]
	// 把段名拼进绑定材料，避免两段互用。
	dek, err := gcmOpen(kek, nW, wrap, extraAAD(aad, "dek"))
	// 没能把段名拼进绑定材料，避免两段互用就停，避免带着残缺继续。
	if err != nil {
		return nil, domain.ErrIntegrity
	}
	// 离开时做收尾，避免资源漏掉。
	defer Zero(dek)
	// 把段名拼进绑定材料，避免两段互用。
	plain, err := gcmOpen(dek, nC, body, extraAAD(aad, "body"))
	// 没能把段名拼进绑定材料，避免两段互用就停，避免带着残缺继续。
	if err != nil {
		return nil, domain.ErrIntegrity
	}
	return plain, nil
}

// IsEnvelope 判断是否已是 WM2 密文：魔数、版本和最短长度都要齐。
func IsEnvelope(blob []byte) bool {
	// 收成文本再交回给调用方。
	return len(blob) >= minEnvelope && string(blob[:3]) == magic && blob[3] == version
}

// RandomKey 生成 32 字节随机钥。
func RandomKey() ([]byte, error) {
	// 准备和摘要一样长的缓冲来接结果。
	b := make([]byte, keySize)
	// 没能抽一段随机字节做盐或令牌就停，避免带着残缺继续。
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// Zero 尽量清掉钥或明文副本。
func Zero(b []byte) {
	// 按下标扫过去，直到越界或提前结束。
	for i := range b {
		// 定下这一段，再交给后面。
		b[i] = 0
	}
}

// AssetAAD 把信封绑到这一行，避免把别人的密文贴过来。
func AssetAAD(id uuid.UUID, rev int64, table string) []byte {
	// 准备放修订、上限、租约或长度的八字节。
	var revb [8]byte
	// 收成整数再参加后面的计算。
	binary.BigEndian.PutUint64(revb[:], uint64(rev))
	// 按需要的长度把缓冲准备好。
	out := make([]byte, 0, 16+8+len(table))
	// 按约定顺序接上这一段，不能前后颠倒。
	out = append(out, id[:]...)
	// 接上修订或版本的字节，顺序不能换。
	out = append(out, revb[:]...)
	// 按约定顺序接上这一段，不能前后颠倒。
	out = append(out, table...)
	return out
}

// TransitAAD 过站信封绑接收厂与该成员身份。
func TransitAAD(factoryID, assetID uuid.UUID, rev int64) []byte {
	// 准备放修订、上限、租约或长度的八字节。
	var revb [8]byte
	// 收成整数再参加后面的计算。
	binary.BigEndian.PutUint64(revb[:], uint64(rev))
	// 按需要的长度把缓冲准备好。
	out := make([]byte, 0, 16+16+8+7)
	// 接上工厂身份，对端才能认出是哪一家。
	out = append(out, factoryID[:]...)
	// 接上资产或目标身份，原文才绑得住。
	out = append(out, assetID[:]...)
	// 接上修订或版本的字节，顺序不能换。
	out = append(out, revb[:]...)
	// 把这一段接进结果，顺序要保持住。
	out = append(out, "transit"...)
	return out
}

// ClientTransitAAD 过站信封绑本厂、接收身份（登录人或本机）和该成员。
func ClientTransitAAD(factoryID, boundID, assetID uuid.UUID, rev int64) []byte {
	// 准备放修订、上限、租约或长度的八字节。
	var revb [8]byte
	// 收成整数再参加后面的计算。
	binary.BigEndian.PutUint64(revb[:], uint64(rev))
	// 按需要的长度把缓冲准备好。
	out := make([]byte, 0, 16+16+16+8+14)
	// 接上工厂身份，对端才能认出是哪一家。
	out = append(out, factoryID[:]...)
	// 接上本机或接收方身份，绑定才不会串。
	out = append(out, boundID[:]...)
	// 接上资产或目标身份，原文才绑得住。
	out = append(out, assetID[:]...)
	// 接上修订或版本的字节，顺序不能换。
	out = append(out, revb[:]...)
	// 把这一段接进结果，顺序要保持住。
	out = append(out, "client-transit"...)
	return out
}

// ClientTransitDEKAAD 过站 DEK 包装绑本厂和接收身份。
func ClientTransitDEKAAD(factoryID, boundID uuid.UUID) []byte {
	// 按需要的长度把缓冲准备好。
	out := make([]byte, 0, 16+16+18)
	// 接上工厂身份，对端才能认出是哪一家。
	out = append(out, factoryID[:]...)
	// 接上本机或接收方身份，绑定才不会串。
	out = append(out, boundID[:]...)
	// 把这一段接进结果，顺序要保持住。
	out = append(out, "client-transit-dek"...)
	return out
}

// kekHint 信封上的 KEK 指纹，解包时不靠它验钥。
func kekHint(kek []byte) []byte {
	// 计算这一段内容的摘要。
	sum := sha256.Sum256(kek)
	return sum[:kekIDSize]
}

// extraAAD 把 DEK/正文段名拼进 AAD，避免两段互用。
func extraAAD(aad []byte, part string) []byte {
	// 按需要的长度把缓冲准备好。
	out := make([]byte, 0, len(aad)+len(part))
	// 按约定顺序接上这一段，不能前后颠倒。
	out = append(out, aad...)
	// 按约定顺序接上这一段，不能前后颠倒。
	out = append(out, part...)
	return out
}

// nonempty 空指针收成空切片，空正文也能封。
func nonempty(b []byte) []byte {
	// 对象还是空的就直接返回，避免碰到空指针。
	if b == nil {
		return []byte{}
	}
	return b
}

// gcmSeal AES-GCM 封一段，每次新 nonce。
func gcmSeal(key, plain, aad []byte) (nonce, ct []byte, err error) {
	// 按这把钥准备分组加密。
	block, err := aes.NewCipher(key)
	// 没能按这把钥准备分组加密就停，避免带着残缺继续。
	if err != nil {
		return nil, nil, err
	}
	// 在分组加密上加上认证。
	aead, err := cipher.NewGCM(block)
	// 没能在分组加密上加上认证就停，避免带着残缺继续。
	if err != nil {
		return nil, nil, err
	}
	// 准备和摘要一样长的缓冲来接结果。
	nonce = make([]byte, nonceSize)
	// 没能抽一段随机字节做盐或令牌就停，避免带着残缺继续。
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	// 交回用封装钥把正文封进信封的结果。
	return nonce, aead.Seal(nil, nonce, plain, aad), nil
}

// gcmOpen 解开一段；失败只回笼统错误。
func gcmOpen(key, nonce, ct, aad []byte) ([]byte, error) {
	// 按这把钥准备分组加密。
	block, err := aes.NewCipher(key)
	// 没能按这把钥准备分组加密就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	// 在分组加密上加上认证。
	aead, err := cipher.NewGCM(block)
	// 没能在分组加密上加上认证就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	// 打开这一份资源，再交给后面。
	plain, err := aead.Open(nil, nonce, ct, aad)
	// 没能打开这一份资源就停，避免带着残缺继续。
	if err != nil {
		// 带上原因交回去，调用方才能知道为何停下。
		return nil, errors.New("open")
	}
	return plain, nil
}
