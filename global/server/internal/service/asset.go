package service

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/store"
)

// 审计对象：身份加修订。
func assetTarget(id uuid.UUID, rev int64) string {
	// 把数字收成文本。
	return id.String() + " rev=" + strconv.FormatInt(rev, 10)
}

// 列表不回正文。
func stripContent(a Asset) Asset {
	// 列表去掉正文，避免工艺参数外泄。
	a.Content = nil
	return a
}

// assetCopyable 工艺按调用方；工程没有可复制，恒为是。
func assetCopyable(kind string, copyable bool) bool {
	// 按是不是工程决定要不要核对焊道和依赖。
	if kind == KindProject {
		return true
	}
	return copyable
}

// loadChecked 按身份取正文并核对摘要，不符则拒绝。
func (s *kernel) loadChecked(ctx context.Context, id uuid.UUID) (Asset, error) {
	// 按资产处理，失败就不能继续。
	a, err := s.store.AssetByID(ctx, id)
	// 摘要或状态不符就拒绝。
	if err != nil {
		return Asset{}, err
	}
	// 摘要对不上当篡改。
	if !digest.Match(a.Content, a.Digest) {
		return Asset{}, domain.ErrIntegrity
	}
	return a, nil
}

// CreatePlatformProcess 由 WAN 管理员制作平台级工艺，默认可复制为否，状态草稿。
func (s *Assets) CreatePlatformProcess(ctx context.Context, token, name string, content []byte, weldKind ...string) (Asset, error) {
	// 新建这一条，已有或没资格则不行。
	return s.CreatePlatformProcessWith(ctx, token, name, content, false, weldKind...)
}

// CreatePlatformProcessWith 创建平台级工艺并记下可复制，状态草稿。
func (s *Assets) CreatePlatformProcessWith(ctx context.Context, token, name string, content []byte, copyable bool, weldKind ...string) (Asset, error) {
	// 只有 WAN 管理员能做平台级工艺。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, nil, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 收成合法值，空或太长不要。
	content, err = s.normalizeContent(ctx, KindProcess, content)
	// 不合法就拒绝，避免脏数据入库。
	if err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 收成单层、多层或 T 排。
	kind, err := store.NormalizeWeldKind(firstWeldKind(weldKind))
	// 别的写法一律拒绝。
	if err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 写入这一条，失败就不能继续。
	row, err := s.store.InsertAsset(ctx, Asset{
		Kind: KindProcess, Name: name, Status: AssetDraft, Copyable: copyable, WeldKind: kind,
		Content: content, Digest: digest.Sum(content), CreatorID: admin.ID,
	})
	// 这一步失败就停，避免留下半截。
	if err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 新建成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	// 去掉正文再返回。
	return stripContent(row), nil
}

// CopyPlatformProcess 平台级工艺或工程另存为新草稿；不看可复制，原件正文原样拷贝，不套模版。
func (s *Assets) CopyPlatformProcess(ctx context.Context, token string, assetID uuid.UUID, name string) (Asset, error) {
	// 只有 WAN 管理员能另存；停用拒绝。可复制只拦厂端升档，不拦云端另存。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, nil, nil, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	// 去掉多余空白或前后缀。
	name = strings.TrimSpace(name)
	// 空和有值走不同路，避免把空白写进名录。
	if name == "" {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, domain.ErrInvalidName
	}
	// 装入并核对摘要。
	src, err := s.loadChecked(ctx, assetID)
	// 不符就不能把这条拿去用。
	if err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	// 不是工艺也不是工程就拒绝，别的种类不归这里。
	if src.Kind != KindProcess && src.Kind != KindProject {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrForbidden
	}
	// 停用件不能另存。
	if src.Status == AssetDisabled {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrAssetNotAvailable
	}
	// 原件正文与依赖原样落新草稿，不套模版、不写升档来源。
	row, err := s.store.InsertAsset(ctx, Asset{
		Kind: src.Kind, Name: name, Status: AssetDraft, Copyable: assetCopyable(src.Kind, false), WeldKind: src.WeldKind,
		Content: src.Content, Digest: src.Digest, CreatorID: admin.ID, Deps: src.Deps,
	})
	// 这一步失败就停，避免留下半截。
	if err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 另存成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	// 把副本放在原件旁边。
	s.placeCopyBeside(ctx, src.ID, row.ID)
	// 去掉正文再返回。
	return stripContent(row), nil
}

