package httpapi

import (
	"encoding/base64"
	"encoding/hex"
	"strings"

	"wmesh/global/internal/platform/domain"
)

func decodePublicKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, domain.ErrInvalidKey
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, domain.ErrInvalidKey
}
