package service

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

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

func replicaAsAsset(r AssetReplica) Asset {
	return Asset{
		ID: r.ID, Kind: r.Kind, Level: AssetLevelPlatform, Name: r.Name,
		Status: r.Status, Copyable: r.Copyable, Revision: r.Revision,
		Content: r.Content, Digest: r.Digest, Deps: r.Deps,
		CreatedAt: r.ReceivedAt, UpdatedAt: r.ReceivedAt,
	}
}

func (s *Assets) loadReplica(ctx context.Context, id uuid.UUID) (Asset, error) {
	r, err := s.store.LatestReplica(ctx, id)
	if err != nil {
		return Asset{}, err
	}
	if !digest.Match(r.Content, r.Digest) {
		return Asset{}, domain.ErrIntegrity
	}
	return replicaAsAsset(r), nil
}

func (s *Assets) loadReplicaMeta(ctx context.Context, id uuid.UUID) (Asset, error) {
	r, err := s.store.LatestReplicaMeta(ctx, id)
	if err != nil {
		return Asset{}, err
	}
	return replicaAsAsset(r), nil
}

func (s *Assets) loadAny(ctx context.Context, id uuid.UUID) (Asset, error) {
	a, err := s.loadChecked(ctx, id)
	if err == nil {
		return a, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return Asset{}, err
	}
	return s.loadReplica(ctx, id)
}

func (s *Assets) loadAnyMeta(ctx context.Context, id uuid.UUID) (Asset, error) {
	a, err := s.store.GovernedAssetMetaByID(ctx, id)
	if err == nil {
		return a, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return Asset{}, err
	}
	return s.loadReplicaMeta(ctx, id)
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

func (s *kernel) loadCheckedMeta(ctx context.Context, id uuid.UUID) (Asset, error) {
	return s.store.GovernedAssetMetaByID(ctx, id)
}

// canAuthorFactory 本厂有效账号都能制作、改厂级；个人级正文仍只创建人。
func (s *kernel) canAuthorFactory(ctx context.Context, acc Account, unit *uuid.UUID) error {
	_ = ctx
	_ = acc
	_ = unit
	return nil
}

// peCovers 下发仍按工艺工程师作用域；制作不再走这里。
func (s *kernel) peCovers(ctx context.Context, acc Account, unit *uuid.UUID) error {
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
	content, err = s.normalizeContent(ctx, kind, content)
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	return s.insertGoverned(ctx, acc, unitID, path, kind, level, name, content, deps)
}

// insertGoverned 落一条草稿；调用方已决定是否套过模版。
func (s *Assets) insertGoverned(ctx context.Context, acc Account, unitID *uuid.UUID, path []PathNode, kind, level, name string, content []byte, deps []AssetDep) (Asset, error) {
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

// CopyProcess 可复制工艺另存为新草稿，原件正文原样拷贝，不套模版；平台级副本落成本厂厂级。
func (s *Assets) CopyProcess(ctx context.Context, token string, assetID uuid.UUID, name string) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, domain.ErrInvalidName
	}
	src, err := s.loadAnyMeta(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	if src.Kind != KindProcess {
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrForbidden
	}
	if src.Status == AssetDisabled {
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrAssetNotAvailable
	}
	if !src.Copyable {
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrAssetNotCopyable
	}
	if src.Level == AssetLevelPersonal && acc.ID != src.CreatorID {
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrForbidden
	}
	src, err = s.loadAny(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	level := AssetLevelFactory
	if src.Level == AssetLevelPersonal {
		level = AssetLevelPersonal
	}
	unitID, path, err := s.resolveAuthorContext(ctx, acc, WorkContext{Direct: true})
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	return s.insertGoverned(ctx, acc, unitID, path, KindProcess, level, name, src.Content, nil)
}

// CreateFactoryProcess 本厂有效账号创建厂级工艺，默认可复制，状态草稿。
func (s *Assets) CreateFactoryProcess(ctx context.Context, token string, wc WorkContext, name string, content []byte) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.insertAuthored(ctx, acc, wc, KindProcess, AssetLevelFactory, name, content, nil)
}

// CreatePersonalProcess 本厂有效账号在工作上下文中写入个人级工艺。
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
		p, err := s.loadCheckedMeta(ctx, d.ID)
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
		p, err := s.store.GovernedAssetMetaByID(ctx, d.ID)
		if err == nil {
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
		r, err := s.store.ReplicaMetaByIDRev(ctx, d.ID, d.Revision)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return domain.ErrAssetDependency
			}
			return err
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
		return store.AssetWrite{Name: name, Digest: cur.Digest, Copyable: cur.Copyable, Status: cur.Status, Deps: cur.Deps, KeepContent: true}, nil
	})
}

