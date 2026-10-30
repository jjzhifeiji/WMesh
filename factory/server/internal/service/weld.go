package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

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

// assertCornerSlots 工程里任何工艺槽都不挂包角工艺，包角组 processId 也一样挂焊接参数；和示教器一样包角几何只在本机选。
// 认不出的身份不在这里拦，缺依赖由依赖核对去报。
func (s *kernel) assertCornerSlots(ctx context.Context, content []byte) error {
	// 按模版收全部工艺引用，包角组上的也在里面。
	ids, err := s.collectProjectProcessIDs(ctx, content)
	// 收不出引用就不在这里判，交给引用核对去报。
	if err != nil {
		return nil
	}
	// 没有引用就不用查库。
	if len(ids) == 0 {
		return nil
	}
	// 查库出错要交回，不能当没挂错。
	var lookErr error
	// 按身份查本厂原件，没有再查已收副本。
	isCorner := func(raw string) (bool, bool) {
		// 已经出错就不再查。
		if lookErr != nil {
			return false, false
		}
		// 不是合法身份认不出。
		id, err := uuid.Parse(raw)
		// 认不出就不管。
		if err != nil {
			return false, false
		}
		// 先查本厂原件。
		p, err := s.store.GovernedAssetMetaByID(ctx, id)
		// 查到就看包角标记。
		if err == nil {
			return p.Kind == KindProcess && p.Corner, true
		}
		// 不是没有记录就记下库错误。
		if !errors.Is(err, domain.ErrNotFound) {
			// 记下库错误，交回给调用方。
			lookErr = err
			return false, false
		}
		// 本厂没有就查已收副本。
		r, err := s.store.LatestReplicaMeta(ctx, id)
		// 查到就看包角标记。
		if err == nil {
			return r.Kind == KindProcess && r.Corner, true
		}
		// 不是没有记录就记下库错误。
		if !errors.Is(err, domain.ErrNotFound) {
			// 记下库错误，交回给调用方。
			lookErr = err
		}
		return false, false
	}
	// 挂错了就拒绝。
	if contenttpl.HasCornerProcess(ids, isCorner) {
		return domain.ErrCornerSlotMismatch
	}
	return lookErr
}