// RenamePlatformAsset 改显示名，身份不变。
func (s *Assets) RenamePlatformAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64, name string) (Asset, error) {
	// 改显示名，身份不变；修订不符就拒绝。
	return s.mutatePlatform(ctx, token, assetID, expected, "rename_asset", func(cur Asset) (store.AssetWrite, error) {
		// 停用后不得改名。
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: cur.Status, Deps: cur.Deps}, nil
	})
}

// PublishPlatformAsset 草稿改为可用。
func (s *Assets) PublishPlatformAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) (Asset, error) {
	// 改发布状态；修订对不上就整次拒绝。
	return s.mutatePlatform(ctx, token, assetID, expected, "publish_asset", func(cur Asset) (store.AssetWrite, error) {
		// 只有草稿能改为可用。
		if cur.Status != AssetDraft {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: AssetAvailable, Deps: cur.Deps}, nil
	})
}

// UpdatePlatformAssetContent 改正文并重算摘要；停用后拒绝。已有正文不套模版。
func (s *Assets) UpdatePlatformAssetContent(ctx context.Context, token string, assetID uuid.UUID, expected int64, content []byte) (Asset, error) {
	// 按当前行改写；修订不符就拒绝。
	return s.mutatePlatform(ctx, token, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		// 停用后不得改正文。
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		// 工程焊道引用必须落在已声明依赖里，且作业类型一致。
		if cur.Kind == KindProject {
			// 对不上就拒绝，避免焊道和模式错配。
			if err := s.assertProjectProcessIDs(ctx, content, cur.Deps); err != nil {
				return store.AssetWrite{}, err
			}
			// 对不上就拒绝，避免焊道和模式错配。
			if err := assertProjectWeldKind(cur.WeldKind, content); err != nil {
				return store.AssetWrite{}, err
			}
		}
		// 按内容算摘要，失败就不能继续。
		return store.AssetWrite{Name: cur.Name, Content: content, Digest: digest.Sum(content), Copyable: cur.Copyable, Status: cur.Status, Deps: cur.Deps}, nil
	})
}

// DisablePlatformAsset 可用改为停用；停用期间不得改正文。
func (s *Assets) DisablePlatformAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) (Asset, error) {
	// 改为停用；状态或修订不对就拒绝。
	return s.mutatePlatform(ctx, token, assetID, expected, "disable_asset", func(cur Asset) (store.AssetWrite, error) {
		// 只有可用能改为停用。
		if cur.Status != AssetAvailable {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: AssetDisabled, Deps: cur.Deps}, nil
	})
}

// ReenablePlatformAsset 停用改回可用。
func (s *Assets) ReenablePlatformAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) (Asset, error) {
	// 改发布状态；修订对不上就整次拒绝。
	return s.mutatePlatform(ctx, token, assetID, expected, "publish_asset", func(cur Asset) (store.AssetWrite, error) {
		// 只有停用能改回可用。
		if cur.Status != AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: AssetAvailable, Deps: cur.Deps}, nil
	})
}

// DeletePlatformAsset 未被工程依赖则可删；仍被引用则拒绝。
func (s *Assets) DeletePlatformAsset(ctx context.Context, token string, assetID uuid.UUID) error {
	// 只有 WAN 管理员能删；仍被引用则拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 删除被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "delete_asset", assetID.String(), audit.Deny)
		return err
	}
	// 装入并核对摘要。
	cur, err := s.loadChecked(ctx, assetID)
	// 不符就不能把这条拿去用。
	if err != nil {
		// 删除被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "delete_asset", assetID.String(), audit.Deny)
		return err
	}
	// 仍被工程依赖则不能删。
	used, err := s.store.AssetIsReferenced(ctx, assetID)
	// 摘要或状态不符就拒绝。
	if err != nil {
		return err
	}
	// 仍被工程依赖则不能删，并记拒绝。
	if used {
		// 删除被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return domain.ErrReferenced
	}
	// 删正文并记收回，给已下发厂补送。
	if err := s.store.DeleteAssetAndRetract(ctx, assetID); err != nil {
		// 删除被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return err
	}
	// 删除成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Allow); err != nil {
		return err
	}
	// 通知厂端收回已删的。
	s.notifyRetract(ctx, assetID)
	// 通知厂端对齐平台目录。
	s.notifyPlatformFS(ctx, cur.Kind)
	return nil
}

