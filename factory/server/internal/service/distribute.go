package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// AcceptPlatformDelivery 夹具把 WAN 送达的平台级闭包写入只读副本。
func (s *Closure) AcceptPlatformDelivery(ctx context.Context, snap ClosureSnapshot) error {
	if err := validateClosure(snap); err != nil {
		_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
		return err
	}
	fid := s.store.FactoryID()
	if snap.TargetFactoryID == nil || *snap.TargetFactoryID != fid {
		_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
		return domain.ErrForbidden
	}
	for _, m := range snap.Members {
		if m.Level != AssetLevelPlatform || m.Copyable {
			_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
			return domain.ErrForbidden
		}
		if _, err := s.store.InsertReplica(ctx, AssetReplica{
			ID: m.ID, Revision: m.Revision, Kind: m.Kind, Level: AssetLevelPlatform,
			Name: m.Name, Status: m.Status, Content: m.Content, Digest: m.Digest, Deps: m.Deps,
		}); err != nil {
			_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
			return err
		}
	}
	return s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Allow)
}

// GrantClientProject 由本厂超管授权已绑定 Client 接收某份可用工程。
func (s *Closure) GrantClientProject(ctx context.Context, token string, projectID, clientID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	target := projectID.String() + " client=" + clientID.String()
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "grant_closure", target, audit.Deny)
		return err
	}
	if err := s.assertGrantableProject(ctx, projectID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "grant_closure", target, audit.Deny)
		return err
	}
	cl, err := s.store.ClientByID(ctx, clientID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "grant_closure", target, audit.Deny)
		return err
	}
	if cl.Status != ClientStatusBound {
		_ = s.audit(ctx, &acc.ID, nil, "grant_closure", target, audit.Deny)
		return domain.ErrBindingVoid
	}
	if _, err := s.store.UpsertClientGrant(ctx, projectID, clientID, acc.ID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "grant_closure", target, audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "grant_closure", target, audit.Allow)
}

// RevokeClientProject 收回 Client 接收该工程的授权。
func (s *Closure) RevokeClientProject(ctx context.Context, token string, projectID, clientID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	target := projectID.String() + " client=" + clientID.String()
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "revoke_closure", target, audit.Deny)
		return err
	}
	if err := s.store.RevokeClientGrant(ctx, projectID, clientID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "revoke_closure", target, audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "revoke_closure", target, audit.Allow)
}

func (s *Closure) assertGrantableProject(ctx context.Context, projectID uuid.UUID) error {
	a, err := s.store.GovernedAssetByID(ctx, projectID)
	if err == nil {
		if a.Kind != KindProject {
			return domain.ErrForbidden
		}
		if a.Level == AssetLevelPersonal {
			return domain.ErrForbidden
		}
		if a.Status != AssetAvailable {
			return domain.ErrAssetNotAvailable
		}
		return nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	r, err := s.store.LatestReplica(ctx, projectID)
	if err != nil {
		return err
	}
	if r.Kind != KindProject || r.Status != AssetAvailable {
		return domain.ErrForbidden
	}
	return nil
}

// SetCacheLimit 由本厂超管设定每 Client 工程缓存上限。
func (s *Closure) SetCacheLimit(ctx context.Context, token string, n int) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "set_cache_limit", "max_cached_projects", audit.Deny)
		return err
	}
	if err := s.store.SetCacheLimit(ctx, n); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "set_cache_limit", "max_cached_projects", audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "set_cache_limit", "max_cached_projects", audit.Allow)
}

// DistributeToClient 由有权工艺工程师把工程闭包写入已授权本机袋。
func (s *Closure) DistributeToClient(ctx context.Context, token string, projectID, clientID uuid.UUID, bag *Bag, clocks Clocks) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	target := projectID.String() + " client=" + clientID.String()
	root, err := s.loadRootForPack(ctx, projectID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	if root.Level == AssetLevelPersonal {
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return domain.ErrForbidden
	}
	if err := s.canPack(ctx, acc, root); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	grant, err := s.store.ClientGrant(ctx, projectID, clientID)
	if err != nil || !grant.Active {
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		if err != nil {
			return err
		}
		return domain.ErrForbidden
	}
	cl, err := s.store.ClientByID(ctx, clientID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	if cl.Status != ClientStatusBound {
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return domain.ErrBindingVoid
	}
	if err := s.assertClientRuntime(ctx, clientID, clocks); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	snap, err := s.packFromMember(ctx, root)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	cid := clientID
	snap.TargetClientID = &cid
	if bag.ClientID != clientID {
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return domain.ErrForbidden
	}
	if err := s.putCached(ctx, bag, snap); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	if _, err := s.store.InsertClientRecord(ctx, store.ClientDistributionRecord{
		ProjectID: snap.AssetID, Revision: snap.Revision, ClientID: clientID,
		ClosureDigest: snap.Digest, Members: recordMembers(snap),
	}); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "distribute_closure", closureTarget(snap), audit.Allow)
}

