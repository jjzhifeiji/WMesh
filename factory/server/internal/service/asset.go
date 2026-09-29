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
	// 把身份和修订拼成审计对象，方便对上是哪一版。
	return id.String() + " rev=" + strconv.FormatInt(rev, 10)
}

// 列表不回正文。
func stripContent(a Asset) Asset {
	// 清掉正文，列表和返回值不带工艺参数。
	a.Content = nil
	return a
}

// assetCopyable 工艺按调用方；工程没有可复制，恒为是。
func assetCopyable(kind string, copyable bool) bool {
	// 工程没有可复制这一说，一律当成可以。
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
	// 副本读失败则不能当组包根。
	if err != nil {
		return Asset{}, err
	}
	// 摘要对不上当被篡改，拒绝继续使用。
	if !digest.Match(r.Content, r.Digest) {
		return Asset{}, domain.ErrIntegrity
	}
	// 把副本看成平台级资产，方便和原件一起筛。
	return replicaAsAsset(r), nil
}

// loadReplicaMeta 只取副本元数据，不解开正文。
func (s *Assets) loadReplicaMeta(ctx context.Context, id uuid.UUID) (Asset, error) {
	// 取副本元数据，先不解开正文。
	r, err := s.store.LatestReplicaMeta(ctx, id)
	// 副本元数据读失败则停下。
	if err != nil {
		return Asset{}, err
	}
	// 把副本看成平台级资产，方便和原件一起筛。
	return replicaAsAsset(r), nil
}

// loadAny 本厂原件优先，没有再看已收副本。
func (s *Assets) loadAny(ctx context.Context, id uuid.UUID) (Asset, error) {
	// 读本厂原件并核对摘要，不符就当坏的。
	a, err := s.loadChecked(ctx, id)
	// 本厂原件读到了就用，不再去查已收副本。
	if err == nil {
		return a, nil
	}
	// 不是找不到则把库错误抛回，避免当成没有。
	if !errors.Is(err, domain.ErrNotFound) {
		return Asset{}, err
	}
	// 本厂没有原件，改读已收副本。
	return s.loadReplica(ctx, id)
}

// loadAnyMeta 只读元数据；原件没有再看副本。
func (s *Assets) loadAnyMeta(ctx context.Context, id uuid.UUID) (Asset, error) {
	// 按身份读本厂原件元数据，不解正文。
	a, err := s.store.GovernedAssetMetaByID(ctx, id)
	// 原件元数据在就用，没有再看副本。
	if err == nil {
		return a, nil
	}
	// 不是找不到则把库错误抛回，避免当成没有。
	if !errors.Is(err, domain.ErrNotFound) {
		return Asset{}, err
	}
	// 原件没有，改读副本元数据。
	return s.loadReplicaMeta(ctx, id)
}

// loadChecked 读本厂原件并核对摘要。
func (s *kernel) loadChecked(ctx context.Context, id uuid.UUID) (Asset, error) {
	// 按身份读本厂原件正文。
	a, err := s.store.GovernedAssetByID(ctx, id)
	// 原件读失败则停下，不把残缺当成功。
	if err != nil {
		return Asset{}, err
	}
	// 摘要对不上当被篡改，拒绝继续使用。
	if !digest.Match(a.Content, a.Digest) {
		return Asset{}, domain.ErrIntegrity
	}
	return a, nil
}

// loadCheckedMeta 只读本厂原件元数据，不解包。
func (s *kernel) loadCheckedMeta(ctx context.Context, id uuid.UUID) (Asset, error) {
	// 按身份读本厂原件元数据，不解正文。
	return s.store.GovernedAssetMetaByID(ctx, id)
}

// canAuthorFactory 本厂有效账号都能制作、改厂级。
func (s *kernel) canAuthorFactory(ctx context.Context, acc Account, unit *uuid.UUID) error {
	// 厂级制作不看上下文，参数留着对齐别的入口。
	_ = ctx
	// 本厂有效账号都能制作，这里不再分人。
	_ = acc
	// 不看组织节点，管理员或有效账号即可。
	_ = unit
	return nil
}

// canTouchPersonal 个人级正文：创建人或覆盖该处的管理员。
func (s *kernel) canTouchPersonal(ctx context.Context, acc Account, a Asset) error {
	// 不是个人级就放行，厂级不在这里拦人。
	if a.Level != AssetLevelPersonal {
		return nil
	}
	// 创建人可以看自己的个人级正文。
	if acc.ID == a.CreatorID {
		return nil
	}
	// 按本厂权限判断当前人能不能做。
	return s.can(ctx, acc, permManageOrg, a.OrgUnitID)
}

// opCovers 下发按操作员作用域；制作不走这里。
func (s *kernel) opCovers(ctx context.Context, acc Account, unit *uuid.UUID) error {
	// 读出当前人的授权，再判断角色。
	grants, err := s.grantsOf(ctx, acc.ID)
	// 授权读不到则拒绝，不当成没权限放过。
	if err != nil {
		return err
	}
	// 只留下操作员授权，制作权不在这里看。
	op := withRoles(grants, RoleOperator)
	// 没指定节点时，只接受整厂范围的操作员。
	if unit == nil {
		// 逐条看操作员授权里有没有整厂范围。
		for _, g := range op {
			// 整厂操作员可以不挂节点就通过。
			if g.ScopeKind == ScopeFactory {
				return nil
			}
		}
		return domain.ErrForbidden
	}
	// 计算操作员授权是否盖住目标节点。
	ok, err := s.covers(ctx, op, unit)
	// 覆盖关系算不清则拒绝下发。
	if err != nil {
		return err
	}
	// 作用域盖住了就允许，没盖住下面会拒绝。
	if ok {
		return nil
	}
	return domain.ErrForbidden
}

// resolveAuthorContext 选定制作时工作位置并冻结当时路径。
func (s *Assets) resolveAuthorContext(ctx context.Context, acc Account, wc WorkContext) (*uuid.UUID, []PathNode, error) {
	// 直属和节点只能二选一，两个都有或都没有则拒绝。
	if wc.Direct == (wc.OrgUnitID != nil) {
		return nil, nil, domain.ErrWorkContext
	}
	// 直属工厂不挂节点，先确认能做厂级。
	if wc.Direct {
		// 没有制作权则拒绝，不改这份资产。
		if err := s.canAuthorFactory(ctx, acc, nil); err != nil {
			return nil, []PathNode{}, err
		}
		return nil, []PathNode{}, nil
	}
	// 读出组织节点，停用的不能当工作位置。
	unit, err := s.store.Unit(ctx, *wc.OrgUnitID)
	// 节点读不到则不能在这里制作。
	if err != nil {
		return wc.OrgUnitID, nil, err
	}
	// 停用的组织节点不能再当新的工作位置。
	if unit.Status != StatusActive {
		return &unit.ID, nil, domain.ErrDisabledOrgUnit // 停用节点不能再当新上下文
	}
	// 必须是当前有效分配。
	ok, err := s.store.HasActiveAssignment(ctx, acc.ID, unit.ID)
	// 分配查不到则不能用这个工作位置。
	if err != nil {
		return &unit.ID, nil, err
	}
	// 不是当前有效分配则拒绝，不能借旧节点制作。
	if !ok {
		return &unit.ID, nil, domain.ErrWorkContext
	}
	// 没有制作权则拒绝，不改这份资产。
	if err := s.canAuthorFactory(ctx, acc, &unit.ID); err != nil {
		return &unit.ID, nil, err
	}
	// 冻结当时组织路径。
	path, err := s.store.PathSnapshot(ctx, unit.ID)
	// 路径冻结失败则拒绝，避免残缺快照。
	if err != nil {
		return &unit.ID, nil, err
	}
	return &unit.ID, path, nil
}

// insertAuthored 套模版后落草稿；工程依赖从参数补齐。
func (s *Assets) insertAuthored(ctx context.Context, acc Account, wc WorkContext, kind, level, name string, content []byte, deps []AssetDep, copyable bool, weldKind ...string) (Asset, error) {
	// 定下制作时的工作位置，并准备冻结路径。
	unitID, path, err := s.resolveAuthorContext(ctx, acc, wc)
	// 工作位置不合法则拒绝，并补记审计。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", kind, audit.Deny, unitID, path)
		return Asset{}, err
	}
	// 把作业类型收成合法值，不合法就拒绝。
	weld, err := store.NormalizeWeldKind(firstWeldKind(weldKind))
	// 作业类型不合法则拒绝，并补记审计。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	// 按当前模版整理正文，套不上就拒绝。
	content, err = s.normalizeContent(ctx, kind, content)
	// 正文套不上模版则记拒绝。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	// 工程要补依赖并核对焊道，工艺不走这段。
	if kind == KindProject {
		// 焊道对不上则拒绝，并补记审计。
		if err := assertProjectWeldKind(weld, content); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		// 参数里选过的工艺补进依赖，钉当前修订。
		deps, err = s.fillProjectDeps(ctx, acc, level, content, deps)
		// 依赖钉不到当前可用工艺则记拒绝。
		if err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		// 个人工程只能钉自己的工艺或厂级可用工艺。
		if level == AssetLevelPersonal {
			// 个人工程只能钉自己的工艺或厂级可用工艺。
			err = s.assertPersonalProjectDeps(ctx, acc, deps)
			// 厂级工程改按厂级和已收工艺来核依赖。
		} else {
			// 厂级工程只能钉可用的本厂或已收工艺。
			err = s.assertFactoryProcessDeps(ctx, deps)
		}
		// 依赖不合格则拒绝，并补记审计。
		if err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		// 焊道类型对不上则记拒绝。
		if err := s.assertDepsWeldKind(ctx, weld, deps); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		// 套完后工程引用须落在已声明依赖里。
		if err := s.assertProjectProcessIDs(ctx, content, deps); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
	}
	// 校验都过了才落成一条资产。
	return s.insertGoverned(ctx, acc, unitID, path, kind, level, name, content, deps, copyable, weld)
}

