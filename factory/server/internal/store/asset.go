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

const (
	KindProcess = "process" // 可复用工艺
	KindProject = "project" // 一次作业工程

	AssetLevelFactory  = "factory"  // 本厂厂级
	AssetLevelPersonal = "personal" // 本厂个人级
	AssetLevelPlatform = "platform" // 已下发到本厂的平台级副本

	AssetDraft     = "draft"     // 草稿：不可依赖、不可升档
	AssetAvailable = "available" // 可用
	AssetDisabled  = "disabled"  // 停用后不得改内容或升档
)

// AssetDep 是工程钉死的一条工艺依赖：身份、修订和当时摘要。
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
	Deps            []AssetDep `json:"deps"`            // 源依赖（身份+修订+摘要）
}

// Asset 是本厂一条工艺或工程的当前行，不含历史正文。
type Asset struct {
	ID             uuid.UUID  `json:"id"`             // 稳定身份
	Kind           string     `json:"kind"`           // process / project
	Level          string     `json:"level"`          // factory / personal
	Name           string     `json:"name"`           // 显示名，不当身份
	Status         string     `json:"status"`         // draft / available / disabled
	Copyable       bool       `json:"copyable"`       // 可否升档
	Revision       int64      `json:"revision"`       // 当前修订
	Content        []byte     `json:"-"`              // 不透明正文；不进列表/元数据
	Digest         []byte     `json:"digest"`         // SHA-256 32 字节
	CreatorID      uuid.UUID  `json:"creatorId"`      // 创建人
	FactoryID      uuid.UUID  `json:"factoryId"`      // 所属本厂
	OrgUnitID      *uuid.UUID `json:"orgUnitId"`      // 创建时节点；直属为空
	OrgPath        []PathNode `json:"orgPath"`        // 创建时路径
	SourceID       *uuid.UUID `json:"sourceId"`       // 升档源身份
	SourceRevision *int64     `json:"sourceRevision"` // 升档源修订
	Deps           []AssetDep `json:"deps"`           // 工艺必须空
	CreatedAt      time.Time  `json:"createdAt"`      // 创建时间
	UpdatedAt      time.Time  `json:"updatedAt"`      // 最近升高修订的时间
}

// AssetWrite 是一次改名/改内容/改可复制/改状态/改依赖的写入。
type AssetWrite struct {
	Name           string     // 显示名
	Content        []byte     // 正文
	Digest         []byte     // 与正文对应的摘要
	Copyable       bool       // 可复制
	Status         string     // 状态
	Deps           []AssetDep // 工程依赖；工艺必须空
	SourceRevision *int64     // 升档覆盖时更新源修订；空则不改
}

type governedAssetRow struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey"`   // 稳定身份
	Kind           string     `gorm:"not null"`               // process / project
	Level          string     `gorm:"not null"`               // factory / personal
	Name           string     `gorm:"not null"`               // 显示名
	Status         string     `gorm:"not null"`               // draft / available / disabled
	Copyable       bool       `gorm:"not null"`               // 可否升档
	Revision       int64      `gorm:"not null"`               // 当前修订
	Content        []byte     `gorm:"type:bytea;not null"`    // 正文
	Digest         []byte     `gorm:"type:bytea;not null"`    // SHA-256
	CreatorID      uuid.UUID  `gorm:"type:uuid;not null"`     // 创建人
	FactoryID      uuid.UUID  `gorm:"type:uuid;not null"`     // 所属本厂
	OrgUnitID      *uuid.UUID `gorm:"type:uuid"`              // 创建时节点
	OrgPath        []byte     `gorm:"type:jsonb;not null"`    // 路径快照
	SourceID       *uuid.UUID `gorm:"type:uuid"`              // 升档源
	SourceRevision *int64     `gorm:"column:source_revision"` // 升档源修订
	Deps           []byte     `gorm:"type:jsonb;not null"`    // 依赖 JSON
	CreatedAt      time.Time  `gorm:"not null"`               // 创建时间
	UpdatedAt      time.Time  `gorm:"not null"`               // 最近升高修订的时间
}

func (governedAssetRow) TableName() string { return "assets" }

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
		updates := map[string]any{
			"name":       w.Name,
			"content":    nonempty(w.Content),
			"digest":     w.Digest,
			"copyable":   w.Copyable,
			"status":     w.Status,
			"deps":       depsJSON,
			"revision":   expected + 1,
			"updated_at": now,
		}
		if w.SourceRevision != nil {
			updates["source_revision"] = *w.SourceRevision
		}
		res := tx.Model(&governedAssetRow{}).Where("id = ? AND revision = ?", assetID, expected).Updates(updates)
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

// ListGovernedAssets 列出本厂工艺/工程当前行，不含正文；按创建时间从新到旧。
func (s *Store) ListGovernedAssets(ctx context.Context) ([]Asset, error) {
	var rows []governedAssetRow
	if err := s.db.WithContext(ctx).Omit("Content").Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Asset, 0, len(rows))
	for _, row := range rows {
		out = append(out, assetFromGoverned(row))
	}
	return out, nil
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

// GovernedAssetBySourceID 按升档源身份找本厂已升档条目。
func (s *Store) GovernedAssetBySourceID(ctx context.Context, sourceID uuid.UUID) (Asset, error) {
	var row governedAssetRow
	if err := s.db.WithContext(ctx).Where("source_id = ?", sourceID).Order("updated_at DESC").First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Asset{}, domain.ErrNotFound
		}
		return Asset{}, err
	}
	return assetFromGoverned(row), nil
}

// AssetIsReferenced 是否仍被某条工程的 deps 引用。
func (s *Store) AssetIsReferenced(ctx context.Context, assetID uuid.UUID) (bool, error) {
	raw, err := json.Marshal([]map[string]string{{"id": assetID.String()}})
	if err != nil {
		return false, err
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&governedAssetRow{}).Where("deps @> ?", raw).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteGovernedAsset 物理删除本厂一条；调用方须先确认未被依赖。
func (s *Store) DeleteGovernedAsset(ctx context.Context, assetID uuid.UUID) error {
	res := s.db.WithContext(ctx).Where("id = ?", assetID).Delete(&governedAssetRow{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
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

// TamperAssetDeps 只改依赖 JSON 不升修订，供缺失夹具使用。
func (s *Store) TamperAssetDeps(ctx context.Context, assetID uuid.UUID, deps []AssetDep) error {
	raw, err := json.Marshal(deps)
	if err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Model(&governedAssetRow{}).Where("id = ?", assetID).Update("deps", raw)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