// SetPlatformCopyable 未停用工艺可改可复制；工程没有这项。
func (s *Assets) SetPlatformCopyable(ctx context.Context, token string, assetID uuid.UUID, expected int64, copyable bool) (Asset, error) {
	// 按当前行改写；修订不符就拒绝。
	return s.mutatePlatform(ctx, token, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		// 不是工艺就拒绝，这项只对工艺开放。
		if cur.Kind != KindProcess {
			return store.AssetWrite{}, domain.ErrForbidden
		}
		// 停用后不得改可复制。
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: copyable, Status: cur.Status, Deps: cur.Deps}, nil
	})
}

// SetPlatformWeldKind 未停用的工艺或工程可改作业类型；工程正文对不上时换成空焊道。
func (s *Assets) SetPlatformWeldKind(ctx context.Context, token string, assetID uuid.UUID, expected int64, weldKind string) (Asset, error) {
	// 按当前行改写；修订不符就拒绝。
	return s.mutatePlatform(ctx, token, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		// 不是工艺也不是工程就拒绝，别的种类不归这里。
		if cur.Kind != KindProcess && cur.Kind != KindProject {
			return store.AssetWrite{}, domain.ErrForbidden
		}
		// 已停用则拒绝变更，正文和依赖都锁住。
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		// 收成单层、多层或 T 排。
		kind, err := store.NormalizeWeldKind(weldKind)
		// 别的写法一律拒绝。
		if err != nil {
			return store.AssetWrite{}, err
		}
		// 先按当前行组一份写回，再按需改字段。
		write := store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: cur.Status, Deps: cur.Deps, WeldKind: kind}
		// 按是不是工程决定要不要核对焊道和依赖。
		if cur.Kind != KindProject {
			return write, nil
		}
		// 做完这一步再继续。
		kept, err := s.depsMatchingWeldKind(ctx, kind, cur.Deps)
		// 这一步失败就停，避免留下半截。
		if err != nil {
			return store.AssetWrite{}, err
		}
		// 只保留和作业类型相符的依赖。
		write.Deps = kept
		// 对不上就拒绝，避免焊道和模式错配。
		if err := assertProjectWeldKind(kind, cur.Content); err != nil {
			// 收成字节，失败就不能继续。
			empty := []byte("[]")
			// 类型对不上就改成空焊道。
			write.Content = empty
			// 按内容算摘要，失败就不能继续。
			write.Digest = digest.Sum(empty)
		}
		return write, nil
	})
}

