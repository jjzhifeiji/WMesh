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
	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// 审计对象：身份加修订。
func assetTarget(id uuid.UUID, rev int64) string {
	return id.String() + " rev=" + strconv.FormatInt(rev, 10)
}

// 列表不回正文。
func stripContent(a Asset) Asset {
	a.Content = nil
	return a
}

// assetCopyable 工艺按调用方；工程没有可复制，恒为是。
func assetCopyable(kind string, copyable bool) bool {
	if kind == KindProject {
		return true
	}
	return copyable
}

// replicaAsAsset 把已收副本当成平台级资产视图。
func replicaAsAsset(r AssetReplica) Asset {
	return Asset{
		ID: r.ID, Kind: r.Kind, Level: AssetLevelPlatform, Name: r.Name, Code: r.Code,
		Status: r.Status, Copyable: r.Copyable, WeldKind: r.WeldKind, Revision: r.Revision,
		Content: r.Content, Digest: r.Digest, Deps: r.Deps,
		CreatedAt: r.ReceivedAt, UpdatedAt: r.ReceivedAt,
	}
}

// loadReplica 取最高修订副本并核对摘要。
func (s *Assets) loadReplica(ctx context.Context, id uuid.UUID) (Asset, error) {
	// 摘要不符当完整性失败。
	r, err := s.store.LatestReplica(ctx, id)
	if err != nil {
		return Asset{}, err
	}
	if !digest.Match(r.Content, r.Digest) {
		return Asset{}, domain.ErrIntegrity
	}
	return replicaAsAsset(r), nil
}

// loadReplicaMeta 只取副本元数据，不解开正文。
func (s *Assets) loadReplicaMeta(ctx context.Context, id uuid.UUID) (Asset, error) {
	r, err := s.store.LatestReplicaMeta(ctx, id)
	if err != nil {
		return Asset{}, err
	}
	return replicaAsAsset(r), nil
}

// loadAny 本厂原件优先，没有再看已收副本。
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

// loadAnyMeta 只读元数据；原件没有再看副本。
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

// loadChecked 读本厂原件并核对摘要。
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

// loadCheckedMeta 只读本厂原件元数据，不解包。
func (s *kernel) loadCheckedMeta(ctx context.Context, id uuid.UUID) (Asset, error) {
	return s.store.GovernedAssetMetaByID(ctx, id)
}

// canAuthorFactory 本厂有效账号都能制作、改厂级。
func (s *kernel) canAuthorFactory(ctx context.Context, acc Account, unit *uuid.UUID) error {
	_ = ctx
	_ = acc
	_ = unit
	return nil
}

// canTouchPersonal 个人级正文：创建人或覆盖该处的管理员。
func (s *kernel) canTouchPersonal(ctx context.Context, acc Account, a Asset) error {
	if a.Level != AssetLevelPersonal {
		return nil
	}
	if acc.ID == a.CreatorID {
		return nil
	}
	return s.can(ctx, acc, permManageOrg, a.OrgUnitID)
}

// opCovers 下发按操作员作用域；制作不走这里。
func (s *kernel) opCovers(ctx context.Context, acc Account, unit *uuid.UUID) error {
	grants, err := s.grantsOf(ctx, acc.ID)
	if err != nil {
		return err
	}
	op := withRoles(grants, RoleOperator)
	if unit == nil {
		for _, g := range op {
			if g.ScopeKind == ScopeFactory {
				return nil
			}
		}
		return domain.ErrForbidden
	}
	ok, err := s.covers(ctx, op, unit)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return domain.ErrForbidden
}

// resolveAuthorContext 选定制作时工作位置并冻结当时路径。
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
		return &unit.ID, nil, domain.ErrDisabledOrgUnit // 停用节点不能再当新上下文
	}
	// 必须是当前有效分配。
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
	// 冻结当时组织路径。
	path, err := s.store.PathSnapshot(ctx, unit.ID)
	if err != nil {
		return &unit.ID, nil, err
	}
	return &unit.ID, path, nil
}

