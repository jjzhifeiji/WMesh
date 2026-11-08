package service

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// memberFromAsset 把本厂原件收成组包成员。
func memberFromAsset(a Asset) ClosureMember {
	// 创建人抄出来，组包成员要带上是谁做的。
	cid := a.CreatorID
	return ClosureMember{
		ID: a.ID, Kind: a.Kind, Level: a.Level, Name: a.Name, Code: a.Code, Status: a.Status,
		Copyable: a.Copyable, WeldKind: a.WeldKind, Revision: a.Revision, Content: a.Content, Digest: a.Digest,
		Deps: a.Deps, CreatorID: &cid,
	}
}

// memberFromReplica 把已收副本当成组包成员。
func memberFromReplica(r AssetReplica) ClosureMember {
	return ClosureMember{
		ID: r.ID, Kind: r.Kind, Level: r.Level, Name: r.Name, Code: r.Code, Status: r.Status,
		Copyable: r.Copyable, WeldKind: r.WeldKind, Revision: r.Revision, Content: r.Content, Digest: r.Digest, Deps: r.Deps,
	}
}

// sealClosure 组包并算整包摘要。
func sealClosure(kind string, root ClosureMember, rest []ClosureMember, facID, clientID *uuid.UUID) ClosureSnapshot {
	// 根加依赖的位置先留好。
	members := make([]ClosureMember, 0, 1+len(rest))
	// 根放在第一位，校验时就认这个。
	members = append(members, root)
	// 依赖成员跟在根后面，顺序和根的 deps 一致。
	members = append(members, rest...)
	// 按包内顺序准备摘要材料。
	parts := make([]digest.Member, len(members))
	// 按成员准备摘要材料，顺序和包内一致。
	for i, m := range members {
		// 这一位的身份、修订、摘要和正文都拿去算整包。
		parts[i] = digest.Member{ID: m.ID, Revision: m.Revision, Digest: m.Digest, Content: m.Content}
	}
	return ClosureSnapshot{
		Kind: kind, AssetID: root.ID, Revision: root.Revision, Level: root.Level,
		Copyable: root.Copyable, Status: root.Status, TargetFactoryID: facID, TargetClientID: clientID,
		Members: members, Digest: digest.ClosureSum(parts),
	}
}

// validateClosure 核对成员摘要、根 deps 与成员是否对上、整包摘要。
func validateClosure(snap ClosureSnapshot) error {
	// 一个成员都没有则包不完整，直接拒绝。
	if len(snap.Members) == 0 {
		return domain.ErrClosureIncomplete
	}
	// 逐个核对成员正文和摘要，对不上整包作废。
	for _, m := range snap.Members {
		// 摘要对不上当被篡改，拒绝继续使用。
		if !digest.Match(m.Content, m.Digest) {
			return domain.ErrIntegrity
		}
	}
	// 按成员顺序重算整包摘要，用来对账。
	parts := make([]digest.Member, len(snap.Members))
	// 按包内顺序重算整包摘要，用来对账。
	for i, m := range snap.Members {
		// 这一位的身份、修订、摘要和正文都拿去算整包。
		parts[i] = digest.Member{ID: m.ID, Revision: m.Revision, Digest: m.Digest, Content: m.Content}
	}
	// 重算的整包摘要对不上则当被篡改。
	if !bytes.Equal(digest.ClosureSum(parts), snap.Digest) {
		return domain.ErrIntegrity
	}
	// 第一位是根，后面用它核身份和依赖。
	root := snap.Members[0]
	// 包头必须和第一位根的身份、修订、种类一致。
	if snap.AssetID != root.ID || snap.Revision != root.Revision || snap.Kind != root.Kind {
		return domain.ErrClosureMismatch
	}
	// 工艺包只能有根自己，不能再夹成员。
	if snap.Kind == KindProcess {
		// 工艺包多了成员就和单工艺对不上。
		if len(snap.Members) != 1 {
			return domain.ErrClosureMismatch
		}
		return nil
	}
	// 不是工程包，或根不是工程，则拒绝。
	if snap.Kind != KindProject || root.Kind != KindProject {
		return domain.ErrClosureMismatch
	}
	// 成员个数必须刚好是根加全部依赖。
	if len(snap.Members) != 1+len(root.Deps) {
		// 成员比依赖少则算没收齐。
		if len(snap.Members) < 1+len(root.Deps) {
			return domain.ErrClosureIncomplete
		}
		return domain.ErrClosureMismatch
	}
	// 快照里根的 deps 由组包时写成当前版，这里只核包内自洽。
	for i, d := range root.Deps {
		// 依赖的下一位必须对上这一条。
		m := snap.Members[i+1]
		// 身份或修订和根 deps 不一致则整包作废。
		if m.ID != d.ID || m.Revision != d.Revision {
			return domain.ErrClosureMismatch
		}
		// 摘要和根 deps 不一致则整包作废。
		if !bytes.Equal(m.Digest, d.Digest) {
			return domain.ErrClosureMismatch
		}
	}
	return nil
}

