package service

import (
	"bytes"
	"context"
	"errors"
	"strconv"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

func assetTarget(id uuid.UUID, rev int64) string {
	return id.String() + " rev=" + strconv.FormatInt(rev, 10)
}

func stripContent(a Asset) Asset {
	a.Content = nil
	return a
}

func (s *kernel) loadChecked(ctx context.Context, id uuid.UUID) (Asset, error) {
	a, err := s.store.GovernedAssetByID(ctx, id)
	if err != nil {
		return Asset{}, err
	}
	if !digest.Match(a.Content, a.Digest) {
		return Asset{}, domain.ErrIntegrity
	}
	return a, nil
}

// canAuthorFactory 只有工艺工程师，且作用域覆盖创建节点；直属须 Factory 作用域。
func (s *kernel) canAuthorFactory(ctx context.Context, acc Account, unit *uuid.UUID) error {
	grants, err := s.grantsOf(ctx, acc.ID)
	if err != nil {
		return err
	}
	pe := withRoles(grants, RoleProcessEngineer)
	if unit == nil {
		for _, g := range pe {
			if g.ScopeKind == ScopeFactory {
				return nil
			}
		}
		return domain.ErrForbidden
	}
	ok, err := s.covers(ctx, pe, unit)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return domain.ErrForbidden
}

func (s *Assets) resolveAuthorContext(ctx context.Context, acc Account, wc WorkContext) (*uuid.UUID, []PathNode, error) {
	if wc.Direct == (wc.OrgUnitID != nil) {
		return nil, nil, domain.ErrWorkContext
	}
	if wc.Direct {
		if err := s.canAuthorFactory(ctx, acc, nil); err != nil {
			return nil, []PathNode{}, err
		}
		return nil, []PathNode{}, nil
	}
	unit, err := s.store.Unit(ctx, *wc.OrgUnitID)
	if err != nil {
		return wc.OrgUnitID, nil, err
	}
	if unit.Status != StatusActive {
		return &unit.ID, nil, domain.ErrDisabledOrgUnit
	}
	ok, err := s.store.HasActiveAssignment(ctx, acc.ID, unit.ID)
	if err != nil {
		return &unit.ID, nil, err
	}
	if !ok {
		return &unit.ID, nil, domain.ErrWorkContext
	}
	if err := s.canAuthorFactory(ctx, acc, &unit.ID); err != nil {
		return &unit.ID, nil, err
	}
	path, err := s.store.PathSnapshot(ctx, unit.ID)
	if err != nil {
		return &unit.ID, nil, err
	}
	return &unit.ID, path, nil
}

func (s *Assets) insertAuthored(ctx context.Context, acc Account, wc WorkContext, kind, level, name string, content []byte, deps []AssetDep) (Asset, error) {
	unitID, path, err := s.resolveAuthorContext(ctx, acc, wc)
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_asset", kind, audit.Deny, unitID, path)
		return Asset{}, err
	}
	row, err := s.store.InsertGovernedAsset(ctx, Asset{
		Kind: kind, Level: level, Name: name, Status: AssetDraft, Copyable: true,
		Content: content, Digest: digest.Sum(content), CreatorID: acc.ID,
		OrgUnitID: unitID, OrgPath: path, Deps: deps,
	})
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	if err := s.auditAt(ctx, &acc.ID, "create_asset", assetTarget(row.ID, row.Revision), audit.Allow, unitID, path); err != nil {
		return Asset{}, err
	}
	return stripContent(row), nil
}

// CreateFactoryProcess 由本厂有权工艺工程师创建厂级工艺，默认可复制，状态草稿。
func (s *Assets) CreateFactoryProcess(ctx context.Context, token string, wc WorkContext, name string, content []byte) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.insertAuthored(ctx, acc, wc, KindProcess, AssetLevelFactory, name, content, nil)
}

// CreatePersonalProcess 由工艺工程师在工作上下文中写入个人级工艺。
func (s *Assets) CreatePersonalProcess(ctx context.Context, token string, wc WorkContext, name string, content []byte) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.insertAuthored(ctx, acc, wc, KindProcess, AssetLevelPersonal, name, content, nil)
}

// CreatePersonalProject 创建个人级工程；依赖须是自己的可用个人级工艺或本厂可用厂级工艺。
func (s *Assets) CreatePersonalProject(ctx context.Context, token string, wc WorkContext, name string, content []byte, deps []AssetDep) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	if err := s.assertPersonalProjectDeps(ctx, acc, deps); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	return s.insertAuthored(ctx, acc, wc, KindProject, AssetLevelPersonal, name, content, deps)
}

