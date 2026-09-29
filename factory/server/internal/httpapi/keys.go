package httpapi

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"wmesh/factory/internal/platform/domain"
)

// 空则不登记；只认 32 字节公钥。
func decodeOptionalPublicKey(s string) ([]byte, error) {
	// 去掉空白再认公钥，纯空白当成没带。
	s = strings.TrimSpace(s)
	// 没带公钥则不登记，绑定仍然可以继续。
	if s == "" {
		return nil, nil
	}
	// 标准编码且长度刚好才收下这份公钥。
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	// 标准编码且长度刚好才收下这份公钥。
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	// 十六进制同样只认规定长度的公钥。
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, domain.ErrInvalidKey
}

// 把 RFC3339 收成 UTC。
func parseTime(s string) (time.Time, error) {
	// 按约定格式解析时刻，失败则拒绝。
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	// 失败则停止，不把这一步当成成功。
	if err != nil {
		return time.Time{}, err
	}
	// 收成同一时区，避免窗口因时区错位。
	return t.UTC(), nil
}
