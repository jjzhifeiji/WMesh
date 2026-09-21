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
	members := make([]ClosureMember, 0, 1+len(rest))
	members = append(members, root)
	members = append(members, rest...)
	parts := make([]digest.Member, len(members))
	for i, m := range members {
		parts[i] = digest.Member{ID: m.ID, Revision: m.Revision, Digest: m.Digest, Content: m.Content}
	}
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
	if len(snap.Members) == 0 {
		return domain.ErrClosureIncomplete
	}
	for _, m := range snap.Members {
		// 每条成员正文都要对上自己的摘要。
		if !digest.Match(m.Content, m.Digest) {
			return domain.ErrIntegrity
		}
	}
	parts := make([]digest.Member, len(snap.Members))
	for i, m := range snap.Members {
		parts[i] = digest.Member{ID: m.ID, Revision: m.Revision, Digest: m.Digest, Content: m.Content}
	}
	// 总摘要对不上当篡改。
	if !bytes.Equal(digest.ClosureSum(parts), snap.Digest) {
		return domain.ErrIntegrity
	}
	root := snap.Members[0]
	if snap.AssetID != root.ID || snap.Revision != root.Revision || snap.Kind != root.Kind {
		return domain.ErrClosureMismatch
	}
	// 工艺闭包只能有自己；工程须带齐依赖且顺序对上。
	if snap.Kind == KindProcess {
		if len(snap.Members) != 1 {
			return domain.ErrClosureMismatch
		}
		return nil
	}
	if snap.Kind != KindProject || root.Kind != KindProject {
		return domain.ErrClosureMismatch
	}
	if len(snap.Members) != 1+len(root.Deps) {
		if len(snap.Members) < 1+len(root.Deps) {
			return domain.ErrClosureIncomplete
		}
		return domain.ErrClosureMismatch
	}
	for i, d := range root.Deps {
		m := snap.Members[i+1]
		if m.ID != d.ID || m.Revision != d.Revision {
			return domain.ErrClosureMismatch
		}
		if !bytes.Equal(m.Digest, d.Digest) {
			return domain.ErrClosureMismatch
		}
	}
	return nil
}

// 审计对象：根、成员身份加目标厂。
func closureTarget(snap ClosureSnapshot) string {
	t := assetTarget(snap.AssetID, snap.Revision)
	for _, m := range snap.Members {
		if m.ID == snap.AssetID {
			continue
		}
		t += " " + assetTarget(m.ID, m.Revision)
	}
	if snap.TargetFactoryID != nil {
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
	rest := make([]ClosureMember, 0, len(root.Deps))
	for _, d := range root.Deps {
		p, err := s.loadChecked(ctx, d.ID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return ClosureSnapshot{}, domain.ErrClosureIncomplete
			}
			return ClosureSnapshot{}, err
		}
		if p.Kind != KindProcess {
			return ClosureSnapshot{}, domain.ErrClosureMismatch
		}
		if p.Revision != d.Revision {
			return ClosureSnapshot{}, domain.ErrClosureIncomplete
		}
		if !bytes.Equal(p.Digest, d.Digest) {
			return ClosureSnapshot{}, domain.ErrClosureMismatch
		}
		rest = append(rest, memberFromAsset(p))
	}
	snap := sealClosure(memberFromAsset(root), rest, uuid.Nil)
	snap.TargetFactoryID = nil
	if err := validateClosure(snap); err != nil {
		return ClosureSnapshot{}, err
	}
	s.attachFSPaths(ctx, &snap)
	return snap, nil
}

// attachFSPaths 把当前目录挂点写进闭包，不进摘要。
func (s *Closure) attachFSPaths(ctx context.Context, snap *ClosureSnapshot) {
	for i := range snap.Members {
		parent, folders, err := s.store.FSLineage(ctx, snap.Members[i].ID)
		if err != nil {
			continue
		}
		snap.Members[i].FSParentID = parent
		snap.Members[i].FSPath = folders
	}
}

