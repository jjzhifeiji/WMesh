package service

import (
	"context"
	"errors"

	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// firstWeldKind 取调用方传入的作业类型；没传则空，后面当单层。
func firstWeldKind(v []string) string {
	// 没有条目就直接返回，不必再校验
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// assertDepsWeldKind 工程钉的工艺必须和工程作业类型相同。
func (s *kernel) assertDepsWeldKind(ctx context.Context, weldKind string, deps []AssetDep) error {
	// 收成单层、多层或T排
	want, err := store.NormalizeWeldKind(weldKind)
	// 作业类型不合法，拒绝这份依赖
	if err != nil {
		return err
	}
	// 逐条工艺依赖核对作业类型，不一致就拿掉
	for _, d := range deps {
		// 读治理行，用来核对作业类型
		p, err := s.store.GovernedAssetMetaByID(ctx, d.ID)
		// 没有治理行，改查已收副本再对类型
		if errors.Is(err, domain.ErrNotFound) {
			// 治理行没有就改查已收副本
			r, rerr := s.store.LatestReplicaMeta(ctx, d.ID)
			// 查副本也失败则停住，不把缺失当对上
			if rerr != nil {
				// 副本也没有，按依赖缺失拒绝
				if errors.Is(rerr, domain.ErrNotFound) {
					return domain.ErrAssetDependency
				}
				return rerr
			}
			// 把副本元数据当成资产来核对类型
			p = replicaAsAsset(r)
			// 其它错误则停住，只有没有记录才另走
		} else if err != nil {
			return err
		}
		// 收成单层、多层或T排
		got, err := store.NormalizeWeldKind(p.WeldKind)
		// 作业类型不合法，拒绝这份依赖
		if err != nil || got != want {
			return domain.ErrWeldKindMismatch
		}
	}
	return nil
}

// depsMatchingWeldKind 只留下与作业类型相同的工艺依赖。
func (s *kernel) depsMatchingWeldKind(ctx context.Context, weldKind string, deps []AssetDep) ([]AssetDep, error) {
	// 没有条目就直接返回，不必再校验
	if len(deps) == 0 {
		return deps, nil
	}
	// 按条数决定是空、超限还是继续
	kept := make([]AssetDep, 0, len(deps))
	// 逐条工艺依赖核对作业类型，不一致就拿掉
	for _, d := range deps {
		// 类型不一致则这条依赖不能留
		if err := s.assertDepsWeldKind(ctx, weldKind, []AssetDep{d}); err != nil {
			if errors.Is(err, domain.ErrWeldKindMismatch) {
				continue
			}
			return nil, err
		}
		// 把这一条收进结果，漏了清单就不齐
		kept = append(kept, d)
	}
	return kept, nil
}

// assertProjectWeldKind 工程正文里能认的焊缝必须和作业类型相同。
func assertProjectWeldKind(weldKind string, content []byte) error {
	// 收成单层、多层或T排
	want, err := store.NormalizeWeldKind(weldKind)
	// 作业类型不合法，拒绝这份依赖
	if err != nil {
		return err
	}
	// 作业类型对不上则拒绝或跳过这条
	if !contenttpl.ContentMatchesWeldKind(want, content) {
		return domain.ErrWeldKindMismatch
	}
	return nil
}
