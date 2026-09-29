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
	argonTime                   = 2         // 慢哈希迭代次数，取推荐下限。
	argonMemory                 = 19 * 1024 // 慢哈希占用的内存，按千字节计。
	argonThreads                = uint8(1)  // 慢哈希并行度，单线程就够用。
	argonKeyLen                 = 32        // 慢哈希输出的字节数。
	saltLen                     = 16        // 盐的字节数，每次哈希都重新抽。
	tokenLen                    = 32        // 会话令牌的随机字节数。
	activationCodeLen           = 8         // 当面交付的激活码长度，数字好念（仅初始超管）
	defaultPersonPasswordSuffix = "123456"  // 厂内普通账号默认密码后缀
)

// DefaultPersonPassword 厂内新建/重置用的默认日常密码：登录名+123456。只算出来给人用，不进库、不进审计。
func DefaultPersonPassword(loginName string) string {
	return loginName + defaultPersonPasswordSuffix
}

// HashPassword 用 Argon2id 生成可存储哈希，只应写入该账号所在库。
func HashPassword(password string) (string, error) {
	// 空口令不能哈希，避免存进空的秘文。
	if password == "" {
		// 带上原因交回去，调用方才能知道为何停下。
		return "", fmt.Errorf("empty password")
	}
	// 准备放盐的缓冲，每次哈希都要新的。
	salt := make([]byte, saltLen)
	// 没能抽一段随机字节做盐或令牌就停，避免带着残缺继续。
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	// 准备一份测试或签名用的字节。
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	// 按模板拼好再交回给调用方。
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword 恒定时间比较，避免用错误串长度泄露。
func VerifyPassword(encoded, password string) bool {
	// 按分隔符把文本切开。
	parts := strings.Split(encoded, "$")
	// 对不上就换一路，避免把不符的当成通过。
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	// 先把这一步的结果放下，后面还要用。
	var mem, timeCost, threads int
	// 这一步没做成就停，避免带着残缺继续。
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &timeCost, &threads); err != nil {
		return false
	}
	// 把编码过的签名解回来。
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	// 没能把编码过的签名解回来就停，避免带着残缺继续。
	if err != nil {
		return false
	}
	// 把编码过的签名解回来。
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	// 没能把编码过的签名解回来就停，避免带着残缺继续。
	if err != nil {
		return false
	}
	// 准备一份测试或签名用的字节。
	got := argon2.IDKey([]byte(password), salt, uint32(timeCost), uint32(mem), uint8(threads), uint32(len(want)))
	// 交回恒定时间比较，避免看出前缀的结果。
	return subtle.ConstantTimeCompare(want, got) == 1
}

// RandomToken 生成不透明令牌原文，调用方立刻交给持有者，库里只存 TokenHash。
func RandomToken() (string, error) {
	// 准备放令牌的缓冲，抽完就交出去。
	b := make([]byte, tokenLen)
	// 没能抽一段随机字节做盐或令牌就停，避免带着残缺继续。
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// 交回把签名收成可以放进口令的文本的结果。
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ActivationCode 生成 8 位数字激活码；只给人看一次，库里只存哈希。
func ActivationCode() (string, error) {
	// 按需要的长度把缓冲准备好。
	out := make([]byte, activationCodeLen)
	// 新建整数，再交给后面。
	mod := big.NewInt(10)
	// 按下标扫过去，直到越界或提前结束。
	for i := range out {
		// 整数，再交给后面，再交给后面。
		n, err := rand.Int(rand.Reader, mod)
		// 没能整数，再交给后面就停，避免带着残缺继续。
		if err != nil {
			return "", err
		}
		// 收成一个字节放进待签原文。
		out[i] = byte('0' + n.Int64())
	}
	// 收成文本再交回给调用方。
	return string(out), nil
}

// TokenHash 是令牌落库形态；令牌本身随机高熵，SHA-256 足够。
func TokenHash(token string) string {
	// 准备一份测试或签名用的字节。
	sum := sha256.Sum256([]byte(token))
	// 交回把签名收成可以放进口令的文本的结果。
	return hex.EncodeToString(sum[:])
}

// Equal 恒定时间比较两段秘密（如共享密码、哈希串），避免按前缀长度泄露。
func Equal(a, b string) bool {
	// 交回恒定时间比较，避免看出前缀的结果。
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
