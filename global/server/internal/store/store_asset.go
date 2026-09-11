package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
)

// InsertAsset 写入一条平台级工艺或工程；可复制强制为否，修订从 1 起。
func (s *Store) InsertAsset(ctx context.Context, in Asset) (Asset, error) {
	deps, err := marshalAssetDeps(in.Kind, in.Deps)
	if err != nil {
		return Asset{}, err
	}
	if err := assertAssetDigest(in.Digest); err != nil {
		return Asset{}, err
	}
	now := time.Now().UTC()
	row := assetRow{
		ID:              valueOrNew(in.ID),
		Kind:            in.Kind,
		Level:           AssetLevelPlatform,
		Name:            in.Name,
		Status:          in.Status,
		Copyable:        false,
		Revision:        1,
		Content:         nonempty(in.Content),
		Digest:          in.Digest,
		CreatorID:       in.CreatorID,
		SourceID:        in.SourceID,
		SourceRevision:  in.SourceRevision,
		SourceFactoryID: in.SourceFactoryID,
		Deps:            deps,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return Asset{}, mapAssetWriteErr(err)
	}
	return assetFromRow(row), nil
}

// UpdateAsset 按期望修订改平台级当前行；可复制保持为否。
func (s *Store) UpdateAsset(ctx context.Context, assetID uuid.UUID, expected int64, w AssetWrite) (Asset, error) {
	if err := assertAssetDigest(w.Digest); err != nil {
		return Asset{}, err
	}
	now := time.Now().UTC()
	var out Asset
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row assetRow
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
		res := tx.Model(&assetRow{}).Where("id = ? AND revision = ?", assetID, expected).Updates(map[string]any{
			"name":       w.Name,
			"content":    nonempty(w.Content),
			"digest":     w.Digest,
			"copyable":   false,
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
		out = assetFromRow(row)
		return nil
	})
	return out, err
}

// AssetByID 读平台级当前行，含正文。
func (s *Store) AssetByID(ctx context.Context, assetID uuid.UUID) (Asset, error) {
	var row assetRow
	if err := s.db.WithContext(ctx).First(&row, "id = ?", assetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Asset{}, domain.ErrNotFound
		}
		return Asset{}, err
	}
	return assetFromRow(row), nil
}

// AssetBySource 按升档源身份和源修订找平台级当前行。
func (s *Store) AssetBySource(ctx context.Context, sourceID uuid.UUID, sourceRevision int64) (Asset, error) {
	var row assetRow
	if err := s.db.WithContext(ctx).First(&row, "source_id = ? AND source_revision = ?", sourceID, sourceRevision).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Asset{}, domain.ErrNotFound
		}
		return Asset{}, err
	}
	return assetFromRow(row), nil
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

func assetFromRow(row assetRow) Asset {
	return Asset{
		ID:              row.ID,
		Kind:            row.Kind,
		Level:           row.Level,
		Name:            row.Name,
		Status:          row.Status,
		Copyable:        row.Copyable,
		Revision:        row.Revision,
		Content:         row.Content,
		Digest:          row.Digest,
		CreatorID:       row.CreatorID,
		SourceID:        row.SourceID,
		SourceRevision:  row.SourceRevision,
		SourceFactoryID: row.SourceFactoryID,
		Deps:            unmarshalAssetDeps(row.Deps),
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

// TamperAssetContent 只改正文不改摘要，供完整性夹具使用。
func (s *Store) TamperAssetContent(ctx context.Context, assetID uuid.UUID, content []byte) error {
	res := s.db.WithContext(ctx).Model(&assetRow{}).Where("id = ?", assetID).Update("content", nonempty(content))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
