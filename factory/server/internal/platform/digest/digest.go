// Package digest 对资产正文做 SHA-256 摘要，用于完整性比对，不是内容签名。
package digest

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"

	"github.com/google/uuid"
)

// Sum 计算 32 字节 SHA-256 摘要。
func Sum(content []byte) []byte {
	sum := sha256.Sum256(content)
	out := make([]byte, sha256.Size)
	copy(out, sum[:])
	return out
}

// Match 判断正文与已登记摘要是否一致。
func Match(content, digest []byte) bool {
	got := Sum(content)
	if len(digest) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare(got, digest) == 1
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
	h := sha256.New()
	var rev [8]byte
	for _, m := range members {
		id := m.ID
		binary.BigEndian.PutUint64(rev[:], uint64(m.Revision))
		h.Write(id[:])
		h.Write(rev[:])
		h.Write(m.Digest)
		h.Write(m.Content)
	}
	sum := h.Sum(nil)
	out := make([]byte, sha256.Size)
	copy(out, sum)
	return out
}
