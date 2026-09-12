// Package secret 做密码哈希和会话令牌；哈希可换算法，但审计里永远不能出现原文。
package secret

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id 参数取 OWASP 推荐下限（19 MiB、2 轮、单线程）；参数随哈希一起存，日后调高不影响旧密码校验。
const (
	argonTime                   = 2
	argonMemory                 = 19 * 1024
	argonThreads                = uint8(1)
	argonKeyLen                 = 32
	saltLen                     = 16
	tokenLen                    = 32
	activationCodeLen           = 8        // 当面交付的激活码长度，数字好念（仅初始超管）
	defaultPersonPasswordSuffix = "123456" // 厂内普通账号默认密码后缀
)

// DefaultPersonPassword 厂内新建/重置用的默认日常密码：登录名+123456。只算出来给人用，不进库、不进审计。
func DefaultPersonPassword(loginName string) string {
	return loginName + defaultPersonPasswordSuffix
}

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

// ActivationCode 生成 8 位数字激活码；只给人看一次，库里只存哈希。
func ActivationCode() (string, error) {
	out := make([]byte, activationCodeLen)
	mod := big.NewInt(10)
	for i := range out {
		n, err := rand.Int(rand.Reader, mod)
		if err != nil {
			return "", err
		}
		out[i] = byte('0' + n.Int64())
	}
	return string(out), nil
}

// TokenHash 是令牌落库形态；令牌本身随机高熵，SHA-256 足够。
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Equal 恒定时间比较两段秘密（如共享密码、哈希串），避免按前缀长度泄露。
func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
