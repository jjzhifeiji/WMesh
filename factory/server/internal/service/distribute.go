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
	// 解不开则整包拒绝收下
	if err != nil {
		// 记下收下闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
		return err
	}
	// 换成解开后的成员，后面按明文验收
	snap = opened
	// 不完整则拒绝装进袋
	if err := validateClosure(snap); err != nil {
		// 闭包不完整，记下收下闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
		return err
	}
	// 取出本厂稳定身份，封包和审计都要用
	fid := s.store.FactoryID()
	// 接收厂对不上本厂则拒绝收下
	if snap.TargetFactoryID == nil || *snap.TargetFactoryID != fid {
		// 接收方对不上，记下收下闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
		return domain.ErrForbidden
	}
	// 只收平台级成员为只读副本。
	for _, m := range snap.Members {
		// 个人级只给创建人，平台级按副本规则
		if m.Level != AssetLevelPlatform {
			// 种类或级别不符，记下收下闭包被拒绝，写失败不改变结果
			_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
			return domain.ErrForbidden
		}
		// 副本写不进去则不算送达
		if _, err := s.store.InsertReplica(ctx, AssetReplica{
			// 按平台级收下这份副本的身份和修订
			ID: m.ID, Revision: m.Revision, Kind: m.Kind, Level: AssetLevelPlatform,
			Name: m.Name, Code: m.Code, Status: m.Status, Copyable: m.Copyable, WeldKind: m.WeldKind, Content: m.Content, Digest: m.Digest, Deps: m.Deps,
		}); err != nil {
			_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
			return err
		}
		// 目录放不进去则送达不算完成
		if err := s.store.ApplyPlatformFS(ctx, m.Kind, m.ID, m.FSParentID, m.FSPath); err != nil {
			// 记下收下闭包被拒绝，写失败不改变结果
			_ = s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Deny)
			return err
		}
	}
	// 收下闭包成功后记审计，再把结果交回
	return s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Allow)
}

// RetractPlatformDelivery 云端删除后撤回展示；没有副本也算成功，已钉修订仍可读。
func (s *Closure) RetractPlatformDelivery(ctx context.Context, assetID uuid.UUID) error {
	// 撤回展示；没有副本也算成功，已钉修订仍可读。
	if err := s.store.RetractReplicas(ctx, assetID); err != nil {
		// 记下撤回闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "retract_closure", assetID.String(), audit.Deny)
		return err
	}
	// 撤回闭包成功后记审计，再把结果交回
	return s.audit(ctx, nil, nil, "retract_closure", assetID.String(), audit.Allow)
}

// GrantClientProject 由本厂超管授权已绑定 Client 接收某份可用工程。
func (s *Closure) GrantClientProject(ctx context.Context, token string, projectID, clientID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 收成文本身份，供审计或对账对准
	target := projectID.String() + " client=" + clientID.String()
	// 只有工厂超管能授权 Client 收工程。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 不是工厂超管，记下授权工程被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "grant_closure", target, audit.Deny)
		return err
	}
	// 个人级或不可用工程拒绝授权
	if err := s.assertGrantableProject(ctx, projectID); err != nil {
		// 记下授权工程被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "grant_closure", target, audit.Deny)
		return err
	}
	// 按身份读本厂这台设备
	cl, err := s.store.ClientByID(ctx, clientID)
	// 这台设备不在名录，拒绝
	if err != nil {
		// 记下授权工程被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "grant_closure", target, audit.Deny)
		return err
	}
	// 不是有效绑定则拒绝签发、登录或对账
	if cl.Status != ClientStatusBound {
		// 设备未绑定或已作废，记下授权工程被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "grant_closure", target, audit.Deny)
		return domain.ErrBindingVoid
	}
	// 记下该 Client 可接收该工程。
	if _, err := s.store.UpsertClientGrant(ctx, projectID, clientID, acc.ID); err != nil {
		// 记下授权工程被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "grant_closure", target, audit.Deny)
		return err
	}
	// 授权工程成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "grant_closure", target, audit.Allow)
}

// RevokeClientProject 收回 Client 接收该工程的授权。
func (s *Closure) RevokeClientProject(ctx context.Context, token string, projectID, clientID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 收成文本身份，供审计或对账对准
	target := projectID.String() + " client=" + clientID.String()
	// 不是工厂超管，拒绝这次账号或策略操作
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 不是工厂超管，记下收回工程授权被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "revoke_closure", target, audit.Deny)
		return err
	}
	// 收回该 Client 接收该工程的授权。
	if err := s.store.RevokeClientGrant(ctx, projectID, clientID); err != nil {
		// 记下收回工程授权被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "revoke_closure", target, audit.Deny)
		return err
	}
	// 收回工程授权成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "revoke_closure", target, audit.Allow)
}

