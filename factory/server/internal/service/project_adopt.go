package service

// 厂级工程不留个人工艺：保存时把引用的个人工艺复制成新的厂级工艺再引用（D-101，2026-11-07 用户确认）。
// 操作员能拿到厂级工艺，拿不到别人的个人工艺；不这样换，这份厂级工程在他手上永远打不开。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// adoptResult 是一次保存把个人工艺换成厂级工艺的结果。
type adoptResult struct {
	content   []byte               // 改写后的正文
	deps      []AssetDep           // 改写后的依赖
	made      []uuid.UUID          // 本次新建的厂级工艺，工程没写成时要回收
	copies    []store.PersonalCopy // 本次新建的厂级工艺，回给调用方看
	rewritten bool                 // 正文和调用方给的不一样了
}

// stamp 把改写后的正文和新工艺写进回包；什么都没换就原样交回，回包和以前一样。
func (r adoptResult) stamp(row Asset) Asset {
	// 新工艺告诉调用方。
	row.PersonalCopies = r.copies
	// 正文改写过才带，工程正文只有工艺引用，不含参数。
	if r.rewritten {
		// 转成文本，和读正文接口同一形状。
		text := string(r.content)
		// 调用方拿这份替换手里的旧正文。
		row.SavedContent = &text
	}
	return row
}

// assertProcessBodyName 工艺正文带 name 键时不能为空或全空白；没有这个键和不是对象的正文不管，
// 旧样例和夹具常这样，读和用也都不靠它。
func assertProcessBodyName(kind string, content []byte) error {
	// 只管工艺；工程正文是焊道列表。
	if kind != KindProcess {
		return nil
	}
	// 只看顶层键，别的键不解。
	var obj map[string]json.RawMessage
	// 不是对象就不管。
	if json.Unmarshal(content, &obj) != nil {
		return nil
	}
	// 取 name 键，没有就不管。
	raw, ok := obj["name"]
	// 没有这个键不算改成空。
	if !ok {
		return nil
	}
	// 空值也算空，所以用指针接。
	var name *string
	// name 不是文本不归这里管。
	if json.Unmarshal(raw, &name) != nil {
		return nil
	}
	// 空值、空串和全空白（含全角空格）都拒绝。
	if name == nil || strings.TrimSpace(*name) == "" {
		return domain.ErrInvalidName
	}
	return nil
}

// copyBehindSource 个人工艺在复制之后真改过参数、当前人又还能重新复制时算落后：这时再选它要的是新参数，不能对回旧副本。
// 只改名不算；原件已删、停用或当前人无权复制也不算，这些情况只能用已有副本。
func (s *kernel) copyBehindSource(ctx context.Context, acc Account, meta Asset) (bool, error) {
	// 没记来源修订就比不出来，当没落后。
	if meta.SourceID == nil || meta.SourceRevision == nil {
		return false, nil
	}
	// 只读原件元数据，不解正文。
	src, err := s.store.GovernedAssetMetaByID(ctx, *meta.SourceID)
	// 原件删了，旧正文写着它也只能对回副本。
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	// 库错误不能当没落后。
	if err != nil {
		return false, err
	}
	// 修订没动或摘要相同（只改过名）就没落后。
	if src.Revision == *meta.SourceRevision || bytes.Equal(src.Digest, meta.Digest) {
		return false, nil
	}
	// 当前人复制不了就只能用已有副本，不因为原件改过就拒绝这次保存。
	return s.canAdoptPersonal(ctx, acc, src) == nil, nil
}

