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
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return FactStub{}, err
	}
	unitID, path, err := s.resolveWorkContext(ctx, acc, wc)
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_fact", "fact", audit.Deny, unitID, path)
		return FactStub{}, err
	}
	row, err := s.store.InsertFact(ctx, acc.ID, unitID, path)
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_fact", "fact", audit.Deny, unitID, path)
		return FactStub{}, err
	}
	return row, s.auditAt(ctx, &acc.ID, "create_fact", row.ID.String(), audit.Allow, unitID, path)
}

// CreatePersonalAsset 留下个人资产桩；内容不进审计，也不因超管身份自动打开。
func (s *Attr) CreatePersonalAsset(ctx context.Context, token string, wc WorkContext, content string) (PersonalAsset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return PersonalAsset{}, err
	}
	unitID, path, err := s.resolveWorkContext(ctx, acc, wc)
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_asset", "asset", audit.Deny, unitID, path)
		return PersonalAsset{}, err
	}
	row, err := s.store.InsertAsset(ctx, acc.ID, unitID, path, content)
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_asset", "asset", audit.Deny, unitID, path)
		return PersonalAsset{}, err
	}
	return row, s.auditAt(ctx, &acc.ID, "create_asset", row.ID.String(), audit.Allow, unitID, path)
}

// GetFact 只读历史事实；用落库快照，不按人员当前分配反查。
func (s *Attr) GetFact(ctx context.Context, token string, factID uuid.UUID) (FactStub, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return FactStub{}, err
	}
	row, err := s.store.FactByID(ctx, factID)
	if err != nil {
		return FactStub{}, err
	}
	if err := s.canReadMeta(ctx, acc, row.CreatorID, row.OrgUnitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "get_fact", factID.String(), audit.Deny)
		return FactStub{}, err
	}
	return row, s.audit(ctx, &acc.ID, nil, "get_fact", factID.String(), audit.Allow)
}

// ListFactsByCreator 供查询已停用账号名下的历史事实，路径不改。
func (s *Attr) ListFactsByCreator(ctx context.Context, token string, creatorID uuid.UUID) ([]FactStub, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := s.can(ctx, acc, permView, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "list_facts", creatorID.String(), audit.Deny)
		return nil, err
	}
	rows, err := s.store.FactsByCreator(ctx, creatorID)
	if err != nil {
		return nil, err
	}
	return rows, s.audit(ctx, &acc.ID, nil, "list_facts", creatorID.String(), audit.Allow)
}

// GetPersonalAsset 只返回元数据和创建时路径，不含内容。
func (s *Attr) GetPersonalAsset(ctx context.Context, token string, assetID uuid.UUID) (PersonalAsset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return PersonalAsset{}, err
	}
	meta, err := s.store.AssetByID(ctx, assetID)
	if err != nil {
		return PersonalAsset{}, err
	}
	if err := s.canReadMeta(ctx, acc, meta.CreatorID, meta.OrgUnitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "get_asset", assetID.String(), audit.Deny)
		return PersonalAsset{}, err
	}
	return meta, s.audit(ctx, &acc.ID, nil, "get_asset", assetID.String(), audit.Allow)
}

// ReadPersonalAssetContent 仅创建人可读；工厂超管也不自动获得内容。
func (s *Attr) ReadPersonalAssetContent(ctx context.Context, token string, assetID uuid.UUID) (string, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return "", err
	}
	creatorID, content, err := s.store.AssetContent(ctx, assetID)
	if err != nil {
		return "", err
	}
	// 管理角色不自动获得个人资产内容。
	if acc.ID != creatorID {
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetID.String(), audit.Deny)
		return "", domain.ErrForbidden
	}
	return content, s.audit(ctx, &acc.ID, nil, "read_asset_content", assetID.String(), audit.Allow)
}

// TransferPersonalAsset 拒绝把个人资产改挂到管理员或改路径。
func (s *Attr) TransferPersonalAsset(ctx context.Context, token string, assetID, to uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	_ = s.audit(ctx, &acc.ID, nil, "transfer_asset", assetID.String()+" "+to.String(), audit.Deny)
	return domain.ErrForbidden
}

// RewriteFactPath 拒绝改写已落库的组织路径快照。
func (s *Attr) RewriteFactPath(ctx context.Context, token string, factID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	_ = s.audit(ctx, &acc.ID, nil, "rewrite_fact_path", factID.String(), audit.Deny)
	return domain.ErrForbidden
}

// resolveWorkContext 必须三选一约束：当前分配的有效节点，或持厂级业务角色的 Factory 直属。
func (s *kernel) resolveWorkContext(ctx context.Context, acc Account, wc WorkContext) (*uuid.UUID, []PathNode, error) {
	if wc.Direct == (wc.OrgUnitID != nil) {
		return nil, nil, domain.ErrWorkContext // 没选或同时选了两个
	}
	if wc.Direct {
		if err := s.canOperateFactory(ctx, acc); err != nil {
			return nil, []PathNode{}, err
		}
		return nil, []PathNode{}, nil
	}
	unit, err := s.store.Unit(ctx, *wc.OrgUnitID)
	if err != nil {
		return wc.OrgUnitID, nil, err
	}
	if unit.Status != StatusActive {
		return &unit.ID, nil, domain.ErrDisabledOrgUnit // 停用节点不能再当新上下文
	}
	ok, err := s.store.HasActiveAssignment(ctx, acc.ID, unit.ID)
	if err != nil {
		return &unit.ID, nil, err
	}
	if !ok {
		return &unit.ID, nil, domain.ErrWorkContext
	}
	if err := s.can(ctx, acc, permOperate, &unit.ID); err != nil {
		return &unit.ID, nil, err
	}
	path, err := s.store.PathSnapshot(ctx, unit.ID)
	if err != nil {
		return &unit.ID, nil, err
	}
	return &unit.ID, path, nil
}

// canOperateFactory 只有带 Factory 作用域的操作员或工程师才能选直属。
func (s *kernel) canOperateFactory(ctx context.Context, acc Account) error {
	grants, err := s.grantsOf(ctx, acc.ID)
	if err != nil {
		return err
	}
	for _, g := range withRoles(grants, RoleOperator, RoleProcessEngineer) {
		if g.ScopeKind == ScopeFactory {
			return nil
		}
	}
	return domain.ErrForbidden
}

func (s *kernel) canReadMeta(ctx context.Context, acc Account, creatorID uuid.UUID, unitID *uuid.UUID) error {
	if acc.ID == creatorID {
		return nil
	}
	return s.can(ctx, acc, permView, unitID)
}

func (s *kernel) auditAt(ctx context.Context, actor *uuid.UUID, action, target, result string, unit *uuid.UUID, path []PathNode) error {
	return s.auditAtSrc(ctx, actor, action, target, result, audit.Server, unit, path)
}

// auditAtSrc 记下带组织路径和时间来源的允许或拒绝。
func (s *kernel) auditAtSrc(ctx context.Context, actor *uuid.UUID, action, target, result, timeSource string, unit *uuid.UUID, path []PathNode) error {
	fid := s.store.FactoryID()
	var raw []byte
	if path != nil {
		raw, _ = json.Marshal(path)
	}
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