// insertGoverned 落一条草稿；调用方已决定是否套过模版。
func (s *Assets) insertGoverned(ctx context.Context, acc Account, unitID *uuid.UUID, path []PathNode, kind, level, name string, content []byte, deps []AssetDep, copyable bool, weldKind ...string) (Asset, error) {
	// 把作业类型收成合法值，不合法就拒绝。
	weld, err := store.NormalizeWeldKind(firstWeldKind(weldKind))
	// 作业类型不合法则拒绝，并补记审计。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	// 写入本厂原件，成功还要记审计。
	return s.putGoverned(ctx, acc, unitID, path, Asset{
		Kind: kind, Level: level, Name: name, Status: AssetDraft, Copyable: assetCopyable(kind, copyable), WeldKind: weld,
		Content: content, Digest: digest.Sum(content), CreatorID: acc.ID,
		OrgUnitID: unitID, OrgPath: path, Deps: deps,
	})
}

// putGoverned 写入本厂原件；状态由调用方决定，管理后台新建仍走草稿。
func (s *Assets) putGoverned(ctx context.Context, acc Account, unitID *uuid.UUID, path []PathNode, in Asset) (Asset, error) {
	// 把这条原件写入本厂库。
	row, err := s.store.InsertGovernedAsset(ctx, in)
	// 落库失败则记拒绝，不假装已经建成。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", in.Name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.auditAt(ctx, &acc.ID, "create_asset", assetTarget(row.ID, row.Revision), audit.Allow, unitID, path); err != nil {
		return Asset{}, err
	}
	// 去掉正文再返回，避免工艺参数外泄。
	return stripContent(row), nil
}

// CreatePadPersonal 平板保存个人级：立刻可用，不走管理后台发布。
func (s *Assets) CreatePadPersonal(ctx context.Context, token string, kind, name string, content []byte, id uuid.UUID, code string, deps []AssetDep, weldKind ...string) (Asset, error) {
	// 交给示教器入口建个人级，厂级另有限制。
	return s.CreatePad(ctx, token, AssetLevelPersonal, kind, name, content, id, code, deps, weldKind...)
}

// CreatePad 示教器新建：个人级谁都可以；厂级只给管理员；身份沿用调用方的 UUIDv7。
func (s *Assets) CreatePad(ctx context.Context, token, level, kind, name string, content []byte, id uuid.UUID, code string, deps []AssetDep, weldKind ...string) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 去掉名字两端空白，空的下一步会拒绝。
	name = strings.TrimSpace(name)
	// 名字空则拒绝创建，并记下这次拒绝。
	if name == "" {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", kind, audit.Deny)
		return Asset{}, domain.ErrInvalidName
	}
	// 只接受工艺或工程，别的种类拒绝。
	if kind != KindProcess && kind != KindProject {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", kind, audit.Deny)
		return Asset{}, domain.ErrNotFound
	}
	// 没指定级别时按个人级，厂级必须点明。
	if level == "" {
		// 没指定级别就按个人级，谁登录都能建。
		level = AssetLevelPersonal
	}
	// 平台级不能在厂内新建，其它级别也拒绝。
	if level == AssetLevelPlatform || (level != AssetLevelPersonal && level != AssetLevelFactory) {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", kind, audit.Deny)
		return Asset{}, domain.ErrForbidden
	}
	// 厂级新建不看组织路径，管理员即可。
	if level == AssetLevelFactory {
		// 不是管理员则拒绝，并补记审计。
		if err := s.canPadAdmin(ctx, acc); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.audit(ctx, &acc.ID, nil, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
	}
	// 定下制作时的工作位置，并准备冻结路径。
	unitID, path, err := s.resolveAuthorContext(ctx, acc, WorkContext{Direct: true})
	// 工作位置不合法则拒绝，并补记审计。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	// 取出调用方给的作业类型，空的后面再推断。
	weld := firstWeldKind(weldKind)
	// 工程没给作业类型时，从正文推断一个。
	if weld == "" && kind == KindProject {
		// 工程没给作业类型时，从正文推断一个。
		weld = contenttpl.InferWeldKind(content)
	}
	// 把作业类型收成合法值，不合法就拒绝。
	weld, err = store.NormalizeWeldKind(weld)
	// 作业类型不合法则拒绝，并补记审计。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	// 工程要先去掉路径、补依赖并核对焊道。
	if kind == KindProject {
		// 仍带工艺路径则记拒绝，避免把参数拆出去。
		if err := contenttpl.RejectProcessPath(content); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, domain.ErrForbidden
		}
		// 焊道对不上则拒绝，并补记审计。
		if err := assertProjectWeldKind(weld, content); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		// 把正文里点到的工艺补进依赖，并钉当前修订。
		deps, err = s.fillProjectDeps(ctx, acc, level, content, deps)
		// 依赖钉不到当前可用工艺则记拒绝。
		if err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		// 依赖不合格则拒绝，并补记审计。
		if err := s.assertPersonalProjectDeps(ctx, acc, deps); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		// 焊道类型对不上则记拒绝。
		if err := s.assertDepsWeldKind(ctx, weld, deps); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
		// 有引用对不上依赖则拒绝，并补记审计。
		if err := s.assertProjectProcessIDs(ctx, content, deps); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
			return Asset{}, err
		}
	}
	// 写入本厂原件，成功还要记审计。
	return s.putGoverned(ctx, acc, unitID, path, Asset{
		ID: id, Kind: kind, Level: level, Name: name, Code: strings.TrimSpace(code),
		Status: AssetAvailable, Copyable: true, WeldKind: weld, Content: content, Digest: digest.Sum(content),
		CreatorID: acc.ID, OrgUnitID: unitID, OrgPath: path, Deps: deps,
	})
}

// CopyProcess 可复制工艺另存为新草稿，原件正文原样拷贝，不套模版；平台级副本落成本厂厂级。
func (s *Assets) CopyProcess(ctx context.Context, token string, assetID uuid.UUID, name string) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 去掉名字两端空白，空的下一步会拒绝。
	name = strings.TrimSpace(name)
	// 有效账号才能另存；失败一律记拒绝。
	if name == "" {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, domain.ErrInvalidName
	}
	// 先读原件元数据，没有再看副本。
	src, err := s.loadAnyMeta(ctx, assetID)
	// 元数据读不到则记拒绝，不继续。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	// 只有工艺能另存，工程不能走这条。
	if src.Kind != KindProcess {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrForbidden
	}
	// 停用的工艺不能另存。
	if src.Status == AssetDisabled {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrAssetNotAvailable
	}
	// 不可复制的工艺拒绝另存，避免参数外泄。
	if !src.Copyable {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrAssetNotCopyable
	}
	// 不是创建人也不是管理员则记拒绝。
	if err := s.canTouchPersonal(ctx, acc, src); err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
	// 本厂原件优先，没有再读已收副本。
	src, err = s.loadAny(ctx, assetID)
	// 原件和副本都读不到则记拒绝。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	// 默认落成厂级；个人级源在下一行改回。
	level := AssetLevelFactory
	// 源是个人级则副本仍是个人级，其余落成厂级。
	if src.Level == AssetLevelPersonal {
		// 源是个人级则副本仍归本人，不升成厂级。
		level = AssetLevelPersonal
	}
	// 定下制作时的工作位置，并准备冻结路径。
	unitID, path, err := s.resolveAuthorContext(ctx, acc, WorkContext{Direct: true})
	// 工作位置不合法则拒绝，并补记审计。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	// 校验都过了才落成一条资产。
	row, err := s.insertGoverned(ctx, acc, unitID, path, KindProcess, level, name, src.Content, nil, true, src.WeldKind)
	// 落库失败则创建不算成功。
	if err != nil {
		return Asset{}, err
	}
	// 副本尽量放到源文件旁边，失败不影响已建成的资产。
	s.placeCopyBeside(ctx, src.ID, row.ID)
	return row, nil
}

