// Package secret 做口令哈希和会话令牌；哈希可换算法，但审计里永远不能出现原文。
package secret

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 1
	argonMemory  = 16 * 1024
	argonThreads = uint8(1)
	argonKeyLen  = 32
	saltLen      = 16
	tokenLen     = 32
)

// HashPassword 用 Argon2id 生成可存储哈希，只应写入该账号所在库。
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("empty password")
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword 恒定时间比较，避免用错误串长度泄露。
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var mem, timeCost, threads int
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &timeCost, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, uint32(timeCost), uint32(mem), uint8(threads), uint32(len(want)))
	return subtle.ConstantTimeCompare(want, got) == 1
}

// RandomToken 生成不透明令牌原文，调用方立刻交给持有者，库里只存 TokenHash。
func RandomToken() (string, error) {
	b := make([]byte, tokenLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
