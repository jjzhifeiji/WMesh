package service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
)

// CopyAsset 网页「复制」入口：工程走复制工程，其余交给另存工艺。
func (s *Assets) CopyAsset(ctx context.Context, token string, assetID uuid.UUID, name string) (Asset, error) {
	// 核对登录仍有效，未登录不去读元数据。
	if _, err := s.RequireActive(ctx, token); err != nil {
		return Asset{}, err
	}
	// 先读元数据看种类，读不到交给另存工艺统一记拒绝。
	meta, err := s.loadAnyMeta(ctx, assetID)
	// 是工程就按复制工程处理。
	if err == nil && meta.Kind == KindProject {
		// 加密工艺保持原身份，普通工艺另复制一份。
		return s.CopyProject(ctx, token, assetID, name)
	}
	// 工艺照旧另存为新草稿。
	return s.CopyProcess(ctx, token, assetID, name)
}

// CopyProject 工程另存为新草稿：加密平台工艺保持原身份引用，普通工艺复制一份并改指新身份；平台级工程落成本厂厂级。
func (s *Assets) CopyProject(ctx context.Context, token string, assetID uuid.UUID, name string) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 去掉名字两端空白，空的下一步会拒绝。
	name = strings.TrimSpace(name)
	// 名字空则拒绝复制。
	if name == "" {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, domain.ErrInvalidName
	}
	// 名字太长焊次收不下，复制时就拒
	if err := checkAssetNameLen(name); err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	// 先读原件元数据，没有再看副本。
	src, err := s.loadAnyMeta(ctx, assetID)
	// 元数据读不到则记拒绝，不继续。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	// 是工程、未停用且有权看才能复制。
	if err := s.canCopyProject(ctx, acc, src); err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
	// 本厂原件优先，没有再读已收副本。
	src, err = s.loadAny(ctx, assetID)
	// 原件和副本都读不到则记拒绝。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	// 默认落成厂级；个人级源在下一行改回。
	level := AssetLevelFactory
	// 源是个人级则副本仍是个人级，其余落成厂级。
	if src.Level == AssetLevelPersonal {
		// 源是个人级则副本仍归本人，不升成厂级。
		level = AssetLevelPersonal
	}
	// 按级别核制作权，没有则拒绝。
	if err := s.canAuthorLevel(ctx, acc, level); err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 逐条依赖决定保留原身份还是复制一份，记下新建的工艺好回收。
	deps, idMap, made, err := s.copyProjectDeps(ctx, acc, level, src.Deps)
	// 依赖处理失败则回收已复制的工艺，整次拒绝。
	if err == nil {
		// 依赖都备好了，再改写正文并落工程。
		var row Asset
		// 改写引用、核依赖并落成草稿。
		row, err = s.insertProjectCopy(ctx, acc, src, level, name, deps, idMap)
		// 工程落成就把副本摆到源旁边，失败不影响已建成的资产。
		if err == nil {
			// 副本尽量放到源文件旁边。
			s.placeCopyBeside(ctx, src.ID, row.ID)
			return row, nil
		}
	}
	// 半截失败时删掉本次复制出的工艺，免得留下没人用的副本。
	for _, id := range made {
		// 回收失败也返回原来的错误，不盖住真因。
		_ = s.DeleteAsset(ctx, token, id)
	}
	// 复制被拒时补记审计，写失败仍维持拒绝。
	_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
	return Asset{}, err
}

// canCopyProject 复制工程的统一口径，单件复制和整夹复制预检共用。
func (s *kernel) canCopyProject(ctx context.Context, acc Account, src Asset) error {
	// 只有工程能走这条，工艺另有口径。
	if src.Kind != KindProject {
		return domain.ErrForbidden
	}
	// 停用的工程不能复制。
	if src.Status == AssetDisabled {
		return domain.ErrAssetNotAvailable
	}
	// 个人级须是创建人或整厂管理员。
	return s.canTouchPersonal(ctx, acc, src)
}

