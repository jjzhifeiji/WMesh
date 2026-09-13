package service

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/store"
)

func assetTarget(id uuid.UUID, rev int64) string {
	return id.String() + " rev=" + strconv.FormatInt(rev, 10)
}

func stripContent(a Asset) Asset {
	a.Content = nil
	return a
}

func (s *kernel) loadChecked(ctx context.Context, id uuid.UUID) (Asset, error) {
	a, err := s.store.AssetByID(ctx, id)
	if err != nil {
		return Asset{}, err
	}
	if !digest.Match(a.Content, a.Digest) {
		return Asset{}, domain.ErrIntegrity
	}
	return a, nil
}

// CreatePlatformProcess 由 WAN 管理员制作平台级工艺，默认可复制为否，状态草稿。
func (s *Assets) CreatePlatformProcess(ctx context.Context, token, name string, content []byte) (Asset, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	content, err = s.normalizeContent(ctx, KindProcess, content)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	row, err := s.store.InsertAsset(ctx, Asset{
		Kind: KindProcess, Name: name, Status: AssetDraft,
		Content: content, Digest: digest.Sum(content), CreatorID: admin.ID,
	})
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	return stripContent(row), nil
}

// CopyPlatformProcess 可复制平台级工艺另存为新草稿，原件正文原样拷贝，不套模版。
func (s *Assets) CopyPlatformProcess(ctx context.Context, token string, assetID uuid.UUID, name string) (Asset, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, domain.ErrInvalidName
	}
	src, err := s.loadChecked(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	if src.Kind != KindProcess {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrForbidden
	}
	if src.Status == AssetDisabled {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrAssetNotAvailable
	}
	if !src.Copyable {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(src.ID, src.Revision), audit.Deny)
		return Asset{}, domain.ErrAssetNotCopyable
	}
	row, err := s.store.InsertAsset(ctx, Asset{
		Kind: KindProcess, Name: name, Status: AssetDraft,
		Content: src.Content, Digest: src.Digest, CreatorID: admin.ID,
	})
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	return stripContent(row), nil
}

// RenamePlatformAsset 改显示名，身份不变。
func (s *Assets) RenamePlatformAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64, name string) (Asset, error) {
	return s.mutatePlatform(ctx, token, assetID, expected, "rename_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: cur.Status, Deps: cur.Deps}, nil
	})
}

// PublishPlatformAsset 草稿改为可用。
func (s *Assets) PublishPlatformAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) (Asset, error) {
	return s.mutatePlatform(ctx, token, assetID, expected, "publish_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status != AssetDraft {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: AssetAvailable, Deps: cur.Deps}, nil
	})
}

// UpdatePlatformAssetContent 改正文并重算摘要；停用后拒绝。已有正文不套模版。
func (s *Assets) UpdatePlatformAssetContent(ctx context.Context, token string, assetID uuid.UUID, expected int64, content []byte) (Asset, error) {
	return s.mutatePlatform(ctx, token, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Content: content, Digest: digest.Sum(content), Copyable: cur.Copyable, Status: cur.Status, Deps: cur.Deps}, nil
	})
}

// DisablePlatformAsset 可用改为停用；停用期间不得改正文。
func (s *Assets) DisablePlatformAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) (Asset, error) {
	return s.mutatePlatform(ctx, token, assetID, expected, "disable_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status != AssetAvailable {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: AssetDisabled, Deps: cur.Deps}, nil
	})
}

// ReenablePlatformAsset 停用改回可用。
func (s *Assets) ReenablePlatformAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) (Asset, error) {
	return s.mutatePlatform(ctx, token, assetID, expected, "publish_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status != AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: cur.Copyable, Status: AssetAvailable, Deps: cur.Deps}, nil
	})
}

// DeletePlatformAsset 未被工程依赖则可删；仍被引用则拒绝。
func (s *Assets) DeletePlatformAsset(ctx context.Context, token string, assetID uuid.UUID) error {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "delete_asset", assetID.String(), audit.Deny)
		return err
	}
	cur, err := s.loadChecked(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "delete_asset", assetID.String(), audit.Deny)
		return err
	}
	used, err := s.store.AssetIsReferenced(ctx, assetID)
	if err != nil {
		return err
	}
	if used {
		_ = s.audit(ctx, &admin.ID, nil, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return domain.ErrReferenced
	}
	if err := s.store.DeleteAssetAndRetract(ctx, assetID); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Deny)
		return err
	}
	return s.audit(ctx, &admin.ID, nil, nil, "delete_asset", assetTarget(assetID, cur.Revision), audit.Allow)
}