// loadDepMember 按身份取工艺当前版成员：本厂原件取当前行，没有再取已收的最高修订副本（已撤回的也算）。
func (s *Closure) loadDepMember(ctx context.Context, depID uuid.UUID) (ClosureMember, error) {
	// 先看本厂原件，没有再取已收副本。
	a, err := s.store.GovernedAssetByID(ctx, depID)
	// 本厂原件在就收成成员，没有再看副本。
	if err == nil {
		// 摘要对不上当被篡改，拒绝继续使用。
		if !digest.Match(a.Content, a.Digest) {
			return ClosureMember{}, domain.ErrIntegrity
		}
		// 依赖必须是工艺，工程不能嵌进包里。
		if a.Kind != KindProcess {
			return ClosureMember{}, domain.ErrClosureMismatch
		}
		// 把本厂原件收成组包成员。
		return memberFromAsset(a), nil
	}
	// 不是找不到则把库错误抛回，避免当成没有。
	if !errors.Is(err, domain.ErrNotFound) {
		return ClosureMember{}, err
	}
	// 取已收的最高修订副本；已撤回的也要，被依赖的工艺撤回后仍可读。
	r, err := s.store.HighestReplica(ctx, depID)
	// 一份都没收过或读失败则闭包不齐。
	if err != nil {
		// 找不到就换成业务上的缺失，不当成库故障。
		if errors.Is(err, domain.ErrNotFound) {
			return ClosureMember{}, domain.ErrClosureIncomplete
		}
		return ClosureMember{}, err
	}
	// 摘要对不上当被篡改，拒绝继续使用。
	if !digest.Match(r.Content, r.Digest) {
		return ClosureMember{}, domain.ErrIntegrity
	}
	// 副本必须是工艺，否则对不上。
	if r.Kind != KindProcess {
		return ClosureMember{}, domain.ErrClosureMismatch
	}
	// 把已收副本收成组包成员。
	return memberFromReplica(r), nil
}

// packFromMember 根须可用，依赖按身份取当前版，快照里根的 deps 改写成当前版，库里的工程行不动。
func (s *Closure) packFromMember(ctx context.Context, root ClosureMember) (ClosureSnapshot, error) {
	// 根不是可用则拒绝组包，草稿和停用都不行。
	if root.Status != AssetAvailable {
		return ClosureSnapshot{}, domain.ErrAssetNotAvailable
	}
	// 按依赖条数准备成员，收不齐就失败。
	rest := make([]ClosureMember, 0, len(root.Deps))
	// 这份快照里根的依赖，按成员当前版改写。
	deps := make([]AssetDep, 0, len(root.Deps))
	// 工程 deps 只定用哪几条工艺，修订一律取当前版。
	for _, d := range root.Deps {
		// 按身份取工艺，原件没有再看副本。
		m, err := s.loadDepMember(ctx, d.ID)
		// 这条依赖收不齐则整包失败。
		if err != nil {
			return ClosureSnapshot{}, err
		}
		// 这条工艺已按当前版收进包。
		rest = append(rest, m)
		// 快照里的依赖写成这次实际带上的修订和摘要。
		deps = append(deps, AssetDep{ID: m.ID, Revision: m.Revision, Digest: m.Digest})
	}
	// 只改传进来的这份根成员，调用方的依赖切片不动。
	root.Deps = deps
	// 成员收齐后算整包摘要。
	snap := sealClosure(root.Kind, root, rest, nil, nil)
	// 闭包对不上则拒绝装入或激活。
	if err := validateClosure(snap); err != nil {
		return ClosureSnapshot{}, err
	}
	return snap, nil
}

