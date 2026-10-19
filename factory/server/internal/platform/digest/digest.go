// Package digest 对资产正文做 SHA-256 摘要，用于完整性比对，不是内容签名。
package digest

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"io"

	"github.com/google/uuid"
)

// Sum 计算 32 字节 SHA-256 摘要。
func Sum(content []byte) []byte {
	// 计算这一段内容的摘要。
	sum := sha256.Sum256(content)
	// 准备和摘要一样长的缓冲来接结果。
	out := make([]byte, sha256.Size)
	// 拷出一份，调用方事后改切片不会弄脏库存。
	copy(out, sum[:])
	return out
}

// Match 判断正文与已登记摘要是否一致。
func Match(content, digest []byte) bool {
	// 算出正文的摘要，再交给后面。
	got := Sum(content)
	// 摘要长度对不上就判成不一致。
	if len(digest) != len(got) {
		return false
	}
	// 交回恒定时间比较，避免看出前缀的结果。
	return subtle.ConstantTimeCompare(got, digest) == 1
}

// SumReader 边读边算 SHA-256，不整包进内存；交回摘要和读到的字节数。
func SumReader(r io.Reader) ([]byte, int64, error) {
	// 准备流式计算摘要。
	h := sha256.New()
	// 整份读一遍喂给摘要，缓冲只占几十 KB。
	n, err := io.Copy(h, r)
	// 读一半断了就交回，半截摘要不可信。
	if err != nil {
		return nil, n, err
	}
	// 交回摘要和读到的字节数。
	return h.Sum(nil), n, nil
}

// Equal 恒定时间比两份摘要；不是 32 字节或对不上都算不一致。
func Equal(got, want []byte) bool {
	// 长度对上再逐字节比，避免看出前缀。
	return len(got) == sha256.Size && subtle.ConstantTimeCompare(got, want) == 1
}

// Member 是闭包摘要的一个成员：身份、修订、已登记摘要和正文。
type Member struct {
	ID       uuid.UUID // 稳定身份
	Revision int64     // 钉死修订
	Digest   []byte    // 该修订已登记摘要
	Content  []byte    // 该修订正文
}

// ClosureSum 按成员顺序拼 id||修订大端||摘要||正文，再 SHA-256。
func ClosureSum(members []Member) []byte {
	// 准备按顺序计算摘要。
	h := sha256.New()
	// 准备放修订、上限、租约或长度的八字节。
	var rev [8]byte
	// 按成员或字段的顺序写进去，顺序不能换。
	for _, m := range members {
		// 定下身份，再交给后面。
		id := m.ID
		// 收成整数再参加后面的计算。
		binary.BigEndian.PutUint64(rev[:], uint64(m.Revision))
		// 按约定顺序把这一段写进摘要或原文。
		h.Write(id[:])
		// 按约定顺序把这一段写进摘要或原文。
		h.Write(rev[:])
		// 按约定顺序把这一段写进摘要或原文。
		h.Write(m.Digest)
		// 按约定顺序把这一段写进摘要或原文。
		h.Write(m.Content)
	}
	// 算出正文的摘要，再交给后面。
	sum := h.Sum(nil)
	// 准备和摘要一样长的缓冲来接结果。
	out := make([]byte, sha256.Size)
	// 拷出一份，调用方事后改切片不会弄脏库存。
	copy(out, sum)
	return out
}
