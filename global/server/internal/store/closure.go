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

// ClosureMember 是闭包里的一条资产快照，含正文。
type ClosureMember struct {
	ID       uuid.UUID  `json:"id"`       // 稳定身份
	Kind     string     `json:"kind"`     // process / project
	Level    string     `json:"level"`    // 固定 platform
	Name     string     `json:"name"`     // 显示名
	Status   string     `json:"status"`   // 组包时状态
	Copyable bool       `json:"copyable"` // 与源相同
	Revision int64      `json:"revision"` // 钉死修订
	Content  []byte     `json:"content"`  // 正文
	Digest   []byte     `json:"digest"`   // 内容 SHA-256
	Deps     []AssetDep `json:"deps"`     // 工艺必须空
}

// ClosureSnapshot 是一份平台级工程或工艺的完整快照，不是新身份。
type ClosureSnapshot struct {
	Kind            string          `json:"kind"`            // process / project
	AssetID         uuid.UUID       `json:"assetId"`         // 根资产身份
	Revision        int64           `json:"revision"`        // 根修订
	Level           string          `json:"level"`           // 固定 platform
	Copyable        bool            `json:"copyable"`        // 与源相同
	Status          string          `json:"status"`          // 组包时状态
	TargetFactoryID *uuid.UUID      `json:"targetFactoryId"` // 目标工厂
	TargetClientID  *uuid.UUID      `json:"targetClientId"`  // WAN→厂为空
	Members         []ClosureMember `json:"members"`         // 根在前，其余按 deps 顺序
	Digest          []byte          `json:"digest"`          // 整包 SHA-256
}

// DistributionGrant 是某平台级资产可否下发到某厂。
type DistributionGrant struct {
	ID        uuid.UUID `json:"id"`        // 授权记录身份
	AssetID   uuid.UUID `json:"assetId"`   // 平台级资产
	FactoryID uuid.UUID `json:"factoryId"` // 目标工厂
	Active    bool      `json:"active"`    // 是否仍有效
	CreatedAt time.Time `json:"createdAt"` // 授权时间
	UpdatedAt time.Time `json:"updatedAt"` // 最近变更
}

// DistributionRecord 是向某厂下发过的一份修订，不含正文。
type DistributionRecord struct {
	ID            uuid.UUID  `json:"id"`            // 记录身份
	AssetID       uuid.UUID  `json:"assetId"`       // 根资产
	Revision      int64      `json:"revision"`      // 下发修订
	FactoryID     uuid.UUID  `json:"factoryId"`     // 目标工厂
	Kind          string     `json:"kind"`          // process / project
	ClosureDigest []byte     `json:"closureDigest"` // 整包摘要
	Members       []AssetDep `json:"members"`       // 成员身份+修订+摘要
	CreatedAt     time.Time  `json:"createdAt"`     // 首次下发时间
}

type distGrantRow struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"` // 授权身份
	AssetID   uuid.UUID `gorm:"type:uuid;not null"`   // 平台级资产
	FactoryID uuid.UUID `gorm:"type:uuid;not null"`   // 工厂
	Active    bool      `gorm:"not null"`             // 是否有效
	CreatedAt time.Time `gorm:"not null"`             // 授权时间
	UpdatedAt time.Time `gorm:"not null"`             // 最近变更
}

func (distGrantRow) TableName() string { return "distribution_grants" }

type distRecordRow struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey"` // 记录身份
	AssetID       uuid.UUID `gorm:"type:uuid;not null"`   // 根资产
	Revision      int64     `gorm:"not null"`             // 修订
	FactoryID     uuid.UUID `gorm:"type:uuid;not null"`   // 工厂
	Kind          string    `gorm:"not null"`             // process / project
	ClosureDigest []byte    `gorm:"type:bytea;not null"`  // 整包摘要
	Members       []byte    `gorm:"type:jsonb;not null"`  // 成员 JSON
	CreatedAt     time.Time `gorm:"not null"`             // 首次下发
}

func (distRecordRow) TableName() string { return "distribution_records" }

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

type retractionRow struct {
	AssetID   uuid.UUID `gorm:"type:uuid;primaryKey"` // 已删除身份
	CreatedAt time.Time `gorm:"not null"`             // 删除时间
}

func (retractionRow) TableName() string { return "asset_retractions" }

// ListRetractions 列出须补送给厂的已删平台级身份。
func (s *Store) ListRetractions(ctx context.Context) ([]uuid.UUID, error) {
	var rows []retractionRow
	if err := s.db.WithContext(ctx).Order("created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.AssetID)
	}
	return out, nil
}
