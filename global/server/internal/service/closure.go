package service

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/store"
)

// memberFromAsset 把平台级资产收成组包成员。
func memberFromAsset(a Asset) ClosureMember {
	return ClosureMember{
		ID: a.ID, Kind: a.Kind, Level: a.Level, Name: a.Name, Code: a.Code, Status: a.Status,
		Copyable: a.Copyable, WeldKind: a.WeldKind, Revision: a.Revision, Content: a.Content, Digest: a.Digest, Deps: a.Deps,
	}
}

// sealClosure 把根和成员收成闭包并算总摘要。
func sealClosure(root ClosureMember, rest []ClosureMember, factoryID uuid.UUID) ClosureSnapshot {
	// 按数量先准备容器。
	members := make([]ClosureMember, 0, 1+len(rest))
	// 把这一项接进结果。
	members = append(members, root)
	// 把这一项接进结果。
	members = append(members, rest...)
	// 按数量先准备容器。
	parts := make([]digest.Member, len(members))
	// 逐个成员核对或封装，一个坏就整包拒绝。
	for i, m := range members {
		// 收进身份、修订、摘要和正文，用来算总摘要。
		parts[i] = digest.Member{ID: m.ID, Revision: m.Revision, Digest: m.Digest, Content: m.Content}
	}
	// 记下工厂身份，授权和审计都按这家厂。
	fid := factoryID
	// 按成员身份、修订、摘要和正文算总摘要。
	return ClosureSnapshot{
		Kind: root.Kind, AssetID: root.ID, Revision: root.Revision, Level: root.Level,
		Copyable: root.Copyable, Status: root.Status, TargetFactoryID: &fid,
		Members: members, Digest: digest.ClosureSum(parts),
	}
}

// validateClosure 核成员正文摘要、闭包总摘要和依赖是否齐。
func validateClosure(snap ClosureSnapshot) error {
	// 空的就按没有处理，避免交出空壳当成功。
	if len(snap.Members) == 0 {
		return domain.ErrClosureIncomplete
	}
	// 逐个成员核对，缺一个就不算完整。
	for _, m := range snap.Members {
		// 每条成员正文都要对上自己的摘要。
		if !digest.Match(m.Content, m.Digest) {
			return domain.ErrIntegrity
		}
	}
	// 按数量先准备容器。
	parts := make([]digest.Member, len(snap.Members))
	// 逐个成员核对，缺一个就不算完整。
	for i, m := range snap.Members {
		// 收进身份、修订、摘要和正文，用来算总摘要。
		parts[i] = digest.Member{ID: m.ID, Revision: m.Revision, Digest: m.Digest, Content: m.Content}
	}
	// 总摘要对不上当篡改。
	if !bytes.Equal(digest.ClosureSum(parts), snap.Digest) {
		return domain.ErrIntegrity
	}
	// 第一条当根，后面的是钉住的成员。
	root := snap.Members[0]
	// 不是当前可用的平台级工艺就不能当依赖。
	if snap.AssetID != root.ID || snap.Revision != root.Revision || snap.Kind != root.Kind {
		return domain.ErrClosureMismatch
	}
	// 工艺闭包只能有自己；工程须带齐依赖且顺序对上。
	if snap.Kind == KindProcess {
		// 条件不满足则拒绝，避免把错状态写进去。
		if len(snap.Members) != 1 {
			return domain.ErrClosureMismatch
		}
		return nil
	}
	// 按是不是工程决定要不要核对焊道和依赖。
	if snap.Kind != KindProject || root.Kind != KindProject {
		return domain.ErrClosureMismatch
	}
	// 条件不满足则拒绝，避免把错状态写进去。
	if len(snap.Members) != 1+len(root.Deps) {
		// 条件不满足则拒绝，避免把错状态写进去。
		if len(snap.Members) < 1+len(root.Deps) {
			return domain.ErrClosureIncomplete
		}
		return domain.ErrClosureMismatch
	}
	// 逐条核对根上的依赖，修订必须对齐。
	for i, d := range root.Deps {
		// 按依赖顺序取下一个成员来核对。
		m := snap.Members[i+1]
		// 条件不满足则拒绝，避免把错状态写进去。
		if m.ID != d.ID || m.Revision != d.Revision {
			return domain.ErrClosureMismatch
		}
		// 摘要相同就不必覆盖，避免无谓升高修订。
		if !bytes.Equal(m.Digest, d.Digest) {
			return domain.ErrClosureMismatch
		}
	}
	return nil
}