// GetPlatformAsset 读平台级元数据；摘要不符则拒绝。
func (s *Assets) GetPlatformAsset(ctx context.Context, token string, assetID uuid.UUID) (Asset, error) {
	// 只有 WAN 管理员能读元数据。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 读取被拒就留审计，不写正文。
		_ = s.audit(ctx, nil, nil, nil, "get_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	// 装入并核对摘要。
	a, err := s.loadChecked(ctx, assetID)
	// 不符就不能把这条拿去用。
	if err != nil {
		// 读取被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "get_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	// 读取成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, nil, "get_asset", assetTarget(a.ID, a.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	// 去掉正文再返回。
	return stripContent(a), nil
}

// ReadPlatformAssetContent 读正文并核对摘要，审计不写正文。
func (s *Assets) ReadPlatformAssetContent(ctx context.Context, token string, assetID uuid.UUID) ([]byte, error) {
	// 只有 WAN 管理员能读正文。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		return nil, err
	}
	// 装入并核对摘要。
	a, err := s.loadChecked(ctx, assetID)
	// 不符就不能把这条拿去用。
	if err != nil {
		// 读正文被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "read_asset_content", assetID.String(), audit.Deny)
		return nil, err
	}
	// 读取成功才记允许，不写正文。
	if err := s.audit(ctx, &admin.ID, nil, nil, "read_asset_content", assetTarget(a.ID, a.Revision), audit.Allow); err != nil {
		return nil, err
	}
	return a.Content, nil
}

// PromoteFromSnapshot 用厂级快照升平台级：第一次复制新身份为草稿；再升按正文摘要跳过或覆盖为草稿。原件不进 WAN。
func (s *Assets) PromoteFromSnapshot(ctx context.Context, token string, snap AssetSnapshot) (Asset, error) {
	// 只有 WAN 管理员能升档。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 升档被拒就留审计。
		_ = s.audit(ctx, nil, nil, &snap.SourceFactoryID, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, err
	}
	// 记下工厂身份，授权和审计都按这家厂。
	fid := snap.SourceFactoryID
	// 厂级不分草稿/停用都可升平台；不可复制工艺仍拒绝，工程不看可复制。
	if snap.Kind == KindProcess && !snap.Copyable {
		// 升档被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, domain.ErrAssetNotCopyable
	}
	// 过站解开后再验摘要；WAN 入库仍是明文。
	body, err := s.OpenSnapshotTransit(ctx, snap)
	// 坏包按损坏拒绝，不能把密文当正文。
	if err != nil {
		// 升档被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, err
	}
	// 摘要对不上则按损坏拒绝，不能入库或下发。
	if !digest.Match(body, snap.Digest) || len(snap.Digest) != 32 {
		// 升档被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, domain.ErrIntegrity
	}
	// 把旧身份改成新的。
	deps, idMap, err := s.rewritePromoteDeps(ctx, snap.Kind, snap.Deps)
	// 改不完就拒绝，避免还指向旧的。
	if err != nil {
		// 升档被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, err
	}
	// 只改工程：按当前模版改写工艺引用，不套模版。
	if snap.Kind == KindProject {
		// 按工程规则取名或核对。
		items, err := s.projectItemSchemas(ctx)
		// 不合法就拒绝，失败就不能继续。
		if err != nil {
			// 升档被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
			return Asset{}, err
		}
		// 把旧身份改成新的。
		body, err = contenttpl.RewriteProcessIDsFromItems(items, body, idMap)
		// 改不完就拒绝，避免还指向旧的。
		if err != nil {
			// 升档被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
			return Asset{}, domain.ErrAssetDependency
		}
	}
	// 作业类型跟源走；工程正文和依赖工艺必须同类型。
	weld, err := store.NormalizeWeldKind(snap.WeldKind)
	// 别的写法一律拒绝。
	if err != nil {
		// 升档被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, err
	}
	// 按是不是工程决定要不要核对焊道和依赖。
	if snap.Kind == KindProject {
		// 对不上就拒绝，避免焊道和模式错配。
		if err := assertProjectWeldKind(weld, body); err != nil {
			// 升档被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
			return Asset{}, err
		}
		// 对不上就拒绝，避免焊道和模式错配。
		if err := s.assertDepsWeldKind(ctx, weld, deps); err != nil {
			// 升档被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
			return Asset{}, err
		}
	}
	// 按内容算摘要，失败就不能继续。
	sum := digest.Sum(body)
	// 记下带来的修订，对不上就拒绝覆盖。
	rev := snap.SourceRevision
	// 按源厂身份找已升过的平台级。
	existing, err := s.store.AssetBySourceID(ctx, snap.SourceID)
	// 没有错误才继续，有错留在后面的分支。
	if err == nil {
		// 摘要相同就不必覆盖，避免无谓升高修订。
		if bytes.Equal(existing.Digest, sum) {
			// 正文未变则幂等返回已有平台级。
			if err := s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", assetTarget(existing.ID, existing.Revision)+" from "+assetTarget(snap.SourceID, snap.SourceRevision), audit.Allow); err != nil {
				return Asset{}, err
			}
			// 去掉正文再返回。
			return stripContent(existing), nil
		}
		// 正文变了则覆盖为草稿，身份不变。
		row, err := s.store.UpdateAsset(ctx, existing.ID, existing.Revision, store.AssetWrite{
			Name: snap.Name, Content: body, Digest: sum, Copyable: existing.Copyable, Status: AssetDraft, Deps: deps, SourceRevision: &rev,
		})
		// 这一步失败就停，避免留下半截。
		if err != nil {
			// 升档被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
			return Asset{}, err
		}
		// 覆盖成功才记允许。
		if err := s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", assetTarget(row.ID, row.Revision)+" from "+assetTarget(snap.SourceID, snap.SourceRevision), audit.Allow); err != nil {
			return Asset{}, err
		}
		// 去掉正文再返回。
		return stripContent(row), nil
	}
	// 不是没有这条，就当真正的故障返回。
	if !errors.Is(err, domain.ErrNotFound) {
		// 升档被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, err
	}
	// 第一次升档则新身份落草稿。
	row, err := s.store.InsertAsset(ctx, Asset{
		Kind: snap.Kind, Name: snap.Name, Status: AssetDraft, Copyable: assetCopyable(snap.Kind, false), WeldKind: weld,
		Content: body, Digest: sum, CreatorID: admin.ID,
		SourceID: &snap.SourceID, SourceRevision: &rev, SourceFactoryID: &snap.SourceFactoryID,
		Deps: deps,
	})
	// 这一步失败就停，避免留下半截。
	if err != nil {
		// 升档被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, err
	}
	// 升档成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", assetTarget(row.ID, row.Revision)+" from "+assetTarget(snap.SourceID, snap.SourceRevision), audit.Allow); err != nil {
		return Asset{}, err
	}
	// 去掉正文再返回。
	return stripContent(row), nil
}