// GrantFactoryAsset 授权某厂接收一条平台级资产。
func (s *Closure) GrantFactoryAsset(ctx context.Context, token string, assetID, factoryID uuid.UUID) error {
	// 只有 WAN 管理员能授权工厂接收。
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return err
	}
	target := assetID.String() + " factory=" + factoryID.String()
	if _, err := s.loadChecked(ctx, assetID); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "grant_closure", target, audit.Deny)
		return err
	}
	// 记下该厂可收这条平台级。
	if _, err := s.store.UpsertFactoryGrant(ctx, assetID, factoryID); err != nil {
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
	if err != nil {
		return err
	}
	target := assetID.String() + " factory=" + factoryID.String()
	// 收回后该厂不再接收这条。
	if err := s.store.RevokeFactoryGrant(ctx, assetID, factoryID); err != nil {
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
	if err != nil {
		return ClosureSnapshot{}, err
	}
	root, err := s.loadChecked(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_closure", assetID.String()+" factory="+factoryID.String(), audit.Deny)
		return ClosureSnapshot{}, err
	}
	if root.Status != AssetAvailable {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_closure", assetID.String()+" factory="+factoryID.String(), audit.Deny)
		return ClosureSnapshot{}, domain.ErrAssetNotAvailable
	}
	snap, err := s.deliverToFactory(ctx, assetID, factoryID)
	if err != nil {
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
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 未授权或已收回则不下发。
	if !grant.Active {
		return ClosureSnapshot{}, domain.ErrForbidden
	}
	root, err := s.loadChecked(ctx, assetID)
	if err != nil {
		return ClosureSnapshot{}, err
	}
	snap, err := s.packPlatform(ctx, root)
	if err != nil {
		return ClosureSnapshot{}, err
	}
	fid := factoryID
	snap.TargetFactoryID = &fid
	members := make([]AssetDep, 0, len(snap.Members))
	for _, m := range snap.Members {
		members = append(members, AssetDep{ID: m.ID, Revision: m.Revision, Digest: m.Digest})
	}
	// 记下这次下发的闭包摘要和成员。
	if _, err := s.store.InsertDistributionRecord(ctx, store.DistributionRecord{
		AssetID: snap.AssetID, Revision: snap.Revision, FactoryID: factoryID,
		Kind: snap.Kind, ClosureDigest: snap.Digest, Members: members,
	}); err != nil {
		return ClosureSnapshot{}, err
	}
	return snap, nil
}

// PackAssetForFactory 把这一条平台级授权并组包给该厂；草稿不组。可用条自动授权，停用条只补已授权厂。
func (s *Closure) PackAssetForFactory(ctx context.Context, assetID, factoryID uuid.UUID) (ClosureSnapshot, error) {
	fac, err := s.store.FactoryByID(ctx, factoryID)
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 只给有效厂组包；停用厂上线前不推。
	if fac.Status != FactoryActive {
		return ClosureSnapshot{}, nil
	}
	a, err := s.loadChecked(ctx, assetID)
	if err != nil {
		return ClosureSnapshot{}, err
	}
	return s.packOneForFactory(ctx, a, factoryID)
}

// PackAvailableForFactory 把当前可用平台级授权并组包给该厂；离线厂上线回放也走这里。已授权的停用条一并补送。
func (s *Closure) PackAvailableForFactory(ctx context.Context, factoryID uuid.UUID) ([]ClosureSnapshot, error) {
	return s.packAvailable(ctx, factoryID, "")
}

// PackKindForFactory 只组该类型当前该给本厂的平台级，供厂端进页补拉。
func (s *Closure) PackKindForFactory(ctx context.Context, factoryID uuid.UUID, kind string) ([]ClosureSnapshot, error) {
	return s.packAvailable(ctx, factoryID, kind)
}

// 按类型列出当前该给该厂的平台级并组包；kind 空则工艺和工程都组。
func (s *Closure) packAvailable(ctx context.Context, factoryID uuid.UUID, kind string) ([]ClosureSnapshot, error) {
	fac, err := s.store.FactoryByID(ctx, factoryID)
	if err != nil {
		return nil, err
	}
	// 只给有效厂组包；停用厂上线前不推。
	if fac.Status != FactoryActive {
		return nil, nil
	}
	// 把当前平台级逐条授权并组包。
	rows, err := s.store.ListAssets(ctx)
	if err != nil {
		return nil, err
	}
	out := []ClosureSnapshot{}
	for _, a := range rows {
		if kind != "" && a.Kind != kind {
			continue
		}
		snap, err := s.packOneForFactory(ctx, a, factoryID)
		if err != nil {
			return nil, err
		}
		if snap.AssetID == uuid.Nil {
			continue
		}
		out = append(out, snap)
	}
	return out, nil
}

// 组这一条给该厂；草稿跳过，组失败不打断其它条。
func (s *Closure) packOneForFactory(ctx context.Context, a Asset, factoryID uuid.UUID) (ClosureSnapshot, error) {
	switch a.Status {
	case AssetAvailable:
		// 可用条自动授权。
		if _, err := s.store.UpsertFactoryGrant(ctx, a.ID, factoryID); err != nil {
			return ClosureSnapshot{}, err
		}
	case AssetDisabled:
		// 停用条只补送已授权的。
		grant, err := s.store.FactoryGrant(ctx, a.ID, factoryID)
		if err != nil || !grant.Active {
			return ClosureSnapshot{}, nil
		}
	default:
		return ClosureSnapshot{}, nil
	}
	_, recErr := s.store.DistributionRecord(ctx, a.ID, a.Revision, factoryID)
	if recErr != nil && !errors.Is(recErr, domain.ErrNotFound) {
		return ClosureSnapshot{}, recErr
	}
	root, err := s.loadChecked(ctx, a.ID)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "distribute_closure", a.ID.String()+" factory="+factoryID.String(), audit.Deny)
		return ClosureSnapshot{}, nil
	}
	snap, err := s.packPlatform(ctx, root)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "distribute_closure", a.ID.String()+" factory="+factoryID.String(), audit.Deny)
		return ClosureSnapshot{}, nil
	}
	fid := factoryID
	snap.TargetFactoryID = &fid
	// 同一修订再组不插记录、不记新审计，避免进页补拉刷屏。
	if recErr == nil {
		return snap, nil
	}
	members := make([]AssetDep, 0, len(snap.Members))
	for _, m := range snap.Members {
		members = append(members, AssetDep{ID: m.ID, Revision: m.Revision, Digest: m.Digest})
	}
	if _, err := s.store.InsertDistributionRecord(ctx, store.DistributionRecord{
		AssetID: snap.AssetID, Revision: snap.Revision, FactoryID: factoryID,
		Kind: snap.Kind, ClosureDigest: snap.Digest, Members: members,
	}); err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "distribute_closure", a.ID.String()+" factory="+factoryID.String(), audit.Deny)
		return ClosureSnapshot{}, nil
	}
	if err := s.audit(ctx, nil, nil, &factoryID, "distribute_closure", closureTarget(snap), audit.Allow); err != nil {
		return ClosureSnapshot{}, err
	}
	return snap, nil
}