// packForPad 只组包，可拉范围和过站加封由调用方办：草稿也拉；工艺单独成包；工程只带根和工艺 Id。
func (s *Closure) packForPad(ctx context.Context, assetID uuid.UUID) (ClosureSnapshot, error) {
	// 组包根优先用本厂原件，否则用副本。
	root, err := s.loadRootForPack(ctx, assetID)
	// 根读不到则不能组包。
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 复制一份依赖，避免改到调用方手里的切片。
	root.Deps = copyDeps(root.Deps)
	// 工艺单独成包，不带工程依赖。
	if root.Kind == KindProcess {
		// 成员收齐后算整包摘要。
		snap := sealClosure(KindProcess, root, nil, nil, nil)
		// 闭包对不上则拒绝装入或激活。
		if err := validateClosure(snap); err != nil {
			return ClosureSnapshot{}, err
		}
		// 把当前目录挂点写进闭包，不参与摘要。
		s.attachFSPaths(ctx, &snap)
		return snap, nil
	}
	// 既不是工艺也不是工程则拒绝组给平板。
	if root.Kind != KindProject {
		return ClosureSnapshot{}, domain.ErrForbidden
	}
	// 工程不把工艺正文打进包，焊道只引用工艺 Id。
	parts := []digest.Member{{ID: root.ID, Revision: root.Revision, Digest: root.Digest, Content: root.Content}}
	// 工程包只带根，工艺正文不打进去。
	snap := ClosureSnapshot{
		Kind: KindProject, AssetID: root.ID, Revision: root.Revision, Level: root.Level,
		Copyable: root.Copyable, Status: root.Status, Members: []ClosureMember{root},
		Digest: digest.ClosureSum(parts),
	}
	// 把当前目录挂点写进闭包，不参与摘要。
	s.attachFSPaths(ctx, &snap)
	return snap, nil
}

// packPinnedForPad 列表外、被工程依赖的平台工艺按已收最高修订单独成包；已撤回的也给，一份都没有才拒。
func (s *Closure) packPinnedForPad(ctx context.Context, depID uuid.UUID) (ClosureSnapshot, error) {
	// 按身份取工艺，原件没有再看副本。
	m, err := s.loadDepMember(ctx, depID)
	// 读不到或对不上则整包拒绝。
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 只给平台副本，本厂原件不走这条口子。
	if m.Level != AssetLevelPlatform {
		return ClosureSnapshot{}, domain.ErrNotFound
	}
	// 标成停用：示教器工艺列表不显示，依赖它的工程照样能用。
	m.Status = AssetDisabled
	// 工艺单独成包，不带别的成员。
	snap := sealClosure(KindProcess, m, nil, nil, nil)
	// 闭包对不上则拒绝下发。
	if err := validateClosure(snap); err != nil {
		return ClosureSnapshot{}, err
	}
	// 把当前目录挂点写进闭包，不参与摘要。
	s.attachFSPaths(ctx, &snap)
	return snap, nil
}

// attachFSPaths 把当前目录挂点写进闭包，不进摘要。
func (s *Closure) attachFSPaths(ctx context.Context, snap *ClosureSnapshot) {
	// 逐个补目录挂点，读不到的跳过。
	for i := range snap.Members {
		// 读这个成员现在挂在哪条目录上。
		parent, folders, err := s.store.FSLineage(ctx, snap.Members[i].ID)
		// 这个成员的目录读不到就跳过，不挡住组包。
		if err != nil {
			continue
		}
		// 挂上当前父文件夹，这块不进摘要。
		snap.Members[i].FSParentID = parent
		// 把目录层级写上，激活时能对上位置。
		snap.Members[i].FSPath = folders
	}
}

