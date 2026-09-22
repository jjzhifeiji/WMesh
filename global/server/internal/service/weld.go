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
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// assertDepsWeldKind 工程钉的工艺必须和工程作业类型相同。
func (s *kernel) assertDepsWeldKind(ctx context.Context, weldKind string, deps []AssetDep) error {
	want, err := store.NormalizeWeldKind(weldKind)
	if err != nil {
		return err
	}
	for _, d := range deps {
		p, err := s.loadChecked(ctx, d.ID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return domain.ErrAssetDependency
			}
			return err
		}
		got, err := store.NormalizeWeldKind(p.WeldKind)
		if err != nil || got != want {
			return domain.ErrWeldKindMismatch
		}
	}
	return nil
}

// depsMatchingWeldKind 只留下与作业类型相同的工艺依赖。
func (s *kernel) depsMatchingWeldKind(ctx context.Context, weldKind string, deps []AssetDep) ([]AssetDep, error) {
	if len(deps) == 0 {
		return deps, nil
	}
	kept := make([]AssetDep, 0, len(deps))
	for _, d := range deps {
		if err := s.assertDepsWeldKind(ctx, weldKind, []AssetDep{d}); err != nil {
			if errors.Is(err, domain.ErrWeldKindMismatch) {
				continue
			}
			return nil, err
		}
		kept = append(kept, d)
	}
	return kept, nil
}

// assertProjectWeldKind 工程正文里能认的焊缝必须和作业类型相同。
func assertProjectWeldKind(weldKind string, content []byte) error {
	want, err := store.NormalizeWeldKind(weldKind)
	if err != nil {
		return err
	}
	if !contenttpl.ContentMatchesWeldKind(want, content) {
		return domain.ErrWeldKindMismatch
	}
	return nil
}