// adoptedCopies 从工程已声明的依赖里找已复制过的厂级工艺，交回个人工艺身份到这条依赖的对照；落后于原件的不算。
func (s *kernel) adoptedCopies(ctx context.Context, acc Account, declared []AssetDep) (map[uuid.UUID]AssetDep, error) {
	// 大多数工程一条都没有。
	out := map[uuid.UUID]AssetDep{}
	// 逐条依赖看它是不是从个人工艺来的。
	for _, d := range declared {
		// 只读元数据，不解正文。
		meta, err := s.store.GovernedAssetMetaByID(ctx, d.ID)
		// 平台副本等不在本厂原件里，不会是副本。
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		// 库错误不能当没有。
		if err != nil {
			return nil, err
		}
		// 厂级工艺且记着来源才算。
		if meta.Kind == KindProcess && meta.Level == AssetLevelFactory && meta.SourceID != nil {
			// 同一来源有多条时用先声明的。
			if _, ok := out[*meta.SourceID]; ok {
				continue
			}
			// 看这份副本是不是落后于原件。
			behind, err := s.copyBehindSource(ctx, acc, meta)
			// 查不清就不往下做。
			if err != nil {
				return nil, err
			}
			// 落后的不对回，后面按新引用重新复制一份。
			if behind {
				continue
			}
			// 记下对照。
			out[*meta.SourceID] = d
		}
	}
	return out, nil
}

// applyAdoptedCopies 调用方手里的正文或依赖还写着已换走的个人工艺时，对回工程依赖里已有的厂级副本。
// 示教器的旧正文、网页同一次保存的后几步都会这样；不新建，重复保存不会多出工艺。
func (s *kernel) applyAdoptedCopies(ctx context.Context, acc Account, declared []AssetDep, content []byte, deps []AssetDep) ([]byte, []AssetDep, error) {
	// 先找工程里已有的副本。
	known, err := s.adoptedCopies(ctx, acc, declared)
	// 查不清就不往下做。
	if err != nil {
		return nil, nil, err
	}
	// 没有副本就什么都不换。
	if len(known) == 0 {
		return content, deps, nil
	}
	// 依赖按对照换。
	nextDeps := replaceDeps(deps, known)
	// 个人身份到副本身份的对照，改写正文用。
	ids := make(map[uuid.UUID]uuid.UUID, len(known))
	// 逐条收下对照。
	for src, d := range known {
		// 个人工艺改指副本。
		ids[src] = d.ID
	}
	// 正文里写到才改写，没写到就保持原样字节。
	body, err := s.rewriteIfReferenced(ctx, content, ids)
	// 改写不了就不往下做。
	if err != nil {
		return nil, nil, err
	}
	return body, nextDeps, nil
}

// rewriteIfReferenced 正文写到对照里的身份才改写；一条都没写到就原样交回，不重排 JSON。
func (s *kernel) rewriteIfReferenced(ctx context.Context, content []byte, ids map[uuid.UUID]uuid.UUID) ([]byte, error) {
	// 没有对照就不用看正文。
	if len(ids) == 0 {
		return content, nil
	}
	// 收集正文里的引用。
	refs, err := s.collectProjectProcessIDs(ctx, content)
	// 带路径键按原因交回，其余按依赖不成立。
	if err != nil {
		// 无权或不许用的原因不能被盖掉。
		if errors.Is(err, domain.ErrForbidden) {
			return nil, err
		}
		return nil, domain.ErrAssetDependency
	}
	// 逐条看有没有写到。
	for _, raw := range refs {
		// 不是合法身份的留给后面的依赖核对去拒。
		id, err := uuid.Parse(raw)
		// 解析失败就看下一条。
		if err != nil {
			continue
		}
		// 写到一条就整份改写。
		if _, ok := ids[id]; ok {
			// 按对照改写，模版收不到的键不动。
			return s.rewriteProjectProcessIDs(ctx, content, ids)
		}
	}
	return content, nil
}

// replaceDeps 按对照换依赖并去重，顺序保持；换完和已有条目同身份的只留一条。
func replaceDeps(deps []AssetDep, known map[uuid.UUID]AssetDep) []AssetDep {
	// 按原条数备好结果。
	out := make([]AssetDep, 0, len(deps))
	// 记下已留的身份。
	seen := make(map[uuid.UUID]struct{}, len(deps))
	// 逐条换。
	for _, d := range deps {
		// 对照里有就换成副本。
		if next, ok := known[d.ID]; ok {
			// 改指副本。
			d = next
		}
		// 同一身份只留一条。
		if _, ok := seen[d.ID]; ok {
			continue
		}
		// 记下这一条。
		seen[d.ID] = struct{}{}
		// 留下。
		out = append(out, d)
	}
	return out
}

