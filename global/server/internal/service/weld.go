package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

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

// assertCornerSlots 工程里任何工艺槽都不挂包角工艺，包角组 processId 也一样挂焊接参数；和示教器一样包角几何只在本机选。
// 认不出的身份不在这里拦，缺依赖由依赖核对去报。
func (s *kernel) assertCornerSlots(ctx context.Context, content []byte) error {
	// 按当前工程模版收引用。
	items, err := s.projectItemSchemas(ctx)
	// 模版读不出就停，不能当没挂错。
	if err != nil {
		return err
	}
	// 收全部工艺引用，包角组上的也在里面。
	ids, err := contenttpl.CollectProcessIDsFromItems(items, content)
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
	// 按身份查平台级当前行。
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
		// 查平台级当前行。
		p, err := s.store.AssetByID(ctx, id)
		// 查到就看包角标记。
		if err == nil {
			return p.Kind == KindProcess && p.Corner, true
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