// insertAuthored 套模版后落草稿；工程依赖从参数补齐。
func (s *Assets) insertAuthored(ctx context.Context, acc Account, wc WorkContext, kind, level, name string, content []byte, deps []AssetDep, copyable bool, weldKind ...string) (Asset, error) {
	unitID, path, err := s.resolveAuthorContext(ctx, acc, wc)
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_asset", kind, audit.Deny, unitID, path)
		return Asset{}, err
	}
	weld, err := store.NormalizeWeldKind(firstWeldKind(weldKind))
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	content, err = s.normalizeContent(ctx, kind, content)
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	if kind == KindProject {
		if err := assertProjectWeldKind(weld, content); err != nil {
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		// 参数里选过的工艺补进依赖，钉当前修订。
		deps, err = s.fillProjectDeps(ctx, acc, level, content, deps)
		if err != nil {
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		if level == AssetLevelPersonal {
			err = s.assertPersonalProjectDeps(ctx, acc, deps)
		} else {
			err = s.assertFactoryProcessDeps(ctx, deps)
		}
		if err != nil {
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		if err := s.assertDepsWeldKind(ctx, weld, deps); err != nil {
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		// 套完后工程引用须落在已声明依赖里。
		if err := s.assertProjectProcessIDs(ctx, content, deps); err != nil {
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
	}
	return s.insertGoverned(ctx, acc, unitID, path, kind, level, name, content, deps, copyable, weld)
}

// insertGoverned 落一条草稿；调用方已决定是否套过模版。
func (s *Assets) insertGoverned(ctx context.Context, acc Account, unitID *uuid.UUID, path []PathNode, kind, level, name string, content []byte, deps []AssetDep, copyable bool, weldKind ...string) (Asset, error) {
	weld, err := store.NormalizeWeldKind(firstWeldKind(weldKind))
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	return s.putGoverned(ctx, acc, unitID, path, Asset{
		Kind: kind, Level: level, Name: name, Status: AssetDraft, Copyable: assetCopyable(kind, copyable), WeldKind: weld,
		Content: content, Digest: digest.Sum(content), CreatorID: acc.ID,
		OrgUnitID: unitID, OrgPath: path, Deps: deps,
	})
}

// putGoverned 写入本厂原件；状态由调用方决定，管理后台新建仍走草稿。
func (s *Assets) putGoverned(ctx context.Context, acc Account, unitID *uuid.UUID, path []PathNode, in Asset) (Asset, error) {
	row, err := s.store.InsertGovernedAsset(ctx, in)
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_asset", in.Name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	if err := s.auditAt(ctx, &acc.ID, "create_asset", assetTarget(row.ID, row.Revision), audit.Allow, unitID, path); err != nil {
		return Asset{}, err
	}
	return stripContent(row), nil
}

// CreatePadPersonal 平板保存个人级：立刻可用，不走管理后台发布。
func (s *Assets) CreatePadPersonal(ctx context.Context, token string, kind, name string, content []byte, id uuid.UUID, code string, deps []AssetDep, weldKind ...string) (Asset, error) {
	return s.CreatePad(ctx, token, AssetLevelPersonal, kind, name, content, id, code, deps, weldKind...)
}

// CreatePad 示教器新建：个人级谁都可以；厂级只给管理员；身份沿用调用方的 UUIDv7。
func (s *Assets) CreatePad(ctx context.Context, token, level, kind, name string, content []byte, id uuid.UUID, code string, deps []AssetDep, weldKind ...string) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", kind, audit.Deny)
		return Asset{}, domain.ErrInvalidName
	}
	if kind != KindProcess && kind != KindProject {
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", kind, audit.Deny)
		return Asset{}, domain.ErrNotFound
	}
	if level == "" {
		level = AssetLevelPersonal
	}
	if level == AssetLevelPlatform || (level != AssetLevelPersonal && level != AssetLevelFactory) {
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", kind, audit.Deny)
		return Asset{}, domain.ErrForbidden
	}
	// 厂级新建不看组织路径，管理员即可。
	if level == AssetLevelFactory {
		if err := s.canPadAdmin(ctx, acc); err != nil {
			_ = s.audit(ctx, &acc.ID, nil, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
	}
	unitID, path, err := s.resolveAuthorContext(ctx, acc, WorkContext{Direct: true})
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	weld := firstWeldKind(weldKind)
	if weld == "" && kind == KindProject {
		weld = contenttpl.InferWeldKind(content)
	}
	weld, err = store.NormalizeWeldKind(weld)
	if err != nil {
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	if kind == KindProject {
		if err := contenttpl.RejectProcessPath(content); err != nil {
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, domain.ErrForbidden
		}
		if err := assertProjectWeldKind(weld, content); err != nil {
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		deps, err = s.fillProjectDeps(ctx, acc, level, content, deps)
		if err != nil {
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		if err := s.assertPersonalProjectDeps(ctx, acc, deps); err != nil {
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		if err := s.assertDepsWeldKind(ctx, weld, deps); err != nil {
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		if err := s.assertProjectProcessIDs(ctx, content, deps); err != nil {
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
	}
	return s.putGoverned(ctx, acc, unitID, path, Asset{
		ID: id, Kind: kind, Level: level, Name: name, Code: strings.TrimSpace(code),
		Status: AssetAvailable, Copyable: true, WeldKind: weld, Content: content, Digest: digest.Sum(content),
		CreatorID: acc.ID, OrgUnitID: unitID, OrgPath: path, Deps: deps,
	})
}

// CopyProcess 可复制工艺另存为新草稿，原件正文原样拷贝，不套模版；平台级副本落成本厂厂级。
func (s *Assets) CopyProcess(ctx context.Context, token string, assetID uuid.UUID, name string) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	name = strings.TrimSpace(name)
	// 有效账号才能另存；失败一律记拒绝。
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
	if err := s.canTouchPersonal(ctx, acc, src); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
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
	row, err := s.insertGoverned(ctx, acc, unitID, path, KindProcess, level, name, src.Content, nil, true, src.WeldKind)
	if err != nil {
		return Asset{}, err
	}
	s.placeCopyBeside(ctx, src.ID, row.ID)
	return row, nil
}

// CreateFactoryProcess 本厂有效账号创建厂级工艺，默认可复制，状态草稿。
func (s *Assets) CreateFactoryProcess(ctx context.Context, token string, wc WorkContext, name string, content []byte, weldKind ...string) (Asset, error) {
	return s.CreateProcess(ctx, token, wc, AssetLevelFactory, name, content, true, weldKind...)
}

// CreatePersonalProcess 本厂有效账号在工作上下文中写入个人级工艺。
func (s *Assets) CreatePersonalProcess(ctx context.Context, token string, wc WorkContext, name string, content []byte, weldKind ...string) (Asset, error) {
	return s.CreateProcess(ctx, token, wc, AssetLevelPersonal, name, content, true, weldKind...)
}

// CreateProcess 本厂有效账号创建工艺；可复制由调用方给定，状态草稿。
func (s *Assets) CreateProcess(ctx context.Context, token string, wc WorkContext, level, name string, content []byte, copyable bool, weldKind ...string) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	if level != AssetLevelFactory && level != AssetLevelPersonal {
		return Asset{}, domain.ErrNotFound
	}
	return s.insertAuthored(ctx, acc, wc, KindProcess, level, name, content, nil, copyable, weldKind...)
}

// CreatePersonalProject 创建个人级工程；参数里的工艺写入 deps，不必另填。
func (s *Assets) CreatePersonalProject(ctx context.Context, token string, wc WorkContext, name string, content []byte, deps []AssetDep, weldKind ...string) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.insertAuthored(ctx, acc, wc, KindProject, AssetLevelPersonal, name, content, deps, true, weldKind...)
}

// assertPersonalProjectDeps 个人工程只能钉自己的个人工艺或厂级可用工艺。
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

// CreateFactoryProject 创建厂级工程；参数里的工艺写入 deps，不必另填。
func (s *Assets) CreateFactoryProject(ctx context.Context, token string, wc WorkContext, name string, content []byte, deps []AssetDep, weldKind ...string) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.insertAuthored(ctx, acc, wc, KindProject, AssetLevelFactory, name, content, deps, true, weldKind...)
}

// assertFactoryProcessDeps 厂级工程可钉本厂可用厂级、个人级或已收平台级工艺。
func (s *kernel) assertFactoryProcessDeps(ctx context.Context, deps []AssetDep) error {
	for _, d := range deps {
		// 先看本厂原件，没有再看已收平台级。
		p, err := s.store.GovernedAssetMetaByID(ctx, d.ID)
		if err == nil {
			if p.Kind != KindProcess || !factoryProjectProcessLevel(p.Level) || p.Revision != d.Revision || !bytes.Equal(p.Digest, d.Digest) {
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

// fillProjectDeps 把参数里的工艺补进 deps，钉当前可用修订。
func (s *kernel) fillProjectDeps(ctx context.Context, acc Account, level string, content []byte, deps []AssetDep) ([]AssetDep, error) {
	ids, err := s.collectProjectProcessIDs(ctx, content)
	if err != nil {
		if errors.Is(err, domain.ErrForbidden) {
			return nil, err
		}
		return nil, domain.ErrAssetDependency
	}
	return mergeProjectDeps(deps, ids, func(id uuid.UUID) (AssetDep, error) {
		if level == AssetLevelPersonal {
			return s.pinPersonalProcess(ctx, acc, id)
		}
		return s.pinFactoryProcess(ctx, id)
	})
}

// pinFactoryProcess 厂级工程钉本厂可用厂级、个人级或已收平台级工艺。
func (s *kernel) pinFactoryProcess(ctx context.Context, id uuid.UUID) (AssetDep, error) {
	p, err := s.store.GovernedAssetMetaByID(ctx, id)
	if err == nil {
		if p.Kind != KindProcess || !factoryProjectProcessLevel(p.Level) {
			return AssetDep{}, domain.ErrAssetDependency
		}
		if p.Status != AssetAvailable {
			return AssetDep{}, domain.ErrAssetNotAvailable
		}
		return AssetDep{ID: p.ID, Revision: p.Revision, Digest: p.Digest}, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return AssetDep{}, err
	}
	r, err := s.store.LatestReplicaMeta(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return AssetDep{}, domain.ErrAssetDependency
		}
		return AssetDep{}, err
	}
	if r.Kind != KindProcess || r.Level != AssetLevelPlatform {
		return AssetDep{}, domain.ErrAssetDependency
	}
	if r.Status != AssetAvailable {
		return AssetDep{}, domain.ErrAssetNotAvailable
	}
	return AssetDep{ID: r.ID, Revision: r.Revision, Digest: r.Digest}, nil
}

// factoryProjectProcessLevel 厂级工程可钉本厂原件里的厂级和个人级工艺。
func factoryProjectProcessLevel(level string) bool {
	return level == AssetLevelFactory || level == AssetLevelPersonal
}

// pinPersonalProcess 个人工程钉自己的个人工艺或本厂厂级可用工艺。
func (s *kernel) pinPersonalProcess(ctx context.Context, acc Account, id uuid.UUID) (AssetDep, error) {
	p, err := s.loadCheckedMeta(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return AssetDep{}, domain.ErrAssetDependency
		}
		return AssetDep{}, err
	}
	if p.Kind != KindProcess {
		return AssetDep{}, domain.ErrAssetDependency
	}
	if p.Status != AssetAvailable {
		return AssetDep{}, domain.ErrAssetNotAvailable
	}
	switch p.Level {
	case AssetLevelFactory:
	case AssetLevelPersonal:
		if p.CreatorID != acc.ID {
			return AssetDep{}, domain.ErrAssetDependency
		}
	default:
		return AssetDep{}, domain.ErrAssetDependency
	}
	return AssetDep{ID: p.ID, Revision: p.Revision, Digest: p.Digest}, nil
}

// resolveProjectDeps 改依赖时按身份重钉当前可用修订，跟上工艺升版。
func (s *kernel) resolveProjectDeps(ctx context.Context, acc Account, level string, deps []AssetDep) ([]AssetDep, error) {
	out := make([]AssetDep, 0, len(deps))
	seen := map[string]struct{}{}
	for _, d := range deps {
		key := d.ID.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		var pin AssetDep
		var err error
		if level == AssetLevelPersonal {
			pin, err = s.pinPersonalProcess(ctx, acc, d.ID)
		} else {
			pin, err = s.pinFactoryProcess(ctx, d.ID)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, pin)
	}
	return out, nil
}

// mergeProjectDeps 保留已声明依赖，再按参数引用补缺。
func mergeProjectDeps(existing []AssetDep, ids []string, lookup func(uuid.UUID) (AssetDep, error)) ([]AssetDep, error) {
	have := make(map[string]struct{}, len(existing)+len(ids))
	out := make([]AssetDep, 0, len(existing)+len(ids))
	for _, d := range existing {
		key := d.ID.String()
		if _, ok := have[key]; ok {
			continue
		}
		have[key] = struct{}{}
		out = append(out, d)
	}
	for _, raw := range ids {
		if _, ok := have[raw]; ok {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, domain.ErrAssetDependency
		}
		d, err := lookup(id)
		if err != nil {
			return nil, err
		}
		have[raw] = struct{}{}
		out = append(out, d)
	}
	return out, nil
}

// assertProjectProcessIDs 按当前工程模版收集引用，非空的必须是本行 deps 的身份。
func (s *kernel) assertProjectProcessIDs(ctx context.Context, content []byte, deps []AssetDep) error {
	ids, err := s.collectProjectProcessIDs(ctx, content)
	if err != nil {
		if errors.Is(err, domain.ErrForbidden) {
			return err
		}
		return domain.ErrAssetDependency
	}
	allowed := make(map[string]struct{}, len(deps))
	for _, d := range deps {
		allowed[d.ID.String()] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := allowed[id]; !ok {
			return domain.ErrAssetDependency
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
		// 工程焊道引用必须落在已声明依赖里。
		if cur.Kind == KindProject {
			if err := s.assertProjectProcessIDs(ctx, content, cur.Deps); err != nil {
				return store.AssetWrite{}, err
			}
			if err := assertProjectWeldKind(cur.WeldKind, content); err != nil {
				return store.AssetWrite{}, err
			}
		}
		return store.AssetWrite{Name: cur.Name, Content: content, Digest: digest.Sum(content), Copyable: cur.Copyable, Status: cur.Status, Deps: cur.Deps}, nil
	})
}

// ApplyAppContent 示教器连上后用本机正文盖当前行，不跟网页对版本号。
func (s *Assets) ApplyAppContent(ctx context.Context, token string, assetID uuid.UUID, content []byte) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	var rev int64
	var last error
	for range 3 {
		row, seen, err := s.applyAppOnce(ctx, acc, assetID, content)
		rev = seen
		if err == nil {
			return row, nil
		}
		// 并发顶掉当前修订就再读一次；仍不对才把冲突交给调用方。
		if !errors.Is(err, domain.ErrRevisionConflict) {
			return Asset{}, err
		}
		last = err
	}
	_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(assetID, rev), audit.Deny)
	if last == nil {
		last = domain.ErrRevisionConflict
	}
	return Asset{}, last
}

// applyAppOnce 按当前修订盖一次；撞上并发不记审计，交给外层重试。
func (s *Assets) applyAppOnce(ctx context.Context, acc Account, assetID uuid.UUID, content []byte) (Asset, int64, error) {
	cur, err := s.loadAnyMeta(ctx, assetID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetID.String(), audit.Deny)
		}
		return Asset{}, 0, err
	}
	// 平台级副本不许回写；厂级只给管理员；个人级只给创建人。
	if err := s.canApplyApp(ctx, acc, cur); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
		return Asset{}, cur.Revision, err
	}
	// 停用仍盖正文，状态保持停用。
	deps := cur.Deps
	if cur.Kind == KindProject {
		if err := contenttpl.RejectProcessPath(content); err != nil {
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
			return Asset{}, cur.Revision, domain.ErrForbidden
		}
		if err := assertProjectWeldKind(cur.WeldKind, content); err != nil {
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
			return Asset{}, cur.Revision, err
		}
		// 先有工艺再盖工程：正文里新引用钉到当前可用修订。
		deps, err = s.fillProjectDeps(ctx, acc, cur.Level, content, cur.Deps)
		if err != nil {
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
			return Asset{}, cur.Revision, err
		}
		if err := s.assertProjectProcessIDs(ctx, content, deps); err != nil {
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
			return Asset{}, cur.Revision, err
		}
	}
	row, err := s.store.UpdateGovernedAsset(ctx, assetID, cur.Revision, store.AssetWrite{
		Name: cur.Name, Content: content, Digest: digest.Sum(content),
		Copyable: cur.Copyable, Status: cur.Status, Deps: deps,
	})
	if err != nil {
		if !errors.Is(err, domain.ErrRevisionConflict) {
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(assetID, cur.Revision), audit.Deny)
		}
		return Asset{}, cur.Revision, err
	}
	if err := s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, row.Revision, err
	}
	return stripContent(row), row.Revision, nil
}