// rewritePromoteDeps 把厂级工艺依赖改写成已升档的平台级可用工艺。
func (s *Assets) rewritePromoteDeps(ctx context.Context, kind string, deps []AssetDep) ([]AssetDep, map[string]string, error) {
	// 按是不是工程决定要不要核对焊道和依赖。
	if kind != KindProject {
		return deps, nil, nil
	}
	// 按数量先准备容器。
	out := make([]AssetDep, 0, len(deps))
	// 按数量先准备容器。
	idMap := make(map[string]string, len(deps))
	// 逐条核对依赖，缺了或类型不同就拒绝。
	for _, d := range deps {
		// 依赖须已升成平台级可用工艺。
		plat, err := s.store.AssetBySourceID(ctx, d.ID)
		// 摘要或状态不符就拒绝。
		if err != nil {
			// 没有这条就按不存在处理，不当成别的故障。
			if errors.Is(err, domain.ErrNotFound) {
				return nil, nil, domain.ErrAssetDependency
			}
			return nil, nil, err
		}
		// 摘要对不上当篡改。
		if !digest.Match(plat.Content, plat.Digest) {
			return nil, nil, domain.ErrIntegrity
		}
		// 不是可用就拒绝，草稿和停用不能当发布。
		if plat.Kind != KindProcess || plat.Status != AssetAvailable {
			return nil, nil, domain.ErrAssetDependency
		}
		// 把这一项接进结果。
		out = append(out, AssetDep{ID: plat.ID, Revision: plat.Revision, Digest: plat.Digest})
		// 收成文本给审计或指令用。
		idMap[d.ID.String()] = plat.ID.String()
	}
	return out, idMap, nil
}

// CreatePlatformProject 创建平台级工程；不设可复制，参数里的工艺写入 deps。
func (s *Assets) CreatePlatformProject(ctx context.Context, token, name string, content []byte, deps []AssetDep, weldKind ...string) (Asset, error) {
	// 只有 WAN 管理员能做平台级工程。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, nil, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 收成单层、多层或 T 排。
	kind, err := store.NormalizeWeldKind(firstWeldKind(weldKind))
	// 别的写法一律拒绝。
	if err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 收成合法值，空或太长不要。
	content, err = s.normalizeContent(ctx, KindProject, content)
	// 不合法就拒绝，避免脏数据入库。
	if err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 对不上就拒绝，避免焊道和模式错配。
	if err := assertProjectWeldKind(kind, content); err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 参数里选过的工艺补进依赖，钉当前修订。
	deps, err = s.fillPlatformProjectDeps(ctx, content, deps)
	// 补不上就拒绝保存。
	if err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 对不上就拒绝，避免焊道和模式错配。
	if err := s.assertPlatformProcessDeps(ctx, deps); err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 对不上就拒绝，避免焊道和模式错配。
	if err := s.assertDepsWeldKind(ctx, kind, deps); err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 套完后焊道引用仍须落在已声明依赖里。
	if err := s.assertProjectProcessIDs(ctx, content, deps); err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 落草稿，带上已核过的工艺依赖。
	row, err := s.store.InsertAsset(ctx, Asset{
		Kind: KindProject, Name: name, Status: AssetDraft, Copyable: true, WeldKind: kind,
		Content: content, Digest: digest.Sum(content), CreatorID: admin.ID, Deps: deps,
	})
	// 这一步失败就停，避免留下半截。
	if err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 新建成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	// 去掉正文再返回。
	return stripContent(row), nil
}