// loadRootForPack 组包根：本厂原件优先，否则已收副本。
func (s *Closure) loadRootForPack(ctx context.Context, assetID uuid.UUID) (ClosureMember, error) {
	// 按身份读本厂原件正文。
	a, err := s.store.GovernedAssetByID(ctx, assetID)
	// 本厂原件在就用它当根，没有再看副本。
	if err == nil {
		// 摘要对不上当被篡改，拒绝继续使用。
		if !digest.Match(a.Content, a.Digest) {
			return ClosureMember{}, domain.ErrIntegrity
		}
		// 把本厂原件收成组包成员。
		return memberFromAsset(a), nil
	}
	// 不是找不到则把库错误抛回，避免当成没有。
	if !errors.Is(err, domain.ErrNotFound) {
		return ClosureMember{}, err
	}
	// 取该身份已收的最高修订副本。
	r, err := s.store.LatestReplica(ctx, assetID)
	// 副本读失败则不能当组包根。
	if err != nil {
		return ClosureMember{}, err
	}
	// 摘要对不上当被篡改，拒绝继续使用。
	if !digest.Match(r.Content, r.Digest) {
		return ClosureMember{}, domain.ErrIntegrity
	}
	// 把已收副本收成组包成员。
	return memberFromReplica(r), nil
}

// 记下这次组包的允许或拒绝。
func (s *Closure) auditPack(ctx context.Context, actor *uuid.UUID, snap ClosureSnapshot, result string) error {
	// 按这次结果记组包审计，写失败则不算组完。
	return s.audit(ctx, actor, nil, "pack_closure", closureTarget(snap), result)
}

// 审计对象：根、成员加目标厂或 Client。
func closureTarget(snap ClosureSnapshot) string {
	// 拼根的身份和修订，再附上其余成员。
	t := assetTarget(snap.AssetID, snap.Revision)
	// 根以外的成员也写进审计对象。
	for _, m := range snap.Members {
		// 根已经写过，成员里再碰到就跳过。
		if m.ID == snap.AssetID {
			continue
		}
		// 拼根的身份和修订，再附上其余成员。
		t += " " + assetTarget(m.ID, m.Revision)
	}
	// 有目标厂就写进审计，方便对上下发对象。
	if snap.TargetFactoryID != nil {
		// 有目标厂就写进审计，方便对上下发对象。
		t += " factory=" + snap.TargetFactoryID.String()
	}
	// 有目标终端就写进审计。
	if snap.TargetClientID != nil {
		// 有目标终端就写进审计。
		t += " client=" + snap.TargetClientID.String()
	}
	return t
}