// canApplyApp 平台级拒绝；厂级只给管理员；个人级只给创建人。
func (s *kernel) canApplyApp(ctx context.Context, acc Account, a Asset) error {
	switch a.Level {
	case AssetLevelPlatform:
		return domain.ErrForbidden
	case AssetLevelPersonal:
		if acc.ID != a.CreatorID {
			return domain.ErrForbidden
		}
		return nil
	case AssetLevelFactory:
		return s.canPadAdmin(ctx, acc)
	default:
		return domain.ErrForbidden
	}
}

// canPadAdmin 超管或任一组织管理员即可改、建厂级，不看组织路径。
func (s *kernel) canPadAdmin(ctx context.Context, acc Account) error {
	grants, err := s.grantsOf(ctx, acc.ID)
	if err != nil {
		return err
	}
	if isFactorySA(grants) {
		return nil
	}
	for _, g := range grants {
		if g.Role == RoleOrgAdmin {
			return nil
		}
	}
	return domain.ErrForbidden
}

// SetAssetCopyable 未停用工艺可改可复制；工程没有这项。
func (s *Assets) SetAssetCopyable(ctx context.Context, token string, assetID uuid.UUID, expected int64, copyable bool) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.mutateAsset(ctx, acc, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Kind != KindProcess {
			return store.AssetWrite{}, domain.ErrForbidden
		}
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Digest: cur.Digest, Copyable: copyable, Status: cur.Status, Deps: cur.Deps, KeepContent: true}, nil
	})
}

