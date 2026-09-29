package store

import (
	"strings"

	"wmesh/factory/internal/platform/domain"
)

// NormalizeWeldKind 资产作业类型；空当作单层焊道。也认模版里的 multi。
func NormalizeWeldKind(v string) (string, error) {
	// 先去掉空白再认作业类型，空的当单层。
	switch strings.TrimSpace(v) {
	// 空和单层都收成单层焊道。
	case "", WeldKindSingle:
		return WeldKindSingle, nil
	// 多层和旧写法都收成多层焊缝。
	case WeldKindMultilayer, "multi":
		return WeldKindMultilayer, nil
	// T 排对接单独收下。
	case WeldKindTBar:
		return WeldKindTBar, nil
	// 其余字样拒绝，避免非法作业类型进库。
	default:
		return "", domain.ErrInvalidWeldKind
	}
}
