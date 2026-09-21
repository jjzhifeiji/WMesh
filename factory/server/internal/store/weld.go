package store

import (
	"strings"

	"wmesh/factory/internal/platform/domain"
)

// NormalizeWeldKind 资产作业类型；空当作单层焊道。也认模版里的 multi。
func NormalizeWeldKind(v string) (string, error) {
	switch strings.TrimSpace(v) {
	case "", WeldKindSingle:
		return WeldKindSingle, nil
	case WeldKindMultilayer, "multi":
		return WeldKindMultilayer, nil
	case WeldKindTBar:
		return WeldKindTBar, nil
	default:
		return "", domain.ErrInvalidWeldKind
	}
}