// copyPersonalDeps 把依赖里的个人工艺复制成新的厂级工艺，并改写正文和依赖；同一条只复制一份。
// declared 是工程已声明的依赖：这次新加的不合规就拒绝，已声明的（存量）不合规就原样留着，不挡别的改动：
// 别人的个人工艺当前人没权看，复制出来就等于把参数公开给厂级，保持原样；停用或不可复制的不能复制，也保持原样。
// 调用方已核过依赖和正文；失败时本函数回收已复制的，成功后工程没写成由调用方回收。
func (s *kernel) copyPersonalDeps(ctx context.Context, acc Account, declared []AssetDep, content []byte, deps []AssetDep) (adoptResult, error) {
	// 什么都不换时原样交回。
	out := adoptResult{content: content, deps: deps}
	// 已声明的身份，判存量用。
	old := make(map[uuid.UUID]struct{}, len(declared))
	// 逐条记下。
	for _, d := range declared {
		// 记下身份。
		old[d.ID] = struct{}{}
	}
	// 个人工艺身份到新依赖的对照。
	known := map[uuid.UUID]AssetDep{}
	// 改写正文用的对照。
	ids := map[uuid.UUID]uuid.UUID{}
	// 逐条依赖看是不是要复制的个人工艺。
	for _, d := range deps {
		// 同一条已经复制过就不再复制。
		if _, ok := known[d.ID]; ok {
			continue
		}
		// 只读元数据，不解正文。
		meta, err := s.store.GovernedAssetMetaByID(ctx, d.ID)
		// 平台副本不在本厂原件里，不是这里管的；加密平台工艺也走这条，保持原身份。
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		// 库错误不能当没有。
		if err != nil {
			// 回收已复制的，失败不盖住真因。
			s.recyclePersonalCopies(ctx, acc, out.made)
			return adoptResult{}, err
		}
		// 只换个人工艺。
		if meta.Kind != KindProcess || meta.Level != AssetLevelPersonal {
			continue
		}
		// 判这一条能不能复制。
		_, isOld := old[d.ID]
		// 不能复制的：存量留着，新加的拒绝。
		if err := s.canAdoptPersonal(ctx, acc, meta); err != nil {
			// 库错误一律停下；业务原因才看是不是存量。
			if !errors.Is(err, domain.ErrAssetDependency) && !errors.Is(err, domain.ErrAssetNotAvailable) && !errors.Is(err, domain.ErrAssetNotCopyable) {
				// 回收已复制的，失败不盖住真因。
				s.recyclePersonalCopies(ctx, acc, out.made)
				return adoptResult{}, err
			}
			// 工程早就钉着的不挡这次保存。
			if isOld {
				continue
			}
			// 新加的直接拒绝。
			// 回收已复制的，失败不盖住真因。
			s.recyclePersonalCopies(ctx, acc, out.made)
			return adoptResult{}, err
		}
		// 复制一份新的厂级工艺。
		row, err := s.copyPersonalToFactory(ctx, acc, d.ID)
		// 复制失败就整次拒绝。
		if err != nil {
			// 回收已复制的，失败不盖住真因。
			s.recyclePersonalCopies(ctx, acc, out.made)
			return adoptResult{}, err
		}
		// 记下来，工程没写成要回收。
		out.made = append(out.made, row.ID)
		// 回包告诉调用方这次多了哪条厂级工艺。
		out.copies = append(out.copies, store.PersonalCopy{SourceID: d.ID, ID: row.ID, Code: row.Code, Name: row.Name, Revision: row.Revision})
		// 依赖改钉新工艺的当前修订。
		known[d.ID] = AssetDep{ID: row.ID, Revision: row.Revision, Digest: row.Digest}
		// 正文引用改指新身份。
		ids[d.ID] = row.ID
	}
	// 一条都没换就原样交回。
	if len(known) == 0 {
		return out, nil
	}
	// 依赖按对照换。
	out.deps = replaceDeps(deps, known)
	// 正文按对照改写。
	body, err := s.rewriteIfReferenced(ctx, content, ids)
	// 改写失败就整次拒绝。
	if err != nil {
		// 回收已复制的，失败不盖住真因。
		s.recyclePersonalCopies(ctx, acc, out.made)
		return adoptResult{}, err
	}
	// 收下改写后的正文。
	out.content = body
	// 和调用方给的不一样才算改写过。
	out.rewritten = !bytes.Equal(body, content)
	return out, nil
}