// CreateFactoryProcess 本厂有效账号创建厂级工艺，默认可复制，状态草稿。
func (s *Assets) CreateFactoryProcess(ctx context.Context, token string, wc WorkContext, name string, content []byte, weldKind ...string) (Asset, error) {
	// 本厂有效账号按级别创建工艺草稿。
	return s.CreateProcess(ctx, token, wc, AssetLevelFactory, name, content, true, weldKind...)
}

// CreatePersonalProcess 本厂有效账号在工作上下文中写入个人级工艺。
func (s *Assets) CreatePersonalProcess(ctx context.Context, token string, wc WorkContext, name string, content []byte, weldKind ...string) (Asset, error) {
	// 本厂有效账号按级别创建工艺草稿。
	return s.CreateProcess(ctx, token, wc, AssetLevelPersonal, name, content, true, weldKind...)
}

// CreateProcess 本厂有效账号创建工艺；可复制由调用方给定，状态草稿。
func (s *Assets) CreateProcess(ctx context.Context, token string, wc WorkContext, level, name string, content []byte, copyable bool, weldKind ...string) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 只接受厂级或个人级，平台级不能在这里建。
	if level != AssetLevelFactory && level != AssetLevelPersonal {
		return Asset{}, domain.ErrNotFound
	}
	// 套完模版并核过依赖后落草稿。
	return s.insertAuthored(ctx, acc, wc, KindProcess, level, name, content, nil, copyable, weldKind...)
}

// CreatePersonalProject 创建个人级工程；参数里的工艺写入 deps，不必另填。
func (s *Assets) CreatePersonalProject(ctx context.Context, token string, wc WorkContext, name string, content []byte, deps []AssetDep, weldKind ...string) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 套完模版并核过依赖后落草稿。
	return s.insertAuthored(ctx, acc, wc, KindProject, AssetLevelPersonal, name, content, deps, true, weldKind...)
}

// assertPersonalProjectDeps 个人工程只能钉自己的个人工艺或厂级可用工艺。
func (s *kernel) assertPersonalProjectDeps(ctx context.Context, acc Account, deps []AssetDep) error {
	// 逐条核对接上的工艺，有一条不合格就整份拒绝。
	for _, d := range deps {
		// 只读本厂原件元数据，先不解开正文。
		p, err := s.loadCheckedMeta(ctx, d.ID)
		// 元数据读不到则拒绝，避免钉上空依赖。
		if err != nil {
			return err
		}
		// 依赖必须是工艺，且修订和摘要都要对上。
		if p.Kind != KindProcess || p.Revision != d.Revision || !bytes.Equal(p.Digest, d.Digest) {
			return domain.ErrAssetDependency
		}
		// 草稿或停用的工艺不能钉进工程。
		if p.Status != AssetAvailable {
			return domain.ErrAssetNotAvailable
		}
		// 按级别决定能不能钉：厂级可以，个人级只限本人。
		switch p.Level {
		// 厂级可用工艺可以直接钉。
		case AssetLevelFactory:
		// 个人级只允许创建人自己的工艺。
		case AssetLevelPersonal:
			// 别人的个人工艺不能钉进自己的工程。
			if p.CreatorID != acc.ID {
				return domain.ErrAssetDependency
			}
		// 平台级或其他级别不能钉在个人工程上。
		default:
			return domain.ErrAssetDependency
		}
	}
	return nil
}

// CreateFactoryProject 创建厂级工程；参数里的工艺写入 deps，不必另填。
func (s *Assets) CreateFactoryProject(ctx context.Context, token string, wc WorkContext, name string, content []byte, deps []AssetDep, weldKind ...string) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 套完模版并核过依赖后落草稿。
	return s.insertAuthored(ctx, acc, wc, KindProject, AssetLevelFactory, name, content, deps, true, weldKind...)
}

// assertFactoryProcessDeps 厂级工程可钉本厂可用厂级、个人级或已收平台级工艺。
func (s *kernel) assertFactoryProcessDeps(ctx context.Context, deps []AssetDep) error {
	// 逐条核依赖，本厂没有再看已收的平台级。
	for _, d := range deps {
		// 先看本厂原件，没有再看已收平台级。
		p, err := s.store.GovernedAssetMetaByID(ctx, d.ID)
		// 本厂原件在就按原件核，没有再看已收副本。
		if err == nil {
			// 本厂原件必须是厂级或个人级工艺，且修订摘要对上。
			if p.Kind != KindProcess || !factoryProjectProcessLevel(p.Level) || p.Revision != d.Revision || !bytes.Equal(p.Digest, d.Digest) {
				return domain.ErrAssetDependency
			}
			// 不是可用则不能当工程依赖。
			if p.Status != AssetAvailable {
				return domain.ErrAssetNotAvailable
			}
			continue
		}
		// 不是找不到则把库错误抛回，避免当成没有。
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		// 按修订取副本元数据，用来核依赖。
		r, err := s.store.ReplicaMetaByIDRev(ctx, d.ID, d.Revision)
		// 副本元数据读失败则不能当依赖。
		if err != nil {
			// 找不到就换成业务上的缺失，不当成库故障。
			if errors.Is(err, domain.ErrNotFound) {
				return domain.ErrAssetDependency
			}
			return err
		}
		// 已收副本必须是平台级工艺，且修订摘要对上。
		if r.Kind != KindProcess || r.Level != AssetLevelPlatform || r.Revision != d.Revision || !bytes.Equal(r.Digest, d.Digest) {
			return domain.ErrAssetDependency
		}
		// 停用或草稿的副本不能钉进厂级工程。
		if r.Status != AssetAvailable {
			return domain.ErrAssetNotAvailable
		}
	}
	return nil
}

// fillProjectDeps 把参数里的工艺补进 deps，钉当前可用修订。
func (s *kernel) fillProjectDeps(ctx context.Context, acc Account, level string, content []byte, deps []AssetDep) ([]AssetDep, error) {
	// 从工程正文收集工艺引用。
	ids, err := s.collectProjectProcessIDs(ctx, content)
	// 引用收集失败则不能补依赖。
	if err != nil {
		// 无权或不许用则按这个原因停下，不误当成别的错。
		if errors.Is(err, domain.ErrForbidden) {
			return nil, err
		}
		return nil, domain.ErrAssetDependency
	}
	// 按级别把引用钉到当前可用工艺，钉不上则整份失败。
	return mergeProjectDeps(deps, ids, func(id uuid.UUID) (AssetDep, error) {
		// 个人级按本人的工艺来钉，厂级走另一条。
		if level == AssetLevelPersonal {
			// 个人工程钉自己的工艺或厂级可用工艺。
			return s.pinPersonalProcess(ctx, acc, id)
		}
		// 把引用钉到当前可用的厂级或已收工艺。
		return s.pinFactoryProcess(ctx, id)
	})
}

// pinFactoryProcess 厂级工程钉本厂可用厂级、个人级或已收平台级工艺。
func (s *kernel) pinFactoryProcess(ctx context.Context, id uuid.UUID) (AssetDep, error) {
	// 按身份读本厂原件元数据，不解正文。
	p, err := s.store.GovernedAssetMetaByID(ctx, id)
	// 本厂原件在就钉原件，没有再看已收副本。
	if err == nil {
		// 本厂这条不是可钉的工艺则依赖不成立。
		if p.Kind != KindProcess || !factoryProjectProcessLevel(p.Level) {
			return AssetDep{}, domain.ErrAssetDependency
		}
		// 不是可用则不能钉进厂级工程。
		if p.Status != AssetAvailable {
			return AssetDep{}, domain.ErrAssetNotAvailable
		}
		return AssetDep{ID: p.ID, Revision: p.Revision, Digest: p.Digest}, nil
	}
	// 不是找不到则把库错误抛回，避免当成没有。
	if !errors.Is(err, domain.ErrNotFound) {
		return AssetDep{}, err
	}
	// 取副本元数据，先不解开正文。
	r, err := s.store.LatestReplicaMeta(ctx, id)
	// 副本元数据读失败则停下。
	if err != nil {
		// 找不到就换成业务上的缺失，不当成库故障。
		if errors.Is(err, domain.ErrNotFound) {
			return AssetDep{}, domain.ErrAssetDependency
		}
		return AssetDep{}, err
	}
	// 已收的必须是平台级工艺，否则不能钉。
	if r.Kind != KindProcess || r.Level != AssetLevelPlatform {
		return AssetDep{}, domain.ErrAssetDependency
	}
	// 副本不是可用则不能当依赖。
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
	// 只读本厂原件元数据，先不解开正文。
	p, err := s.loadCheckedMeta(ctx, id)
	// 元数据读不到则拒绝，避免钉上空依赖。
	if err != nil {
		// 找不到就换成业务上的缺失，不当成库故障。
		if errors.Is(err, domain.ErrNotFound) {
			return AssetDep{}, domain.ErrAssetDependency
		}
		return AssetDep{}, err
	}
	// 只能钉工艺，工程或其他种类都不行。
	if p.Kind != KindProcess {
		return AssetDep{}, domain.ErrAssetDependency
	}
	// 不是可用则个人工程不能钉它。
	if p.Status != AssetAvailable {
		return AssetDep{}, domain.ErrAssetNotAvailable
	}
	// 厂级可以直接钉，个人级只限本人。
	switch p.Level {
	// 厂级可用工艺可以直接钉进个人工程。
	case AssetLevelFactory:
	// 个人级只允许创建人自己的。
	case AssetLevelPersonal:
		// 别人的个人工艺不能钉。
		if p.CreatorID != acc.ID {
			return AssetDep{}, domain.ErrAssetDependency
		}
	// 平台级原件不能钉在个人工程上。
	default:
		return AssetDep{}, domain.ErrAssetDependency
	}
	return AssetDep{ID: p.ID, Revision: p.Revision, Digest: p.Digest}, nil
}