// ListRetractions 列出已删除、须补送给厂的平台级身份。
func (s *Closure) ListRetractions(ctx context.Context) ([]uuid.UUID, error) {
	return s.store.ListRetractions(ctx)
}

// SetPlatformProjectDeps 显式改平台级工程依赖并升高修订。
func (s *Closure) SetPlatformProjectDeps(ctx context.Context, token string, assetID uuid.UUID, expected int64, deps []AssetDep) (Asset, error) {
	return s.mutatePlatform(ctx, token, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Kind != KindProject {
			return store.AssetWrite{}, domain.ErrAssetDependency
		}
		// 停用后不得改依赖。
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		if err := s.assertPlatformProcessDeps(ctx, deps); err != nil {
			return store.AssetWrite{}, err
		}
		if err := s.assertDepsWeldKind(ctx, cur.WeldKind, deps); err != nil {
			return store.AssetWrite{}, err
		}
		// 改依赖后，当前模版下的旧引用仍须落在新 deps 里。
		if err := s.assertProjectProcessIDs(ctx, cur.Content, deps); err != nil {
			return store.AssetWrite{}, err
		}
		out := make([]AssetDep, len(deps))
		copy(out, deps)
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: cur.Status, Deps: out}, nil
	})
}

// HasDistributedTo 夹具查询是否曾向该厂下发过该资产。
func (s *Closure) HasDistributedTo(ctx context.Context, assetID, factoryID uuid.UUID) (bool, error) {
	return s.store.HasDistributionTo(ctx, assetID, factoryID)
}
