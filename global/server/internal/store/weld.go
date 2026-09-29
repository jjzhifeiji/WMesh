package store

import (
	"strings"

	"wmesh/global/internal/platform/domain"
)

// NormalizeWeldKind 资产作业类型；空当作单层焊道。也认模版里的 multi。
func NormalizeWeldKind(v string) (string, error) {
	// 空和别名收成规范作业类型，认不出就拒绝。
	switch strings.TrimSpace(v) {
	// 空值和单层都按单层焊道收下。
	case "", WeldKindSingle:
		return WeldKindSingle, nil
	// 多层和模版里的别名收成同一类。
	case WeldKindMultilayer, "multi":
		return WeldKindMultilayer, nil
	// T 排对接单独成一类。
	case WeldKindTBar:
		return WeldKindTBar, nil
	// 其余写法不认，避免脏类型落进库。
	default:
		return "", domain.ErrInvalidWeldKind
	}
}