func (s *Closure) assertClientRuntime(ctx context.Context, clientID uuid.UUID, clocks Clocks) error {
	g, err := s.store.LatestRuntimeGrant(ctx, clientID)
	if err != nil {
		return err
	}
	now := clocks.Server
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !g.CanRun || now.Before(g.NotBefore) || now.After(g.NotAfter) {
		return domain.ErrForbidden
	}
	return nil
}

// CachePersonalProject 创建人把自己的可用个人级工程装进已持人员授权的本机袋。
func (s *Closure) CachePersonalProject(ctx context.Context, token string, projectID, clientID uuid.UUID, bag *Bag, clocks Clocks) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	target := projectID.String() + " client=" + clientID.String()
	a, err := s.loadChecked(ctx, projectID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return err
	}
	if a.Kind != KindProject || a.Level != AssetLevelPersonal || a.CreatorID != acc.ID {
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return domain.ErrForbidden
	}
	pg, err := s.store.LatestPersonOfflineGrant(ctx, acc.ID, clientID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return err
	}
	now := clocks.Server
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if now.Before(pg.NotBefore) || now.After(pg.NotAfter) {
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return domain.ErrForbidden
	}
	if bag.ClientID != clientID {
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return domain.ErrForbidden
	}
	snap, err := s.packFromMember(ctx, memberFromAsset(a))
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return err
	}
	cid := clientID
	snap.TargetClientID = &cid
	if err := s.putCached(ctx, bag, snap); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "cache_closure", closureTarget(snap), audit.Allow)
}

// ForwardToFactory 厂内不得把已收平台级转发给另一厂。
func (s *Closure) ForwardToFactory(ctx context.Context, token string, assetID, factoryID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	_ = s.audit(ctx, &acc.ID, nil, "forward_closure", assetID.String()+" factory="+factoryID.String(), audit.Deny)
	return domain.ErrForbidden
}

// SetReplicaCopyable 平台级副本不得改可复制。
func (s *Closure) SetReplicaCopyable(ctx context.Context, token string, assetID uuid.UUID, copyable bool) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	_ = s.audit(ctx, &acc.ID, nil, "update_replica", assetID.String(), audit.Deny)
	return domain.ErrForbidden
}

// PromoteReplica 平台级副本不得升档。
func (s *Closure) PromoteReplica(ctx context.Context, token string, assetID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetID.String(), audit.Deny)
	return domain.ErrForbidden
}

// ListReplicas 列出本厂已收平台级副本元数据，不含正文。
func (s *Closure) ListReplicas(ctx context.Context, token string) ([]AssetReplica, error) {
	if _, err := s.RequireActive(ctx, token); err != nil {
		return nil, err
	}
	rows, err := s.store.ListReplicas(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AssetReplica, 0, len(rows))
	for _, r := range rows {
		r.Content = nil
		out = append(out, r)
	}
	return out, nil
}

// GetReplica 读一条已收副本元数据；摘要不符则拒绝。
func (s *Closure) GetReplica(ctx context.Context, token string, assetID uuid.UUID, revision int64) (AssetReplica, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return AssetReplica{}, err
	}
	r, err := s.store.ReplicaByIDRev(ctx, assetID, revision)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "get_replica", assetTarget(assetID, revision), audit.Deny)
		return AssetReplica{}, err
	}
	if err := s.canViewReplica(ctx, acc); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "get_replica", assetTarget(assetID, revision), audit.Deny)
		return AssetReplica{}, err
	}
	if !digest.Match(r.Content, r.Digest) {
		_ = s.audit(ctx, &acc.ID, nil, "get_replica", assetTarget(assetID, revision), audit.Deny)
		return AssetReplica{}, domain.ErrIntegrity
	}
	r.Content = nil
	if err := s.audit(ctx, &acc.ID, nil, "get_replica", assetTarget(assetID, revision), audit.Allow); err != nil {
		return AssetReplica{}, err
	}
	return r, nil
}

func (s *Closure) canViewReplica(ctx context.Context, acc Account) error {
	grants, err := s.grantsOf(ctx, acc.ID)
	if err != nil {
		return err
	}
	if isFactorySA(grants) || s.isFactoryScopePE(ctx, acc) {
		return nil
	}
	if len(withRoles(grants, RoleProcessEngineer, RoleOperator)) > 0 {
		return nil
	}
	return domain.ErrForbidden
}