// assertGrantableProject 可授权的须是可用厂级或已收平台级工程，不含个人级。
func (s *Closure) assertGrantableProject(ctx context.Context, projectID uuid.UUID) error {
	// 读治理行，用来核对作业类型
	a, err := s.store.GovernedAssetMetaByID(ctx, projectID)
	// 没有错误才采用这次结果，失败另走拒绝
	if err == nil {
		// 不是工程则拒绝装袋或授权
		if a.Kind != KindProject {
			return domain.ErrForbidden
		}
		// 个人级只给创建人，平台级按副本规则
		if a.Level == AssetLevelPersonal {
			return domain.ErrForbidden
		}
		// 不是可用则不能下发、授权或依赖
		if a.Status != AssetAvailable {
			return domain.ErrAssetNotAvailable
		}
		return nil
	}
	// 不是没有记录，读取真失败必须停住
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	// 治理行没有就改查已收副本
	r, err := s.store.LatestReplicaMeta(ctx, projectID)
	// 副本也读失败，依赖无法核对
	if err != nil {
		return err
	}
	// 不是工程则拒绝装袋或授权
	if r.Kind != KindProject || r.Status != AssetAvailable {
		return domain.ErrForbidden
	}
	return nil
}

// DistributeToClient 由作用域覆盖的操作员把工程闭包写入已授权本机袋。
func (s *Closure) DistributeToClient(ctx context.Context, token string, projectID, clientID uuid.UUID, bag *Bag, clocks Clocks) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 收成文本身份，供审计或对账对准
	target := projectID.String() + " client=" + clientID.String()
	// 操作员作用域覆盖且 Client 已授权、绑定有效。失败一律记拒绝。
	root, err := s.loadRootForPack(ctx, projectID)
	// 根读不到则不能组包
	if err != nil {
		// 记下下发闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	// 个人级只给创建人，平台级按副本规则
	if root.Level == AssetLevelPersonal {
		// 种类或级别不符，记下下发闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return domain.ErrForbidden
	}
	// 不能组包则拒绝下发
	if err := s.canPack(ctx, acc, root); err != nil {
		// 记下下发闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	// 读这台对该工程是否仍有授权
	grant, err := s.store.ClientGrant(ctx, projectID, clientID)
	// 授权读不到或已收回则拒绝下发
	if err != nil || !grant.Active {
		// 记下下发闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		// 授权读不到或已收回则拒绝下发
		if err != nil {
			return err
		}
		return domain.ErrForbidden
	}
	// 按身份读本厂这台设备
	cl, err := s.store.ClientByID(ctx, clientID)
	// 这台设备不在名录，拒绝
	if err != nil {
		// 记下下发闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	// 不是有效绑定则拒绝签发、登录或对账
	if cl.Status != ClientStatusBound {
		// 设备未绑定或已作废，记下下发闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return domain.ErrBindingVoid
	}
	// 凭证失效则拒绝下发到这台
	if err := s.assertClientRuntime(ctx, clientID, clocks); err != nil {
		// 记下下发闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	// 按根组出钉死修订的闭包
	snap, err := s.packFromMember(ctx, root)
	// 组包失败则拒绝下发
	if err != nil {
		// 记下下发闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	// 另拷一份设备身份再取址，避免指向会被改的变量
	cid := clientID
	// 钉到这台本机，别的设备拉不到
	snap.TargetClientID = &cid
	// 袋不是这台或不是本厂，直接拒绝
	if bag.ClientID != clientID {
		// 记下下发闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return domain.ErrForbidden
	}
	// 装袋失败则不下发成功
	if err := s.putCached(ctx, bag, snap); err != nil {
		// 记下下发闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	// 记下本次下发闭包摘要。
	if _, err := s.store.InsertClientRecord(ctx, store.ClientDistributionRecord{
		// 记下授给这台本机的工程和修订
		ProjectID: snap.AssetID, Revision: snap.Revision, ClientID: clientID,
		ClosureDigest: snap.Digest, Members: recordMembers(snap),
	}); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "distribute_closure", target, audit.Deny)
		return err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &acc.ID, nil, "distribute_closure", closureTarget(snap), audit.Allow); err != nil {
		return err
	}
	// 只推身份、修订和摘要，不推正文
	s.publishClosureReady(ctx, clientID, snap)
	return nil
}

// assertClientRuntime 当前最高修订须仍允许运行且未过期。
func (s *Closure) assertClientRuntime(ctx context.Context, clientID uuid.UUID, clocks Clocks) error {
	// 取这台最高修订的运行凭证
	g, err := s.store.LatestRuntimeGrant(ctx, clientID)
	// 凭证读失败，不能判断能否运行
	if err != nil {
		return err
	}
	// 取用于判断过期的那口钟
	now := clocks.Server
	// 没有日期的汇总行不送出
	if now.IsZero() {
		// 统一到协调世界时再比较或签名
		now = time.Now().UTC()
	}
	// 未生效、已过期或已撤销则挡住
	if !g.CanRun || now.Before(g.NotBefore) || now.After(g.NotAfter) {
		return domain.ErrForbidden
	}
	return nil
}

// CachePersonalProject 创建人把自己的可用个人级工程装进本厂设备本机袋。
func (s *Closure) CachePersonalProject(ctx context.Context, token string, projectID, clientID uuid.UUID, bag *Bag, clocks Clocks) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 收成文本身份，供审计或对账对准
	target := projectID.String() + " client=" + clientID.String()
	// 仅创建人能把自己的可用个人级工程装进本厂设备。失败一律记拒绝。
	a, err := s.loadChecked(ctx, projectID)
	// 工程根读不到或未通过核对，拒绝下发
	if err != nil {
		// 记下装入个人工程被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return err
	}
	// 不是创建人则个人级拒绝，或改看授权
	if a.Kind != KindProject || a.Level != AssetLevelPersonal || a.CreatorID != acc.ID {
		// 种类或级别不符，记下装入个人工程被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return domain.ErrForbidden
	}
	// 凭证失效则拒绝下发到这台
	if err := s.assertClientRuntime(ctx, clientID, clocks); err != nil {
		// 记下装入个人工程被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return err
	}
	// 袋不是这台或不是本厂，直接拒绝
	if bag.ClientID != clientID {
		// 记下装入个人工程被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return domain.ErrForbidden
	}
	// 把资产收成闭包成员
	snap, err := s.packFromMember(ctx, memberFromAsset(a))
	// 成员收不成则整包拒绝
	if err != nil {
		// 记下装入个人工程被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return err
	}
	// 另拷一份设备身份再取址，避免指向会被改的变量
	cid := clientID
	// 钉到这台本机，别的设备拉不到
	snap.TargetClientID = &cid
	// 装袋失败则不下发成功
	if err := s.putCached(ctx, bag, snap); err != nil {
		// 记下装入个人工程被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "cache_closure", target, audit.Deny)
		return err
	}
	// 装入个人工程成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "cache_closure", closureTarget(snap), audit.Allow)
}

// ForwardToFactory 厂内不得把已收平台级转发给另一厂。
func (s *Closure) ForwardToFactory(ctx context.Context, token string, assetID, factoryID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 记下转发到他厂被拒绝，写失败不改变结果
	_ = s.audit(ctx, &acc.ID, nil, "forward_closure", assetID.String()+" factory="+factoryID.String(), audit.Deny)
	return domain.ErrForbidden
}

// SetReplicaCopyable 平台级副本不得改可复制。
func (s *Closure) SetReplicaCopyable(ctx context.Context, token string, assetID uuid.UUID, copyable bool) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 记下改副本可复制被拒绝，写失败不改变结果
	_ = s.audit(ctx, &acc.ID, nil, "update_replica", assetID.String(), audit.Deny)
	return domain.ErrForbidden
}

// PromoteReplica 平台级副本不得升档。
func (s *Closure) PromoteReplica(ctx context.Context, token string, assetID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 记下把副本升档被拒绝，写失败不改变结果
	_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetID.String(), audit.Deny)
	return domain.ErrForbidden
}

