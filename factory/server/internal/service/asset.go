package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
		Status: r.Status, Copyable: r.Copyable, WeldKind: r.WeldKind, Corner: r.Corner, TemplateID: r.TemplateID, Revision: r.Revision,
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

// canAuthorFactory 厂级的制作和修改只给超管或管理员。
func (s *kernel) canAuthorFactory(ctx context.Context, acc Account) error {
	// 厂级工艺工程归管理员管
	return s.can(ctx, acc, permManageAssets)
}

// canAuthorLevel 按级别核制作权：厂级要管理员，个人级本厂有效账号都能建。
func (s *kernel) canAuthorLevel(ctx context.Context, acc Account, level string) error {
	// 厂级要管理员，个人级不再拦
	if level == AssetLevelFactory {
		// 厂级交给管理员口径判
		return s.canAuthorFactory(ctx, acc)
	}
	return nil
}

// canTouchPersonal 个人级：只有创建人和工厂超管。
func (s *kernel) canTouchPersonal(ctx context.Context, acc Account, a Asset) error {
	// 不是个人级就放行，厂级不在这里拦人。
	if a.Level != AssetLevelPersonal {
		return nil
	}
	// 创建人可以看自己的个人级正文。
	if acc.ID == a.CreatorID {
		return nil
	}
	// 其余只认超管，和个人文件夹同口径。
	return s.can(ctx, acc, permManageAccount)
}

// opCovers 下发和组包要操作权；制作不走这里。
func (s *kernel) opCovers(ctx context.Context, acc Account) error {
	// 示教器作业归操作员
	return s.can(ctx, acc, permOperate)
}

// MaxAssetName 工艺、工程名去首尾空白后的字数上限；和焊次能收的工程名上限一致，
// 免得建得出的工程焊完后焊次因名太长永远收不下。
const MaxAssetName = store.WeldMaxProjectName

// checkAssetNameLen 名字超过上限就拒，原因明说太长。
func checkAssetNameLen(name string) error {
	// 按字数算，多字节字不按字节算多
	if utf8.RuneCountInString(strings.TrimSpace(name)) > MaxAssetName {
		return domain.ErrNameTooLong
	}
	return nil
}

// insertAuthored 套模版后落草稿；工程依赖从参数补齐。
func (s *Assets) insertAuthored(ctx context.Context, acc Account, wc WorkContext, kind, level, name string, content []byte, deps []AssetDep, copyable bool, weldKind ...string) (Asset, error) {
	// 名字太长焊次收不下，建件时就拒
	if err := checkAssetNameLen(name); err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", kind, audit.Deny)
		return Asset{}, err
	}
	// 按级别核制作权，没有则拒绝。
	if err := s.canAuthorLevel(ctx, acc, level); err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", kind, audit.Deny)
		return Asset{}, err
	}
	// 目标文件夹先核写权限，不过就记拒绝，不建资产。
	if err := s.checkNewAssetFolder(ctx, acc, wc.FolderID); err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 把作业类型收成合法值，不合法就拒绝。
	weld, err := store.NormalizeWeldKind(firstWeldKind(weldKind))
	// 作业类型不合法则拒绝，并补记审计。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 按当前模版整理正文，套不上就拒绝。
	content, err = s.normalizeContent(ctx, kind, weld, content)
	// 正文套不上模版则记拒绝。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 只有焊接参数才分适用作业，包角和自建种类一律记单层。
	weld = singleUnlessWeld(kind, weld, content)
	// 工程要补依赖并核对焊道，工艺不走这段。
	if kind == KindProject {
		// 焊道对不上则拒绝，并补记审计。
		if err := assertProjectWeldKind(weld, content); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
		// 参数里选过的工艺补进依赖，钉当前修订。
		deps, err = s.fillProjectDeps(ctx, acc, level, content, deps)
		// 依赖钉不到当前可用工艺则记拒绝。
		if err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
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
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
		// 焊道类型对不上则记拒绝。
		if err := s.assertDepsWeldKind(ctx, weld, deps); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
		// 套完后工程引用须落在已声明依赖里。
		if err := s.assertProjectProcessIDs(ctx, content, deps); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
		// 工艺槽（含包角组）不挂包角工艺，免得示教器焊接时缺工艺。
		if err := s.assertWeldSlots(ctx, content); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
	}
	// 校验都过了，厂级工程引用的个人工艺复制成厂级再落；别的都不走这步。
	var adopted adoptResult
	// 只有厂级工程要换。
	if kind == KindProject && level == AssetLevelFactory {
		// 新建没有旧依赖，引用的个人工艺都要复制。
		adopted, err = s.copyPersonalDeps(ctx, acc, nil, content, deps)
		// 复制不了就整份拒绝，已复制的里面回收过了。
		if err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
		// 后面落的是改写过的正文和依赖。
		content, deps = adopted.content, adopted.deps
	}
	// 落成一条资产，并在同一笔里挂进目标文件夹。
	row, err := s.insertGovernedAt(ctx, acc, assetPlace{ParentID: wc.FolderID}, kind, level, name, content, deps, copyable, weld)
	// 工程没落成就回收刚复制的工艺，免得留下没人用的厂级工艺。
	if err != nil {
		// 回收失败不盖住真因。
		s.recyclePersonalCopies(ctx, acc, adopted.made)
		return Asset{}, err
	}
	// 把改写后的正文和新工艺告诉调用方。
	return adopted.stamp(row), nil
}

// insertGoverned 落一条草稿并挂到对应树根；调用方已决定是否套过模版。
func (s *Assets) insertGoverned(ctx context.Context, acc Account, kind, level, name string, content []byte, deps []AssetDep, copyable bool, weldKind ...string) (Asset, error) {
	// 不指定文件夹，文件挂到对应树根。
	return s.insertGovernedAt(ctx, acc, assetPlace{}, kind, level, name, content, deps, copyable, weldKind...)
}