// UpdateAssetContent 改正文并重算摘要；停用后拒绝。已有正文不套模版。
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

// SetAssetCopyable 未停用即可改可复制，发布后也能改回是或否。
func (s *Assets) SetAssetCopyable(ctx context.Context, token string, assetID uuid.UUID, expected int64, copyable bool) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.mutateAsset(ctx, acc, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Digest: cur.Digest, Copyable: copyable, Status: cur.Status, Deps: cur.Deps, KeepContent: true}, nil
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
		return store.AssetWrite{Name: cur.Name, Digest: cur.Digest, Copyable: cur.Copyable, Status: AssetAvailable, Deps: cur.Deps, KeepContent: true}, nil
	})
}

// DisableAsset 可用改为停用；停用期间不得改正文或升档。
func (s *Assets) DisableAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.mutateAsset(ctx, acc, assetID, expected, "disable_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status != AssetAvailable {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Digest: cur.Digest, Copyable: cur.Copyable, Status: AssetDisabled, Deps: cur.Deps, KeepContent: true}, nil
	})
}

// ReenableAsset 停用改回可用。
func (s *Assets) ReenableAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.mutateAsset(ctx, acc, assetID, expected, "publish_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status != AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Digest: cur.Digest, Copyable: cur.Copyable, Status: AssetAvailable, Deps: cur.Deps, KeepContent: true}, nil
	})
}

// DeleteAsset 未被工程依赖则可删；仍被引用则拒绝。
func (s *Assets) DeleteAsset(ctx context.Context, token string, assetID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	cur, err := s.loadCheckedMeta(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetID.String(), audit.Deny)
		return err
	}
	if err := s.canAuthorFactory(ctx, acc, cur.OrgUnitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return err
	}
	if cur.Level == AssetLevelPersonal && acc.ID != cur.CreatorID {
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return domain.ErrForbidden
	}
	used, err := s.store.AssetIsReferenced(ctx, assetID)
	if err != nil {
		return err
	}
	if used {
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return domain.ErrReferenced
	}
	if err := s.store.DeleteGovernedAsset(ctx, assetID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Allow)
}

func (s *kernel) mutateAsset(ctx context.Context, acc Account, assetID uuid.UUID, expected int64, action string, patch func(Asset) (store.AssetWrite, error)) (Asset, error) {
	cur, err := s.loadCheckedMeta(ctx, assetID)
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

// canViewAssetMeta 个人级元数据给本厂有效账号看；厂级有效账号都能看。
func (s *Assets) canViewAssetMeta(ctx context.Context, acc Account, a Asset) error {
	_ = ctx
	_ = acc
	_ = a
	return nil
}

// GetAsset 读元数据，不解包正文。
func (s *Assets) GetAsset(ctx context.Context, token string, assetID uuid.UUID) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	a, err := s.loadAnyMeta(ctx, assetID)
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

// ReadAssetContent 读正文并核对摘要；个人级仅创建人；不可复制的平台级不给人看。
func (s *Assets) ReadAssetContent(ctx context.Context, token string, assetID uuid.UUID) ([]byte, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return nil, err
	}
	meta, err := s.loadAnyMeta(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetID.String(), audit.Deny)
		return nil, err
	}
	// 不可复制的平台级是严格保密件，厂端人员不得看参数，连解包都不做。
	if meta.Level == AssetLevelPlatform && !meta.Copyable {
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetTarget(meta.ID, meta.Revision), audit.Deny)
		return nil, domain.ErrForbidden
	}
	if meta.Level == AssetLevelPersonal && acc.ID != meta.CreatorID {
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetTarget(meta.ID, meta.Revision), audit.Deny)
		return nil, domain.ErrForbidden
	}
	if err := s.canViewAssetMeta(ctx, acc, meta); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetTarget(meta.ID, meta.Revision), audit.Deny)
		return nil, err
	}
	a, err := s.loadAny(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetID.String(), audit.Deny)
		return nil, err
	}
	if err := s.audit(ctx, &acc.ID, nil, "read_asset_content", assetTarget(a.ID, a.Revision), audit.Allow); err != nil {
		return nil, err
	}
	return a.Content, nil
}

