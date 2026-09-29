package service

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
)

// CreateFact 产生一条运行事实桩，必须明确选定工作上下文，并冻结当时组织路径。
func (s *Attr) CreateFact(ctx context.Context, token string, wc WorkContext) (FactStub, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return FactStub{}, err
	}
	// 按当前分配解析并准备冻结路径
	unitID, path, err := s.resolveWorkContext(ctx, acc, wc)
	// 上下文不合法，拒绝写下事实或资产
	if err != nil {
		// 记下写运行事实被拒绝，写失败不改变结果
		_ = s.auditAt(ctx, &acc.ID, "create_fact", "fact", audit.Deny, unitID, path)
		return FactStub{}, err
	}
	// 冻结当时组织路径，写入事实桩。
	row, err := s.store.InsertFact(ctx, acc.ID, unitID, path)
	// 写入失败则停住，避免留下半截状态
	if err != nil {
		// 记下写运行事实被拒绝，写失败不改变结果
		_ = s.auditAt(ctx, &acc.ID, "create_fact", "fact", audit.Deny, unitID, path)
		return FactStub{}, err
	}
	// 写运行事实成功后记审计，再把结果交回
	return row, s.auditAt(ctx, &acc.ID, "create_fact", row.ID.String(), audit.Allow, unitID, path)
}

// CreatePersonalAsset 留下个人资产桩；内容不进审计，也不因超管身份自动打开。
func (s *Attr) CreatePersonalAsset(ctx context.Context, token string, wc WorkContext, content string) (PersonalAsset, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return PersonalAsset{}, err
	}
	// 按当前分配解析并准备冻结路径
	unitID, path, err := s.resolveWorkContext(ctx, acc, wc)
	// 上下文不合法，拒绝写下事实或资产
	if err != nil {
		// 记下写个人资产被拒绝，写失败不改变结果
		_ = s.auditAt(ctx, &acc.ID, "create_asset", "asset", audit.Deny, unitID, path)
		return PersonalAsset{}, err
	}
	// 内容不进审计。
	row, err := s.store.InsertAsset(ctx, acc.ID, unitID, path, content)
	// 写入失败则停住，避免留下半截状态
	if err != nil {
		// 记下写个人资产被拒绝，写失败不改变结果
		_ = s.auditAt(ctx, &acc.ID, "create_asset", "asset", audit.Deny, unitID, path)
		return PersonalAsset{}, err
	}
	// 写个人资产成功后记审计，再把结果交回
	return row, s.auditAt(ctx, &acc.ID, "create_asset", row.ID.String(), audit.Allow, unitID, path)
}

// GetFact 只读历史事实；用落库快照，不按人员当前分配反查。
func (s *Attr) GetFact(ctx context.Context, token string, factID uuid.UUID) (FactStub, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return FactStub{}, err
	}
	// 按落库快照读，不按当前分配反查。
	row, err := s.store.FactByID(ctx, factID)
	// 事实不存在或读失败则拒绝查看
	if err != nil {
		return FactStub{}, err
	}
	// 既不是创建人又无只读许可，拒绝查看
	if err := s.canReadMeta(ctx, acc, row.CreatorID, row.OrgUnitID); err != nil {
		// 记下读事实被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "get_fact", factID.String(), audit.Deny)
		return FactStub{}, err
	}
	// 读事实成功后记审计，再把结果交回
	return row, s.audit(ctx, &acc.ID, nil, "get_fact", factID.String(), audit.Allow)
}

// ListFactsByCreator 供查询已停用账号名下的历史事实，路径不改。
func (s *Attr) ListFactsByCreator(ctx context.Context, token string, creatorID uuid.UUID) ([]FactStub, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return nil, err
	}
	// 没有只读许可，拒绝查看
	if err := s.can(ctx, acc, permView, nil); err != nil {
		// 没有只读许可，记下列历史事实被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "list_facts", creatorID.String(), audit.Deny)
		return nil, err
	}
	// 已停用账号名下的路径不改。
	rows, err := s.store.FactsByCreator(ctx, creatorID)
	// 列表读失败则不交残缺事实
	if err != nil {
		return nil, err
	}
	// 列历史事实成功后记审计，再把结果交回
	return rows, s.audit(ctx, &acc.ID, nil, "list_facts", creatorID.String(), audit.Allow)
}

// GetPersonalAsset 只返回元数据和创建时路径，不含内容。
func (s *Attr) GetPersonalAsset(ctx context.Context, token string, assetID uuid.UUID) (PersonalAsset, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return PersonalAsset{}, err
	}
	// 只取元数据和创建时路径，不含内容。
	meta, err := s.store.AssetByID(ctx, assetID)
	// 资产不存在或读失败则拒绝
	if err != nil {
		return PersonalAsset{}, err
	}
	// 既不是创建人又无只读许可，拒绝查看
	if err := s.canReadMeta(ctx, acc, meta.CreatorID, meta.OrgUnitID); err != nil {
		// 记下读资产元数据被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "get_asset", assetID.String(), audit.Deny)
		return PersonalAsset{}, err
	}
	// 读资产元数据成功后记审计，再把结果交回
	return meta, s.audit(ctx, &acc.ID, nil, "get_asset", assetID.String(), audit.Allow)
}

// ReadPersonalAssetContent 仅创建人可读；工厂超管也不自动获得内容。
func (s *Attr) ReadPersonalAssetContent(ctx context.Context, token string, assetID uuid.UUID) (string, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return "", err
	}
	// 读资产正文，超管不因此自动可见
	creatorID, content, err := s.store.AssetContent(ctx, assetID)
	// 正文读失败，拒绝交给调用方
	if err != nil {
		return "", err
	}
	// 管理角色不自动获得个人资产内容。
	if acc.ID != creatorID {
		// 记下读资产正文被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetID.String(), audit.Deny)
		return "", domain.ErrForbidden
	}
	// 读资产正文成功后记审计，再把结果交回
	return content, s.audit(ctx, &acc.ID, nil, "read_asset_content", assetID.String(), audit.Allow)
}