// insertGovernedAt 落一条草稿并挂到指定位置；挂不上则资产一起回滚。
func (s *Assets) insertGovernedAt(ctx context.Context, acc Account, at assetPlace, kind, level, name string, content []byte, deps []AssetDep, copyable bool, weldKind ...string) (Asset, error) {
	// 把作业类型收成合法值，不合法就拒绝。
	weld, err := store.NormalizeWeldKind(firstWeldKind(weldKind))
	// 作业类型不合法则拒绝，并补记审计。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 写入本厂原件，成功还要记审计。
	return s.putGoverned(ctx, acc, at, Asset{
		Kind: kind, Level: level, Name: name, Status: AssetDraft, Copyable: assetCopyable(kind, copyable), WeldKind: weld,
		Content: content, Digest: digest.Sum(content), CreatorID: acc.ID,
		Deps: deps,
	})
}

// putGoverned 写入本厂原件并挂目录；状态由调用方决定，管理后台新建仍走草稿。
func (s *Assets) putGoverned(ctx context.Context, acc Account, at assetPlace, in Asset) (Asset, error) {
	// 原件和文件节点同一事务写入，挂不上就一起回滚。
	row, err := s.store.InsertGovernedAssetInto(ctx, in, at.ParentID, at.OrRoot)
	// 落库失败则记拒绝，不假装已经建成。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", in.Name, audit.Deny)
		return Asset{}, err
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.auditAt(ctx, &acc.ID, "create_asset", assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
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
// 身份必须是本厂没有的新身份；自带号只认本厂登记过的 C 段；本厂删过的身份只能沿用原号。
func (s *Assets) CreatePad(ctx context.Context, token, level, kind, name string, content []byte, id uuid.UUID, code string, deps []AssetDep, weldKind ...string) (Asset, error) {
	// 不指定文件夹，交给带目录的核心挂到对应树根。
	return s.CreatePadIn(ctx, token, level, kind, name, content, id, code, deps, uuid.Nil, weldKind...)
}

// CreatePadIn 示教器新建并挂进指定文件夹；空父级挂树根。
// 目录不在、不是文件夹、种类树或主人不对、无写权限都改挂该资产所属树根，只有库错误才报错。
func (s *Assets) CreatePadIn(ctx context.Context, token, level, kind, name string, content []byte, id uuid.UUID, code string, deps []AssetDep, parentID uuid.UUID, weldKind ...string) (Asset, error) {
	// 核对登录仍有效且还握着角色，角色全收回的人不再从示教器建件。
	acc, err := s.requirePadActor(ctx, token)
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
	// 名字太长焊次收不下，建件时就拒
	if err := checkAssetNameLen(name); err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", kind, audit.Deny)
		return Asset{}, err
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
	// 目标文件夹不在或不可写就改挂树根，只有库错误才拒绝。
	parentID, err = s.padNewAssetFolder(ctx, acc, parentID)
	// 核目录时库出错则拒绝，并补记审计。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
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
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 按已收内容模版补缺的键，示教器写了的键和值原样留着，和网页新建一样落全字段
	content = s.padFillTemplate(ctx, kind, weld, content)
	// 工程要先去掉路径、补依赖并核对焊道。
	if kind == KindProject {
		// 仍带工艺路径则记拒绝，避免把参数拆出去。
		if err := contenttpl.RejectProcessPath(content); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
			return Asset{}, domain.ErrForbidden
		}
		// 焊道对不上则拒绝，并补记审计。
		if err := assertProjectWeldKind(weld, content); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
		// 把正文里点到的工艺补进依赖，并钉当前修订。
		deps, err = s.fillProjectDeps(ctx, acc, level, content, deps)
		// 依赖钉不到当前可用工艺则记拒绝。
		if err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
		// 依赖规则分级别，和网页新建保持同一口径。
		if level == AssetLevelPersonal {
			// 个人工程只能钉自己的工艺或厂级可用工艺。
			err = s.assertPersonalProjectDeps(ctx, acc, deps)
			// 厂级工程改按本厂可用和已收平台工艺来核。
		} else {
			// 厂级工程可钉同事的个人工艺和已收平台副本。
			err = s.assertFactoryProcessDeps(ctx, deps)
		}
		// 依赖不合格则拒绝，并补记审计。
		if err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
		// 焊道类型对不上则记拒绝。
		if err := s.assertDepsWeldKind(ctx, weld, deps); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
		// 有引用对不上依赖则拒绝，并补记审计。
		if err := s.assertProjectProcessIDs(ctx, content, deps); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
		// 工艺槽（含包角组）不挂包角工艺，和网页新建同一口径。
		if err := s.assertWeldSlots(ctx, content); err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
	}
	// 示教器另存厂级工程和网页同一规则：引用的个人工艺复制成厂级。
	var adopted adoptResult
	// 只有厂级工程要换。
	if kind == KindProject && level == AssetLevelFactory {
		// 新建没有旧依赖，引用的个人工艺都要复制。
		adopted, err = s.copyPersonalDeps(ctx, acc, nil, content, deps)
		// 复制不了就整份拒绝，已复制的里面回收过了。
		if err != nil {
			// 创建被拒时补记审计，写失败仍维持拒绝。
			_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
			return Asset{}, err
		}
		// 后面落的是改写过的正文和依赖。
		content, deps = adopted.content, adopted.deps
	}
	// 写入本厂原件并挂目录，事务里挂不上也退回树根。
	row, err := s.putGoverned(ctx, acc, assetPlace{ParentID: parentID, OrRoot: true}, Asset{
		ID: id, Kind: kind, Level: level, Name: name, Code: strings.TrimSpace(code),
		Status: AssetAvailable, Copyable: true, WeldKind: weld, Content: content, Digest: digest.Sum(content),
		CreatorID: acc.ID, Deps: deps,
	})
	// 工程没落成（比如号冲突）就回收刚复制的工艺；示教器换号重试时会重新复制。
	if err != nil {
		// 回收失败不盖住真因。
		s.recyclePersonalCopies(ctx, acc, adopted.made)
		return Asset{}, err
	}
	// 把改写后的正文和新工艺告诉调用方；示教器按回包摘要发现正文变了会重拉。
	return adopted.stamp(row), nil
}

// padFillTemplate 示教器新建时按已收模版补键：只补正文里没有的键（含工程焊道的模版身份），
// 不删示教器自己的键、不改已写的值；模版套不上就原样留着，免得示教器上传从此被拒。
func (s *Assets) padFillTemplate(ctx context.Context, kind, weld string, content []byte) []byte {
	// 先按网页新建同一口径套一遍模版
	applied, err := s.normalizeContent(ctx, kind, weld, content)
	// 套不上就原样留着，后面的校验照常判
	if err != nil {
		// 记下哪类正文没套上，日志里不写正文
		slog.Warn("pad content template not applied", "kind", kind, "weld", weld, "err", err)
		return content
	}
	// 以原正文为准补缺的键
	out, err := contenttpl.FillMissing(content, applied)
	// 补不了就原样留着
	if err != nil {
		return content
	}
	return out
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
	// 名字太长焊次收不下，另存时就拒
	if err := checkAssetNameLen(name); err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	// 先读原件元数据，没有再看副本。
	src, err := s.loadAnyMeta(ctx, assetID)
	// 元数据读不到则记拒绝，不继续。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	// 是工艺、未停用、可复制且有权看正文才能另存。
	if err := s.canCopyProcess(ctx, acc, src); err != nil {
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
	// 按级别核制作权，没有则拒绝。
	if err := s.canAuthorLevel(ctx, acc, level); err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 校验都过了才落成一条资产。
	row, err := s.insertGoverned(ctx, acc, KindProcess, level, name, src.Content, nil, true, src.WeldKind)
	// 落库失败则创建不算成功。
	if err != nil {
		return Asset{}, err
	}
	// 副本尽量放到源文件旁边，失败不影响已建成的资产。
	s.placeCopyBeside(ctx, src.ID, row.ID)
	return row, nil
}

// canCopyProcess 另存工艺的统一口径，单件另存和整夹复制预检共用。
func (s *kernel) canCopyProcess(ctx context.Context, acc Account, src Asset) error {
	// 只有工艺能另存，工程不能走这条。
	if src.Kind != KindProcess {
		return domain.ErrForbidden
	}
	// 停用的工艺不能另存。
	if src.Status == AssetDisabled {
		return domain.ErrAssetNotAvailable
	}
	// 不可复制的工艺拒绝另存，避免参数外泄。
	if !src.Copyable {
		return domain.ErrAssetNotCopyable
	}
	// 个人级须是创建人或整厂管理员。
	return s.canTouchPersonal(ctx, acc, src)
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

// assertPersonalProjectDeps 个人工程只能钉自己的个人工艺、厂级可用工艺或已收的加密平台工艺。
func (s *kernel) assertPersonalProjectDeps(ctx context.Context, acc Account, deps []AssetDep) error {
	// 逐条核对接上的工艺，有一条不合格就整份拒绝。
	for _, d := range deps {
		// 只读本厂原件元数据，先不解开正文。
		p, err := s.loadCheckedMeta(ctx, d.ID)
		// 本厂没有原件，再按声明的修订核加密平台工艺。
		if errors.Is(err, domain.ErrNotFound) {
			// 加密平台工艺按原身份引用，不合格就整份拒绝。
			if err := s.assertPlatformDep(ctx, d, personalPlatformDep); err != nil {
				return err
			}
			continue
		}
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

// assertPlatformDep 核写入时声明的已收平台副本：种类级别由 ok 判，修订摘要对上且可用。
func (s *kernel) assertPlatformDep(ctx context.Context, d AssetDep, ok func(AssetReplica) bool) error {
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
	// 种类级别不合口径，或修订摘要对不上，则依赖不成立。
	if !ok(r) || r.Revision != d.Revision || !bytes.Equal(r.Digest, d.Digest) {
		return domain.ErrAssetDependency
	}
	// 停用或草稿的副本不能钉进工程。
	if r.Status != AssetAvailable {
		return domain.ErrAssetNotAvailable
	}
	return nil
}

// platformProcessDep 厂级工程可直接引用的平台副本：平台级工艺，加密与否都行。
func platformProcessDep(r AssetReplica) bool {
	// 种类和级别对上就算。
	return r.Kind == KindProcess && r.Level == AssetLevelPlatform
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

// pinPersonalProcess 个人工程钉自己的个人工艺、本厂厂级可用工艺或已收的加密平台工艺。
func (s *kernel) pinPersonalProcess(ctx context.Context, acc Account, id uuid.UUID) (AssetDep, error) {
	// 只读本厂原件元数据，先不解开正文。
	p, err := s.loadCheckedMeta(ctx, id)
	// 本厂没有原件，再看是不是已收的加密平台工艺。
	if errors.Is(err, domain.ErrNotFound) {
		// 只有加密平台工艺能按原身份钉进个人工程。
		return s.pinPersonalPlatformProcess(ctx, id)
	}
	// 元数据读不到则拒绝，避免钉上空依赖。
	if err != nil {
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

// pinPersonalPlatformProcess 个人工程按原身份钉已收的加密平台工艺；普通平台工艺须先另存个人副本再钉。
func (s *kernel) pinPersonalPlatformProcess(ctx context.Context, id uuid.UUID) (AssetDep, error) {
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
	// 只认加密的平台级工艺；可复制的按「普通工艺复制一份」另存后再钉。
	if !personalPlatformDep(r) {
		return AssetDep{}, domain.ErrAssetDependency
	}
	// 副本不是可用则不能当依赖。
	if r.Status != AssetAvailable {
		return AssetDep{}, domain.ErrAssetNotAvailable
	}
	return AssetDep{ID: r.ID, Revision: r.Revision, Digest: r.Digest}, nil
}

// personalPlatformDep 个人工程可直接引用的平台副本：加密（不可复制）的平台级工艺。
func personalPlatformDep(r AssetReplica) bool {
	// 种类、级别、可复制三样都对上才算。
	return r.Kind == KindProcess && r.Level == AssetLevelPlatform && !r.Copyable
}

// resolveProjectDeps 改依赖时按身份重钉当前可用修订，跟上工艺升版；工程已声明、后来停用或被云端删除的原样留着，只有新加的才拒。
func (s *kernel) resolveProjectDeps(ctx context.Context, acc Account, level string, declared, deps []AssetDep) ([]AssetDep, error) {
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
		// 钉不住先看是不是工程早已声明的，是就留原样；否则这条依赖不成立。
		if err != nil {
			// 已声明的工艺只是后来不可用，不该挡住改别的依赖。
			kept, ok := s.keepDeclaredDep(ctx, declared, d.ID, err)
			// 不是已声明或不是这类原因就拒绝。
			if !ok {
				return nil, err
			}
			// 记录保持声明时的修订，工程照常按身份用它。
			pin = kept
		}
		// 把重钉后的修订放进结果。
		out = append(out, pin)
	}
	return out, nil
}

// keepDeclaredDep 工程已声明的工艺钉不上当前版时，判断能否原样留下：停用的和云端已删的留，别的原因（无权、不是工艺）不留。
func (s *kernel) keepDeclaredDep(ctx context.Context, declared []AssetDep, id uuid.UUID, pinErr error) (AssetDep, bool) {
	// 逐条找这个身份是不是已声明过。
	for _, d := range declared {
		// 身份对不上就看下一条。
		if d.ID != id {
			continue
		}
		// 停用的工程仍按身份用，留着。
		if errors.Is(pinErr, domain.ErrAssetNotAvailable) {
			return d, true
		}
		// 其余原因只放过云端已删：未撤回的读不到、已收的还在。
		if !errors.Is(pinErr, domain.ErrAssetDependency) {
			return AssetDep{}, false
		}
		// 还有未撤回的副本，说明不是删除，是不许依赖。
		if _, err := s.store.LatestReplicaMeta(ctx, id); !errors.Is(err, domain.ErrNotFound) {
			return AssetDep{}, false
		}
		// 收过任一修订才算云端已删，一份没有就是真缺。
		if _, err := s.store.HighestReplicaMeta(ctx, id); err != nil {
			return AssetDep{}, false
		}
		return d, true
	}
	return AssetDep{}, false
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
	// 名字两端空白不要，和新建同一口径。
	name = strings.TrimSpace(name)
	// 示教器盖正文时停用的也照盖（ApplyAppContent），改名同步要跟上，否则这件每轮都报没收下。
	fromApp := s.isAppSession(ctx, token)
	// 只改显示名；网页改已停用的拒绝，正文保持不动。
	return s.mutateAsset(ctx, acc, assetID, expected, "rename_asset", func(cur Asset) (store.AssetWrite, error) {
		// 网页上停用后不能改名。
		if cur.Status == AssetDisabled && !fromApp {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		// 空名字拒绝；已有的空名资产读和用都不受影响。
		if name == "" {
			return store.AssetWrite{}, domain.ErrInvalidName
		}
		// 名字太长焊次收不下，改名时就拒
		if err := checkAssetNameLen(name); err != nil {
			return store.AssetWrite{}, err
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
	// 这次把个人工艺换成厂级的结果，写完告诉调用方，写失败要回收。
	var adopted adoptResult
	// 网页读到的工程遮过包角几何，写的时候要盖回；示教器送来的是全的，不盖。
	fromApp := s.isAppSession(ctx, token)
	// 改正文并重算摘要；停用拒绝，工程还要核引用。
	row, err := s.mutateAsset(ctx, acc, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		// 停用后不能改正文。
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		// 工艺正文名字改成空的拒绝，免得资产名和正文名对不上还查不出来（D-110）。
		if err := assertProcessBodyName(cur.Kind, content); err != nil {
			return store.AssetWrite{}, err
		}
		// 工艺种类定了不能换，在 JSON 里加减包角标记或所属身份也不行。
		if err := assertProcessKindKept(cur, content); err != nil {
			return store.AssetWrite{}, err
		}
		// 接下来写入的正文和依赖，工程可能换掉个人工艺。
		body, deps := content, cur.Deps
		// 工程焊道引用必须落在已声明依赖里。
		if cur.Kind == KindProject {
			// 页面手里的正文还写着已换走的个人工艺时，先对回已有的厂级副本，不再复制。
			if cur.Level == AssetLevelFactory {
				// 只查依赖里已有的副本，这一步不新建。
				var err error
				// 正文和依赖都换成已有副本。
				body, deps, err = s.applyAdoptedCopies(ctx, acc, cur.Deps, body, deps)
				// 换不了就拒绝保存。
				if err != nil {
					return store.AssetWrite{}, err
				}
			}
			// 工艺槽只能挂焊接参数；先于依赖核对，挂错种类时给出的是这条提示。
			if err := s.assertWeldSlots(ctx, body); err != nil {
				return store.AssetWrite{}, err
			}
			// 有引用对不上依赖则拒绝保存。
			if err := s.assertProjectProcessIDs(ctx, body, deps); err != nil {
				return store.AssetWrite{}, err
			}
			// 焊道对不上则拒绝保存。
			if err := assertProjectWeldKind(cur.WeldKind, body); err != nil {
				return store.AssetWrite{}, err
			}
			// 校验都过了，厂级工程还钉着的个人工艺复制成厂级再写。
			if cur.Level == AssetLevelFactory {
				// 修订已经对不上就别复制了，写也写不进去。
				if cur.Revision != expected {
					return store.AssetWrite{}, domain.ErrRevisionConflict
				}
				// 复制并改写，复制不了就拒绝保存。
				var err error
				// 记下结果，写失败要回收。
				adopted, err = s.copyPersonalDeps(ctx, acc, cur.Deps, body, deps)
				// 复制不了就拒绝保存，已复制的里面回收过了。
				if err != nil {
					return store.AssetWrite{}, err
				}
				// 写入的是改写过的正文和依赖。
				body, deps = adopted.content, adopted.deps
			}
		}
		// 服务端改写过正文就要告诉调用方，这里记下来；回包给的还是遮着的这份。
		adopted.content = body
		// 和收到的不一样才算改写过。
		adopted.rewritten = !bytes.Equal(body, content)
		// 真正落库的正文，工程要把遮掉的包角几何盖回。
		final := body
		// 只有网页改工程才要盖回。
		if cur.Kind == KindProject && !fromApp {
			// 读现存正文当底。
			stored, err := s.loadChecked(ctx, cur.ID)
			// 读不到不能写，否则几何会丢。
			if err != nil {
				return store.AssetWrite{}, err
			}
			// 按焊道身份盖回几何和点列。
			final = contenttpl.RestoreCornerGeometry(stored.Content, body)
		}
		// 拼出本次要写入的内容，没通过的字段不改。
		return store.AssetWrite{Name: cur.Name, Content: final, Digest: digest.Sum(final), Copyable: cur.Copyable, Status: cur.Status, Deps: deps}, nil
	})
	// 没写成就回收刚复制的工艺。
	if err != nil {
		// 回收失败不盖住真因。
		s.recyclePersonalCopies(ctx, acc, adopted.made)
		return Asset{}, err
	}
	// 把改写后的正文和新工艺告诉调用方。
	return adopted.stamp(row), nil
}

// ApplyAppContent 示教器连上后用本机正文盖当前行，不跟网页对版本号。
func (s *Assets) ApplyAppContent(ctx context.Context, token string, assetID uuid.UUID, content []byte) (Asset, error) {
	// 核对登录仍有效且还握着角色，角色全收回的人不再从示教器改件。
	acc, err := s.requirePadActor(ctx, token)
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
	// 工艺正文名字改成空的拒绝（D-110）；示教器每轮同步最多再试一次，不会连着重发。
	if err := assertProcessBodyName(cur.Kind, content); err != nil {
		// 盖写被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
		return Asset{}, cur.Revision, err
	}
	// 示教器盖正文同样不许换工艺种类。
	if err := assertProcessKindKept(cur, content); err != nil {
		// 盖写被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
		return Asset{}, cur.Revision, err
	}
	// 停用仍盖正文，状态保持停用。
	deps := cur.Deps
	// 这一轮要写的正文；外层重试时收到的正文不能被改掉。
	body := content
	// 把个人工艺换成厂级的结果，写完告诉调用方，写失败要回收。
	var adopted adoptResult
	// 工程要先核焊道并补新引用，工艺直接盖。
	if cur.Kind == KindProject {
		// 仍带工艺路径则记拒绝，避免把参数拆出去。
		if err := contenttpl.RejectProcessPath(content); err != nil {
			// 盖写被拒时补记审计，写失败仍维持拒绝。
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
			return Asset{}, cur.Revision, domain.ErrForbidden
		}
		// 焊道对不上则拒绝，并补记审计。
		if err := assertProjectWeldKind(cur.WeldKind, body); err != nil {
			// 盖写被拒时补记审计，写失败仍维持拒绝。
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
			return Asset{}, cur.Revision, err
		}
		// 示教器手里的旧正文还写着已换走的个人工艺时，先对回已有的厂级副本，不再复制。
		if cur.Level == AssetLevelFactory {
			// 只查依赖里已有的副本，这一步不新建。
			body, deps, err = s.applyAdoptedCopies(ctx, acc, cur.Deps, body, deps)
			// 换不了就拒绝。
			if err != nil {
				// 盖写被拒时补记审计，写失败仍维持拒绝。
				_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
				return Asset{}, cur.Revision, err
			}
		}
		// 先有工艺再盖工程：正文里新引用钉到当前可用修订。
		deps, err = s.fillProjectDeps(ctx, acc, cur.Level, body, deps)
		// 依赖钉不到当前可用工艺则记拒绝。
		if err != nil {
			// 盖写被拒时补记审计，写失败仍维持拒绝。
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
			return Asset{}, cur.Revision, err
		}
		// 只核这次新钉的工艺类型；旧依赖不拦，免得改过类型的工程盖不回。
		if err := s.assertDepsWeldKind(ctx, cur.WeldKind, newlyPinnedDeps(cur.Deps, deps)); err != nil {
			// 盖写被拒时补记审计，写失败仍维持拒绝。
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
			return Asset{}, cur.Revision, err
		}
		// 有引用对不上依赖则拒绝，并补记审计。
		if err := s.assertProjectProcessIDs(ctx, body, deps); err != nil {
			// 盖写被拒时补记审计，写失败仍维持拒绝。
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
			return Asset{}, cur.Revision, err
		}
		// 工艺槽挂了包角工艺则拒绝盖写，和网页改正文同一口径。
		if err := s.assertWeldSlots(ctx, body); err != nil {
			// 盖写被拒时补记审计，写失败仍维持拒绝。
			_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
			return Asset{}, cur.Revision, err
		}
		// 校验都过了，厂级工程还钉着的个人工艺复制成厂级再写。
		if cur.Level == AssetLevelFactory {
			// 复制并改写，记下结果，写失败要回收。
			adopted, err = s.copyPersonalDeps(ctx, acc, cur.Deps, body, deps)
			// 复制不了就拒绝，已复制的里面回收过了。
			if err != nil {
				// 盖写被拒时补记审计，写失败仍维持拒绝。
				_ = s.audit(ctx, &acc.ID, nil, "apply_app", assetTarget(cur.ID, cur.Revision), audit.Deny)
				return Asset{}, cur.Revision, err
			}
			// 写入的是改写过的正文和依赖。
			body, deps = adopted.content, adopted.deps
		}
	}
	// 服务端改写过正文就要告诉调用方，这里记下来。
	adopted.content = body
	// 和收到的不一样才算改写过。
	adopted.rewritten = !bytes.Equal(body, content)
	// 按期望修订写入，冲突就拒绝覆盖。
	row, err := s.store.UpdateGovernedAsset(ctx, assetID, cur.Revision, store.AssetWrite{
		Name: cur.Name, Content: body, Digest: digest.Sum(body),
		Copyable: cur.Copyable, Status: cur.Status, Deps: deps,
	})
	// 失败则记拒绝并停下，不继续往下改。
	if err != nil {
		// 没写成就回收刚复制的工艺；并发重试时外层会再走一遍。
		s.recyclePersonalCopies(ctx, acc, adopted.made)
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
	// 去掉正文再返回，避免工艺参数外泄；改写过的工程正文另带。
	return adopted.stamp(stripContent(row)), row.Revision, nil
}

// newlyPinnedDeps 挑出合并后身份不在原依赖里的条目，即这次新钉的工艺。
func newlyPinnedDeps(old, merged []AssetDep) []AssetDep {
	// 记下原依赖的身份，后面据此分出哪些是新补的。
	have := make(map[uuid.UUID]struct{}, len(old))
	// 逐条收下原依赖的身份，修订不同也算旧依赖。
	for _, d := range old {
		// 用身份当键，同一工艺只算一次。
		have[d.ID] = struct{}{}
	}
	// 只装这次新补进来的依赖。
	var out []AssetDep
	// 逐条看合并后的依赖，挑出原来没有的身份。
	for _, d := range merged {
		// 原来就钉着的跳过，不在这里核作业类型。
		if _, ok := have[d.ID]; ok {
			continue
		}
		// 新钉的这条留下，交给作业类型核对。
		out = append(out, d)
	}
	return out
}

// isPlatformReplica 本厂有这份身份的已收平台副本。
func (s *kernel) isPlatformReplica(ctx context.Context, id uuid.UUID) bool {
	// 读得到副本元数据就算有。
	_, err := s.store.LatestReplicaMeta(ctx, id)
	return err == nil
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

// canPadAdmin 超管或管理员即可改、建厂级。
func (s *kernel) canPadAdmin(ctx context.Context, acc Account) error {
	// 和管理端同一口径
	return s.canAuthorFactory(ctx, acc)
}

// SetAssetWeldKind 未停用的本厂工艺或工程可改作业类型；在用工艺不许换，工程正文对不上时换成空焊道。
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
		// 只有焊接参数才分适用作业，包角和自建种类不许改。
		if cur.Kind == KindProcess && kind != cur.WeldKind && cur.TemplateID != contenttpl.SeedTplWeld {
			return store.AssetWrite{}, domain.ErrProcessKindFixed
		}
		// 工艺真要换类型时，先看有没有工程还钉着它。
		if cur.Kind == KindProcess && kind != cur.WeldKind {
			// 查工程依赖里是否还钉着这条工艺。
			used, err := s.store.AssetIsReferenced(ctx, cur.ID)
			// 引用关系查不清就先不改类型。
			if err != nil {
				return store.AssetWrite{}, err
			}
			// 仍被工程钉着则拒绝，免得那些工程类型对不上。
			if used {
				return store.AssetWrite{}, domain.ErrWeldKindInUse
			}
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
		// 正文里认得出的焊道类型对不上，就要换成空焊道。
		wipe := assertProjectWeldKind(kind, body) != nil
		// 类型核对放过时，再看正文是否还引用被筛掉的工艺。
		if !wipe {
			// 自定义模版认不出类型，按引用和筛掉的依赖再核一次。
			wipe, err = s.refsDroppedDeps(ctx, body, cur.Deps, kept)
			// 收集引用失败就整次拒绝，不清正文。
			if err != nil {
				return store.AssetWrite{}, err
			}
		}
		// 类型不符或引用了被筛掉的工艺，正文换成空焊道。
		if wipe {
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

// refsDroppedDeps 正文是否引用了改类型时被筛掉的依赖；没有筛掉就不看正文。
func (s *kernel) refsDroppedDeps(ctx context.Context, content []byte, all, kept []AssetDep) (bool, error) {
	// 记下留下来的依赖身份，剩下的就是被筛掉的。
	keep := make(map[uuid.UUID]struct{}, len(kept))
	// 逐条登记留下的依赖。
	for _, d := range kept {
		// 用身份当键，同一工艺只记一次。
		keep[d.ID] = struct{}{}
	}
	// 按数量先准备被筛掉的身份集合。
	dropped := make(map[string]struct{}, len(all))
	// 逐条看原依赖，不在留下集合里的就是被筛掉的。
	for _, d := range all {
		// 留下来的跳过，只收被筛掉的。
		if _, ok := keep[d.ID]; ok {
			continue
		}
		// 收成文本，和正文里的引用对得上。
		dropped[d.ID.String()] = struct{}{}
	}
	// 一条都没筛掉，正文引用不会悬空。
	if len(dropped) == 0 {
		return false, nil
	}
	// 按已收模版收集引用；读模版失败或路径键都整次拒绝。
	ids, err := s.collectProjectProcessIDs(ctx, content)
	// 收集失败就原样交回，不当成命中去清正文。
	if err != nil {
		return false, err
	}
	// 逐个引用看是否落在被筛掉的部分。
	for _, id := range ids {
		// 命中一条就得清正文，免得下发缺工艺。
		if _, ok := dropped[id]; ok {
			return true, nil
		}
	}
	return false, nil
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
	// 内部回收和整夹删除不核修订
	return s.deleteAsset(ctx, token, assetID, 0)
}

// DeleteAssetAt 网页删除：带期望修订，持旧页面删别人刚改过的件回修订冲突，原件不变。
// 示教器同步删件不跟网页对版本号（同 ApplyAppContent），没带修订照旧删。
func (s *Assets) DeleteAssetAt(ctx context.Context, token string, assetID uuid.UUID, expected int64) error {
	// 示教器会话没带修订，按本机清单删，不核修订
	if expected < 1 && s.isAppSession(ctx, token) {
		// 按零修订删，跳过核对
		return s.deleteAsset(ctx, token, assetID, 0)
	}
	// 期望修订从 1 起，网页没带就当对不上，免得绕开冲突检查
	if expected < 1 {
		// 记下删除被拒，写失败仍不删
		s.auditDeleteDenied(ctx, token, assetID, expected)
		return domain.ErrRevisionConflict
	}
	// 带上期望修订去删
	return s.deleteAsset(ctx, token, assetID, expected)
}

// auditDeleteDenied 期望修订缺失时补记一条删除拒绝；会话无效就只交回错误不记人。
func (s *Assets) auditDeleteDenied(ctx context.Context, token string, assetID uuid.UUID, expected int64) {
	// 会话还在才记到人头上
	if acc, err := s.RequireActive(ctx, token); err == nil {
		// 删除被拒时补记审计，写失败仍不删。
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, expected), audit.Deny)
	}
}

// deleteAsset 删本厂原件；expected 大于零时还要核期望修订。
func (s *Assets) deleteAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) error {
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
	// 按级别核制作权：厂级要管理员，个人级另验创建人或超管。
	if err := s.canAuthorLevel(ctx, acc, cur.Level); err != nil {
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
	// 带了期望修订就先比一次，别人刚改过就不删，提示刷新
	if expected > 0 && cur.Revision != expected {
		// 删除被拒时补记审计，写失败仍不删。
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return domain.ErrRevisionConflict
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
	// 无引用才删本厂原件；带了期望修订的在删的那一刻再核一次，挡住比完之后又被改的。
	if err := s.deleteGovernedRow(ctx, assetID, expected); err != nil {
		// 删除被拒时补记审计，写失败仍不删。
		_ = s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return err
	}
	// 删掉之后记成功；审计失败则调用方当没删成。
	return s.audit(ctx, &acc.ID, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Allow)
}

// deleteGovernedRow 按是否带期望修订选删法。
func (s *Assets) deleteGovernedRow(ctx context.Context, assetID uuid.UUID, expected int64) error {
	// 没带期望修订就直接删
	if expected == 0 {
		// 按身份删，不核修订
		return s.store.DeleteGovernedAsset(ctx, assetID)
	}
	// 带了就只删这一修订
	return s.store.DeleteGovernedAssetAt(ctx, assetID, expected)
}

// mutateAsset 按期望修订改本厂原件；个人级须创建人或整厂管理员。
func (s *kernel) mutateAsset(ctx context.Context, acc Account, assetID uuid.UUID, expected int64, action string, patch func(Asset) (store.AssetWrite, error)) (Asset, error) {
	// 须有制作权；个人级另验创建人或管理员。失败一律记拒绝。
	cur, err := s.loadCheckedMeta(ctx, assetID)
	// 元数据读不到则记拒绝，避免钉上空依赖。
	if err != nil {
		// 这一步被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, action, assetTarget(assetID, expected), audit.Deny)
		// 平台级只在云端配置，厂端一律无权改，不当成找不到。
		if errors.Is(err, domain.ErrNotFound) && s.isPlatformReplica(ctx, assetID) {
			return Asset{}, domain.ErrForbidden
		}
		return Asset{}, err
	}
	// 按级别核制作权：厂级要管理员，个人级另验创建人或超管。
	if err := s.canAuthorLevel(ctx, acc, cur.Level); err != nil {
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

// canViewAssetMeta 厂级和平台级人人可见；个人级和个人文件夹同一口径，只给创建人和工厂超管。
func (s *kernel) canViewAssetMeta(ctx context.Context, acc Account, a Asset) error {
	// 个人级只给创建人和超管，其余级别放行
	return s.canTouchPersonal(ctx, acc, a)
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
	// 超管能看全部个人级，只判一次。
	sa := s.isComputerAdmin(ctx, acc)
	// 先攒当前人能看的条目，正文稍后去掉。
	visible := []Asset{}
	// 逐条看本厂原件，种类或权限不合就跳过。
	for _, a := range rows {
		// 种类对不上筛选就跳过。
		if kind != "" && a.Kind != kind {
			continue
		}
		// 别人的个人级只给超管看，和个人文件夹同口径。
		if a.Level == AssetLevelPersonal && a.CreatorID != acc.ID && !sa {
			continue
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
	// 个人级只给创建人或整厂管理员；不可复制平台级工艺不给人看。失败一律记拒绝。
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
	// 工程正文遮掉包角几何：加密包角的层参数也写在里面，给人读的入口不交；示教器走闭包，不走这里。
	if a.Kind == KindProject {
		// 没有包角几何的工程字节不动。
		return contenttpl.MaskCornerGeometry(a.Content), nil
	}
	return a.Content, nil
}

// PromoteToFactory 把未停用的个人级复制一份到所选厂级文件夹，落成草稿，改好再发布；每次升都另起一份，不回写。
func (s *Assets) PromoteToFactory(ctx context.Context, token string, assetID, folderID uuid.UUID) (Asset, error) {
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
	// 只有个人级能升厂级，厂级和平台级拒绝。
	if src.Level != AssetLevelPersonal {
		// 升档被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrForbidden
	}
	// 个人级只给创建人和超管碰；还要有厂级制作权。
	if err := s.canTouchPersonal(ctx, acc, src); err != nil {
		// 升档被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
	// 没有厂级制作权则记拒绝。
	if err := s.canAuthorFactory(ctx, acc); err != nil {
		// 升档被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
	// 停用不能升；草稿可以。
	if src.Status == AssetDisabled {
		// 升档被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrAssetNotAvailable
	}
	// 工程依赖须已是可用厂级工艺。
	if err := s.assertPromoteToFactoryDeps(ctx, src); err != nil {
		// 升档被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
	// 目标文件夹先核写权限，空则挂厂级树根。
	if err := s.checkNewAssetFolder(ctx, acc, folderID); err != nil {
		// 升档被拒时补记审计，写失败仍维持拒绝。
		_ = s.audit(ctx, &acc.ID, nil, "promote_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, err
	}
	// 记下源修订，只当出处。
	rev := src.Revision
	// 复制一份新身份的厂级草稿，挂进所选文件夹，同一事务挂不上就一起回滚。
	row, err := s.store.InsertGovernedAssetInto(ctx, Asset{
		Kind: src.Kind, Level: AssetLevelFactory, Name: src.Name, Status: AssetDraft, Copyable: true, WeldKind: src.WeldKind,
		Content: src.Content, Digest: src.Digest, CreatorID: acc.ID,
		SourceID: &src.ID, SourceRevision: &rev, Deps: src.Deps,
	}, folderID, false)
	// 落库失败则记拒绝，不假装已经建成。
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
	// 升厂级时逐条确认依赖已是可用厂级工艺或已收平台工艺。
	for _, d := range src.Deps {
		// 只读本厂原件元数据，先不解开正文。
		p, err := s.loadCheckedMeta(ctx, d.ID)
		// 本厂没有原件，加密平台工艺按原身份跟着升。
		if errors.Is(err, domain.ErrNotFound) {
			// 平台副本按身份核当前版：声明的旧修订在不在、旧行什么状态都不算数，不合格就拒绝升档。
			if _, err := s.pinFactoryProcess(ctx, d.ID); err != nil {
				return err
			}
			continue
		}
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
	if err := s.canAuthorFactory(ctx, acc); err != nil {
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
	// 带包角几何的工程不从这里交给人：加密包角的层参数也写在里面；真升档走通道（SnapshotForChannel），到 WAN 才解开。
	if src.Kind == KindProject && contenttpl.HasCornerGeometry(src.Content) {
		// 外送被拒时补记审计，写失败仍不外送。
		_ = s.audit(ctx, &acc.ID, nil, "export_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return AssetSnapshot{}, domain.ErrForbidden
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

// ListPromotable 列给 WAN 升档看的本厂厂级可用工艺/工程，不含正文；个人件、草稿、停用件和平台副本都不出厂。
func (s *Assets) ListPromotable(ctx context.Context, kind string) ([]PromotableAsset, error) {
	// 只列工艺或工程，别的种类拒绝。
	if kind != "" && kind != KindProcess && kind != KindProject {
		return nil, domain.ErrNotFound
	}
	// 读本厂原件，再按级别和状态筛，不含正文。
	rows, err := s.store.ListGovernedAssets(ctx)
	// 列表读不出来则整份失败，不回半截。
	if err != nil {
		return nil, err
	}
	// 升档列表只攒元数据，不含正文。
	out := []PromotableAsset{}
	// 逐条收本厂原件元数据，只认厂级可用。
	for _, a := range rows {
		// 种类对不上就跳过。
		if kind != "" && a.Kind != kind {
			continue
		}
		// 个人件只有属主可见，草稿和停用件不能升档，都不列给 WAN。
		if !promotableByChannel(a) {
			continue
		}
		// 只收元数据放进升档列表，正文不带走。
		out = append(out, toPromotable(a))
	}
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.audit(ctx, nil, nil, "list_promotable", kind, audit.Allow); err != nil {
		return nil, err
	}
	return out, nil
}

// promotableByChannel 通道升档只认本厂厂级可用件。
func promotableByChannel(a Asset) bool {
	// 级别是厂级、状态是可用才算可升档。
	return a.Level == AssetLevelFactory && a.Status == AssetAvailable
}

// SnapshotForChannel 通道升档用：只有本厂厂级可用件可出快照；工艺须可复制，工程不看可复制。
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
	// 个人件只有属主可见，通道不外送。
	if src.Level != AssetLevelFactory {
		// 外送被拒时补记审计，写失败仍不外送。
		_ = s.audit(ctx, nil, nil, "export_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return AssetSnapshot{}, domain.ErrForbidden
	}
	// 草稿和停用件不是可用件，通道不外送。
	if src.Status != AssetAvailable {
		// 外送被拒时补记审计，写失败仍不外送。
		_ = s.audit(ctx, nil, nil, "export_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return AssetSnapshot{}, domain.ErrAssetNotAvailable
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