// resolveProjectDeps 改依赖时按身份重钉当前可用修订，跟上工艺升版。
func (s *kernel) resolveProjectDeps(ctx context.Context, acc Account, level string, deps []AssetDep) ([]AssetDep, error) {
	// 按依赖条数准备结果，重钉后逐条放进来。
	out := make([]AssetDep, 0, len(deps))
	// 记下已经重钉的身份，避免同一工艺钉两次。
	seen := map[string]struct{}{}
	// 逐条按身份重钉，重复的身份只留一次。
	for _, d := range deps {
		// 用身份当去重键，同一工艺只钉一次。
		key := d.ID.String()
		// 同一工艺已经钉过就跳过，避免依赖重复。
		if _, ok := seen[key]; ok {
			continue
		}
		// 这份身份已经处理，后面的重复跳过。
		seen[key] = struct{}{}
		// 先放空钉，个人级和厂级下面分别填。
		var pin AssetDep
		// 错误放到分支外面，两条钉法共用。
		var err error
		// 个人级按本人规则重钉，厂级走另一条。
		if level == AssetLevelPersonal {
			// 个人工程钉自己的工艺或厂级可用工艺。
			pin, err = s.pinPersonalProcess(ctx, acc, d.ID)
			// 厂级按本厂或已收的可用工艺重钉修订。
		} else {
			// 把引用钉到当前可用的厂级或已收工艺。
			pin, err = s.pinFactoryProcess(ctx, d.ID)
		}
		// 钉不住则这条依赖不成立。
		if err != nil {
			return nil, err
		}
		// 把重钉后的修订放进结果。
		out = append(out, pin)
	}
	return out, nil
}

// mergeProjectDeps 保留已声明依赖，再按参数引用补缺。
func mergeProjectDeps(existing []AssetDep, ids []string, lookup func(uuid.UUID) (AssetDep, error)) ([]AssetDep, error) {
	// 记下已有依赖的身份，补的时候避开。
	have := make(map[string]struct{}, len(existing)+len(ids))
	// 结果先装已有依赖，再补正文里的引用。
	out := make([]AssetDep, 0, len(existing)+len(ids))
	// 先收下已经声明的依赖。
	for _, d := range existing {
		// 用身份当去重键，同一工艺只钉一次。
		key := d.ID.String()
		// 重复身份丢掉，只留第一次。
		if _, ok := have[key]; ok {
			continue
		}
		// 记下这份已保留的身份。
		have[key] = struct{}{}
		// 这份依赖留下，顺序保持调用方给出的先后。
		out = append(out, d)
	}
	// 正文里多出来的引用再补钉。
	for _, raw := range ids {
		// 这份引用已经在依赖里就不再补。
		if _, ok := have[raw]; ok {
			continue
		}
		// 引用必须是合法身份，否则整份依赖不成立。
		id, err := uuid.Parse(raw)
		// 身份不合法则整份依赖失败。
		if err != nil {
			return nil, domain.ErrAssetDependency
		}
		// 把正文里的引用钉到已入库工艺。
		d, err := lookup(id)
		// 对不上已入库工艺则整份依赖失败。
		if err != nil {
			return nil, err
		}
		// 补钉成功后记下来，避免再补一次。
		have[raw] = struct{}{}
		// 这份依赖留下，顺序保持调用方给出的先后。
		out = append(out, d)
	}
	return out, nil
}

// assertProjectProcessIDs 按当前工程模版收集引用，非空的必须是本行 deps 的身份。
func (s *kernel) assertProjectProcessIDs(ctx context.Context, content []byte, deps []AssetDep) error {
	// 从工程正文收集工艺引用。
	ids, err := s.collectProjectProcessIDs(ctx, content)
	// 引用收集失败则不能补依赖。
	if err != nil {
		// 无权或不许用则按这个原因停下，不误当成别的错。
		if errors.Is(err, domain.ErrForbidden) {
			return err
		}
		return domain.ErrAssetDependency
	}
	// 依赖身份收成集合，用来核对正文引用。
	allowed := make(map[string]struct{}, len(deps))
	// 先把已声明依赖的身份收成允许集合。
	for _, d := range deps {
		// 记下已声明依赖的身份，用来核对正文引用。
		allowed[d.ID.String()] = struct{}{}
	}
	// 逐条核对正文引用都落在依赖里。
	for _, id := range ids {
		// 有引用不在依赖里则整份工程拒绝。
		if _, ok := allowed[id]; !ok {
			return domain.ErrAssetDependency
		}
	}
	return nil
}

// CreatePlatformProcess 厂内不能写平台级原件。
func (s *Assets) CreatePlatformProcess(ctx context.Context, token string, name string, content []byte) error {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return err
	}
	// 厂内不能写平台级，补记拒绝，写失败也拒绝。
	_ = s.audit(ctx, &acc.ID, nil, "create_platform_asset", name, audit.Deny)
	return domain.ErrForbidden
}

// RenameAsset 改显示名，身份不变，修订升高。
func (s *Assets) RenameAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64, name string) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 只改显示名；已停用的拒绝，正文保持不动。
	return s.mutateAsset(ctx, acc, assetID, expected, "rename_asset", func(cur Asset) (store.AssetWrite, error) {
		// 停用后不能改名。
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: name, Digest: cur.Digest, Copyable: cur.Copyable, Status: cur.Status, Deps: cur.Deps, KeepContent: true}, nil
	})
}

// UpdateAssetContent 改正文并重算摘要；停用后拒绝。已有正文不套模版。
func (s *Assets) UpdateAssetContent(ctx context.Context, token string, assetID uuid.UUID, expected int64, content []byte) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 改正文并重算摘要；停用拒绝，工程还要核引用。
	return s.mutateAsset(ctx, acc, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		// 停用后不能改正文。
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		// 工程焊道引用必须落在已声明依赖里。
		if cur.Kind == KindProject {
			// 有引用对不上依赖则拒绝保存。
			if err := s.assertProjectProcessIDs(ctx, content, cur.Deps); err != nil {
				return store.AssetWrite{}, err
			}
			// 焊道对不上则拒绝保存。
			if err := assertProjectWeldKind(cur.WeldKind, content); err != nil {
				return store.AssetWrite{}, err
			}
		}
		// 拼出本次要写入的内容，没通过的字段不改。
		return store.AssetWrite{Name: cur.Name, Content: content, Digest: digest.Sum(content), Copyable: cur.Copyable, Status: cur.Status, Deps: cur.Deps}, nil
	})
}

// ApplyAppContent 示教器连上后用本机正文盖当前行，不跟网页对版本号。
func (s *Assets) ApplyAppContent(ctx context.Context, token string, assetID uuid.UUID, content []byte) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 记下最后一次看见的修订，拒绝审计要用。
	var rev int64
	// 留下最后一次冲突，三次用尽再交给调用方。
	var last error
	// 最多盖三次，躲开并发把修订顶掉。
	for range 3 {
		// 按当前修订盖一次正文，撞上并发就交给外层重试。
		row, seen, err := s.applyAppOnce(ctx, acc, assetID, content)
		// 记住这次读到的修订，方便拒绝时写审计。
		rev = seen
		// 这一次盖写成功就返回，不再重试。
		if err == nil {
			return row, nil
		}
		// 并发顶掉当前修订就再读一次；仍不对才把冲突交给调用方。
		if !errors.Is(err, domain.ErrRevisionConflict) {
			return Asset{}, err
		}
		// 这次是冲突就先记下，还有次数就再试。
		last = err
	}
	// 盖写被拒时补记审计，写失败仍维持拒绝。
	_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(assetID, rev), audit.Deny)
	// 三次都没留下错误时，按修订冲突拒绝。
	if last == nil {
		// 三次都没明确错误时，按修订冲突拒绝。
		last = domain.ErrRevisionConflict
	}
	return Asset{}, last
}

