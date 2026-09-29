package service

import (
	"context"
	"errors"

	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/store"
)

// firstWeldKind 取调用方传入的作业类型；没传则空，后面当单层。
func firstWeldKind(v []string) string {
	// 没有条目就直接返回，不再往下空查。
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// assertDepsWeldKind 工程钉的工艺必须和工程作业类型相同。
func (s *kernel) assertDepsWeldKind(ctx context.Context, weldKind string, deps []AssetDep) error {
	// 收成单层、多层或 T 排。
	want, err := store.NormalizeWeldKind(weldKind)
	// 别的写法一律拒绝。
	if err != nil {
		return err
	}
	// 逐条核对依赖，缺了或类型不同就拒绝。
	for _, d := range deps {
		// 装入并核对摘要。
		p, err := s.loadChecked(ctx, d.ID)
		// 不符就不能把这条拿去用。
		if err != nil {
			// 没有这条就按不存在处理，不当成别的故障。
			if errors.Is(err, domain.ErrNotFound) {
				return domain.ErrAssetDependency
			}
			return err
		}
		// 收成单层、多层或 T 排。
		got, err := store.NormalizeWeldKind(p.WeldKind)
		// 作业类型和期望不一致则拒绝。
		if err != nil || got != want {
			return domain.ErrWeldKindMismatch
		}
	}
	return nil
}

// depsMatchingWeldKind 只留下与作业类型相同的工艺依赖。
func (s *kernel) depsMatchingWeldKind(ctx context.Context, weldKind string, deps []AssetDep) ([]AssetDep, error) {
	// 没有条目就直接返回，不再往下空查。
	if len(deps) == 0 {
		return deps, nil
	}
	// 按数量先准备容器。
	kept := make([]AssetDep, 0, len(deps))
	// 逐条核对依赖，缺了或类型不同就拒绝。
	for _, d := range deps {
		// 对不上就拒绝，避免焊道和模式错配。
		if err := s.assertDepsWeldKind(ctx, weldKind, []AssetDep{d}); err != nil {
			if errors.Is(err, domain.ErrWeldKindMismatch) {
				continue
			}
			return nil, err
		}
		// 把这一项接进结果。
		kept = append(kept, d)
	}
	return kept, nil
}

// assertProjectWeldKind 工程正文里能认的焊缝必须和作业类型相同。
func assertProjectWeldKind(weldKind string, content []byte) error {
	// 收成单层、多层或 T 排。
	want, err := store.NormalizeWeldKind(weldKind)
	// 别的写法一律拒绝。
	if err != nil {
		return err
	}
	// 焊缝和作业类型不一致则拒绝。
	if !contenttpl.ContentMatchesWeldKind(want, content) {
		return domain.ErrWeldKindMismatch
	}
	return nil
}