// 审计对象：根、成员身份加目标厂。
func closureTarget(snap ClosureSnapshot) string {
	// 拼上身份和修订。
	t := assetTarget(snap.AssetID, snap.Revision)
	// 逐个成员核对，缺一个就不算完整。
	for _, m := range snap.Members {
		// 条件不满足则拒绝，避免把错状态写进去。
		if m.ID == snap.AssetID {
			continue
		}
		// 拼上身份和修订。
		t += " " + assetTarget(m.ID, m.Revision)
	}
	// 还没有准备好就停，避免空着往下用。
	if snap.TargetFactoryID != nil {
		// 收成文本给审计或指令用。
		t += " factory=" + snap.TargetFactoryID.String()
	}
	return t
}

// packPlatform 组可用或停用平台级闭包；草稿不能组。
func (s *Closure) packPlatform(ctx context.Context, root Asset) (ClosureSnapshot, error) {
	// 草稿不能组包；停用条仍可补送。
	if root.Status != AssetAvailable && root.Status != AssetDisabled {
		return ClosureSnapshot{}, domain.ErrAssetNotAvailable
	}
	// 按数量先准备容器。
	rest := make([]ClosureMember, 0, len(root.Deps))
	// 逐条核对根上的依赖，修订必须对齐。
	for _, d := range root.Deps {
		// 装入并核对摘要。
		p, err := s.loadChecked(ctx, d.ID)
		// 不符就不能把这条拿去用。
		if err != nil {
			// 没有这条就按不存在处理，不当成别的故障。
			if errors.Is(err, domain.ErrNotFound) {
				return ClosureSnapshot{}, domain.ErrClosureIncomplete
			}
			return ClosureSnapshot{}, err
		}
		// 不是工艺就拒绝，这项只对工艺开放。
		if p.Kind != KindProcess {
			return ClosureSnapshot{}, domain.ErrClosureMismatch
		}
		// 不是当前可用的平台级工艺就不能当依赖。
		if p.Revision != d.Revision {
			return ClosureSnapshot{}, domain.ErrClosureIncomplete
		}
		// 摘要相同就不必覆盖，避免无谓升高修订。
		if !bytes.Equal(p.Digest, d.Digest) {
			return ClosureSnapshot{}, domain.ErrClosureMismatch
		}
		// 把这一项接进结果。
		rest = append(rest, memberFromAsset(p))
	}
	// 收成封闭包并算总摘要。
	snap := sealClosure(memberFromAsset(root), rest, uuid.Nil)
	// 平台包先不指定厂，授权时再填上。
	snap.TargetFactoryID = nil
	// 不齐全就拒绝，残包不能下发。
	if err := validateClosure(snap); err != nil {
		return ClosureSnapshot{}, err
	}
	// 把目录位置挂上。
	s.attachFSPaths(ctx, &snap)
	return snap, nil
}

// attachFSPaths 把当前目录挂点写进闭包，不进摘要。
func (s *Closure) attachFSPaths(ctx context.Context, snap *ClosureSnapshot) {
	// 逐个成员核对，缺一个就不算完整。
	for i := range snap.Members {
		// 按目录节点处理。
		parent, folders, err := s.store.FSLineage(ctx, snap.Members[i].ID)
		// 没有这个节点或会成环就拒绝。
		if err != nil {
			continue
		}
		// 写上目录父节点，不参与闭包摘要。
		snap.Members[i].FSParentID = parent
		// 写上文件夹链，厂端好挂到同样位置。
		snap.Members[i].FSPath = folders
	}
}