// applyAppOnce 按当前修订盖一次；撞上并发不记审计，交给外层重试。
func (s *Assets) applyAppOnce(ctx context.Context, acc Account, assetID uuid.UUID, content []byte) (Asset, int64, error) {
	// 先读原件元数据，没有再看副本。
	cur, err := s.loadAnyMeta(ctx, assetID)
	// 元数据读不到则记拒绝，不继续。
	if err != nil {
		// 找不到就换成业务上的缺失，不当成库故障。
		if errors.Is(err, domain.ErrNotFound) {
			// 盖写被拒时补记审计，写失败仍维持拒绝。
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetID.String(), audit.Deny)
		}
		return Asset{}, 0, err
	}
	// 平台级副本不许回写；厂级只给管理员；个人级只给创建人。
	if err := s.canApplyApp(ctx, acc, cur); err != nil {
		// 盖写被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
		return Asset{}, cur.Revision, err
	}
	// 停用仍盖正文，状态保持停用。
	deps := cur.Deps
	// 工程要先核焊道并补新引用，工艺直接盖。
	if cur.Kind == KindProject {
		// 仍带工艺路径则记拒绝，避免把参数拆出去。
		if err := contenttpl.RejectProcessPath(content); err != nil {
			// 盖写被拒时补记审计，写失败仍维持拒绝。
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
			return Asset{}, cur.Revision, domain.ErrForbidden
		}
		// 焊道对不上则拒绝，并补记审计。
		if err := assertProjectWeldKind(cur.WeldKind, content); err != nil {
			// 盖写被拒时补记审计，写失败仍维持拒绝。
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
			return Asset{}, cur.Revision, err
		}
		// 先有工艺再盖工程：正文里新引用钉到当前可用修订。
		deps, err = s.fillProjectDeps(ctx, acc, cur.Level, content, cur.Deps)
		// 依赖钉不到当前可用工艺则记拒绝。
		if err != nil {
			// 盖写被拒时补记审计，写失败仍维持拒绝。
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
			return Asset{}, cur.Revision, err
		}
		// 有引用对不上依赖则拒绝，并补记审计。
		if err := s.assertProjectProcessIDs(ctx, content, deps); err != nil {
			// 盖写被拒时补记审计，写失败仍维持拒绝。
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
			return Asset{}, cur.Revision, err
		}
	}
	// 按期望修订写入，冲突就拒绝覆盖。
	row, err := s.store.UpdateGovernedAsset(ctx, assetID, cur.Revision, store.AssetWrite{
		Name: cur.Name, Content: content, Digest: digest.Sum(content),
		Copyable: cur.Copyable, Status: cur.Status, Deps: deps,
	})
	// 失败则记拒绝并停下，不继续往下改。
	if err != nil {
		// 不是并发冲突就记拒绝，冲突留给外层重试。
		if !errors.Is(err, domain.ErrRevisionConflict) {
			// 盖写被拒时补记审计，写失败仍维持拒绝。
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(assetID, cur.Revision), audit.Deny)
		}
		return Asset{}, cur.Revision, err
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, row.Revision, err
	}
	// 去掉正文再返回，避免工艺参数外泄。
	return stripContent(row), row.Revision, nil
}

// canApplyApp 平台级拒绝；厂级只给管理员；个人级只给创建人。
func (s *kernel) canApplyApp(ctx context.Context, acc Account, a Asset) error {
	// 平台级拒绝回写；厂级看管理员；个人级看创建人。
	switch a.Level {
	// 平台级副本不许从厂端回写。
	case AssetLevelPlatform:
		return domain.ErrForbidden
	// 个人级只允许创建人盖自己的正文。
	case AssetLevelPersonal:
		// 不是创建人则拒绝盖写个人级。
		if acc.ID != a.CreatorID {
			return domain.ErrForbidden
		}
		return nil
	// 厂级只给管理员盖，不看组织路径。
	case AssetLevelFactory:
		// 厂级新建只给超管或组织管理员。
		return s.canPadAdmin(ctx, acc)
	// 不认识的级别一律拒绝回写。
	default:
		return domain.ErrForbidden
	}
}

// canPadAdmin 超管或任一组织管理员即可改、建厂级，不看组织路径。
func (s *kernel) canPadAdmin(ctx context.Context, acc Account) error {
	// 读出当前人的授权，再判断角色。
	grants, err := s.grantsOf(ctx, acc.ID)
	// 授权读不到则拒绝，不当成没权限放过。
	if err != nil {
		return err
	}
	// 工厂超管可以直接建、改厂级。
	if isFactorySA(grants) {
		return nil
	}
	// 逐条授权，找到组织管理员即可。
	for _, g := range grants {
		// 任一组织管理员即可，不必盖住具体节点。
		if g.Role == RoleOrgAdmin {
			return nil
		}
	}
	return domain.ErrForbidden
}

// SetAssetCopyable 未停用工艺可改可复制；工程没有这项。
func (s *Assets) SetAssetCopyable(ctx context.Context, token string, assetID uuid.UUID, expected int64, copyable bool) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 只允许未停用的工艺改可复制，工程没有这项。
	return s.mutateAsset(ctx, acc, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		// 只有工艺能改可复制，工程没有这项。
		if cur.Kind != KindProcess {
			return store.AssetWrite{}, domain.ErrForbidden
		}
		// 停用的工艺不能改可复制。
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Digest: cur.Digest, Copyable: copyable, Status: cur.Status, Deps: cur.Deps, KeepContent: true}, nil
	})
}

// SetAssetWeldKind 未停用的本厂工艺或工程可改作业类型；工程正文对不上时换成空焊道。
func (s *Assets) SetAssetWeldKind(ctx context.Context, token string, assetID uuid.UUID, expected int64, weldKind string) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 改作业类型；工程焊道对不上时改成空焊道。
	return s.mutateAsset(ctx, acc, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		// 只有工艺或工程能改作业类型。
		if cur.Kind != KindProcess && cur.Kind != KindProject {
			return store.AssetWrite{}, domain.ErrForbidden
		}
		// 停用后不能改作业类型。
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		// 把作业类型收成合法值，不合法就拒绝。
		kind, err := store.NormalizeWeldKind(weldKind)
		// 作业类型不合法则拒绝保存。
		if err != nil {
			return store.AssetWrite{}, err
		}
		// 先按新作业类型准备写入，正文默认不动。
		write := store.AssetWrite{Name: cur.Name, Digest: cur.Digest, Copyable: cur.Copyable, Status: cur.Status, Deps: cur.Deps, WeldKind: kind, KeepContent: true}
		// 工艺只改类型；工程还要筛依赖和焊道。
		if cur.Kind != KindProject {
			return write, nil
		}
		// 只留下和作业类型一致的依赖。
		kept, err := s.depsMatchingWeldKind(ctx, kind, cur.Deps)
		// 依赖筛失败则这次不改作业类型。
		if err != nil {
			return store.AssetWrite{}, err
		}
		// 工程只保留和作业类型一致的依赖。
		write.Deps = kept
		// 用当前正文核焊道；没有再去读全文。
		body := cur.Content
		// 元数据没带正文就再读一次，用来核焊道。
		if len(body) == 0 {
			// 读本厂原件并核对摘要，不符就当坏的。
			full, err := s.loadChecked(ctx, cur.ID)
			// 原件读不到或摘要不符则拒绝。
			if err != nil {
				return store.AssetWrite{}, err
			}
			// 补上全文后再核焊道，避免空正文误判。
			body = full.Content
		}
		// 焊道对不上则拒绝保存。
		if err := assertProjectWeldKind(kind, body); err != nil {
			// 焊道对不上时用空列表替换正文。
			empty := []byte("[]")
			// 焊道对不上就改成空焊道，不再沿用旧正文。
			write.KeepContent = false
			// 空焊道用空列表，避免留下对不上的引用。
			write.Content = empty
			// 空焊道也要重算摘要，不能沿用旧的。
			write.Digest = digest.Sum(empty)
		}
		return write, nil
	})
}

// PublishAsset 草稿改为可用。
func (s *Assets) PublishAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 只有草稿能改成可用，其它状态拒绝。
	return s.mutateAsset(ctx, acc, assetID, expected, "publish_asset", func(cur Asset) (store.AssetWrite, error) {
		// 只有草稿能发布成可用。
		if cur.Status != AssetDraft {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Digest: cur.Digest, Copyable: cur.Copyable, Status: AssetAvailable, Deps: cur.Deps, KeepContent: true}, nil
	})
}

// DisableAsset 可用改为停用；停用期间不得改正文或升档。
func (s *Assets) DisableAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 只有可用能改成停用，草稿不能直接停。
	return s.mutateAsset(ctx, acc, assetID, expected, "disable_asset", func(cur Asset) (store.AssetWrite, error) {
		// 只有可用能改成停用。
		if cur.Status != AssetAvailable {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Digest: cur.Digest, Copyable: cur.Copyable, Status: AssetDisabled, Deps: cur.Deps, KeepContent: true}, nil
	})
}