// fillPlatformProjectDeps 把参数里的工艺补进 deps，钉当前可用修订。
func (s *kernel) fillPlatformProjectDeps(ctx context.Context, content []byte, deps []AssetDep) ([]AssetDep, error) {
	// 按工程规则取名或核对。
	items, err := s.projectItemSchemas(ctx)
	// 不合法就拒绝，失败就不能继续。
	if err != nil {
		return nil, err
	}
	// 按当前模版收集引用；路径键直接拒绝。
	ids, err := contenttpl.CollectProcessIDsFromItems(items, content)
	// 出现路径键就直接拒绝。
	if err != nil {
		// 正文里出现路径键则拒绝，必须改用身份。
		if errors.Is(err, contenttpl.ErrProcessPath) {
			return nil, domain.ErrForbidden
		}
		return nil, domain.ErrAssetDependency
	}
	// 按身份钉住工艺，缺了或不可用就失败。
	return mergeProjectDeps(deps, ids, func(id uuid.UUID) (AssetDep, error) {
		// 钉住当前可用的工艺。
		return s.pinPlatformProcess(ctx, id)
	})
}

// pinPlatformProcess 平台级工程钉当前可用平台级工艺。
func (s *kernel) pinPlatformProcess(ctx context.Context, id uuid.UUID) (AssetDep, error) {
	// 装入并核对摘要。
	p, err := s.loadChecked(ctx, id)
	// 不符就不能把这条拿去用。
	if err != nil {
		// 没有这条就按不存在处理，不当成别的故障。
		if errors.Is(err, domain.ErrNotFound) {
			return AssetDep{}, domain.ErrAssetDependency
		}
		return AssetDep{}, err
	}
	// 不是工艺就拒绝，这项只对工艺开放。
	if p.Kind != KindProcess || p.Level != AssetLevelPlatform {
		return AssetDep{}, domain.ErrAssetDependency
	}
	// 不是可用就拒绝，草稿和停用不能当发布。
	if p.Status != AssetAvailable {
		return AssetDep{}, domain.ErrAssetNotAvailable
	}
	return AssetDep{ID: p.ID, Revision: p.Revision, Digest: p.Digest}, nil
}

// resolvePlatformProjectDeps 改依赖时按身份重钉当前可用修订。
func (s *kernel) resolvePlatformProjectDeps(ctx context.Context, deps []AssetDep) ([]AssetDep, error) {
	// 按数量先准备容器。
	out := make([]AssetDep, 0, len(deps))
	// 用来挡住同一键被写两次。
	seen := map[string]struct{}{}
	// 逐条核对依赖，缺了或类型不同就拒绝。
	for _, d := range deps {
		// 收成文本给审计或指令用。
		key := d.ID.String()
		// 同一键已经见过则拒绝，防止写两遍。
		if _, ok := seen[key]; ok {
			continue
		}
		// 记下已见过的键，防止同一粒写两次。
		seen[key] = struct{}{}
		// 钉住当前可用的工艺。
		pin, err := s.pinPlatformProcess(ctx, d.ID)
		// 草稿和厂级不能当依赖。
		if err != nil {
			return nil, err
		}
		// 把这一项接进结果。
		out = append(out, pin)
	}
	return out, nil
}