// SetPlatformCopyable 未停用即可改可复制，发布后也能改回是或否。新建默认否。
func (s *Assets) SetPlatformCopyable(ctx context.Context, token string, assetID uuid.UUID, expected int64, copyable bool) (Asset, error) {
	return s.mutatePlatform(ctx, token, assetID, expected, "update_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Copyable: copyable, Status: cur.Status, Deps: cur.Deps}, nil
	})
}

// GetPlatformAsset 读平台级元数据；摘要不符则拒绝。
func (s *Assets) GetPlatformAsset(ctx context.Context, token string, assetID uuid.UUID) (Asset, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "get_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	a, err := s.loadChecked(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "get_asset", assetID.String(), audit.Deny)
		return Asset{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "get_asset", assetTarget(a.ID, a.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	return stripContent(a), nil
}

// ReadPlatformAssetContent 读正文并核对摘要，审计不写正文。
func (s *Assets) ReadPlatformAssetContent(ctx context.Context, token string, assetID uuid.UUID) ([]byte, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return nil, err
	}
	a, err := s.loadChecked(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "read_asset_content", assetID.String(), audit.Deny)
		return nil, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "read_asset_content", assetTarget(a.ID, a.Revision), audit.Allow); err != nil {
		return nil, err
	}
	return a.Content, nil
}

// PromoteFromSnapshot 用厂级快照升平台级：第一次复制新身份为草稿；再升按正文摘要跳过或覆盖为草稿。原件不进 WAN。
func (s *Assets) PromoteFromSnapshot(ctx context.Context, token string, snap AssetSnapshot) (Asset, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &snap.SourceFactoryID, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, err
	}
	fid := snap.SourceFactoryID
	// 厂级不分草稿/停用都可升平台；不可复制仍拒绝。
	if !snap.Copyable {
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, domain.ErrAssetNotCopyable
	}
	// 过站解开后再验摘要；WAN 入库仍是明文。
	body, err := s.OpenSnapshotTransit(ctx, snap)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, err
	}
	if !digest.Match(body, snap.Digest) || len(snap.Digest) != 32 {
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, domain.ErrIntegrity
	}
	sum := snap.Digest
	deps, err := s.rewritePromoteDeps(ctx, snap.Kind, snap.Deps)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, err
	}
	rev := snap.SourceRevision
	existing, err := s.store.AssetBySourceID(ctx, snap.SourceID)
	if err == nil {
		if bytes.Equal(existing.Digest, sum) {
			if err := s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", assetTarget(existing.ID, existing.Revision)+" from "+assetTarget(snap.SourceID, snap.SourceRevision), audit.Allow); err != nil {
				return Asset{}, err
			}
			return stripContent(existing), nil
		}
		row, err := s.store.UpdateAsset(ctx, existing.ID, existing.Revision, store.AssetWrite{
			Name: snap.Name, Content: body, Digest: sum, Copyable: existing.Copyable, Status: AssetDraft, Deps: deps, SourceRevision: &rev,
		})
		if err != nil {
			_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
			return Asset{}, err
		}
		if err := s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", assetTarget(row.ID, row.Revision)+" from "+assetTarget(snap.SourceID, snap.SourceRevision), audit.Allow); err != nil {
			return Asset{}, err
		}
		return stripContent(row), nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, err
	}
	row, err := s.store.InsertAsset(ctx, Asset{
		Kind: snap.Kind, Name: snap.Name, Status: AssetDraft,
		Content: body, Digest: sum, CreatorID: admin.ID,
		SourceID: &snap.SourceID, SourceRevision: &rev, SourceFactoryID: &snap.SourceFactoryID,
		Deps: deps,
	})
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", assetTarget(row.ID, row.Revision)+" from "+assetTarget(snap.SourceID, snap.SourceRevision), audit.Allow); err != nil {
		return Asset{}, err
	}
	return stripContent(row), nil
}

func (s *Assets) rewritePromoteDeps(ctx context.Context, kind string, deps []AssetDep) ([]AssetDep, error) {
	if kind != KindProject {
		return deps, nil
	}
	out := make([]AssetDep, 0, len(deps))
	for _, d := range deps {
		plat, err := s.store.AssetBySourceID(ctx, d.ID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, domain.ErrAssetDependency
			}
			return nil, err
		}
		if !digest.Match(plat.Content, plat.Digest) {
			return nil, domain.ErrIntegrity
		}
		if plat.Kind != KindProcess || plat.Status != AssetAvailable {
			return nil, domain.ErrAssetDependency
		}
		out = append(out, AssetDep{ID: plat.ID, Revision: plat.Revision, Digest: plat.Digest})
	}
	return out, nil
}

