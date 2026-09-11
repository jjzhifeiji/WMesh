package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
)

// InsertGovernedAsset 写入本厂一条工艺或工程，修订从 1 起；摘要原样存。
func (s *Store) InsertGovernedAsset(ctx context.Context, in Asset) (Asset, error) {
	if err := s.assertPersonExists(ctx, in.CreatorID); err != nil {
		return Asset{}, err
	}
	if in.OrgUnitID != nil {
		if _, err := s.getUnit(ctx, *in.OrgUnitID); err != nil {
			return Asset{}, err
		}
	}
	deps, err := marshalAssetDeps(in.Kind, in.Deps)
	if err != nil {
		return Asset{}, err
	}
	path, err := marshalPath(in.OrgPath)
	if err != nil {
		return Asset{}, err
	}
	if err := assertAssetDigest(in.Digest); err != nil {
		return Asset{}, err
	}
	now := time.Now().UTC()
	row := governedAssetRow{
		ID:             valueOrNew(in.ID),
		Kind:           in.Kind,
		Level:          in.Level,
		Name:           in.Name,
		Status:         in.Status,
		Copyable:       in.Copyable,
		Revision:       1,
		Content:        in.Content,
		Digest:         in.Digest,
		CreatorID:      in.CreatorID,
		FactoryID:      s.factoryID,
		OrgUnitID:      in.OrgUnitID,
		OrgPath:        path,
		SourceID:       in.SourceID,
		SourceRevision: in.SourceRevision,
		Deps:           deps,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if row.Content == nil {
		row.Content = []byte{}
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return Asset{}, mapAssetWriteErr(err)
	}
	return assetFromGoverned(row), nil
}

// UpdateGovernedAsset 按期望修订改当前行；对不上则原件不变。
func (s *Store) UpdateGovernedAsset(ctx context.Context, assetID uuid.UUID, expected int64, w AssetWrite) (Asset, error) {
	if err := assertAssetDigest(w.Digest); err != nil {
		return Asset{}, err
	}
	now := time.Now().UTC()
	var out Asset
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row governedAssetRow
		if err := tx.First(&row, "id = ?", assetID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		depsJSON, err := marshalAssetDeps(row.Kind, w.Deps)
		if err != nil {
			return err
		}
		res := tx.Model(&governedAssetRow{}).Where("id = ? AND revision = ?", assetID, expected).Updates(map[string]any{
			"name":       w.Name,
			"content":    nonempty(w.Content),
			"digest":     w.Digest,
			"copyable":   w.Copyable,
			"status":     w.Status,
			"deps":       depsJSON,
			"revision":   expected + 1,
			"updated_at": now,
		})
		if res.Error != nil {
			return mapAssetWriteErr(res.Error)
		}
		if res.RowsAffected == 0 {
			return domain.ErrRevisionConflict
		}
		if err := tx.First(&row, "id = ?", assetID).Error; err != nil {
			return err
		}
		out = assetFromGoverned(row)
		return nil
	})
	return out, err
}

// GovernedAssetByID 读本厂一条工艺/工程当前行，含正文。
func (s *Store) GovernedAssetByID(ctx context.Context, assetID uuid.UUID) (Asset, error) {
	var row governedAssetRow
	if err := s.db.WithContext(ctx).First(&row, "id = ?", assetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Asset{}, domain.ErrNotFound
		}
		return Asset{}, err
	}
	return assetFromGoverned(row), nil
}

// ExportAssetSnapshot 导出升平台用快照；不改原件。
func (s *Store) ExportAssetSnapshot(ctx context.Context, assetID uuid.UUID) (AssetSnapshot, error) {
	a, err := s.GovernedAssetByID(ctx, assetID)
	if err != nil {
		return AssetSnapshot{}, err
	}
	return AssetSnapshot{
		SourceID:        a.ID,
		SourceRevision:  a.Revision,
		SourceFactoryID: s.factoryID,
		Kind:            a.Kind,
		Name:            a.Name,
		Content:         a.Content,
		Digest:          a.Digest,
		Copyable:        a.Copyable,
		Status:          a.Status,
		Deps:            a.Deps,
	}, nil
}

func marshalAssetDeps(kind string, deps []AssetDep) ([]byte, error) {
	if deps == nil {
		deps = []AssetDep{}
	}
	if kind == KindProcess && len(deps) > 0 {
		return nil, domain.ErrAssetDependency
	}
	return json.Marshal(deps)
}

func unmarshalAssetDeps(raw []byte) []AssetDep {
	if len(raw) == 0 {
		return []AssetDep{}
	}
	var deps []AssetDep
	if err := json.Unmarshal(raw, &deps); err != nil || deps == nil {
		return []AssetDep{}
	}
	return deps
}

func assertAssetDigest(d []byte) error {
	if len(d) != 32 {
		return domain.ErrIntegrity
	}
	return nil
}

func mapAssetWriteErr(err error) error {
	if domain.IsCheckViolation(err) {
		return domain.ErrIntegrity
	}
	if domain.IsForeignKeyViolation(err) {
		return domain.ErrNotFound
	}
	return err
}

func valueOrNew(given uuid.UUID) uuid.UUID {
	if given == uuid.Nil {
		return id.New()
	}
	return given
}

func nonempty(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}

func assetFromGoverned(row governedAssetRow) Asset {
	return Asset{
		ID:             row.ID,
		Kind:           row.Kind,
		Level:          row.Level,
		Name:           row.Name,
		Status:         row.Status,
		Copyable:       row.Copyable,
		Revision:       row.Revision,
		Content:        row.Content,
		Digest:         row.Digest,
		CreatorID:      row.CreatorID,
		FactoryID:      row.FactoryID,
		OrgUnitID:      row.OrgUnitID,
		OrgPath:        unmarshalPath(row.OrgPath),
		SourceID:       row.SourceID,
		SourceRevision: row.SourceRevision,
		Deps:           unmarshalAssetDeps(row.Deps),
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

// TamperAssetContent 只改正文不改摘要，供完整性夹具使用。
func (s *Store) TamperAssetContent(ctx context.Context, assetID uuid.UUID, content []byte) error {
	res := s.db.WithContext(ctx).Model(&governedAssetRow{}).Where("id = ?", assetID).Update("content", nonempty(content))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