// mergeProjectDeps 保留已声明依赖，再按参数引用补缺。
func mergeProjectDeps(existing []AssetDep, ids []string, lookup func(uuid.UUID) (AssetDep, error)) ([]AssetDep, error) {
	// 按数量先准备容器。
	have := make(map[string]struct{}, len(existing)+len(ids))
	// 按数量先准备容器。
	out := make([]AssetDep, 0, len(existing)+len(ids))
	// 逐条看已有项，避免重复处理同一条。
	for _, d := range existing {
		// 收成文本给审计或指令用。
		key := d.ID.String()
		// 已经有这条引用就跳过，避免重复去钉。
		if _, ok := have[key]; ok {
			continue
		}
		// 标记正文里已经有的引用，缺的再补。
		have[key] = struct{}{}
		// 把这一项接进结果。
		out = append(out, d)
	}
	// 逐个身份处理，缺了就整次失败。
	for _, raw := range ids {
		// 已经有这条引用就跳过，避免重复去钉。
		if _, ok := have[raw]; ok {
			continue
		}
		// 解析这段文本，失败就不能继续。
		id, err := uuid.Parse(raw)
		// 格式不对就拒绝，不接收坏数据。
		if err != nil {
			return nil, domain.ErrAssetDependency
		}
		// 按路径找回身份。
		d, err := lookup(id)
		// 对不上就当没选，不能猜一条。
		if err != nil {
			return nil, err
		}
		// 标记原始引用已见，避免重复钉同一条。
		have[raw] = struct{}{}
		// 把这一项接进结果。
		out = append(out, d)
	}
	return out, nil
}

// assertPlatformProcessDeps 依赖必须是平台级可用工艺，修订和摘要都要对上。
func (s *kernel) assertPlatformProcessDeps(ctx context.Context, deps []AssetDep) error {
	// 逐条核对依赖，缺了或类型不同就拒绝。
	for _, d := range deps {
		// 装入并核对摘要。
		p, err := s.loadChecked(ctx, d.ID)
		// 不符就不能把这条拿去用。
		if err != nil {
			// 没有这条就按不存在处理，不当成别的故障。
			if errors.Is(err, domain.ErrNotFound) {
				return domain.ErrAssetDependency
			}
			return err
		}
		// 不是工艺就拒绝，这项只对工艺开放。
		if p.Kind != KindProcess || p.Level != AssetLevelPlatform || p.Revision != d.Revision || !bytes.Equal(p.Digest, d.Digest) {
			return domain.ErrAssetDependency
		}
		// 停用或草稿工艺不能当依赖。
		if p.Status != AssetAvailable {
			return domain.ErrAssetNotAvailable
		}
	}
	return nil
}

// assertProjectProcessIDs 按当前工程模版收集引用，非空的必须是本行 deps 的身份。
func (s *kernel) assertProjectProcessIDs(ctx context.Context, content []byte, deps []AssetDep) error {
	// 按工程规则取名或核对。
	items, err := s.projectItemSchemas(ctx)
	// 不合法就拒绝，失败就不能继续。
	if err != nil {
		return err
	}
	// 按当前模版收集引用；路径键直接拒绝。
	ids, err := contenttpl.CollectProcessIDsFromItems(items, content)
	// 出现路径键就直接拒绝。
	if err != nil {
		// 正文里出现路径键则拒绝，必须改用身份。
		if errors.Is(err, contenttpl.ErrProcessPath) {
			return domain.ErrForbidden
		}
		return domain.ErrAssetDependency
	}
	// 按数量先准备容器。
	allowed := make(map[string]struct{}, len(deps))
	// 逐条核对依赖，缺了或类型不同就拒绝。
	for _, d := range deps {
		// 收成文本给审计或指令用。
		allowed[d.ID.String()] = struct{}{}
	}
	// 逐个身份处理，缺了就整次失败。
	for _, id := range ids {
		// 对不上就跳过或拒绝，避免用错那一条。
		if _, ok := allowed[id]; !ok {
			return domain.ErrAssetDependency
		}
	}
	return nil
}

// CreateFactoryProcess 拒绝 WAN 代建厂级工艺。
func (s *Assets) CreateFactoryProcess(ctx context.Context, token string, factoryID uuid.UUID, name string, _ []byte) error {
	// 拒绝代管厂内人员、组织和角色。
	return s.denyFactoryManage(ctx, token, factoryID, "create_factory_asset", name)
}

// GetFactoryAsset 从 WAN 查厂级/个人级原件，一律拒绝。
func (s *Assets) GetFactoryAsset(ctx context.Context, token string, factoryID, assetID uuid.UUID) error {
	// 拒绝代管厂内人员、组织和角色。
	return s.denyFactoryManage(ctx, token, factoryID, "get_factory_asset", assetID.String())
}

// ReadFactoryAssetContent 从 WAN 读厂内正文，一律拒绝。
func (s *Assets) ReadFactoryAssetContent(ctx context.Context, token string, factoryID, assetID uuid.UUID) error {
	// 拒绝代管厂内人员、组织和角色。
	return s.denyFactoryManage(ctx, token, factoryID, "read_factory_asset", assetID.String())
}

