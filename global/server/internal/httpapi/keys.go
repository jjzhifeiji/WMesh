package httpapi

import (
	"encoding/base64"
	"encoding/hex"
	"strings"

	"wmesh/global/internal/platform/domain"
)

// 空则不登记；只认 32 字节公钥。
func decodeOptionalPublicKey(s string) ([]byte, error) {
	// 去掉空白，空串表示这次不带公钥。
	s = strings.TrimSpace(s)
	// 没带公钥就跳过，等现场上线再登记。
	if s == "" {
		return nil, nil
	}
	// 标准编码且正好 32 字节才收下。
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	// 无填充编码同样只认 32 字节公钥。
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	// 十六进制也只认 32 字节，其余当非法钥。
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, domain.ErrInvalidKey
}