// TransferPersonalAsset 拒绝把个人资产改挂到管理员或改路径。
func (s *Attr) TransferPersonalAsset(ctx context.Context, token string, assetID, to uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 记下转走个人资产被拒绝，写失败不改变结果
	_ = s.audit(ctx, &acc.ID, nil, "transfer_asset", assetID.String()+" "+to.String(), audit.Deny)
	return domain.ErrForbidden
}

// RewriteFactPath 拒绝改写已落库的组织路径快照。
func (s *Attr) RewriteFactPath(ctx context.Context, token string, factID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 记下改写历史路径被拒绝，写失败不改变结果
	_ = s.audit(ctx, &acc.ID, nil, "rewrite_fact_path", factID.String(), audit.Deny)
	return domain.ErrForbidden
}

// resolveWorkContext 必须三选一约束：当前分配的有效节点，或持厂级业务角色的 Factory 直属。
func (s *kernel) resolveWorkContext(ctx context.Context, acc Account, wc WorkContext) (*uuid.UUID, []PathNode, error) {
	// 直属和节点只能二选一，同时有或同时没有都拒绝
	if wc.Direct == (wc.OrgUnitID != nil) {
		return nil, nil, domain.ErrWorkContext // 没选或同时选了两个
	}
	// 选了工厂直属才核厂级操作员，否则看节点分配
	if wc.Direct {
		// 没有厂级操作员，不能选工厂直属
		if err := s.canOperateFactory(ctx, acc); err != nil {
			return nil, []PathNode{}, err
		}
		return nil, []PathNode{}, nil
	}
	// 读组织节点，停用的不能当新上下文
	unit, err := s.store.Unit(ctx, *wc.OrgUnitID)
	// 节点读失败，不能继续分配或操作
	if err != nil {
		return wc.OrgUnitID, nil, err
	}
	// 不是有效状态则拒绝，待启用和停用都不能做
	if unit.Status != StatusActive {
		return &unit.ID, nil, domain.ErrDisabledOrgUnit // 停用节点不能再当新上下文
	}
	// 必须是当前有效分配，不能拿别人的节点当上下文。
	ok, err := s.store.HasActiveAssignment(ctx, acc.ID, unit.ID)
	// 分配关系读失败则拒绝这次操作
	if err != nil {
		return &unit.ID, nil, err
	}
	// 没有取到则按缺失拒绝或留空
	if !ok {
		return &unit.ID, nil, domain.ErrWorkContext
	}
	// 没有覆盖该节点的操作员角色，拒绝操作
	if err := s.can(ctx, acc, permOperate, &unit.ID); err != nil {
		return &unit.ID, nil, err
	}
	// 冻结当时组织路径，事后改树也不改这条。
	path, err := s.store.PathSnapshot(ctx, unit.ID)
	// 路径冻结失败，拒绝写下残缺事实
	if err != nil {
		return &unit.ID, nil, err
	}
	return &unit.ID, path, nil
}

// canOperateFactory 只有带 Factory 作用域的操作员才能选直属。
func (s *kernel) canOperateFactory(ctx context.Context, acc Account) error {
	// 只取当前有效角色，收回的不参与
	grants, err := s.grantsOf(ctx, acc.ID)
	// 角色读失败就无法判定，拒绝放行
	if err != nil {
		return err
	}
	// 逐条有效授权看是否盖住目标
	for _, g := range withRoles(grants, RoleOperator) {
		// 整厂作用域盖住全部节点
		if g.ScopeKind == ScopeFactory {
			return nil
		}
	}
	return domain.ErrForbidden
}

// canReadMeta 创建人或对该节点有只读许可才能看元数据。
func (s *kernel) canReadMeta(ctx context.Context, acc Account, creatorID uuid.UUID, unitID *uuid.UUID) error {
	// 创建人可以看自己的元数据或焊次
	if acc.ID == creatorID {
		return nil
	}
	// 按角色并集核对有没有这份许可，再交回调用方
	return s.can(ctx, acc, permView, unitID)
}

// 用服务器时间记下带组织路径的允许或拒绝。
func (s *kernel) auditAt(ctx context.Context, actor *uuid.UUID, action, target, result string, unit *uuid.UUID, path []PathNode) error {
	// 改用服务器钟，把带组织路径的允许或拒绝记下
	return s.auditAtSrc(ctx, actor, action, target, result, audit.Server, unit, path)
}

// auditAtSrc 记下带组织路径和时间来源的允许或拒绝。
func (s *kernel) auditAtSrc(ctx context.Context, actor *uuid.UUID, action, target, result, timeSource string, unit *uuid.UUID, path []PathNode) error {
	// 取出本厂稳定身份，封包和审计都要用
	fid := s.store.FactoryID()
	// 路径为空就不带快照正文，避免写下空对象
	var raw []byte
	// 有路径快照才写入审计，没有则只记动作
	if path != nil {
		// 收成稳定正文，避免留下不一致的结果
		raw, _ = json.Marshal(path)
	}
	// 把允许或拒绝写入审计，不含口令
	return s.store.AppendAudit(ctx, audit.Event{
		ActorID:    actor,
		FactoryID:  &fid,
		OrgUnitID:  unit,
		OrgPath:    raw,
		Action:     action,
		Target:     target,
		Result:     result,
		TimeSource: timeSource,
	})
}