func (s *kernel) assertPersonalProjectDeps(ctx context.Context, acc Account, deps []AssetDep) error {
	for _, d := range deps {
		p, err := s.loadChecked(ctx, d.ID)
		if err != nil {
			return err
		}
		if p.Kind != KindProcess || p.Revision != d.Revision || !bytes.Equal(p.Digest, d.Digest) {
			return domain.ErrAssetDependency
		}
		if p.Status != AssetAvailable {
			return domain.ErrAssetNotAvailable
		}
		switch p.Level {
		case AssetLevelFactory:
		case AssetLevelPersonal:
			if p.CreatorID != acc.ID {
				return domain.ErrAssetDependency
			}
		default:
			return domain.ErrAssetDependency
		}
	}
	return nil
}

// CreateFactoryProject 创建厂级工程；依赖必须是本厂可用厂级工艺且修订、摘要对得上。
func (s *Assets) CreateFactoryProject(ctx context.Context, token string, wc WorkContext, name string, content []byte, deps []AssetDep) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	if err := s.assertFactoryProcessDeps(ctx, deps); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	return s.insertAuthored(ctx, acc, wc, KindProject, AssetLevelFactory, name, content, deps)
}

func (s *kernel) assertFactoryProcessDeps(ctx context.Context, deps []AssetDep) error {
	for _, d := range deps {
		p, err := s.store.GovernedAssetByID(ctx, d.ID)
		if err == nil {
			if !digest.Match(p.Content, p.Digest) {
				return domain.ErrIntegrity
			}
			if p.Kind != KindProcess || p.Level != AssetLevelFactory || p.Revision != d.Revision || !bytes.Equal(p.Digest, d.Digest) {
				return domain.ErrAssetDependency
			}
			if p.Status != AssetAvailable {
				return domain.ErrAssetNotAvailable
			}
			continue
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		r, err := s.store.ReplicaByIDRev(ctx, d.ID, d.Revision)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return domain.ErrAssetDependency
			}
			return err
		}
		if !digest.Match(r.Content, r.Digest) {
			return domain.ErrIntegrity
		}
		if r.Kind != KindProcess || r.Level != AssetLevelPlatform || r.Revision != d.Revision || !bytes.Equal(r.Digest, d.Digest) {
			return domain.ErrAssetDependency
		}
		if r.Status != AssetAvailable {
			return domain.ErrAssetNotAvailable
		}
	}
	return nil
}

// CreatePlatformProcess 厂内不能写平台级原件。
func (s *Assets) CreatePlatformProcess(ctx context.Context, token string, name string, content []byte) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	_ = s.audit(ctx, &acc.ID, nil, "create_platform_asset", name, audit.Deny)
	return domain.ErrForbidden
}

// RenameAsset 改显示名，身份不变，修订升高。
func (s *Assets) RenameAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64, name string) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.mutateAsset(ctx, acc, assetID, expected, "rename_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: cur.Status, Deps: cur.Deps}, nil
	})
}

// UpdateAssetContent 改正文并重算摘要；停用后拒绝。
func (s *Assets) UpdateAssetContent(ctx context.Context, token string, assetID uuid.UUID, expected int64, content []byte) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.mutateAsset(ctx, acc, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Content: content, Digest: digest.Sum(content), Copyable: cur.Copyable, Status: cur.Status, Deps: cur.Deps}, nil
	})
}

// SetAssetCopyable 草稿可改可复制；可用后只允许是改为否。
func (s *Assets) SetAssetCopyable(ctx context.Context, token string, assetID uuid.UUID, expected int64, copyable bool) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.mutateAsset(ctx, acc, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		if cur.Status == AssetAvailable && copyable {
			return store.AssetWrite{}, domain.ErrAssetNotCopyable
		}
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: copyable, Status: cur.Status, Deps: cur.Deps}, nil
	})
}

// PublishAsset 草稿改为可用。
func (s *Assets) PublishAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.mutateAsset(ctx, acc, assetID, expected, "publish_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status != AssetDraft {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: AssetAvailable, Deps: cur.Deps}, nil
	})
}

// DisableAsset 可用改为停用；之后不得改回可用。
func (s *Assets) DisableAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.mutateAsset(ctx, acc, assetID, expected, "disable_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status != AssetAvailable {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: AssetDisabled, Deps: cur.Deps}, nil
	})
}

// ReenableAsset 停用后不得改回可用。
func (s *Assets) ReenableAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	_, err = s.mutateAsset(ctx, acc, assetID, expected, "publish_asset", func(cur Asset) (store.AssetWrite, error) {
		return store.AssetWrite{}, domain.ErrAssetNotAvailable
	})
	return err
}

