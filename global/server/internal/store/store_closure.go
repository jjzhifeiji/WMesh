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

// UpsertFactoryGrant 授予或重新激活某平台级资产到某厂。
func (s *Store) UpsertFactoryGrant(ctx context.Context, assetID, factoryID uuid.UUID) (DistributionGrant, error) {
	now := time.Now().UTC()
	var existing distGrantRow
	err := s.db.WithContext(ctx).First(&existing, "asset_id = ? AND factory_id = ?", assetID, factoryID).Error
	if err == nil {
		existing.Active = true
		existing.UpdatedAt = now
		if err := s.db.WithContext(ctx).Save(&existing).Error; err != nil {
			return DistributionGrant{}, err
		}
		return grantFromRow(existing), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return DistributionGrant{}, err
	}
	row := distGrantRow{
		ID: id.New(), AssetID: assetID, FactoryID: factoryID,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsForeignKeyViolation(err) {
			return DistributionGrant{}, domain.ErrNotFound
		}
		return DistributionGrant{}, err
	}
	return grantFromRow(row), nil
}

// RevokeFactoryGrant 收回某资产对某厂的下发授权。
func (s *Store) RevokeFactoryGrant(ctx context.Context, assetID, factoryID uuid.UUID) error {
	res := s.db.WithContext(ctx).Model(&distGrantRow{}).Where("asset_id = ? AND factory_id = ?", assetID, factoryID).Updates(map[string]any{
		"active":     false,
		"updated_at": time.Now().UTC(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// FactoryGrant 读某资产对某厂的授权。
func (s *Store) FactoryGrant(ctx context.Context, assetID, factoryID uuid.UUID) (DistributionGrant, error) {
	var row distGrantRow
	if err := s.db.WithContext(ctx).First(&row, "asset_id = ? AND factory_id = ?", assetID, factoryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DistributionGrant{}, domain.ErrNotFound
		}
		return DistributionGrant{}, err
	}
	return grantFromRow(row), nil
}

// InsertDistributionRecord 写下发记录；同一资产修订对同一厂幂等。
func (s *Store) InsertDistributionRecord(ctx context.Context, rec DistributionRecord) (DistributionRecord, error) {
	if err := assertAssetDigest(rec.ClosureDigest); err != nil {
		return DistributionRecord{}, err
	}
	members, err := json.Marshal(rec.Members)
	if err != nil {
		return DistributionRecord{}, err
	}
	row := distRecordRow{
		ID: id.New(), AssetID: rec.AssetID, Revision: rec.Revision, FactoryID: rec.FactoryID,
		Kind: rec.Kind, ClosureDigest: rec.ClosureDigest, Members: members, CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return s.DistributionRecord(ctx, rec.AssetID, rec.Revision, rec.FactoryID)
		}
		if domain.IsForeignKeyViolation(err) {
			return DistributionRecord{}, domain.ErrNotFound
		}
		return DistributionRecord{}, err
	}
	return recordFromRow(row), nil
}

// DistributionRecord 读向某厂下发过的一份修订。
func (s *Store) DistributionRecord(ctx context.Context, assetID uuid.UUID, revision int64, factoryID uuid.UUID) (DistributionRecord, error) {
	var row distRecordRow
	if err := s.db.WithContext(ctx).First(&row, "asset_id = ? AND revision = ? AND factory_id = ?", assetID, revision, factoryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DistributionRecord{}, domain.ErrNotFound
		}
		return DistributionRecord{}, err
	}
	return recordFromRow(row), nil
}

// HasDistributionTo 是否曾向该厂下发过该资产任一修订。
func (s *Store) HasDistributionTo(ctx context.Context, assetID, factoryID uuid.UUID) (bool, error) {
	var n int64
	if err := s.db.WithContext(ctx).Model(&distRecordRow{}).Where("asset_id = ? AND factory_id = ?", assetID, factoryID).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func grantFromRow(row distGrantRow) DistributionGrant {
	return DistributionGrant{
		ID: row.ID, AssetID: row.AssetID, FactoryID: row.FactoryID, Active: row.Active,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func recordFromRow(row distRecordRow) DistributionRecord {
	return DistributionRecord{
		ID: row.ID, AssetID: row.AssetID, Revision: row.Revision, FactoryID: row.FactoryID,
		Kind: row.Kind, ClosureDigest: row.ClosureDigest, Members: unmarshalAssetDeps(row.Members),
		CreatedAt: row.CreatedAt,
	}
}