// PromoteToFactory 把可复制且可用的个人级升为厂级：第一次复制新身份；再升按正文摘要跳过或覆盖。
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
	existing, err := s.store.GovernedAssetBySourceID(ctx, src.ID)
	if err == nil {
		if bytes.Equal(existing.Digest, src.Digest) {
			if err := s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(existing.ID, existing.Revision)+" from "+assetTarget(src.ID, src.Revision), audit.Allow); err != nil {
				return Asset{}, err
			}
			return stripContent(existing), nil
		}
		row, err := s.store.UpdateGovernedAsset(ctx, existing.ID, existing.Revision, store.AssetWrite{
			Name: src.Name, Content: src.Content, Digest: src.Digest, Copyable: true, Status: AssetAvailable, Deps: src.Deps, SourceRevision: &rev,
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
	if !errors.Is(err, domain.ErrNotFound) {
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
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
		p, err := s.loadCheckedMeta(ctx, d.ID)
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

// PromotableAsset 是给 WAN 升档看的本厂资产元数据，不含正文。
type PromotableAsset struct {
	ID       uuid.UUID `json:"id"`       // 稳定身份
	Kind     string    `json:"kind"`     // process / project
	Level    string    `json:"level"`    // factory / personal / platform
	Name     string    `json:"name"`     // 显示名
	Revision int64     `json:"revision"` // 当前修订
	Digest   []byte    `json:"digest"`   // 内容摘要
	Status   string    `json:"status"`   // draft / available / disabled
	Copyable bool      `json:"copyable"` // 原样带回，升档时仍按可复制判定
}

func toPromotable(a Asset) PromotableAsset {
	return PromotableAsset{
		ID: a.ID, Kind: a.Kind, Level: a.Level, Name: a.Name, Revision: a.Revision,
		Digest: a.Digest, Status: a.Status, Copyable: a.Copyable,
	}
}

// ListPromotable 列给 WAN 升档看的本厂全部工艺/工程：厂级、个人级、已下发平台级，不分状态，不含正文。
func (s *Assets) ListPromotable(ctx context.Context, kind string) ([]PromotableAsset, error) {
	if kind != "" && kind != KindProcess && kind != KindProject {
		return nil, domain.ErrNotFound
	}
	rows, err := s.store.ListGovernedAssets(ctx)
	if err != nil {
		return nil, err
	}
	out := []PromotableAsset{}
	seen := map[uuid.UUID]struct{}{}
	for _, a := range rows {
		if kind != "" && a.Kind != kind {
			continue
		}
		out = append(out, toPromotable(a))
		seen[a.ID] = struct{}{}
	}
	replicas, err := s.store.ListReplicas(ctx)
	if err != nil {
		return nil, err
	}
	latest := map[uuid.UUID]AssetReplica{}
	for _, r := range replicas {
		if kind != "" && r.Kind != kind {
			continue
		}
		prev, ok := latest[r.ID]
		if !ok || r.Revision > prev.Revision {
			latest[r.ID] = r
		}
	}
	for _, r := range latest {
		if _, ok := seen[r.ID]; ok {
			continue
		}
		out = append(out, toPromotable(replicaAsAsset(r)))
	}
	if err := s.audit(ctx, nil, nil, "list_promotable", kind, audit.Allow); err != nil {
		return nil, err
	}
	return out, nil
}

// SnapshotForChannel 通道升档用：本厂厂级和个人级可复制即可出快照，不分状态；平台级副本已在云端，不再从厂升。
func (s *Assets) SnapshotForChannel(ctx context.Context, assetID uuid.UUID) (AssetSnapshot, error) {
	src, err := s.loadChecked(ctx, assetID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			if _, rerr := s.loadReplicaMeta(ctx, assetID); rerr == nil {
				_ = s.audit(ctx, nil, nil, "export_asset", assetID.String(), audit.Deny)
				return AssetSnapshot{}, domain.ErrForbidden
			}
		}
		_ = s.audit(ctx, nil, nil, "export_asset", assetID.String(), audit.Deny)
		return AssetSnapshot{}, err
	}
	if !src.Copyable {
		_ = s.audit(ctx, nil, nil, "export_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return AssetSnapshot{}, domain.ErrAssetNotCopyable
	}
	// 先解厂库信封拿到明文，再另封过站。
	snap, err := s.store.ExportAssetSnapshot(ctx, assetID)
	if err != nil {
		return AssetSnapshot{}, err
	}
	// 通道上仍是过站密文，不把厂库信封送出。
	sealed, err := s.sealTransitContent(snap.SourceID, snap.SourceRevision, snap.Content)
	if err != nil {
		return AssetSnapshot{}, err
	}
	snap.Content = sealed
	return snap, s.audit(ctx, nil, nil, "export_asset", assetTarget(src.ID, src.Revision), audit.Allow)
}

// AssetAuthorContext 是制作工艺/工程时可选的工作位置。
type AssetAuthorContext struct {
	AllowDirect bool      `json:"allowDirect"` // 有效账号可直属工厂
	OrgUnits    []OrgUnit `json:"orgUnits"`    // 本人已分配的有效节点
}

// AuthorContext 给出当前账号可用来创建资产的工作位置。
func (s *Assets) AuthorContext(ctx context.Context, token string) (AssetAuthorContext, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return AssetAuthorContext{}, err
	}
	out := AssetAuthorContext{AllowDirect: true, OrgUnits: []OrgUnit{}}
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
		out.OrgUnits = append(out.OrgUnits, unit)
	}
	return out, nil
}

// AssetView 给管理端看的资产行，带创建人名字，不含正文。
type AssetView struct {
	Asset
	CreatorLogin   string `json:"creatorLogin,omitempty"`   // 创建人登录名
	CreatorDisplay string `json:"creatorDisplay,omitempty"` // 创建人显示名
}

// ListAssets 按许可过滤本厂工艺/工程元数据；kind 空则两种都回，不含正文。
func (s *Assets) ListAssets(ctx context.Context, token, kind string) ([]AssetView, error) {
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
	visible := []Asset{}
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
		visible = append(visible, stripContent(a))
	}
	replicas, err := s.store.ListReplicas(ctx)
	if err != nil {
		return nil, err
	}
	latest := map[uuid.UUID]AssetReplica{}
	firstAt := map[uuid.UUID]time.Time{}
	for _, r := range replicas {
		if kind != "" && r.Kind != kind {
			continue
		}
		if t, ok := firstAt[r.ID]; !ok || r.ReceivedAt.Before(t) {
			firstAt[r.ID] = r.ReceivedAt
		}
		prev, ok := latest[r.ID]
		if !ok || r.Revision > prev.Revision {
			latest[r.ID] = r
		}
	}
	seen := map[uuid.UUID]struct{}{}
	for _, a := range visible {
		seen[a.ID] = struct{}{}
	}
	for _, r := range latest {
		if _, ok := seen[r.ID]; ok {
			continue
		}
		// 厂端只展示云端当前可用的；停用/草稿不进列表，已钉修订仍可按身份读。
		if r.Status != AssetAvailable {
			continue
		}
		a := replicaAsAsset(r)
		if t, ok := firstAt[r.ID]; ok {
			a.CreatedAt = t
		}
		if err := s.canViewAssetMeta(ctx, acc, a); err != nil {
			if errors.Is(err, domain.ErrForbidden) {
				continue
			}
			return nil, err
		}
		visible = append(visible, stripContent(a))
	}
	sort.SliceStable(visible, func(i, j int) bool {
		if visible[i].CreatedAt.Equal(visible[j].CreatedAt) {
			return visible[i].ID.String() > visible[j].ID.String()
		}
		return visible[i].CreatedAt.After(visible[j].CreatedAt)
	})
	return s.decorateAssets(ctx, visible)
}

func (s *Assets) decorateAssets(ctx context.Context, rows []Asset) ([]AssetView, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, a := range rows {
		ids = append(ids, a.CreatorID)
	}
	people, err := s.store.PeopleByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]AssetView, 0, len(rows))
	for _, a := range rows {
		view := AssetView{Asset: a}
		if p, ok := people[a.CreatorID]; ok {
			view.CreatorLogin = p.LoginName
			view.CreatorDisplay = p.DisplayName
		}
		out = append(out, view)
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