// SetAssetWeldKind 未停用的本厂工艺或工程可改作业类型；工程正文对不上时换成空焊道。
func (s *Assets) SetAssetWeldKind(ctx context.Context, token string, assetID uuid.UUID, expected int64, weldKind string) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	return s.mutateAsset(ctx, acc, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Kind != KindProcess && cur.Kind != KindProject {
			return store.AssetWrite{}, domain.ErrForbidden
		}
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		kind, err := store.NormalizeWeldKind(weldKind)
		if err != nil {
			return store.AssetWrite{}, err
		}
		write := store.AssetWrite{Name: cur.Name, Digest: cur.Digest, Copyable: cur.Copyable, Status: cur.Status, Deps: cur.Deps, WeldKind: kind, KeepContent: true}
		if cur.Kind != KindProject {
			return write, nil
		}
		kept, err := s.depsMatchingWeldKind(ctx, kind, cur.Deps)
		if err != nil {
			return store.AssetWrite{}, err
		}
		write.Deps = kept
		body := cur.Content
		if len(body) == 0 {
			full, err := s.loadChecked(ctx, cur.ID)
			if err != nil {
				return store.AssetWrite{}, err
			}
			body = full.Content
		}
		if err := assertProjectWeldKind(kind, body); err != nil {
			empty := []byte("[]")
			write.KeepContent = false
			write.Content = empty
			write.Digest = digest.Sum(empty)
		}
		return write, nil
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
	// 须有制作权；个人级另验创建人或管理员。失败一律记拒绝。
	cur, err := s.loadCheckedMeta(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetID.String(), audit.Deny)
		return err
	}
	if err := s.canAuthorFactory(ctx, acc, cur.OrgUnitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return err
	}
	if err := s.canTouchPersonal(ctx, acc, cur); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return err
	}
	// 仍被工程钉着则拒绝。
	used, err := s.store.AssetIsReferenced(ctx, assetID)
	if err != nil {
		return err
	}
	if used {
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return domain.ErrReferenced
	}
	// 无引用才删本厂原件。
	if err := s.store.DeleteGovernedAsset(ctx, assetID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Allow)
}