// AssembleProject 从本厂当前行或已收副本组包，工艺按身份取当前版；草稿/停用/缺失拒绝。
func (s *Closure) AssembleProject(ctx context.Context, token string, assetID uuid.UUID) (ClosureSnapshot, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 有权才能组包。失败一律记拒绝。
	root, err := s.loadRootForPack(ctx, assetID)
	// 失败则记拒绝并停下，不继续往下改。
	if err != nil {
		// 组包或装袋被拒时补记审计。
		_ = s.audit(ctx, &acc.ID, nil, "pack_closure", assetID.String(), audit.Deny)
		return ClosureSnapshot{}, err
	}
	// 无权组包则记拒绝，不把包发出去。
	if err := s.canPack(ctx, acc, root); err != nil {
		// 组包或装袋被拒时补记审计。
		_ = s.audit(ctx, &acc.ID, nil, "pack_closure", assetTarget(root.ID, root.Revision), audit.Deny)
		return ClosureSnapshot{}, err
	}
	// 根须可用，依赖按身份取当前版收齐再封包。
	snap, err := s.packFromMember(ctx, root)
	// 组包失败则记拒绝。
	if err != nil {
		// 组包或装袋被拒时补记审计。
		_ = s.audit(ctx, &acc.ID, nil, "pack_closure", assetTarget(root.ID, root.Revision), audit.Deny)
		return ClosureSnapshot{}, err
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.auditPack(ctx, &acc.ID, snap, audit.Allow); err != nil {
		return ClosureSnapshot{}, err
	}
	return snap, nil
}

// canPack 个人级仅创建人；平台级须整厂作用域操作员；厂级看操作员作用域。
func (s *Closure) canPack(ctx context.Context, acc Account, root ClosureMember) error {
	// 个人级组包只允许创建人。
	if root.Level == AssetLevelPersonal {
		// 不是创建人则拒绝组这份个人级包。
		if root.CreatorID == nil || *root.CreatorID != acc.ID {
			return domain.ErrForbidden
		}
		return nil
	}
	// 平台级要有操作权才能组。
	if root.Level == AssetLevelPlatform {
		// 没有操作权则拒绝组平台级。
		return s.opCovers(ctx, acc)
	}
	// 厂级也只看操作权。
	return s.opCovers(ctx, acc)
}

// 复制依赖切片，避免改到调用方。
func copyDeps(deps []AssetDep) []AssetDep {
	// 按原样复制依赖，避免改到调用方。
	out := make([]AssetDep, len(deps))
	// 拷走一份，后面改切片不会碰到原件。
	copy(out, deps)
	return out
}

// SetProjectDeps 显式改工程依赖并升高修订；所填工艺必须仍能读到。
func (s *Closure) SetProjectDeps(ctx context.Context, token string, assetID uuid.UUID, expected int64, deps []AssetDep) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 把个人工艺换成厂级的结果，写完告诉调用方，写失败要回收。
	var adopted adoptResult
	// 只改工程依赖；停用拒绝，引用必须仍能读到。
	row, err := s.mutateAsset(ctx, acc, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		// 只有工程能改依赖，工艺拒绝。
		if cur.Kind != KindProject {
			return store.AssetWrite{}, domain.ErrAssetDependency
		}
		// 停用的工程不能改依赖。
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		// 正文原件后面要核引用，厂级工程还可能要改写。
		opened, err := s.loadChecked(ctx, cur.ID)
		// 原件读不到或摘要不符则拒绝。
		if err != nil {
			return store.AssetWrite{}, err
		}
		// 接下来要写的正文，不换工艺就是原件。
		body := opened.Content
		// 厂级工程先把已换走的个人工艺对回已有的厂级副本，不再复制。
		wanted := deps
		// 只有厂级工程有这一步。
		if cur.Level == AssetLevelFactory {
			// 只查依赖里已有的副本，这一步不新建。
			body, wanted, err = s.applyAdoptedCopies(ctx, acc, cur.Deps, body, wanted)
			// 换不了就整份依赖不改。
			if err != nil {
				return store.AssetWrite{}, err
			}
		}
		// 按身份重钉当前可用修订，跟上工艺升版；已声明的停用或云端已删工艺原样留着。
		resolved, err := s.resolveProjectDeps(ctx, acc, cur.Level, cur.Deps, wanted)
		// 有一条重钉失败则整份依赖不改。
		if err != nil {
			return store.AssetWrite{}, err
		}
		// 焊道类型对不上则拒绝。
		if err := s.assertDepsWeldKind(ctx, cur.WeldKind, resolved); err != nil {
			return store.AssetWrite{}, err
		}
		// 改依赖后，当前模版下的旧引用仍须落在新 deps 里。
		if err := s.assertProjectProcessIDs(ctx, body, resolved); err != nil {
			return store.AssetWrite{}, err
		}
		// 校验都过了，厂级工程还钉着的个人工艺复制成厂级再写。
		if cur.Level == AssetLevelFactory {
			// 修订已经对不上就别复制了，写也写不进去。
			if cur.Revision != expected {
				return store.AssetWrite{}, domain.ErrRevisionConflict
			}
			// 复制并改写，记下结果，写失败要回收。
			adopted, err = s.copyPersonalDeps(ctx, acc, cur.Deps, body, resolved)
			// 复制不了就整份依赖不改，已复制的里面回收过了。
			if err != nil {
				return store.AssetWrite{}, err
			}
			// 写入的是改写过的正文和依赖。
			body, resolved = adopted.content, adopted.deps
		}
		// 拼出本次要写入的内容，没通过的字段不改。
		write := store.AssetWrite{Name: cur.Name, Digest: cur.Digest, Copyable: cur.Copyable, Status: cur.Status, Deps: copyDeps(resolved), KeepContent: true}
		// 服务端改写过正文才写正文，否则正文和摘要都不动。
		if !bytes.Equal(body, opened.Content) {
			// 改写过，换成新正文。
			write.KeepContent = false
			// 写入改写后的正文。
			write.Content = body
			// 摘要跟着新正文算。
			write.Digest = digest.Sum(body)
			// 调用方手里的正文旧了，回包要带上；这份从库里读的，包角几何要遮。
			adopted.content, adopted.rewritten = contenttpl.MaskCornerGeometry(body), true
		}
		return write, nil
	})
	// 没写成就回收刚复制的工艺。
	if err != nil {
		// 回收失败不盖住真因。
		s.recyclePersonalCopies(ctx, acc, adopted.made)
		return Asset{}, err
	}
	// 把改写后的正文和新工艺告诉调用方。
	return adopted.stamp(row), nil
}
