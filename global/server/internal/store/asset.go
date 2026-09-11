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

const (
	KindProcess = "process" // 可复用工艺
	KindProject = "project" // 一次作业工程

	AssetLevelPlatform = "platform" // 平台级，只在 WAN

	AssetDraft     = "draft"     // 草稿
	AssetAvailable = "available" // 可用
	AssetDisabled  = "disabled"  // 停用
)

// AssetDep 是工程钉死的一条工艺依赖。
type AssetDep struct {
	ID       uuid.UUID `json:"id"`       // 被依赖工艺稳定身份
	Revision int64     `json:"revision"` // 钉死的工艺修订
	Digest   []byte    `json:"digest"`   // 当时该修订的 SHA-256 摘要
}

// AssetSnapshot 是厂级升平台用的内存快照，不测协议。
type AssetSnapshot struct {
	SourceID        uuid.UUID  `json:"sourceId"`        // 源厂级身份
	SourceRevision  int64      `json:"sourceRevision"`  // 源修订
	SourceFactoryID uuid.UUID  `json:"sourceFactoryId"` // 源厂
	Kind            string     `json:"kind"`            // process / project
	Name            string     `json:"name"`            // 显示名
	Content         []byte     `json:"content"`         // 正文
	Digest          []byte     `json:"digest"`          // 摘要
	Copyable        bool       `json:"copyable"`        // 源是否可复制
	Status          string     `json:"status"`          // 源状态
	Deps            []AssetDep `json:"deps"`            // 源依赖
}

// Asset 是一条平台级工艺或工程的当前行。
type Asset struct {
	ID              uuid.UUID  `json:"id"`              // 稳定身份
	Kind            string     `json:"kind"`            // process / project
	Level           string     `json:"level"`           // 固定 platform
	Name            string     `json:"name"`            // 显示名
	Status          string     `json:"status"`          // draft / available / disabled
	Copyable        bool       `json:"copyable"`        // 平台级必须为否
	Revision        int64      `json:"revision"`        // 当前修订
	Content         []byte     `json:"-"`               // 正文；不进列表/元数据
	Digest          []byte     `json:"digest"`          // SHA-256 32 字节
	CreatorID       uuid.UUID  `json:"creatorId"`       // WAN 管理员
	SourceID        *uuid.UUID `json:"sourceId"`        // 升档源厂级身份
	SourceRevision  *int64     `json:"sourceRevision"`  // 升档源修订
	SourceFactoryID *uuid.UUID `json:"sourceFactoryId"` // 升档源厂
	Deps            []AssetDep `json:"deps"`            // 工艺必须空
	CreatedAt       time.Time  `json:"createdAt"`       // 创建时间
	UpdatedAt       time.Time  `json:"updatedAt"`       // 最近升高修订的时间
}

// AssetWrite 是一次改名/改内容/改状态/改依赖的写入；可复制在 WAN 保持为否。
type AssetWrite struct {
	Name    string     // 显示名
	Content []byte     // 正文
	Digest  []byte     // 与正文对应的摘要
	Status  string     // 状态
	Deps    []AssetDep // 工程依赖；工艺必须空
}

type assetRow struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey"`   // 稳定身份
	Kind            string     `gorm:"not null"`               // process / project
	Level           string     `gorm:"not null"`               // 固定 platform
	Name            string     `gorm:"not null"`               // 显示名
	Status          string     `gorm:"not null"`               // draft / available / disabled
	Copyable        bool       `gorm:"not null"`               // 必须为否
	Revision        int64      `gorm:"not null"`               // 当前修订
	Content         []byte     `gorm:"type:bytea;not null"`    // 正文
	Digest          []byte     `gorm:"type:bytea;not null"`    // SHA-256
	CreatorID       uuid.UUID  `gorm:"type:uuid;not null"`     // WAN 管理员
	SourceID        *uuid.UUID `gorm:"type:uuid"`              // 升档源厂级身份
	SourceRevision  *int64     `gorm:"column:source_revision"` // 升档源修订
	SourceFactoryID *uuid.UUID `gorm:"type:uuid"`              // 升档源厂
	Deps            []byte     `gorm:"type:jsonb;not null"`    // 依赖 JSON
	CreatedAt       time.Time  `gorm:"not null"`               // 创建时间
	UpdatedAt       time.Time  `gorm:"not null"`               // 最近升高修订的时间
}

func (assetRow) TableName() string { return "assets" }

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

// ListAssets 列出平台级当前行，不含正文。
func (s *Store) ListAssets(ctx context.Context) ([]Asset, error) {
	var rows []assetRow
	if err := s.db.WithContext(ctx).Omit("Content").Order("updated_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Asset, 0, len(rows))
	for _, row := range rows {
		out = append(out, assetFromRow(row))
	}
	return out, nil
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
