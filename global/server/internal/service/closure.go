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

func memberFromAsset(a Asset) ClosureMember {
	return ClosureMember{
		ID: a.ID, Kind: a.Kind, Level: a.Level, Name: a.Name, Status: a.Status,
		Copyable: a.Copyable, Revision: a.Revision, Content: a.Content, Digest: a.Digest, Deps: a.Deps,
	}
}

func sealClosure(root ClosureMember, rest []ClosureMember, factoryID uuid.UUID) ClosureSnapshot {
	members := make([]ClosureMember, 0, 1+len(rest))
	members = append(members, root)
	members = append(members, rest...)
	parts := make([]digest.Member, len(members))
	for i, m := range members {
		parts[i] = digest.Member{ID: m.ID, Revision: m.Revision, Digest: m.Digest, Content: m.Content}
	}
	fid := factoryID
	return ClosureSnapshot{
		Kind: root.Kind, AssetID: root.ID, Revision: root.Revision, Level: root.Level,
		Copyable: root.Copyable, Status: root.Status, TargetFactoryID: &fid,
		Members: members, Digest: digest.ClosureSum(parts),
	}
}

func validateClosure(snap ClosureSnapshot) error {
	if len(snap.Members) == 0 {
		return domain.ErrClosureIncomplete
	}
	for _, m := range snap.Members {
		if !digest.Match(m.Content, m.Digest) {
			return domain.ErrIntegrity
		}
	}
	parts := make([]digest.Member, len(snap.Members))
	for i, m := range snap.Members {
		parts[i] = digest.Member{ID: m.ID, Revision: m.Revision, Digest: m.Digest, Content: m.Content}
	}
	if !bytes.Equal(digest.ClosureSum(parts), snap.Digest) {
		return domain.ErrIntegrity
	}
	root := snap.Members[0]
	if snap.AssetID != root.ID || snap.Revision != root.Revision || snap.Kind != root.Kind {
		return domain.ErrClosureMismatch
	}
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

func (s *Closure) packPlatform(ctx context.Context, root Asset) (ClosureSnapshot, error) {
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
	return snap, nil
}

// GrantFactoryAsset 授权某厂接收一条平台级资产。
func (s *Closure) GrantFactoryAsset(ctx context.Context, token string, assetID, factoryID uuid.UUID) error {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return err
	}
	target := assetID.String() + " factory=" + factoryID.String()
	if _, err := s.loadChecked(ctx, assetID); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "grant_closure", target, audit.Deny)
		return err
	}
	if _, err := s.store.UpsertFactoryGrant(ctx, assetID, factoryID); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "grant_closure", target, audit.Deny)
		return err
	}
	return s.audit(ctx, &admin.ID, nil, &factoryID, "grant_closure", target, audit.Allow)
}

// RevokeFactoryAsset 收回某厂接收该平台级资产的授权。
func (s *Closure) RevokeFactoryAsset(ctx context.Context, token string, assetID, factoryID uuid.UUID) error {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return err
	}
	target := assetID.String() + " factory=" + factoryID.String()
	if err := s.store.RevokeFactoryGrant(ctx, assetID, factoryID); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "revoke_closure", target, audit.Deny)
		return err
	}
	return s.audit(ctx, &admin.ID, nil, &factoryID, "revoke_closure", target, audit.Allow)
}

// DistributeToFactory 把可用平台级工艺或工程闭包下发到已授权工厂。
func (s *Closure) DistributeToFactory(ctx context.Context, token string, assetID, factoryID uuid.UUID) (ClosureSnapshot, error) {
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
	if err := s.audit(ctx, &admin.ID, nil, &factoryID, "distribute_closure", closureTarget(snap), audit.Allow); err != nil {
		return ClosureSnapshot{}, err
	}
	return snap, nil
}

func (s *Closure) deliverToFactory(ctx context.Context, assetID, factoryID uuid.UUID) (ClosureSnapshot, error) {
	grant, err := s.store.FactoryGrant(ctx, assetID, factoryID)
	if err != nil {
		return ClosureSnapshot{}, err
	}
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
	if _, err := s.store.InsertDistributionRecord(ctx, store.DistributionRecord{
		AssetID: snap.AssetID, Revision: snap.Revision, FactoryID: factoryID,
		Kind: snap.Kind, ClosureDigest: snap.Digest, Members: members,
	}); err != nil {
		return ClosureSnapshot{}, err
	}
	return snap, nil
}

// PackAvailableForFactory 把当前可用平台级授权并组包给该厂；已授权的停用条一并补送。
func (s *Closure) PackAvailableForFactory(ctx context.Context, factoryID uuid.UUID) ([]ClosureSnapshot, error) {
	fac, err := s.store.FactoryByID(ctx, factoryID)
	if err != nil {
		return nil, err
	}
	if fac.Status != FactoryActive {
		return nil, nil
	}
	rows, err := s.store.ListAssets(ctx)
	if err != nil {
		return nil, err
	}
	out := []ClosureSnapshot{}
	for _, a := range rows {
		switch a.Status {
		case AssetAvailable:
			if _, err := s.store.UpsertFactoryGrant(ctx, a.ID, factoryID); err != nil {
				return nil, err
			}
		case AssetDisabled:
			grant, err := s.store.FactoryGrant(ctx, a.ID, factoryID)
			if err != nil || !grant.Active {
				continue
			}
		default:
			continue
		}
		snap, err := s.deliverToFactory(ctx, a.ID, factoryID)
		if err != nil {
			_ = s.audit(ctx, nil, nil, &factoryID, "distribute_closure", a.ID.String()+" factory="+factoryID.String(), audit.Deny)
			continue
		}
		if err := s.audit(ctx, nil, nil, &factoryID, "distribute_closure", closureTarget(snap), audit.Allow); err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, nil
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
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		if err := s.assertPlatformProcessDeps(ctx, deps); err != nil {
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