// ReenableAsset 停用改回可用。
func (s *Assets) ReenableAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 只有停用能改回可用，其它状态拒绝。
	return s.mutateAsset(ctx, acc, assetID, expected, "publish_asset", func(cur Asset) (store.AssetWrite, error) {
		// 只有停用能改回可用。
		if cur.Status != AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Digest: cur.Digest, Copyable: cur.Copyable, Status: AssetAvailable, Deps: cur.Deps, KeepContent: true}, nil
	})
}

// DeleteAsset 未被工程依赖则可删；仍被引用则拒绝。
func (s *Assets) DeleteAsset(ctx context.Context, token string, assetID uuid.UUID) error {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return err
	}
	// 须有制作权；个人级另验创建人或管理员。失败一律记拒绝。
	cur, err := s.loadCheckedMeta(ctx, assetID)
	// 元数据读不到则记拒绝，避免钉上空依赖。
	if err != nil {
		// 删除被拒时补记审计，写失败仍不删。
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetID.String(), audit.Deny)
		return err
	}
	// 没有制作权则记拒绝，不改这份资产。
	if err := s.canAuthorFactory(ctx, acc, cur.OrgUnitID); err != nil {
		// 删除被拒时补记审计，写失败仍不删。
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return err
	}
	// 不是创建人也不是管理员则记拒绝。
	if err := s.canTouchPersonal(ctx, acc, cur); err != nil {
		// 删除被拒时补记审计，写失败仍不删。
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return err
	}
	// 仍被工程钉着则拒绝。
	used, err := s.store.AssetIsReferenced(ctx, assetID)
	// 引用关系查不清则先不删。
	if err != nil {
		return err
	}
	// 仍被工程钉着则拒绝删除。
	if used {
		// 删除被拒时补记审计，写失败仍不删。
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return domain.ErrReferenced
	}
	// 无引用才删本厂原件。
	if err := s.store.DeleteGovernedAsset(ctx, assetID); err != nil {
		// 删除被拒时补记审计，写失败仍不删。
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return err
	}
	// 删掉之后记成功；审计失败则调用方当没删成。
	return s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Allow)
}