// mutateAsset 按期望修订改本厂原件；个人级须创建人或覆盖该处的管理员。
func (s *kernel) mutateAsset(ctx context.Context, acc Account, assetID uuid.UUID, expected int64, action string, patch func(Asset) (store.AssetWrite, error)) (Asset, error) {
	// 须有制作权；个人级另验创建人或管理员。失败一律记拒绝。
	cur, err := s.loadCheckedMeta(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, expected), audit.Deny)
		return Asset{}, err
	}
	if err := s.canAuthorFactory(ctx, acc, cur.OrgUnitID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, cur.Revision), audit.Deny)
		return Asset{}, err
	}
	if err := s.canTouchPersonal(ctx, acc, cur); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, cur.Revision), audit.Deny)
		return Asset{}, err
	}
	w, err := patch(cur)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, cur.Revision), audit.Deny)
		return Asset{}, err
	}
	// 按期望修订写入，冲突则拒绝。
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
func (s *kernel) canViewAssetMeta(ctx context.Context, acc Account, a Asset) error {
	_ = ctx
	_ = acc
	_ = a
	return nil
}

// listVisibleAssets 厂端列表与平板登录共用同一份可见元数据，不含正文。
func (s *kernel) listVisibleAssets(ctx context.Context, acc Account, kind string) ([]Asset, error) {
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
	return visible, nil
}