// copyProjectDeps 加密平台工艺和无权看正文的别人个人工艺保持原身份、修订记当前版，其余工艺按目标级别复制一份。
// 交回新依赖、旧身份到新身份的对照，以及本次新建的工艺身份。
func (s *Assets) copyProjectDeps(ctx context.Context, acc Account, level string, deps []AssetDep) ([]AssetDep, map[uuid.UUID]uuid.UUID, []uuid.UUID, error) {
	// 新依赖按原顺序排。
	out := make([]AssetDep, 0, len(deps))
	// 旧身份到新身份的对照，保留原引用的也记一条。
	idMap := make(map[uuid.UUID]uuid.UUID, len(deps))
	// 本次新建的工艺，失败时由调用方回收。
	made := []uuid.UUID{}
	// 逐条依赖处理，同一身份只处理一次。
	for _, d := range deps {
		// 已经处理过的身份不再复制第二份。
		if _, ok := idMap[d.ID]; ok {
			continue
		}
		// 先读元数据看级别和可复制，不解正文。
		meta, err := s.loadAnyMeta(ctx, d.ID)
		// 读不到则依赖不成立。
		if err != nil {
			// 找不到就换成业务上的缺失，不当成库故障。
			if errors.Is(err, domain.ErrNotFound) {
				return nil, nil, made, domain.ErrAssetDependency
			}
			return nil, nil, made, err
		}
		// 依赖必须是工艺。
		if meta.Kind != KindProcess {
			return nil, nil, made, domain.ErrAssetDependency
		}
		// 加密平台工艺保持原身份，不复制；另存是一次新声明，修订记当前版，当前版停用就拒绝。
		if meta.Level == AssetLevelPlatform && !meta.Copyable {
			// 按目标级别钉到当前可用修订，不看源工程声明的旧修订。
			pin, err := s.pinProjectProcess(ctx, acc, level, d.ID)
			// 钉不上就整次拒绝。
			if err != nil {
				return nil, nil, made, err
			}
			// 留下这条依赖。
			out = append(out, pin)
			// 对照里指回自己，正文引用不改。
			idMap[d.ID] = d.ID
			continue
		}
		// 别人的个人工艺当前人看不到正文，复制会外泄参数，保持原引用。
		if s.canTouchPersonal(ctx, acc, meta) != nil {
			// 按目标级别重钉当前可用修订，钉不上则依赖不成立。
			pin, err := s.pinProjectProcess(ctx, acc, level, d.ID)
			// 钉不上就整次拒绝。
			if err != nil {
				return nil, nil, made, err
			}
			// 留下重钉后的原身份。
			out = append(out, pin)
			// 对照里指回自己，正文引用不改。
			idMap[d.ID] = d.ID
			continue
		}
		// 普通工艺复制一份，落成目标级别的可用件。
		row, err := s.copyDepProcess(ctx, acc, level, d.ID)
		// 复制失败则整次拒绝。
		if err != nil {
			return nil, nil, made, err
		}
		// 记下新建的身份，失败时好回收。
		made = append(made, row.ID)
		// 新依赖钉到副本的当前修订。
		out = append(out, AssetDep{ID: row.ID, Revision: row.Revision, Digest: row.Digest})
		// 旧身份改指副本。
		idMap[d.ID] = row.ID
	}
	return out, idMap, made, nil
}

// pinProjectProcess 按工程级别把引用钉到当前可用修订。
func (s *kernel) pinProjectProcess(ctx context.Context, acc Account, level string, id uuid.UUID) (AssetDep, error) {
	// 个人工程按本人规则钉。
	if level == AssetLevelPersonal {
		// 个人工程钉自己的工艺、厂级可用工艺或加密平台工艺。
		return s.pinPersonalProcess(ctx, acc, id)
	}
	// 厂级工程钉本厂可用或已收工艺。
	return s.pinFactoryProcess(ctx, id)
}

// copyDepProcess 把工程依赖的普通工艺复制一份：正文原样，落成目标级别的可用件，放到源工艺旁边。
func (s *Assets) copyDepProcess(ctx context.Context, acc Account, level string, srcID uuid.UUID) (Asset, error) {
	// 本厂原件优先，没有再读已收副本，摘要不符当坏的。
	src, err := s.loadAny(ctx, srcID)
	// 读不到则不能复制。
	if err != nil {
		return Asset{}, err
	}
	// 停用或草稿的工艺不能跟着工程复制。
	if src.Status != AssetAvailable {
		return Asset{}, domain.ErrAssetNotAvailable
	}
	// 工程副本要能直接用，工艺副本落成可用件，不另走发布。
	row, err := s.putGoverned(ctx, acc, assetPlace{}, Asset{
		Kind: KindProcess, Level: level, Name: src.Name, Status: AssetAvailable, Copyable: true, WeldKind: src.WeldKind,
		Content: src.Content, Digest: digest.Sum(src.Content), CreatorID: acc.ID,
	})
	// 落库失败则复制不算成功。
	if err != nil {
		return Asset{}, err
	}
	// 副本尽量放到源文件旁边，失败不影响已建成的资产。
	s.placeCopyBeside(ctx, src.ID, row.ID)
	return row, nil
}

// insertProjectCopy 改写正文引用、按目标级别核依赖，再落成工程草稿。
func (s *Assets) insertProjectCopy(ctx context.Context, acc Account, src Asset, level, name string, deps []AssetDep, idMap map[uuid.UUID]uuid.UUID) (Asset, error) {
	// 正文里的工艺引用改指新身份，加密的不动。
	content, err := s.rewriteProjectProcessIDs(ctx, src.Content, idMap)
	// 改写失败则整次拒绝。
	if err != nil {
		return Asset{}, err
	}
	// 依赖按目标级别的口径核。
	if level == AssetLevelPersonal {
		// 个人工程只能钉自己的工艺、厂级可用工艺或加密平台工艺。
		err = s.assertPersonalProjectDeps(ctx, acc, deps)
		// 厂级工程按本厂可用和已收平台工艺来核。
	} else {
		// 厂级工程可钉同事的个人工艺和已收平台副本。
		err = s.assertFactoryProcessDeps(ctx, deps)
	}
	// 依赖不合格则拒绝。
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
	// 落成新身份的工程草稿，挂对应树根。
	return s.insertGoverned(ctx, acc, KindProject, level, name, content, deps, true, src.WeldKind)
}

// rewriteProjectProcessIDs 按当前工程模版改写正文里的工艺引用；对照里没有的身份原样保留。
func (s *kernel) rewriteProjectProcessIDs(ctx context.Context, content []byte, idMap map[uuid.UUID]uuid.UUID) ([]byte, error) {
	// 取已收的对象根工程模版。
	items, err := s.projectItems(ctx)
	// 模版读失败则不能改写。
	if err != nil {
		return nil, err
	}
	// 没有已收模版就用空库三份明细。
	if len(items) == 0 {
		// 和收集引用同一口径。
		items = contenttpl.SeedProjectItems()
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
