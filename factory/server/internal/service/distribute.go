package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// AcceptPlatformDelivery 先解开过站信封再验收；无 WM2 前缀的夹具明文也能收。
func (s *Closure) AcceptPlatformDelivery(ctx context.Context, snap ClosureSnapshot) error {
	// 先解开过站信封再验收。
	opened, err := s.openTransitMembers(snap)
	if err != nil {
		_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
		return err
	}
	snap = opened
	if err := validateClosure(snap); err != nil {
		_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
		return err
	}
	fid := s.store.FactoryID()
	if snap.TargetFactoryID == nil || *snap.TargetFactoryID != fid {
		_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
		return domain.ErrForbidden
	}
	// 只收平台级成员为只读副本。
	for _, m := range snap.Members {
		if m.Level != AssetLevelPlatform {
			_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
			return domain.ErrForbidden
		}
		if _, err := s.store.InsertReplica(ctx, AssetReplica{
			ID: m.ID, Revision: m.Revision, Kind: m.Kind, Level: AssetLevelPlatform,
			Name: m.Name, Code: m.Code, Status: m.Status, Copyable: m.Copyable, Content: m.Content, Digest: m.Digest, Deps: m.Deps,
		}); err != nil {
			_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
			return err
		}
	}
	return s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Allow)
}

// RetractPlatformDelivery 云端删除后撤回展示；没有副本也算成功，已钉修订仍可读。
func (s *Closure) RetractPlatformDelivery(ctx context.Context, assetID uuid.UUID) error {
	// 撤回展示；没有副本也算成功，已钉修订仍可读。
	if err := s.store.RetractReplicas(ctx, assetID); err != nil {
		_ = s.audit(ctx, nil, nil, "retract_closure", assetID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, nil, nil, "retract_closure", assetID.String(), audit.Allow)
}

// GrantClientProject 由本厂超管授权已绑定 Client 接收某份可用工程。
func (s *Closure) GrantClientProject(ctx context.Context, token string, projectID, clientID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	target := projectID.String() + " client=" + clientID.String()
	// 只有工厂超管能授权 Client 收工程。失败一律记拒绝。
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
	// 记下该 Client 可接收该工程。
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
	// 收回该 Client 接收该工程的授权。
	if err := s.store.RevokeClientGrant(ctx, projectID, clientID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "revoke_closure", target, audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "revoke_closure", target, audit.Allow)
}

// assertGrantableProject 可授权的须是可用厂级或已收平台级工程，不含个人级。
func (s *Closure) assertGrantableProject(ctx context.Context, projectID uuid.UUID) error {
	a, err := s.store.GovernedAssetMetaByID(ctx, projectID)
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
	r, err := s.store.LatestReplicaMeta(ctx, projectID)
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
	// 只有工厂超管能改缓存上限。失败记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "set_cache_limit", "max_cached_projects", audit.Deny)
		return err
	}
	// 每 Client 工程缓存上限。
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
	// 有权工艺工程师且 Client 已授权、绑定有效。失败一律记拒绝。
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
	// 记下本次下发闭包摘要。
	if _, err := s.store.InsertClientRecord(ctx, store.ClientDistributionRecord{
		ProjectID: snap.AssetID, Revision: snap.Revision, ClientID: clientID,
		ClosureDigest: snap.Digest, Members: recordMembers(snap),
	}); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	if err := s.audit(ctx, &acc.ID, nil, "distribute_closure", closureTarget(snap), audit.Allow); err != nil {
		return err
	}
	s.publishClosureReady(ctx, clientID, snap)
	return nil
}

// assertClientRuntime 当前最高修订须仍允许运行且未过期。
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

// CachePersonalProject 创建人把自己的可用个人级工程装进本厂设备本机袋。
func (s *Closure) CachePersonalProject(ctx context.Context, token string, projectID, clientID uuid.UUID, bag *Bag, clocks Clocks) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	target := projectID.String() + " client=" + clientID.String()
	// 仅创建人能把自己的可用个人级工程装进本厂设备。失败一律记拒绝。
	a, err := s.loadChecked(ctx, projectID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return err
	}
	if a.Kind != KindProject || a.Level != AssetLevelPersonal || a.CreatorID != acc.ID {
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return domain.ErrForbidden
	}
	if err := s.assertClientRuntime(ctx, clientID, clocks); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return err
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
	// 不含正文。
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

// GetReplica 读一条已收副本元数据，不解包。
func (s *Closure) GetReplica(ctx context.Context, token string, assetID uuid.UUID, revision int64) (AssetReplica, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return AssetReplica{}, err
	}
	// 只读已收副本元数据，不解包。
	r, err := s.store.ReplicaMetaByIDRev(ctx, assetID, revision)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "get_replica", assetTarget(assetID, revision), audit.Deny)
		return AssetReplica{}, err
	}
	if err := s.canViewReplica(ctx, acc); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "get_replica", assetTarget(assetID, revision), audit.Deny)
		return AssetReplica{}, err
	}
	r.Content = nil
	if err := s.audit(ctx, &acc.ID, nil, "get_replica", assetTarget(assetID, revision), audit.Allow); err != nil {
		return AssetReplica{}, err
	}
	return r, nil
}

// canViewReplica 已收副本元数据本厂有效账号都能看。
func (s *Closure) canViewReplica(ctx context.Context, acc Account) error {
	_ = ctx
	_ = acc
	return nil
}