// GrantFactoryAsset 授权某厂接收一条平台级资产。
func (s *Closure) GrantFactoryAsset(ctx context.Context, token string, assetID, factoryID uuid.UUID) error {
	// 只有 WAN 管理员能授权工厂接收。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		return err
	}
	// 收成文本给审计或指令用。
	target := assetID.String() + " factory=" + factoryID.String()
	// 不符就不能把这条拿去用。
	if _, err := s.loadChecked(ctx, assetID); err != nil {
		// 授权被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "grant_closure", target, audit.Deny)
		return err
	}
	// 记下该厂可收这条平台级。
	if _, err := s.store.UpsertFactoryGrant(ctx, assetID, factoryID); err != nil {
		// 授权被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "grant_closure", target, audit.Deny)
		return err
	}
	// 授权成功才记允许。
	return s.audit(ctx, &admin.ID, nil, &factoryID, "grant_closure", target, audit.Allow)
}

// RevokeFactoryAsset 收回某厂接收该平台级资产的授权。
func (s *Closure) RevokeFactoryAsset(ctx context.Context, token string, assetID, factoryID uuid.UUID) error {
	// 只有 WAN 管理员能收回授权。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		return err
	}
	// 收成文本给审计或指令用。
	target := assetID.String() + " factory=" + factoryID.String()
	// 收回后该厂不再接收这条。
	if err := s.store.RevokeFactoryGrant(ctx, assetID, factoryID); err != nil {
		// 收回被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "revoke_closure", target, audit.Deny)
		return err
	}
	// 收回成功才记允许。
	return s.audit(ctx, &admin.ID, nil, &factoryID, "revoke_closure", target, audit.Allow)
}

// DistributeToFactory 把可用平台级工艺或工程闭包下发到已授权工厂。
func (s *Closure) DistributeToFactory(ctx context.Context, token string, assetID, factoryID uuid.UUID) (ClosureSnapshot, error) {
	// 只有 WAN 管理员能下发；未授权或不可用都拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 装入并核对摘要。
	root, err := s.loadChecked(ctx, assetID)
	// 不符就不能把这条拿去用。
	if err != nil {
		// 下发被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_closure", assetID.String()+" factory="+factoryID.String(), audit.Deny)
		return ClosureSnapshot{}, err
	}
	// 不是可用就拒绝，草稿和停用不能当发布。
	if root.Status != AssetAvailable {
		// 下发被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_closure", assetID.String()+" factory="+factoryID.String(), audit.Deny)
		return ClosureSnapshot{}, domain.ErrAssetNotAvailable
	}
	// 做完这一步再继续。
	snap, err := s.deliverToFactory(ctx, assetID, factoryID)
	// 这一步失败就停，避免留下半截。
	if err != nil {
		// 下发被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_closure", assetID.String()+" factory="+factoryID.String(), audit.Deny)
		return ClosureSnapshot{}, err
	}
	// 下发成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_closure", closureTarget(snap), audit.Allow); err != nil {
		return ClosureSnapshot{}, err
	}
	return snap, nil
}

// deliverToFactory 已授权才组包并记下发记录。
func (s *Closure) deliverToFactory(ctx context.Context, assetID, factoryID uuid.UUID) (ClosureSnapshot, error) {
	// 查该厂是否仍有接收权。
	grant, err := s.store.FactoryGrant(ctx, assetID, factoryID)
	// 没有这家厂或状态不对就拒绝。
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 未授权或已收回则不下发。
	if !grant.Active {
		return ClosureSnapshot{}, domain.ErrForbidden
	}
	// 装入并核对摘要。
	root, err := s.loadChecked(ctx, assetID)
	// 不符就不能把这条拿去用。
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 组好这一包，失败就不能继续。
	snap, err := s.packPlatform(ctx, root)
	// 成员不齐就拒绝，残包不能下发。
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 记下工厂身份，授权和审计都按这家厂。
	fid := factoryID
	// 这包只给这家厂，别的厂不能拆。
	snap.TargetFactoryID = &fid
	// 按数量先准备容器。
	members := make([]AssetDep, 0, len(snap.Members))
	// 逐个成员核对，缺一个就不算完整。
	for _, m := range snap.Members {
		// 把这一项接进结果。
		members = append(members, AssetDep{ID: m.ID, Revision: m.Revision, Digest: m.Digest})
	}
	// 记下这次下发的闭包摘要和成员。
	if _, err := s.store.InsertDistributionRecord(ctx, store.DistributionRecord{
		// 记下资产、修订和接收厂，方便以后对账。
		AssetID: snap.AssetID, Revision: snap.Revision, FactoryID: factoryID,
		Kind: snap.Kind, ClosureDigest: snap.Digest, Members: members,
	}); err != nil {
		return ClosureSnapshot{}, err
	}
	return snap, nil
}

