package service

import (
	"bytes"
	"context"
	"errors"
	"strconv"

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

func (s *Service) loadChecked(ctx context.Context, id uuid.UUID) (Asset, error) {
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
func (s *Service) CreatePlatformProcess(ctx context.Context, token, name string, content []byte) (Asset, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "create_asset", name, audit.Deny)
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

// RenamePlatformAsset 改显示名，身份不变。
func (s *Service) RenamePlatformAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64, name string) (Asset, error) {
	return s.mutatePlatform(ctx, token, assetID, expected, "rename_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: name, Content: cur.Content, Digest: cur.Digest, Status: cur.Status, Deps: cur.Deps}, nil
	})
}

// PublishPlatformAsset 草稿改为可用。
func (s *Service) PublishPlatformAsset(ctx context.Context, token string, assetID uuid.UUID, expected int64) (Asset, error) {
	return s.mutatePlatform(ctx, token, assetID, expected, "publish_asset", func(cur Asset) (store.AssetWrite, error) {
		if cur.Status != AssetDraft {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		return store.AssetWrite{Name: cur.Name, Content: cur.Content, Digest: cur.Digest, Status: AssetAvailable, Deps: cur.Deps}, nil
	})
}

// SetPlatformCopyable 平台级不得改为可复制。
func (s *Service) SetPlatformCopyable(ctx context.Context, token string, assetID uuid.UUID, expected int64, copyable bool) error {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return err
	}
	if copyable {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_asset", assetTarget(assetID, expected), audit.Deny)
		return domain.ErrAssetNotCopyable
	}
	return nil
}

// GetPlatformAsset 读平台级元数据；摘要不符则拒绝。
func (s *Service) GetPlatformAsset(ctx context.Context, token string, assetID uuid.UUID) (Asset, error) {
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
func (s *Service) ReadPlatformAssetContent(ctx context.Context, token string, assetID uuid.UUID) ([]byte, error) {
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

// PromoteFromSnapshot 用厂级快照复制出新平台级；原件不进 WAN。
func (s *Service) PromoteFromSnapshot(ctx context.Context, token string, snap AssetSnapshot) (Asset, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &snap.SourceFactoryID, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, err
	}
	fid := snap.SourceFactoryID
	if !digest.Match(snap.Content, snap.Digest) || len(snap.Digest) != 32 {
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, domain.ErrIntegrity
	}
	if snap.Status != AssetAvailable {
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, domain.ErrAssetNotAvailable
	}
	if !snap.Copyable {
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, domain.ErrAssetNotCopyable
	}
	deps, err := s.rewritePromoteDeps(ctx, snap.Kind, snap.Deps)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &fid, "promote_asset", snap.SourceID.String(), audit.Deny)
		return Asset{}, err
	}
	rev := snap.SourceRevision
	row, err := s.store.InsertAsset(ctx, Asset{
		Kind: snap.Kind, Name: snap.Name, Status: AssetAvailable,
		Content: snap.Content, Digest: snap.Digest, CreatorID: admin.ID,
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

func (s *Service) rewritePromoteDeps(ctx context.Context, kind string, deps []AssetDep) ([]AssetDep, error) {
	if kind != KindProject {
		return deps, nil
	}
	out := make([]AssetDep, 0, len(deps))
	for _, d := range deps {
		plat, err := s.store.AssetBySource(ctx, d.ID, d.Revision)
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
func (s *Service) CreatePlatformProject(ctx context.Context, token, name string, content []byte, deps []AssetDep) (Asset, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	if err := s.assertPlatformProcessDeps(ctx, deps); err != nil {
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

func (s *Service) assertPlatformProcessDeps(ctx context.Context, deps []AssetDep) error {
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
func (s *Service) CreateFactoryProcess(ctx context.Context, token string, factoryID uuid.UUID, name string, _ []byte) error {
	return s.denyFactoryManage(ctx, token, factoryID, "create_factory_asset", name)
}

// GetFactoryAsset 从 WAN 查厂级/个人级原件，一律拒绝。
func (s *Service) GetFactoryAsset(ctx context.Context, token string, factoryID, assetID uuid.UUID) error {
	return s.denyFactoryManage(ctx, token, factoryID, "get_factory_asset", assetID.String())
}

// ReadFactoryAssetContent 从 WAN 读厂内正文，一律拒绝。
func (s *Service) ReadFactoryAssetContent(ctx context.Context, token string, factoryID, assetID uuid.UUID) error {
	return s.denyFactoryManage(ctx, token, factoryID, "read_factory_asset", assetID.String())
}

// UpdateFactoryAsset 拒绝从 WAN 改厂库原件。
func (s *Service) UpdateFactoryAsset(ctx context.Context, token string, factoryID, assetID uuid.UUID) error {
	return s.denyFactoryManage(ctx, token, factoryID, "update_factory_asset", assetID.String())
}

// ListPlatformAssets 列出平台级元数据；kind 空则两种都回，不含正文。
func (s *Service) ListPlatformAssets(ctx context.Context, token, kind string) ([]Asset, error) {
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
	out := []Asset{}
	for _, a := range rows {
		if kind != "" && a.Kind != kind {
			continue
		}
		out = append(out, stripContent(a))
	}
	return out, nil
}

func (s *Service) mutatePlatform(ctx context.Context, token string, assetID uuid.UUID, expected int64, action string, patch func(Asset) (store.AssetWrite, error)) (Asset, error) {
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