// GetAsset 读元数据，不解包正文。
func (s *Assets) GetAsset(ctx context.Context, token string, assetID uuid.UUID) (Asset, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Asset{}, err
	}
	// 读元数据，不解包正文。失败记拒绝。
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

// ReadAssetContent 读正文并核对摘要；个人级创建人或管理员；不可复制的平台级工艺不给人看。
func (s *Assets) ReadAssetContent(ctx context.Context, token string, assetID uuid.UUID) ([]byte, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return nil, err
	}
	// 个人级创建人或覆盖该处的管理员；不可复制平台级工艺不给人看。失败一律记拒绝。
	meta, err := s.loadAnyMeta(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetID.String(), audit.Deny)
		return nil, err
	}
	// 不可复制的平台级工艺是严格保密件，厂端人员不得看参数；工程不保密。
	if meta.Kind == KindProcess && meta.Level == AssetLevelPlatform && !meta.Copyable {
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetTarget(meta.ID, meta.Revision), audit.Deny)
		return nil, domain.ErrForbidden
	}
	if err := s.canTouchPersonal(ctx, acc, meta); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetTarget(meta.ID, meta.Revision), audit.Deny)
		return nil, err
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

// PromoteToFactory 把可复制的未停用个人级升为厂级：草稿也可升；第一次复制新身份；再升按正文摘要跳过或覆盖。
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
	// 停用不能升；草稿可以，厂级新条目仍记可用。
	if src.Status == AssetDisabled {
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrAssetNotAvailable
	}
	if src.Kind == KindProcess && !src.Copyable {
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrAssetNotCopyable
	}
	if err := s.assertPromoteToFactoryDeps(ctx, src); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
	rev := src.Revision
	// 已升过则按摘要跳过或覆盖。
	existing, err := s.store.GovernedAssetBySourceID(ctx, src.ID)
	if err == nil {
		if bytes.Equal(existing.Digest, src.Digest) {
			if err := s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(existing.ID, existing.Revision)+" from "+assetTarget(src.ID, src.Revision), audit.Allow); err != nil {
				return Asset{}, err
			}
			return stripContent(existing), nil
		}
		// 正文变了才覆盖厂级。
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
	// 第一次升档复制新身份。
	row, err := s.store.InsertGovernedAsset(ctx, Asset{
		Kind: src.Kind, Level: AssetLevelFactory, Name: src.Name, Status: AssetAvailable, Copyable: true, WeldKind: src.WeldKind,
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

// assertPromoteToFactoryDeps 升厂级时工程依赖必须已是可用厂级工艺。
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
	// 导出明文快照给 WAN 升档。
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
	Code     string    `json:"code"`     // 只读编号
	Revision int64     `json:"revision"` // 当前修订
	Digest   []byte    `json:"digest"`   // 内容摘要
	Status   string    `json:"status"`   // draft / available / disabled
	Copyable bool      `json:"copyable"` // 工艺才有；工程恒为是
	WeldKind string    `json:"weldKind"` // 作业类型
}

// 升档列表只带元数据，不含正文。
func toPromotable(a Asset) PromotableAsset {
	return PromotableAsset{
		ID: a.ID, Kind: a.Kind, Level: a.Level, Name: a.Name, Code: a.Code, Revision: a.Revision,
		Digest: a.Digest, Status: a.Status, Copyable: a.Copyable, WeldKind: a.WeldKind,
	}
}

// ListPromotable 列给 WAN 升档看的本厂全部工艺/工程：厂级、个人级、已下发平台级，不分状态，不含正文。
func (s *Assets) ListPromotable(ctx context.Context, kind string) ([]PromotableAsset, error) {
	if kind != "" && kind != KindProcess && kind != KindProject {
		return nil, domain.ErrNotFound
	}
	// 本厂原件和已下发平台级一并列出，不含正文。
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

// SnapshotForChannel 通道升档用：本厂厂级和个人级可出快照；工艺须可复制，工程不看可复制。
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
	if src.Kind == KindProcess && !src.Copyable {
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
	// 只收本人已分配的有效节点。
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
	visible, err := s.listVisibleAssets(ctx, acc, kind)
	if err != nil {
		return nil, err
	}
	return s.decorateAssets(ctx, visible)
}

// decorateAssets 补创建人登录名和显示名，不含正文。
func (s *Assets) decorateAssets(ctx context.Context, rows []Asset) ([]AssetView, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, a := range rows {
		ids = append(ids, a.CreatorID)
	}
	// 补创建人名字。
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
