// Package digest 对资产正文做 SHA-256 摘要，用于完整性比对，不是内容签名。
package digest

import (
	"crypto/sha256"
	"crypto/subtle"
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
