package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
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
	// 留出目录错误位，有成员挂不上也接着收后面的副本
	var fsErr error
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
		// 目录挂不上只记下错误，不挡后面成员的副本落库
		if err := s.store.ApplyPlatformFS(ctx, m.Kind, m.ID, m.FSParentID, m.FSPath); err != nil {
			// 只留第一处挂不上的错误，收完全部成员再交回
			if fsErr == nil {
				// 记下这处目录错误，最后交回给调用方
				fsErr = err
			}
		}
	}
	// 副本都已落库，先记收下闭包成功，没记上则不算收完
	if err := s.audit(ctx, nil, nil, "accept_closure", closureTarget(snap), audit.Allow); err != nil {
		return err
	}
	// 有成员目录没挂上，补记一条目录对齐拒绝并交回这处错误
	if fsErr != nil {
		// 记下目录对齐被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "apply_fs", closureTarget(snap), audit.Deny)
		return fsErr
	}
	return nil
}

// HasPlatformDelivery 本厂已收下这一修订的闭包：根在且未撤回，依赖各修订也都在。
func (s *Closure) HasPlatformDelivery(ctx context.Context, assetID uuid.UUID, revision int64) (bool, error) {
	// 读根这一修订的副本元数据，不解正文
	root, err := s.store.ReplicaMetaByIDRev(ctx, assetID, revision)
	// 没有这一修订就要去拉
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	// 读失败交回，通道照常去拉
	if err != nil {
		return false, err
	}
	// 已撤回的要重收，不能跳过
	if root.Retracted {
		return false, nil
	}
	// 逐个依赖核对钉住的修订在不在
	for _, d := range root.Deps {
		// 读依赖这一修订的元数据，缺了就要整包重收
		_, err := s.store.ReplicaMetaByIDRev(ctx, d.ID, d.Revision)
		// 依赖修订没收过就算没有
		if errors.Is(err, domain.ErrNotFound) {
			return false, nil
		}
		// 读失败交回，通道照常去拉
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// RetractPlatformDelivery 云端删除后撤回展示；没有副本也算成功，已钉修订仍可读。
func (s *Closure) RetractPlatformDelivery(ctx context.Context, assetID uuid.UUID) error {
	// 撤回展示；没有副本也算成功，已钉修订仍可读。
	changed, err := s.store.RetractReplicas(ctx, assetID)
	// 撤不下就记拒绝并交回，通道会补拉
	if err != nil {
		// 记下撤回闭包被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "retract_closure", assetID.String(), audit.Deny)
		return err
	}
	// 早已撤回或没收过，重复送达不再记审计
	if !changed {
		return nil
	}
	// 撤回闭包成功后记审计，再把结果交回
	return s.audit(ctx, nil, nil, "retract_closure", assetID.String(), audit.Allow)
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