// CreatePlatformProject 创建平台级工程；只允许依赖平台级可用工艺。
func (s *Assets) CreatePlatformProject(ctx context.Context, token, name string, content []byte, deps []AssetDep) (Asset, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	if err := s.assertPlatformProcessDeps(ctx, deps); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	content, err = s.normalizeContent(ctx, KindProject, content)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	row, err := s.store.InsertAsset(ctx, Asset{
		Kind: KindProject, Name: name, Status: AssetDraft,
		Content: content, Digest: digest.Sum(content), CreatorID: admin.ID, Deps: deps,
	})
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	return stripContent(row), nil
}

func (s *kernel) assertPlatformProcessDeps(ctx context.Context, deps []AssetDep) error {
	for _, d := range deps {
		p, err := s.loadChecked(ctx, d.ID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return domain.ErrAssetDependency
			}
			return err
		}
		if p.Kind != KindProcess || p.Level != AssetLevelPlatform || p.Revision != d.Revision || !bytes.Equal(p.Digest, d.Digest) {
			return domain.ErrAssetDependency
		}
		if p.Status != AssetAvailable {
			return domain.ErrAssetNotAvailable
		}
	}
	return nil
}

// CreateFactoryProcess 拒绝 WAN 代建厂级工艺。
func (s *Assets) CreateFactoryProcess(ctx context.Context, token string, factoryID uuid.UUID, name string, _ []byte) error {
	return s.denyFactoryManage(ctx, token, factoryID, "create_factory_asset", name)
}

// GetFactoryAsset 从 WAN 查厂级/个人级原件，一律拒绝。
func (s *Assets) GetFactoryAsset(ctx context.Context, token string, factoryID, assetID uuid.UUID) error {
	return s.denyFactoryManage(ctx, token, factoryID, "get_factory_asset", assetID.String())
}

// ReadFactoryAssetContent 从 WAN 读厂内正文，一律拒绝。
func (s *Assets) ReadFactoryAssetContent(ctx context.Context, token string, factoryID, assetID uuid.UUID) error {
	return s.denyFactoryManage(ctx, token, factoryID, "read_factory_asset", assetID.String())
}

// UpdateFactoryAsset 拒绝从 WAN 改厂库原件。
func (s *Assets) UpdateFactoryAsset(ctx context.Context, token string, factoryID, assetID uuid.UUID) error {
	return s.denyFactoryManage(ctx, token, factoryID, "update_factory_asset", assetID.String())
}

// ListPlatformAssets 列出平台级元数据；kind 空则两种都回，不含正文。
func (s *Assets) ListPlatformAssets(ctx context.Context, token, kind string) ([]Asset, error) {
	if _, err := s.RequireAdmin(ctx, token); err != nil {
		return nil, err
	}
	if kind != "" && kind != KindProcess && kind != KindProject {
		return nil, domain.ErrNotFound
	}
	rows, err := s.store.ListAssets(ctx)
	if err != nil {
		return nil, err
	}
	facs, err := s.store.ListFactories(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[uuid.UUID]string, len(facs))
	for _, f := range facs {
		names[f.ID] = f.Name
	}
	out := []Asset{}
	for _, a := range rows {
		if kind != "" && a.Kind != kind {
			continue
		}
		out = append(out, decorateSourceFactory(stripContent(a), names))
	}
	return out, nil
}

// decorateSourceFactory 给列表写来源厂显示名，不把厂身份交给前端。
func decorateSourceFactory(a Asset, names map[uuid.UUID]string) Asset {
	if a.SourceFactoryID == nil {
		a.SourceFactoryName = "本端新建"
		return a
	}
	if n := names[*a.SourceFactoryID]; n != "" {
		a.SourceFactoryName = n
		return a
	}
	a.SourceFactoryName = "未知工厂"
	return a
}

func (s *kernel) mutatePlatform(ctx context.Context, token string, assetID uuid.UUID, expected int64, action string, patch func(Asset) (store.AssetWrite, error)) (Asset, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, action, assetTarget(assetID, expected), audit.Deny)
		return Asset{}, err
	}
	cur, err := s.loadChecked(ctx, assetID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, action, assetTarget(assetID, expected), audit.Deny)
		return Asset{}, err
	}
	w, err := patch(cur)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, action, assetTarget(assetID, cur.Revision), audit.Deny)
		return Asset{}, err
	}
	row, err := s.store.UpdateAsset(ctx, assetID, expected, w)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, action, assetTarget(assetID, expected), audit.Deny)
		return Asset{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, action, assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	return stripContent(row), nil
}