// UpdateFactoryAsset 拒绝从 WAN 改厂库原件。
func (s *Assets) UpdateFactoryAsset(ctx context.Context, token string, factoryID, assetID uuid.UUID) error {
	// 拒绝代管厂内人员、组织和角色。
	return s.denyFactoryManage(ctx, token, factoryID, "update_factory_asset", assetID.String())
}

// ListPlatformAssets 列出平台级元数据；kind 空则两种都回，不含正文。
func (s *Assets) ListPlatformAssets(ctx context.Context, token, kind string) ([]Asset, error) {
	// 只有 WAN 管理员能列平台级。
	if _, err := s.RequireAdmin(ctx, token); err != nil {
		return nil, err
	}
	// 按是不是工程决定要不要核对焊道和依赖。
	if kind != "" && kind != KindProcess && kind != KindProject {
		return nil, domain.ErrNotFound
	}
	// 只取元数据，不含正文。
	rows, err := s.store.ListAssets(ctx)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return nil, err
	}
	// 用来源厂名装饰列表。
	facs, err := s.store.ListFactories(ctx)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return nil, err
	}
	// 按数量先准备容器。
	names := make(map[uuid.UUID]string, len(facs))
	// 逐厂处理，某一厂失败不改其他厂。
	for _, f := range facs {
		// 先记名录显示名，列表上用来替换身份。
		names[f.ID] = f.Name
	}
	// 先留空列表，再按种类滤掉正文。
	out := []Asset{}
	// 逐行整理，坏的一行就整批拒绝。
	for _, a := range rows {
		// 空和有值走不同路，避免把空白写进名录。
		if kind != "" && a.Kind != kind {
			continue
		}
		// 把这一项接进结果。
		out = append(out, decorateSourceFactory(stripContent(a), names))
	}
	return out, nil
}

// decorateSourceFactory 给列表写来源厂显示名，不把厂身份交给前端。
func decorateSourceFactory(a Asset, names map[uuid.UUID]string) Asset {
	// 还没有准备好就停，避免空着往下用。
	if a.SourceFactoryID == nil {
		// 没有来源厂，标成在云端新建的。
		a.SourceFactoryName = "本端新建"
		return a
	}
	// 空和有值走不同路，避免把空白写进名录。
	if n := names[*a.SourceFactoryID]; n != "" {
		// 写上来源厂的显示名，不写工厂身份。
		a.SourceFactoryName = n
		return a
	}
	// 名录里对不上，避免把内部号露出去。
	a.SourceFactoryName = "未知工厂"
	return a
}

// mutatePlatform 按期望修订改平台级；停用保护由补丁判定。
func (s *kernel) mutatePlatform(ctx context.Context, token string, assetID uuid.UUID, expected int64, action string, patch func(Asset) (store.AssetWrite, error)) (Asset, error) {
	// 只有 WAN 管理员能改平台级。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 被拒绝时留审计，不写口令或正文。
		_ = s.audit(ctx, nil, nil, nil, action, assetTarget(assetID, expected), audit.Deny)
		return Asset{}, err
	}
	// 装入并核对摘要。
	cur, err := s.loadChecked(ctx, assetID)
	// 不符就不能把这条拿去用。
	if err != nil {
		// 被拒绝时留审计，不写口令或正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, action, assetTarget(assetID, expected), audit.Deny)
		return Asset{}, err
	}
	// 做完这一步再继续。
	w, err := patch(cur)
	// 这一步失败就停，避免留下半截。
	if err != nil {
		// 被拒绝时留审计，不写口令或正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, action, assetTarget(assetID, cur.Revision), audit.Deny)
		return Asset{}, err
	}
	// 按期望修订写入，冲突则拒绝。
	row, err := s.store.UpdateAsset(ctx, assetID, expected, w)
	// 更新失败就停，避免写成半新半旧。
	if err != nil {
		// 被拒绝时留审计，不写口令或正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, action, assetTarget(assetID, expected), audit.Deny)
		return Asset{}, err
	}
	// 改写成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, nil, action, assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	// 去掉正文再返回。
	out := stripContent(row)
	// 通知厂端这条资产有新修订。
	s.notifyAsset(ctx, out)
	return out, nil
}