// PackAssetForFactory 把这一条平台级授权并组包给该厂；草稿不组。可用条自动授权，停用条只补已授权厂。
func (s *Closure) PackAssetForFactory(ctx context.Context, assetID, factoryID uuid.UUID) (ClosureSnapshot, error) {
	// 按工厂名录处理。
	fac, err := s.store.FactoryByID(ctx, factoryID)
	// 没有这家厂或状态不对就拒绝。
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 只给有效厂组包；停用厂上线前不推。
	if fac.Status != FactoryActive {
		return ClosureSnapshot{}, nil
	}
	// 装入并核对摘要。
	a, err := s.loadChecked(ctx, assetID)
	// 不符就不能把这条拿去用。
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 组好这一包，失败就不能继续。
	return s.packOneForFactory(ctx, a, factoryID)
}

// PackAvailableForFactory 把当前可用平台级授权并组包给该厂；离线厂上线回放也走这里。已授权的停用条一并补送。
func (s *Closure) PackAvailableForFactory(ctx context.Context, factoryID uuid.UUID) ([]ClosureSnapshot, error) {
	// 组好这一包，失败就不能继续。
	return s.packAvailable(ctx, factoryID, "")
}

// PackKindForFactory 只组该类型当前该给本厂的平台级，供厂端进页补拉。
func (s *Closure) PackKindForFactory(ctx context.Context, factoryID uuid.UUID, kind string) ([]ClosureSnapshot, error) {
	// 组好这一包，失败就不能继续。
	return s.packAvailable(ctx, factoryID, kind)
}

// 按类型列出当前该给该厂的平台级并组包；kind 空则工艺和工程都组。
func (s *Closure) packAvailable(ctx context.Context, factoryID uuid.UUID, kind string) ([]ClosureSnapshot, error) {
	// 按工厂名录处理。
	fac, err := s.store.FactoryByID(ctx, factoryID)
	// 没有这家厂或状态不对就拒绝。
	if err != nil {
		return nil, err
	}
	// 只给有效厂组包；停用厂上线前不推。
	if fac.Status != FactoryActive {
		return nil, nil
	}
	// 把当前平台级逐条授权并组包。
	rows, err := s.store.ListAssets(ctx)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return nil, err
	}
	// 先留空，授权通过的包再放进来。
	out := []ClosureSnapshot{}
	// 逐行整理，坏的一行就整批拒绝。
	for _, a := range rows {
		// 空和有值走不同路，避免把空白写进名录。
		if kind != "" && a.Kind != kind {
			continue
		}
		// 组好这一包，失败就不能继续。
		snap, err := s.packOneForFactory(ctx, a, factoryID)
		// 成员不齐就拒绝，残包不能下发。
		if err != nil {
			return nil, err
		}
		// 没有组出包就按不存在，草稿不会下发。
		if snap.AssetID == uuid.Nil {
			continue
		}
		// 把这一项接进结果。
		out = append(out, snap)
	}
	return out, nil
}