// DeleteAsset 拒绝物理删除。
func (s *Assets) DeleteAsset(ctx context.Context, token string, assetID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetID.String(), audit.Deny)
	return domain.ErrReferenced
}

func (s *kernel) mutateAsset(ctx context.Context, acc Account, assetID uuid.UUID, expected int64, action string, patch func(Asset) (store.AssetWrite, error)) (Asset, error) {
	cur, err := s.loadChecked(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, expected), audit.Deny)
		return Asset{}, err
	}
	if err := s.canAuthorFactory(ctx, acc, cur.OrgUnitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, cur.Revision), audit.Deny)
		return Asset{}, err
	}
	if cur.Level == AssetLevelPersonal && acc.ID != cur.CreatorID {
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, cur.Revision), audit.Deny)
		return Asset{}, domain.ErrForbidden
	}
	w, err := patch(cur)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, cur.Revision), audit.Deny)
		return Asset{}, err
	}
	row, err := s.store.UpdateGovernedAsset(ctx, assetID, expected, w)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, expected), audit.Deny)
		return Asset{}, err
	}
	if err := s.audit(ctx, &acc.ID, nil, action, assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	return stripContent(row), nil
}

// canViewAssetMeta 个人级元数据给创建人、超管和覆盖路径的工艺工程师；厂级另给可用时的操作员。
func (s *Assets) canViewAssetMeta(ctx context.Context, acc Account, a Asset) error {
	if a.Level == AssetLevelPersonal {
		if acc.ID == a.CreatorID {
			return nil
		}
		grants, err := s.grantsOf(ctx, acc.ID)
		if err != nil {
			return err
		}
		if isFactorySA(grants) {
			return nil
		}
		return s.canAuthorFactory(ctx, acc, a.OrgUnitID)
	}
	grants, err := s.grantsOf(ctx, acc.ID)
	if err != nil {
		return err
	}
	if isFactorySA(grants) {
		return nil
	}
	if err := s.canAuthorFactory(ctx, acc, a.OrgUnitID); err == nil {
		return nil
	} else if !errors.Is(err, domain.ErrForbidden) {
		return err
	}
	if a.Status != AssetAvailable {
		return domain.ErrForbidden
	}
	ok, err := s.covers(ctx, withRoles(grants, RoleOperator), a.OrgUnitID)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return domain.ErrForbidden
}

// GetAsset 读元数据；无许可或摘要不符则拒绝，不回正文。
func (s *Assets) GetAsset(ctx context.Context, token string, assetID uuid.UUID) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	a, err := s.loadChecked(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "get_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	if err := s.canViewAssetMeta(ctx, acc, a); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "get_asset", assetTarget(a.ID, a.Revision), audit.Deny)
		return Asset{}, err
	}
	if err := s.audit(ctx, &acc.ID, nil, "get_asset", assetTarget(a.ID, a.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	return stripContent(a), nil
}

// ReadAssetContent 读正文并核对摘要；个人级仅创建人。
func (s *Assets) ReadAssetContent(ctx context.Context, token string, assetID uuid.UUID) ([]byte, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return nil, err
	}
	a, err := s.loadChecked(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetID.String(), audit.Deny)
		return nil, err
	}
	if a.Level == AssetLevelPersonal && acc.ID != a.CreatorID {
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetTarget(a.ID, a.Revision), audit.Deny)
		return nil, domain.ErrForbidden
	}
	if err := s.audit(ctx, &acc.ID, nil, "read_asset_content", assetTarget(a.ID, a.Revision), audit.Allow); err != nil {
		return nil, err
	}
	return a.Content, nil
}

