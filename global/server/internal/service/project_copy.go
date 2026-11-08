package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
)

// copyPlatformProject 平台工程另存为新草稿：加密工艺保持原身份、修订记当前版，普通工艺复制成新的可用平台工艺并改指新身份。
// 普通工艺副本直接落可用：草稿工程发布时依赖须是可用工艺，副本落草稿会让新工程的依赖不成立。
func (s *Assets) copyPlatformProject(ctx context.Context, token string, admin Admin, src Asset, name string) (Asset, error) {
	// 逐条依赖决定保留原引用还是复制一份，记下新建的工艺好回收。
	deps, idMap, made, err := s.copyPlatformProjectDeps(ctx, admin, src.Deps)
	// 依赖都备好了，再改写正文并落工程。
	if err == nil {
		// 改写引用、核依赖并落成草稿。
		row, ierr := s.insertPlatformProjectCopy(ctx, admin, src, name, deps, idMap)
		// 工程落成后再通知复制出的工艺，免得厂端拉到马上要回收的副本。
		if ierr == nil {
			// 逐份通知厂端有新可用工艺。
			for _, a := range made {
				// 通知厂端这份副本可拉。
				s.notifyAsset(ctx, a)
			}
			// 有副本挂进了工艺目录就让厂端对齐目录。
			if len(made) > 0 {
				// 通知厂端对齐平台工艺目录。
				s.notifyPlatformFS(ctx, KindProcess)
			}
			return row, nil
		}
		// 落工程失败，交给下面回收。
		err = ierr
	}
	// 半截失败时删掉本次复制出的工艺，免得留下没人用的副本。
	for _, a := range made {
		// 回收失败也返回原来的错误，不盖住真因。
		_ = s.DeletePlatformAsset(ctx, token, a.ID)
	}
	// 另存被拒就留审计。
	_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
	return Asset{}, err
}

// copyPlatformProjectDeps 加密工艺保持原身份、修订记当前版，普通工艺复制一份；交回新依赖、旧到新身份对照和本次新建的工艺。
func (s *Assets) copyPlatformProjectDeps(ctx context.Context, admin Admin, deps []AssetDep) ([]AssetDep, map[uuid.UUID]uuid.UUID, []Asset, error) {
	// 新依赖按原顺序排。
	out := make([]AssetDep, 0, len(deps))
	// 旧身份到新身份的对照，保留原引用的也记一条。
	idMap := make(map[uuid.UUID]uuid.UUID, len(deps))
	// 本次新建的工艺，失败时由调用方回收。
	made := []Asset{}
	// 逐条依赖处理，同一身份只处理一次。
	for _, d := range deps {
		// 已经处理过的身份不再复制第二份。
		if _, ok := idMap[d.ID]; ok {
			continue
		}
		// 装入并核对摘要。
		p, err := s.loadChecked(ctx, d.ID)
		// 读不到则依赖不成立。
		if err != nil {
			// 没有这条就按依赖缺失处理，不当成别的故障。
			if errors.Is(err, domain.ErrNotFound) {
				return nil, nil, made, domain.ErrAssetDependency
			}
			return nil, nil, made, err
		}
		// 依赖必须是工艺。
		if p.Kind != KindProcess {
			return nil, nil, made, domain.ErrAssetDependency
		}
		// 加密工艺保持原身份，不复制；另存是一次新声明，修订记当前版，当前版停用或草稿就拒绝。
		if !p.Copyable {
			// 钉到当前可用修订，不看源工程声明的旧修订。
			pin, err := s.pinPlatformProcess(ctx, d.ID)
			// 钉不上则整次拒绝。
			if err != nil {
				return nil, nil, made, err
			}
			// 留下这条依赖。
			out = append(out, pin)
			// 对照里指回自己，正文引用不改。
			idMap[d.ID] = d.ID
			continue
		}
		// 停用或草稿的普通工艺不能跟着工程复制。
		if p.Status != AssetAvailable {
			return nil, nil, made, domain.ErrAssetNotAvailable
		}
		// 普通工艺复制一份，正文原样，仍可复制，直接可用。
		row, err := s.store.InsertAsset(ctx, Asset{
			Kind: KindProcess, Name: p.Name, Status: AssetAvailable, Copyable: true, WeldKind: p.WeldKind,
			Content: p.Content, Digest: digest.Sum(p.Content), CreatorID: admin.ID,
		})
		// 落库失败则整次拒绝。
		if err != nil {
			return nil, nil, made, err
		}
		// 去掉正文再记下，回收和通知都只用元数据。
		row = stripContent(row)
		// 记下新建的工艺，失败时好回收。
		made = append(made, row)
		// 新建成功留审计，审计写不上就当没建成。
		if err := s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
			return nil, nil, made, err
		}
		// 副本放到源工艺旁边，失败不影响已建成的资产。
		s.placeCopyBeside(ctx, p.ID, row.ID)
		// 新依赖钉到副本的当前修订。
		out = append(out, AssetDep{ID: row.ID, Revision: row.Revision, Digest: row.Digest})
		// 旧身份改指副本。
		idMap[d.ID] = row.ID
	}
	return out, idMap, made, nil
}