// canAdoptPersonal 这条个人工艺当前人能不能复制成厂级：创建人或超管，可用且可复制。
func (s *kernel) canAdoptPersonal(ctx context.Context, acc Account, meta Asset) error {
	// 看不到正文的人不能复制，不然等于公开别人的参数；回依赖不成立，不说这条存在。
	if err := s.canTouchPersonal(ctx, acc, meta); err != nil {
		// 不是无权就是库错误，原样交回。
		if !errors.Is(err, domain.ErrForbidden) {
			return err
		}
		return domain.ErrAssetDependency
	}
	// 草稿和停用都不能被依赖。
	if meta.Status != AssetAvailable {
		return domain.ErrAssetNotAvailable
	}
	// 不可复制的工艺不得升档，复制成厂级同一口径。
	if !meta.Copyable {
		return domain.ErrAssetNotCopyable
	}
	return nil
}

// copyPersonalToFactory 复制一条新的厂级工艺：正文和作业类型原样，名字沿用，可用可复制，创建人记保存的人，挂厂级树根，
// 记下来源修订，示教器再传旧正文时能对回这条，个人工艺后面再改也不影响它。
func (s *kernel) copyPersonalToFactory(ctx context.Context, acc Account, srcID uuid.UUID) (Asset, error) {
	// 读原件正文并核摘要，摘要不符当坏的。
	src, err := s.loadChecked(ctx, srcID)
	// 读不到就不能复制。
	if err != nil {
		return Asset{}, err
	}
	// 记下来源修订，只当出处。
	rev := src.Revision
	// 工程马上要钉它，落成可用件，不另走发布。
	row, err := s.store.InsertGovernedAssetInto(ctx, Asset{
		Kind: KindProcess, Level: AssetLevelFactory, Name: src.Name, Status: AssetAvailable, Copyable: true, WeldKind: src.WeldKind,
		Content: src.Content, Digest: digest.Sum(src.Content), CreatorID: acc.ID,
		SourceID: &src.ID, SourceRevision: &rev,
	}, uuid.Nil, false)
	// 落库失败则记拒绝。
	if err != nil {
		// 这一步被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
	// 成功必须记上审计，没记上就删掉，当没做成。
	if err := s.auditAt(ctx, &acc.ID, "create_asset", assetTarget(row.ID, row.Revision)+" from "+assetTarget(src.ID, src.Revision), audit.Allow); err != nil {
		// 回收失败不盖住真因。
		_ = s.store.DeleteGovernedAsset(ctx, row.ID)
		return Asset{}, err
	}
	return row, nil
}

// recyclePersonalCopies 工程没写成时删掉本次复制出的工艺；回收失败只留审计，不盖住真因。
// 工程写进库后记审计失败也会走到这里，那时工程已钉着副本；被钉着的不能删，否则工程缺件。
func (s *kernel) recyclePersonalCopies(ctx context.Context, acc Account, made []uuid.UUID) {
	// 逐条回收。
	for _, id := range made {
		// 删之前看有没有工程钉着。
		used, err := s.store.AssetIsReferenced(ctx, id)
		// 查不清或还钉着都不删：多留一条厂级工艺，好过工程缺件。
		if err != nil || used {
			continue
		}
		// 删不掉也接着删下一条。
		if err := s.store.DeleteGovernedAsset(ctx, id); err != nil {
			// 记下没删成，日志里只有身份。
			_ = s.auditAt(ctx, &acc.ID, "delete_asset", id.String(), audit.Deny)
			continue
		}
		// 记下回收，写失败不改变结果。
		_ = s.auditAt(ctx, &acc.ID, "delete_asset", id.String(), audit.Allow)
	}
}