// PromoteToFactory 把可复制且可用的个人级复制为新厂级，不改原件、不回个人正文。
func (s *Assets) PromoteToFactory(ctx context.Context, token string, assetID uuid.UUID) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	src, err := s.loadChecked(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	if err := s.canAuthorFactory(ctx, acc, src.OrgUnitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
	if src.Level != AssetLevelPersonal {
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrForbidden
	}
	if src.Status != AssetAvailable {
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrAssetNotAvailable
	}
	if !src.Copyable {
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrAssetNotCopyable
	}
	if err := s.assertPromoteToFactoryDeps(ctx, src); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
	rev := src.Revision
	row, err := s.store.InsertGovernedAsset(ctx, Asset{
		Kind: src.Kind, Level: AssetLevelFactory, Name: src.Name, Status: AssetAvailable, Copyable: true,
		Content: src.Content, Digest: src.Digest, CreatorID: acc.ID,
		OrgUnitID: src.OrgUnitID, OrgPath: src.OrgPath,
		SourceID: &src.ID, SourceRevision: &rev, Deps: src.Deps,
	})
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
	if err := s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(row.ID, row.Revision)+" from "+assetTarget(src.ID, src.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	return stripContent(row), nil
}

func (s *Assets) assertPromoteToFactoryDeps(ctx context.Context, src Asset) error {
	if src.Kind != KindProject {
		return nil
	}
	for _, d := range src.Deps {
		p, err := s.loadChecked(ctx, d.ID)
		if err != nil {
			return err
		}
		if p.Kind != KindProcess || p.Level != AssetLevelFactory || p.Status != AssetAvailable {
			return domain.ErrAssetDependency
		}
	}
	return nil
}

// ExportAssetSnapshot 导出厂级快照给 WAN 升档夹具。
func (s *Assets) ExportAssetSnapshot(ctx context.Context, token string, assetID uuid.UUID) (AssetSnapshot, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return AssetSnapshot{}, err
	}
	src, err := s.loadChecked(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "export_asset", assetID.String(), audit.Deny)
		return AssetSnapshot{}, err
	}
	if err := s.canAuthorFactory(ctx, acc, src.OrgUnitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "export_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return AssetSnapshot{}, err
	}
	if src.Level != AssetLevelFactory {
		_ = s.audit(ctx, &acc.ID, nil, "export_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return AssetSnapshot{}, domain.ErrForbidden
	}
	if src.Status != AssetAvailable {
		_ = s.audit(ctx, &acc.ID, nil, "export_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return AssetSnapshot{}, domain.ErrAssetNotAvailable
	}
	snap, err := s.store.ExportAssetSnapshot(ctx, assetID)
	if err != nil {
		return AssetSnapshot{}, err
	}
	return snap, s.audit(ctx, &acc.ID, nil, "export_asset", assetTarget(src.ID, src.Revision), audit.Allow)
}

// AssetAuthorContext 是制作工艺/工程时可选的工作位置。
type AssetAuthorContext struct {
	AllowDirect bool      `json:"allowDirect"` // 整厂作用域工艺工程师可直属工厂
	OrgUnits    []OrgUnit `json:"orgUnits"`    // 本人已分配且作用域覆盖的有效节点
}

// AuthorContext 给出当前账号可用来创建资产的工作位置。
func (s *Assets) AuthorContext(ctx context.Context, token string) (AssetAuthorContext, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return AssetAuthorContext{}, err
	}
	out := AssetAuthorContext{OrgUnits: []OrgUnit{}}
	if err := s.canAuthorFactory(ctx, acc, nil); err == nil {
		out.AllowDirect = true
	} else if !errors.Is(err, domain.ErrForbidden) {
		return AssetAuthorContext{}, err
	}
	assigns, err := s.store.ActiveAssignments(ctx, acc.ID)
	if err != nil {
		return AssetAuthorContext{}, err
	}
	for _, a := range assigns {
		unit, err := s.store.Unit(ctx, a.OrgUnitID)
		if err != nil {
			return AssetAuthorContext{}, err
		}
		if unit.Status != StatusActive {
			continue
		}
		if err := s.canAuthorFactory(ctx, acc, &unit.ID); err != nil {
			if errors.Is(err, domain.ErrForbidden) {
				continue
			}
			return AssetAuthorContext{}, err
		}
		out.OrgUnits = append(out.OrgUnits, unit)
	}
	return out, nil
}

// ListAssets 按许可过滤本厂工艺/工程元数据；kind 空则两种都回，不含正文。
func (s *Assets) ListAssets(ctx context.Context, token, kind string) ([]Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return nil, err
	}
	if kind != "" && kind != KindProcess && kind != KindProject {
		return nil, domain.ErrNotFound
	}
	rows, err := s.store.ListGovernedAssets(ctx)
	if err != nil {
		return nil, err
	}
	out := []Asset{}
	for _, a := range rows {
		if kind != "" && a.Kind != kind {
			continue
		}
		if err := s.canViewAssetMeta(ctx, acc, a); err != nil {
			if errors.Is(err, domain.ErrForbidden) {
				continue
			}
			return nil, err
		}
		out = append(out, stripContent(a))
	}
	return out, nil
}

// RehomeAsset 拒绝改挂创建人或创建路径。
func (s *Assets) RehomeAsset(ctx context.Context, token string, assetID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	_ = s.audit(ctx, &acc.ID, nil, "rehome_asset", assetID.String(), audit.Deny)
	return domain.ErrForbidden
}

// CreateProcessFromProject 拒绝把工程自有参数拆成新工艺。
func (s *Assets) CreateProcessFromProject(ctx context.Context, token string, projectID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	_ = s.audit(ctx, &acc.ID, nil, "create_process_from_project", projectID.String(), audit.Deny)
	return domain.ErrForbidden
}