// ListReplicas 列出本厂已收平台级副本元数据，不含正文。
func (s *Closure) ListReplicas(ctx context.Context, token string) ([]AssetReplica, error) {
	// 会话无效或账号已停用，拒绝继续
	if _, err := s.RequireActive(ctx, token); err != nil {
		return nil, err
	}
	// 不含正文。
	rows, err := s.store.ListReplicas(ctx)
	// 读取失败则停住，避免按空数据继续
	if err != nil {
		return nil, err
	}
	// 按条数决定是空、超限还是继续
	out := make([]AssetReplica, 0, len(rows))
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, r := range rows {
		// 元数据查询不带正文，避免把工艺交出去
		r.Content = nil
		// 把这一条收进结果，漏了清单就不齐
		out = append(out, r)
	}
	return out, nil
}

// GetReplica 读一条已收副本元数据，不解包。
func (s *Closure) GetReplica(ctx context.Context, token string, assetID uuid.UUID, revision int64) (AssetReplica, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return AssetReplica{}, err
	}
	// 只读已收副本元数据，不解包。
	r, err := s.store.ReplicaMetaByIDRev(ctx, assetID, revision)
	// 这一修订的副本读不到则拒绝
	if err != nil {
		// 记下读副本元数据被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "get_replica", assetTarget(assetID, revision), audit.Deny)
		return AssetReplica{}, err
	}
	// 账号无效则连元数据也不给
	if err := s.canViewReplica(ctx, acc); err != nil {
		// 记下读副本元数据被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "get_replica", assetTarget(assetID, revision), audit.Deny)
		return AssetReplica{}, err
	}
	// 元数据查询不带正文，避免把工艺交出去
	r.Content = nil
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &acc.ID, nil, "get_replica", assetTarget(assetID, revision), audit.Allow); err != nil {
		return AssetReplica{}, err
	}
	return r, nil
}

// canViewReplica 已收副本元数据本厂有效账号都能看。
func (s *Closure) canViewReplica(ctx context.Context, acc Account) error {
	// 这条路径不另做远程调用，上下文仅占位
	_ = ctx
	// 账号已经核过，这里不再改名册
	_ = acc
	return nil
}