// mutateAsset 按期望修订改本厂原件；个人级须创建人或覆盖该处的管理员。
func (s *kernel) mutateAsset(ctx context.Context, acc Account, assetID uuid.UUID, expected int64, action string, patch func(Asset) (store.AssetWrite, error)) (Asset, error) {
	// 须有制作权；个人级另验创建人或管理员。失败一律记拒绝。
	cur, err := s.loadCheckedMeta(ctx, assetID)
	// 元数据读不到则记拒绝，避免钉上空依赖。
	if err != nil {
		// 这一步被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, expected), audit.Deny)
		return Asset{}, err
	}
	// 没有制作权则记拒绝，不改这份资产。
	if err := s.canAuthorFactory(ctx, acc, cur.OrgUnitID); err != nil {
		// 这一步被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, cur.Revision), audit.Deny)
		return Asset{}, err
	}
	// 不是创建人也不是管理员则记拒绝。
	if err := s.canTouchPersonal(ctx, acc, cur); err != nil {
		// 这一步被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, cur.Revision), audit.Deny)
		return Asset{}, err
	}
	// 让本次改动决定写入内容，不通过就拒绝。
	w, err := patch(cur)
	// 改动不被允许则记拒绝，原件不动。
	if err != nil {
		// 这一步被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, cur.Revision), audit.Deny)
		return Asset{}, err
	}
	// 按期望修订写入，冲突则拒绝。
	row, err := s.store.UpdateGovernedAsset(ctx, assetID, expected, w)
	// 写入失败或修订冲突则记拒绝。
	if err != nil {
		// 这一步被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, expected), audit.Deny)
		return Asset{}, err
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.audit(ctx, &acc.ID, nil, action, assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	// 去掉正文再返回，避免工艺参数外泄。
	return stripContent(row), nil
}

// canViewAssetMeta 个人级元数据给本厂有效账号看；厂级有效账号都能看。
func (s *kernel) canViewAssetMeta(ctx context.Context, acc Account, a Asset) error {
	// 看元数据不再查库，参数留着对齐别的入口。
	_ = ctx
	// 本厂有效账号都能看元数据，不再分人。
	_ = acc
	// 级别已在外面筛过，这里不再按资产拦。
	_ = a
	return nil
}

// listVisibleAssets 厂端列表与平板登录共用同一份可见元数据，不含正文。
func (s *kernel) listVisibleAssets(ctx context.Context, acc Account, kind string) ([]Asset, error) {
	// 只接受工艺或工程，别的种类拒绝。
	if kind != "" && kind != KindProcess && kind != KindProject {
		return nil, domain.ErrNotFound
	}
	// 读出本厂全部原件，后面再按人筛选。
	rows, err := s.store.ListGovernedAssets(ctx)
	// 列表读不出来则整份失败，不回半截。
	if err != nil {
		return nil, err
	}
	// 先攒当前人能看的条目，正文稍后去掉。
	visible := []Asset{}
	// 逐条看本厂原件，种类或权限不合就跳过。
	for _, a := range rows {
		// 种类对不上筛选就跳过。
		if kind != "" && a.Kind != kind {
			continue
		}
		// 无权看的跳过，其它错误则整份失败。
		if err := s.canViewAssetMeta(ctx, acc, a); err != nil {
			// 无权或不许用则按这个原因停下，不误当成别的错。
			if errors.Is(err, domain.ErrForbidden) {
				continue
			}
			return nil, err
		}
		// 去掉正文再返回，避免工艺参数外泄。
		visible = append(visible, stripContent(a))
	}
	// 读出已收副本，准备按身份取最新。
	replicas, err := s.store.ListReplicas(ctx)
	// 副本列表读失败则整份不返回。
	if err != nil {
		return nil, err
	}
	// 同一身份只留最高修订的副本。
	latest := map[uuid.UUID]AssetReplica{}
	// 记下每个身份最早到达的时间。
	firstAt := map[uuid.UUID]time.Time{}
	// 逐条副本，同身份记下最早到达和最高修订。
	for _, r := range replicas {
		// 副本种类对不上筛选就跳过。
		if kind != "" && r.Kind != kind {
			continue
		}
		// 同一身份留下更早的到达时间，当创建时间。
		if t, ok := firstAt[r.ID]; !ok || r.ReceivedAt.Before(t) {
			// 更早的到达时间留下来当创建时间。
			firstAt[r.ID] = r.ReceivedAt
		}
		// 看这个身份是否已有更高修订。
		prev, ok := latest[r.ID]
		// 同一身份只留更高修订。
		if !ok || r.Revision > prev.Revision {
			// 留下更高修订，旧的不再展示。
			latest[r.ID] = r
		}
	}
	// 记下本厂原件身份，副本不要重复列出。
	seen := map[uuid.UUID]struct{}{}
	// 先记下本厂原件的身份，副本不要再算一遍。
	for _, a := range visible {
		// 这个身份已经在原件列表里。
		seen[a.ID] = struct{}{}
	}
	// 本厂没有的最高副本再按权限补进列表。
	for _, r := range latest {
		// 本厂已有同身份原件则不再用副本顶上。
		if _, ok := seen[r.ID]; ok {
			continue
		}
		// 厂端只展示云端当前可用的；停用/草稿不进列表，已钉修订仍可按身份读。
		if r.Status != AssetAvailable {
			continue
		}
		// 把副本看成平台级资产，方便和原件一起筛。
		a := replicaAsAsset(r)
		// 同一身份留下更早的到达时间，当创建时间。
		if t, ok := firstAt[r.ID]; ok {
			// 副本的创建时间改用最早到达，不跟修订走。
			a.CreatedAt = t
		}
		// 无权看的跳过，其它错误则整份失败。
		if err := s.canViewAssetMeta(ctx, acc, a); err != nil {
			// 无权或不许用则按这个原因停下，不误当成别的错。
			if errors.Is(err, domain.ErrForbidden) {
				continue
			}
			return nil, err
		}
		// 去掉正文再返回，避免工艺参数外泄。
		visible = append(visible, stripContent(a))
	}
	// 新的排前面；同一时刻按身份倒序，避免跳动。
	sort.SliceStable(visible, func(i, j int) bool {
		// 同一时刻改用身份倒序，避免刷新后乱跳。
		if visible[i].CreatedAt.Equal(visible[j].CreatedAt) {
			// 同一时刻按身份倒序，避免列表来回跳。
			return visible[i].ID.String() > visible[j].ID.String()
		}
		// 创建更晚的排在前面。
		return visible[i].CreatedAt.After(visible[j].CreatedAt)
	})
	return visible, nil
}

// GetAsset 读元数据，不解包正文。
func (s *Assets) GetAsset(ctx context.Context, token string, assetID uuid.UUID) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 读元数据，不解包正文。失败记拒绝。
	a, err := s.loadAnyMeta(ctx, assetID)
	// 元数据读不到则记拒绝，不继续。
	if err != nil {
		// 读取被拒时补记审计，不把没读成当成已读。
		_ = s.audit(ctx, &acc.ID, nil, "get_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	// 无权看则跳过或记拒绝，不把这条露出去。
	if err := s.canViewAssetMeta(ctx, acc, a); err != nil {
		// 读取被拒时补记审计，不把没读成当成已读。
		_ = s.audit(ctx, &acc.ID, nil, "get_asset", assetTarget(a.ID, a.Revision), audit.Deny)
		return Asset{}, err
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.audit(ctx, &acc.ID, nil, "get_asset", assetTarget(a.ID, a.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	// 去掉正文再返回，避免工艺参数外泄。
	return stripContent(a), nil
}

// ReadAssetContent 读正文并核对摘要；个人级创建人或管理员；不可复制的平台级工艺不给人看。
func (s *Assets) ReadAssetContent(ctx context.Context, token string, assetID uuid.UUID) ([]byte, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return nil, err
	}
	// 个人级创建人或覆盖该处的管理员；不可复制平台级工艺不给人看。失败一律记拒绝。
	meta, err := s.loadAnyMeta(ctx, assetID)
	// 元数据读不到则记拒绝，不继续。
	if err != nil {
		// 读正文被拒时补记审计，参数不能漏出去。
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetID.String(), audit.Deny)
		return nil, err
	}
	// 不可复制的平台级工艺是严格保密件，厂端人员不得看参数；工程不保密。
	if meta.Kind == KindProcess && meta.Level == AssetLevelPlatform && !meta.Copyable {
		// 读正文被拒时补记审计，参数不能漏出去。
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetTarget(meta.ID, meta.Revision), audit.Deny)
		return nil, domain.ErrForbidden
	}
	// 不是创建人也不是管理员则记拒绝。
	if err := s.canTouchPersonal(ctx, acc, meta); err != nil {
		// 读正文被拒时补记审计，参数不能漏出去。
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetTarget(meta.ID, meta.Revision), audit.Deny)
		return nil, err
	}
	// 无权看则跳过或记拒绝，不把这条露出去。
	if err := s.canViewAssetMeta(ctx, acc, meta); err != nil {
		// 读正文被拒时补记审计，参数不能漏出去。
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetTarget(meta.ID, meta.Revision), audit.Deny)
		return nil, err
	}
	// 本厂原件优先，没有再读已收副本。
	a, err := s.loadAny(ctx, assetID)
	// 原件和副本都读不到则记拒绝。
	if err != nil {
		// 读正文被拒时补记审计，参数不能漏出去。
		_ = s.audit(ctx, &acc.ID, nil, "read_asset_content", assetID.String(), audit.Deny)
		return nil, err
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.audit(ctx, &acc.ID, nil, "read_asset_content", assetTarget(a.ID, a.Revision), audit.Allow); err != nil {
		return nil, err
	}
	return a.Content, nil
}

// PromoteToFactory 把可复制的未停用个人级升为厂级：草稿也可升；第一次复制新身份；再升按正文摘要跳过或覆盖。
func (s *Assets) PromoteToFactory(ctx context.Context, token string, assetID uuid.UUID) (Asset, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return Asset{}, err
	}
	// 读本厂原件并核对摘要，不符就当坏的。
	src, err := s.loadChecked(ctx, assetID)
	// 原件读不到或摘要不符则记拒绝。
	if err != nil {
		// 升档被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	// 没有制作权则记拒绝，不改这份资产。
	if err := s.canAuthorFactory(ctx, acc, src.OrgUnitID); err != nil {
		// 升档被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
	// 只有个人级能升厂级，厂级和平台级拒绝。
	if src.Level != AssetLevelPersonal {
		// 升档被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrForbidden
	}
	// 停用不能升；草稿可以，厂级新条目仍记可用。
	if src.Status == AssetDisabled {
		// 升档被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrAssetNotAvailable
	}
	// 不可复制的工艺拒绝升厂级，避免参数外带。
	if src.Kind == KindProcess && !src.Copyable {
		// 升档被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrAssetNotCopyable
	}
	// 失败则记拒绝并停下，不继续往下改。
	if err := s.assertPromoteToFactoryDeps(ctx, src); err != nil {
		// 升档被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
	// 记下源修订，覆盖或新建时带到厂级。
	rev := src.Revision
	// 已升过则按摘要跳过或覆盖。
	existing, err := s.store.GovernedAssetBySourceID(ctx, src.ID)
	// 已经升过则按摘要决定跳过还是覆盖。
	if err == nil {
		// 正文没变就直接用已升的那条，不再覆盖。
		if bytes.Equal(existing.Digest, src.Digest) {
			// 成功必须记上审计，没记上则本次不算做成。
			if err := s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(existing.ID, existing.Revision)+" from "+assetTarget(src.ID, src.Revision), audit.Allow); err != nil {
				return Asset{}, err
			}
			// 去掉正文再返回，避免工艺参数外泄。
			return stripContent(existing), nil
		}
		// 正文变了才覆盖厂级。
		row, err := s.store.UpdateGovernedAsset(ctx, existing.ID, existing.Revision, store.AssetWrite{
			Name: src.Name, Content: src.Content, Digest: src.Digest, Copyable: true, Status: AssetAvailable, Deps: src.Deps, SourceRevision: &rev,
		})
		// 失败则记拒绝并停下，不继续往下改。
		if err != nil {
			// 升档被拒时补记审计，写失败仍维持拒绝。
			_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
			return Asset{}, err
		}
		// 成功必须记上审计，没记上则本次不算做成。
		if err := s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(row.ID, row.Revision)+" from "+assetTarget(src.ID, src.Revision), audit.Allow); err != nil {
			return Asset{}, err
		}
		// 去掉正文再返回，避免工艺参数外泄。
		return stripContent(row), nil
	}
	// 不是找不到则把库错误抛回，避免当成没有。
	if !errors.Is(err, domain.ErrNotFound) {
		// 升档被拒时补记审计，写失败仍维持拒绝。
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
	// 失败则记拒绝并停下，不继续往下改。
	if err != nil {
		// 升档被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(row.ID, row.Revision)+" from "+assetTarget(src.ID, src.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	// 去掉正文再返回，避免工艺参数外泄。
	return stripContent(row), nil
}

// assertPromoteToFactoryDeps 升厂级时工程依赖必须已是可用厂级工艺。
func (s *Assets) assertPromoteToFactoryDeps(ctx context.Context, src Asset) error {
	// 不是工程就没有依赖要核，直接通过。
	if src.Kind != KindProject {
		return nil
	}
	// 升厂级时逐条确认依赖已是可用厂级工艺。
	for _, d := range src.Deps {
		// 只读本厂原件元数据，先不解开正文。
		p, err := s.loadCheckedMeta(ctx, d.ID)
		// 元数据读不到则拒绝，避免钉上空依赖。
		if err != nil {
			return err
		}
		// 依赖必须已是可用厂级工艺，个人级还不能跟着升。
		if p.Kind != KindProcess || p.Level != AssetLevelFactory || p.Status != AssetAvailable {
			return domain.ErrAssetDependency
		}
	}
	return nil
}

// ExportAssetSnapshot 导出厂级快照给 WAN 升档夹具。
func (s *Assets) ExportAssetSnapshot(ctx context.Context, token string, assetID uuid.UUID) (AssetSnapshot, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return AssetSnapshot{}, err
	}
	// 读本厂原件并核对摘要，不符就当坏的。
	src, err := s.loadChecked(ctx, assetID)
	// 原件读不到或摘要不符则记拒绝。
	if err != nil {
		// 外送被拒时补记审计，写失败仍不外送。
		_ = s.audit(ctx, &acc.ID, nil, "export_asset", assetID.String(), audit.Deny)
		return AssetSnapshot{}, err
	}
	// 没有制作权则记拒绝，不改这份资产。
	if err := s.canAuthorFactory(ctx, acc, src.OrgUnitID); err != nil {
		// 外送被拒时补记审计，写失败仍不外送。
		_ = s.audit(ctx, &acc.ID, nil, "export_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return AssetSnapshot{}, err
	}
	// 只有厂级能导出给云端升档。
	if src.Level != AssetLevelFactory {
		// 外送被拒时补记审计，写失败仍不外送。
		_ = s.audit(ctx, &acc.ID, nil, "export_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return AssetSnapshot{}, domain.ErrForbidden
	}
	// 不是可用则拒绝导出。
	if src.Status != AssetAvailable {
		// 外送被拒时补记审计，写失败仍不外送。
		_ = s.audit(ctx, &acc.ID, nil, "export_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return AssetSnapshot{}, domain.ErrAssetNotAvailable
	}
	// 导出明文快照给 WAN 升档。
	snap, err := s.store.ExportAssetSnapshot(ctx, assetID)
	// 快照导不出则拒绝这次外送。
	if err != nil {
		return AssetSnapshot{}, err
	}
	// 快照交出并记成功；审计失败则当没外送。
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
	// 只列工艺或工程，别的种类拒绝。
	if kind != "" && kind != KindProcess && kind != KindProject {
		return nil, domain.ErrNotFound
	}
	// 本厂原件和已下发平台级一并列出，不含正文。
	rows, err := s.store.ListGovernedAssets(ctx)
	// 列表读不出来则整份失败，不回半截。
	if err != nil {
		return nil, err
	}
	// 升档列表只攒元数据，不含正文。
	out := []PromotableAsset{}
	// 记下已列出的身份，副本不要重复。
	seen := map[uuid.UUID]struct{}{}
	// 逐条收本厂原件元数据，不分状态。
	for _, a := range rows {
		// 种类对不上就跳过。
		if kind != "" && a.Kind != kind {
			continue
		}
		// 只收元数据放进升档列表，正文不带走。
		out = append(out, toPromotable(a))
		// 这个身份的原件已经收进列表。
		seen[a.ID] = struct{}{}
	}
	// 读出已收副本，准备按身份取最新。
	replicas, err := s.store.ListReplicas(ctx)
	// 副本列表读失败则整份不返回。
	if err != nil {
		return nil, err
	}
	// 同一身份只留最高修订。
	latest := map[uuid.UUID]AssetReplica{}
	// 再看已下发副本，同身份只留最高修订。
	for _, r := range replicas {
		// 种类对不上就跳过。
		if kind != "" && r.Kind != kind {
			continue
		}
		// 看这个身份是否已有一份副本。
		prev, ok := latest[r.ID]
		// 同一身份只留更高修订。
		if !ok || r.Revision > prev.Revision {
			// 留下更高修订给升档列表。
			latest[r.ID] = r
		}
	}
	// 本厂没有的副本再补进升档列表。
	for _, r := range latest {
		// 原件已列出的身份不再用副本重复。
		if _, ok := seen[r.ID]; ok {
			continue
		}
		// 只收元数据放进升档列表，正文不带走。
		out = append(out, toPromotable(replicaAsAsset(r)))
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.audit(ctx, nil, nil, "list_promotable", kind, audit.Allow); err != nil {
		return nil, err
	}
	return out, nil
}

// SnapshotForChannel 通道升档用：本厂厂级和个人级可出快照；工艺须可复制，工程不看可复制。
func (s *Assets) SnapshotForChannel(ctx context.Context, assetID uuid.UUID) (AssetSnapshot, error) {
	// 读本厂原件并核对摘要，不符就当坏的。
	src, err := s.loadChecked(ctx, assetID)
	// 原件读不到或摘要不符则记拒绝。
	if err != nil {
		// 找不到就换成业务上的缺失，不当成库故障。
		if errors.Is(err, domain.ErrNotFound) {
			// 本厂没有原件、但有副本，则拒绝外送平台级。
			if _, rerr := s.loadReplicaMeta(ctx, assetID); rerr == nil {
				// 外送被拒时补记审计，写失败仍不外送。
				_ = s.audit(ctx, nil, nil, "export_asset", assetID.String(), audit.Deny)
				return AssetSnapshot{}, domain.ErrForbidden
			}
		}
		// 外送被拒时补记审计，写失败仍不外送。
		_ = s.audit(ctx, nil, nil, "export_asset", assetID.String(), audit.Deny)
		return AssetSnapshot{}, err
	}
	// 不可复制的工艺拒绝经通道外送。
	if src.Kind == KindProcess && !src.Copyable {
		// 外送被拒时补记审计，写失败仍不外送。
		_ = s.audit(ctx, nil, nil, "export_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return AssetSnapshot{}, domain.ErrAssetNotCopyable
	}
	// 先解厂库信封拿到明文，再另封过站。
	snap, err := s.store.ExportAssetSnapshot(ctx, assetID)
	// 快照导不出则拒绝这次外送。
	if err != nil {
		return AssetSnapshot{}, err
	}
	// 通道上仍是过站密文，不把厂库信封送出。
	sealed, err := s.sealTransitContent(snap.SourceID, snap.SourceRevision, snap.Content)
	// 这一步失败则停下，避免带着残缺结果继续。
	if err != nil {
		return AssetSnapshot{}, err
	}
	// 换成过站密文，厂库信封不能送出通道。
	snap.Content = sealed
	// 快照交出并记成功；审计失败则当没外送。
	return snap, s.audit(ctx, nil, nil, "export_asset", assetTarget(src.ID, src.Revision), audit.Allow)
}

// AssetAuthorContext 是制作工艺/工程时可选的工作位置。
type AssetAuthorContext struct {
	AllowDirect bool      `json:"allowDirect"` // 有效账号可直属工厂
	OrgUnits    []OrgUnit `json:"orgUnits"`    // 本人已分配的有效节点
}

// AuthorContext 给出当前账号可用来创建资产的工作位置。
func (s *Assets) AuthorContext(ctx context.Context, token string) (AssetAuthorContext, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return AssetAuthorContext{}, err
	}
	// 有效账号默认可直属工厂，节点下面再填。
	out := AssetAuthorContext{AllowDirect: true, OrgUnits: []OrgUnit{}}
	// 只收本人已分配的有效节点。
	assigns, err := s.store.ActiveAssignments(ctx, acc.ID)
	// 分配读失败则给不出可选工作位置。
	if err != nil {
		return AssetAuthorContext{}, err
	}
	// 逐条分配取节点，停用的不放进可选项。
	for _, a := range assigns {
		// 读出组织节点，停用的不能当工作位置。
		unit, err := s.store.Unit(ctx, a.OrgUnitID)
		// 节点读不到则不能在这里制作。
		if err != nil {
			return AssetAuthorContext{}, err
		}
		// 停用节点不给选，不能再当新的工作位置。
		if unit.Status != StatusActive {
			continue
		}
		// 有效节点放进可选工作位置。
		out.OrgUnits = append(out.OrgUnits, unit)
	}
	return out, nil
}

// AssetView 给管理端看的资产行，带创建人名字，不含正文。
type AssetView struct {
	Asset                 // 嵌入的资产行，正文已去掉
	CreatorLogin   string `json:"creatorLogin,omitempty"`   // 创建人登录名
	CreatorDisplay string `json:"creatorDisplay,omitempty"` // 创建人显示名
}

// ListAssets 按许可过滤本厂工艺/工程元数据；kind 空则两种都回，不含正文。
func (s *Assets) ListAssets(ctx context.Context, token, kind string) ([]AssetView, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return nil, err
	}
	// 按当前人能看的范围取元数据，不含正文。
	visible, err := s.listVisibleAssets(ctx, acc, kind)
	// 可见列表算不出来则整份失败。
	if err != nil {
		return nil, err
	}
	// 给每行补上创建人名字，仍不含正文。
	return s.decorateAssets(ctx, visible)
}

// decorateAssets 补创建人登录名和显示名，不含正文。
func (s *Assets) decorateAssets(ctx context.Context, rows []Asset) ([]AssetView, error) {
	// 先收集创建人身份，再一次把名字补上。
	ids := make([]uuid.UUID, 0, len(rows))
	// 逐行走过资产行，收集创建人或补上名字。
	for _, a := range rows {
		// 记下这一行的创建人。
		ids = append(ids, a.CreatorID)
	}
	// 补创建人名字。
	people, err := s.store.PeopleByIDs(ctx, ids)
	// 名字补不上则整份列表失败。
	if err != nil {
		return nil, err
	}
	// 按行数准备结果，每行再补名字。
	out := make([]AssetView, 0, len(rows))
	// 逐行走过资产行，收集创建人或补上名字。
	for _, a := range rows {
		// 先套上资产行，名字有则再补。
		view := AssetView{Asset: a}
		// 查到创建人就补上名字，没有就留空。
		if p, ok := people[a.CreatorID]; ok {
			// 补上创建人登录名，方便认人。
			view.CreatorLogin = p.LoginName
			// 补上创建人显示名。
			view.CreatorDisplay = p.DisplayName
		}
		// 名字补完的一行放进列表。
		out = append(out, view)
	}
	return out, nil
}

// RehomeAsset 拒绝改挂创建人或创建路径。
func (s *Assets) RehomeAsset(ctx context.Context, token string, assetID uuid.UUID) error {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return err
	}
	// 改挂创建路径一律拒绝，并补记审计。
	_ = s.audit(ctx, &acc.ID, nil, "rehome_asset", assetID.String(), audit.Deny)
	return domain.ErrForbidden
}

// CreateProcessFromProject 拒绝把工程自有参数拆成新工艺。
func (s *Assets) CreateProcessFromProject(ctx context.Context, token string, projectID uuid.UUID) error {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return err
	}
	// 不许把工程参数拆成新工艺，并补记拒绝。
	_ = s.audit(ctx, &acc.ID, nil, "create_process_from_project", projectID.String(), audit.Deny)
	return domain.ErrForbidden
}