// insertPlatformProjectCopy 改写正文引用、核依赖，再落成工程草稿并放到源工程旁边。
func (s *Assets) insertPlatformProjectCopy(ctx context.Context, admin Admin, src Asset, name string, deps []AssetDep, idMap map[uuid.UUID]uuid.UUID) (Asset, error) {
	// 正文里的工艺引用改指新身份，加密的不动。
	content, err := s.rewriteProjectProcessIDs(ctx, src.Content, idMap)
	// 改写失败则整次拒绝。
	if err != nil {
		return Asset{}, err
	}
	// 依赖的作业类型须和工程一致。
	if err := s.assertDepsWeldKind(ctx, src.WeldKind, deps); err != nil {
		return Asset{}, err
	}
	// 改写后的引用须全落在新依赖里。
	if err := s.assertProjectProcessIDs(ctx, content, deps); err != nil {
		return Asset{}, err
	}
	// 落成新身份的工程草稿，不写升档来源。
	row, err := s.store.InsertAsset(ctx, Asset{
		Kind: KindProject, Name: name, Status: AssetDraft, Copyable: assetCopyable(KindProject, false), WeldKind: src.WeldKind,
		Content: content, Digest: digest.Sum(content), CreatorID: admin.ID, Deps: deps,
	})
	// 落库失败则复制不算成功。
	if err != nil {
		return Asset{}, err
	}
	// 另存成功才记允许，审计写不上就当没建成。
	if err := s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	// 把副本放在原件旁边。
	s.placeCopyBeside(ctx, src.ID, row.ID)
	// 去掉正文再返回。
	return stripContent(row), nil
}

// rewriteProjectProcessIDs 按当前工程模版改写正文里的工艺引用；对照里没有的身份原样保留。
func (s *kernel) rewriteProjectProcessIDs(ctx context.Context, content []byte, idMap map[uuid.UUID]uuid.UUID) ([]byte, error) {
	// 取当前工程各份字段表。
	items, err := s.projectItemSchemas(ctx)
	// 模版读不出就不能改写。
	if err != nil {
		return nil, err
	}
	// 先收集正文里的引用，原样文本作键。
	ids, err := contenttpl.CollectProcessIDsFromItems(items, content)
	// 收集失败则不能改写。
	if err != nil {
		// 带工艺路径按无权拒绝。
		if errors.Is(err, contenttpl.ErrProcessPath) {
			return nil, domain.ErrForbidden
		}
		return nil, domain.ErrAssetDependency
	}
	// 按正文原样文本建对照，大小写不同也能对上。
	full := make(map[string]string, len(ids))
	// 逐条引用找新身份，没有就指回自己。
	for _, raw := range ids {
		// 先当不改。
		full[raw] = raw
		// 引用不是合法身份则留给后面的依赖核对去拒。
		id, err := uuid.Parse(raw)
		// 解析失败就保持原样。
		if err != nil {
			continue
		}
		// 对照里有就改指新身份。
		if next, ok := idMap[id]; ok {
			// 改写成新身份的规范写法。
			full[raw] = next.String()
		}
	}
	// 按对照改写正文。
	out, err := contenttpl.RewriteProcessIDsFromItems(items, content, full)
	// 改写失败则依赖不成立。
	if err != nil {
		return nil, domain.ErrAssetDependency
	}
	return out, nil
}