// 组这一条给该厂；草稿跳过，组失败不打断其它条。
func (s *Closure) packOneForFactory(ctx context.Context, a Asset, factoryID uuid.UUID) (ClosureSnapshot, error) {
	// 按当前状态决定能否改、发或下发。
	switch a.Status {
	// 可用才允许继续下发或变更。
	case AssetAvailable:
		// 可用条自动授权。
		if _, err := s.store.UpsertFactoryGrant(ctx, a.ID, factoryID); err != nil {
			return ClosureSnapshot{}, err
		}
	// 已停用则拒绝改正文或依赖。
	case AssetDisabled:
		// 停用条只补送已授权的。
		grant, err := s.store.FactoryGrant(ctx, a.ID, factoryID)
		// 授权不在或查询失败则不下发。
		if err != nil || !grant.Active {
			return ClosureSnapshot{}, nil
		}
	// 其余情况走这里，避免漏掉一种状态。
	default:
		return ClosureSnapshot{}, nil
	}
	// 查或记下这次下发。
	_, recErr := s.store.DistributionRecord(ctx, a.ID, a.Revision, factoryID)
	// 不是没有这条，就当真正的故障返回。
	if recErr != nil && !errors.Is(recErr, domain.ErrNotFound) {
		return ClosureSnapshot{}, recErr
	}
	// 装入并核对摘要。
	root, err := s.loadChecked(ctx, a.ID)
	// 不符就不能把这条拿去用。
	if err != nil {
		// 下发被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "distribute_closure", a.ID.String()+" factory="+factoryID.String(), audit.Deny)
		return ClosureSnapshot{}, nil
	}
	// 组好这一包，失败就不能继续。
	snap, err := s.packPlatform(ctx, root)
	// 成员不齐就拒绝，残包不能下发。
	if err != nil {
		// 下发被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "distribute_closure", a.ID.String()+" factory="+factoryID.String(), audit.Deny)
		return ClosureSnapshot{}, nil
	}
	// 记下工厂身份，授权和审计都按这家厂。
	fid := factoryID
	// 这包只给这家厂，别的厂不能拆。
	snap.TargetFactoryID = &fid
	// 同一修订再组不插记录、不记新审计，避免进页补拉刷屏。
	if recErr == nil {
		return snap, nil
	}
	// 按数量先准备容器。
	members := make([]AssetDep, 0, len(snap.Members))
	// 逐个成员核对，缺一个就不算完整。
	for _, m := range snap.Members {
		// 把这一项接进结果。
		members = append(members, AssetDep{ID: m.ID, Revision: m.Revision, Digest: m.Digest})
	}
	// 条件不满足则拒绝，避免把错状态写进去。
	if _, err := s.store.InsertDistributionRecord(ctx, store.DistributionRecord{
		// 记下资产、修订和接收厂，方便以后对账。
		AssetID: snap.AssetID, Revision: snap.Revision, FactoryID: factoryID,
		Kind: snap.Kind, ClosureDigest: snap.Digest, Members: members,
	}); err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "distribute_closure", a.ID.String()+" factory="+factoryID.String(), audit.Deny)
		return ClosureSnapshot{}, nil
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, nil, nil, &factoryID, "distribute_closure", closureTarget(snap), audit.Allow); err != nil {
		return ClosureSnapshot{}, err
	}
	return snap, nil
}

// ListRetractions 列出已删除、须补送给厂的平台级身份。
func (s *Closure) ListRetractions(ctx context.Context) ([]uuid.UUID, error) {
	// 列出这一批供后面筛选。
	return s.store.ListRetractions(ctx)
}

// SetPlatformProjectDeps 显式改平台级工程依赖并升高修订。
func (s *Closure) SetPlatformProjectDeps(ctx context.Context, token string, assetID uuid.UUID, expected int64, deps []AssetDep) (Asset, error) {
	// 按当前行改写；修订不符就拒绝。
	return s.mutatePlatform(ctx, token, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		// 按是不是工程决定要不要核对焊道和依赖。
		if cur.Kind != KindProject {
			return store.AssetWrite{}, domain.ErrAssetDependency
		}
		// 停用后不得改依赖。
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		// 做完这一步再继续。
		deps, err := s.resolvePlatformProjectDeps(ctx, deps)
		// 这一步失败就停，避免留下半截。
		if err != nil {
			return store.AssetWrite{}, err
		}
		// 对不上就拒绝，避免焊道和模式错配。
		if err := s.assertDepsWeldKind(ctx, cur.WeldKind, deps); err != nil {
			return store.AssetWrite{}, err
		}
		// 改依赖后，当前模版下的旧引用仍须落在新 deps 里。
		if err := s.assertProjectProcessIDs(ctx, cur.Content, deps); err != nil {
			return store.AssetWrite{}, err
		}
		// 按数量先准备容器。
		out := make([]AssetDep, len(deps))
		// 复制一份，原件保持不动。
		copy(out, deps)
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: cur.Status, Deps: out}, nil
	})
}

// HasDistributedTo 夹具查询是否曾向该厂下发过该资产。
func (s *Closure) HasDistributedTo(ctx context.Context, assetID, factoryID uuid.UUID) (bool, error) {
	// 看是否已经具备。
	return s.store.HasDistributionTo(ctx, assetID, factoryID)
}
