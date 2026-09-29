// Package secret 做密码哈希和会话令牌；哈希可换算法，但审计里永远不能出现原文。
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

// Argon2id 参数取 OWASP 推荐下限（19 MiB、2 轮、单线程）；参数随哈希一起存，日后调高不影响旧密码校验。
const (
	argonTime    = 2         // 迭代轮数取推荐下限
	argonMemory  = 19 * 1024 // 内存约十九兆，取推荐下限
	argonThreads = uint8(1)  // 单线程，避免抢机器
	argonKeyLen  = 32        // 派生结果固定三十二字节
	saltLen      = 16        // 每份密码单独的盐长度
	tokenLen     = 32        // 会话令牌的随机字节数
)

// HashPassword 用 Argon2id 生成可存储哈希，只应写入该账号所在库。
func HashPassword(password string) (string, error) {
	// 空密码不能哈希，否则登录无法区分。
	if password == "" {
		// 空密码在这里直接拒绝。
		return "", fmt.Errorf("empty password")
	}
	// 为这一份密码准备盐的缓冲。
	salt := make([]byte, saltLen)
	// 盐必须来自系统随机数。
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	// 按当前参数派生密钥。
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	// 参数和盐、密钥一起编码，以后调高不影响旧密码。
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword 恒定时间比较，避免用错误串长度泄露。
func VerifyPassword(encoded, password string) bool {
	// 按美元符号拆存放格式。
	parts := strings.Split(encoded, "$")
	// 段数或算法名不对就当校验失败。
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	// 准备接存放时的内存、轮数和线程。
	var mem, timeCost, threads int
	// 参数段解析失败就当校验失败。
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &timeCost, &threads); err != nil {
		return false
	}
	// 把盐从编码还原成字节。
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	// 盐解码失败就当校验失败。
	if err != nil {
		return false
	}
	// 还原当时的派生结果。
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	// 结果解码失败就当校验失败。
	if err != nil {
		return false
	}
	// 用存放的参数对这次口令再派生一次。
	got := argon2.IDKey([]byte(password), salt, uint32(timeCost), uint32(mem), uint8(threads), uint32(len(want)))
	// 常数时间比对，避免从耗时猜对错。
	return subtle.ConstantTimeCompare(want, got) == 1
}

// RandomToken 生成不透明令牌原文，调用方立刻交给持有者，库里只存 TokenHash。
func RandomToken() (string, error) {
	// 准备令牌的随机缓冲。
	b := make([]byte, tokenLen)
	// 读不到随机数就不能发令牌。
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// 收成网址安全的文本交给持有者。
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// TokenHash 是令牌落库形态；令牌本身随机高熵，SHA-256 足够。
func TokenHash(token string) string {
	// 对令牌原文做摘要，库里不留原文。
	sum := sha256.Sum256([]byte(token))
	// 收成十六进制再落库。
	return hex.EncodeToString(sum[:])
}

// Equal 恒定时间比较两段秘密（如共享密码、哈希串），避免按前缀长度泄露。
func Equal(a, b string) bool {
	// 常数时间比对两段秘密，避免从耗时猜前缀。
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
