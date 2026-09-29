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
	// 对正文算出这一版的摘要。
	sum := sha256.Sum256(content)
	// 拷出定长结果，避免把数组头交出去。
	out := make([]byte, sha256.Size)
	// 把摘要字节抄进独立切片。
	copy(out, sum[:])
	return out
}

// Match 判断正文与已登记摘要是否一致。
func Match(content, digest []byte) bool {
	// 先按同一算法重算正文摘要。
	got := Sum(content)
	// 长度不同就不是同一份摘要。
	if len(digest) != len(got) {
		return false
	}
	// 长度相同才做常数时间比对。
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
	// 按成员顺序滚动摘要。
	h := sha256.New()
	// 修订号先放进定长缓冲。
	var rev [8]byte
	// 每个成员都按同一顺序写进摘要。
	for _, m := range members {
		// 取出身份，才能按字节写入。
		id := m.ID
		// 修订号按大端写入，两边字节一致。
		binary.BigEndian.PutUint64(rev[:], uint64(m.Revision))
		// 先把身份写进滚动摘要。
		h.Write(id[:])
		// 接着把修订写进滚动摘要。
		h.Write(rev[:])
		// 再把已登记摘要写进去。
		h.Write(m.Digest)
		// 最后写正文，闭包摘要才绑住内容。
		h.Write(m.Content)
	}
	// 收成这一串的摘要。
	sum := h.Sum(nil)
	// 拷出定长结果再交回。
	out := make([]byte, sha256.Size)
	// 抄进独立切片，避免改到内部缓冲。
	copy(out, sum)
	return out
}
