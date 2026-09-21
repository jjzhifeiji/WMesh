package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
	WeldKind        string     `json:"weldKind"`        // 作业类型：与源相同
	Status          string     `json:"status"`          // 源状态
	Deps            []AssetDep `json:"deps"`            // 源依赖
}

// Asset 是一条平台级工艺或工程的当前行。
type Asset struct {
	ID                uuid.UUID  `json:"id"`             // 稳定身份
	Kind              string     `json:"kind"`           // process / project
	Level             string     `json:"level"`          // 固定 platform
	Name              string     `json:"name"`           // 显示名
	Code              string     `json:"code"`          // 只读编号，创建后不改
	Status            string     `json:"status"`         // draft / available / disabled
	Copyable          bool       `json:"copyable"`       // 可否被上一级复制；新建默认为否
	WeldKind          string     `json:"weldKind"`       // 作业类型：single / multilayer / tbar
	Revision          int64      `json:"revision"`       // 当前修订
	Content           []byte     `json:"-"`              // 正文；不进列表/元数据
	Digest            []byte     `json:"digest"`         // SHA-256 32 字节
	CreatorID         uuid.UUID  `json:"creatorId"`      // WAN 管理员
	SourceID          *uuid.UUID `json:"sourceId"`       // 升档源厂级身份
	SourceRevision    *int64     `json:"sourceRevision"` // 升档源修订
	SourceFactoryID   *uuid.UUID `json:"-"`              // 升档源厂；列表不把身份交给前端
	SourceFactoryName string     `json:"sourceFactory"`  // 来源厂显示名：本端新建 / 厂名 / 未知工厂
	Deps              []AssetDep `json:"deps"`           // 工艺必须空
	CreatedAt         time.Time  `json:"createdAt"`      // 创建时间
	UpdatedAt         time.Time  `json:"updatedAt"`      // 最近升高修订的时间
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

type assetRow struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey"`   // 稳定身份
	Kind            string     `gorm:"not null"`               // process / project
	Level           string     `gorm:"not null"`               // 固定 platform
	Name            string     `gorm:"not null"`               // 显示名
	Code            string     `gorm:"not null"`               // 只读编号
	Status          string     `gorm:"not null"`               // draft / available / disabled
	Copyable        bool       `gorm:"not null"`               // 可复制
	WeldKind        string     `gorm:"column:weld_kind;not null"` // 作业类型
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

// InsertAsset 写入一条平台级工艺或工程；可复制由调用方给定，修订从 1 起。
func (s *Store) InsertAsset(ctx context.Context, in Asset) (Asset, error) {
	deps, err := marshalAssetDeps(in.Kind, in.Deps)
	if err != nil {
		return Asset{}, err
	}
	if err := assertAssetDigest(in.Digest); err != nil {
		return Asset{}, err
	}
	weldKind, err := NormalizeWeldKind(in.WeldKind)
	if err != nil {
		return Asset{}, err
	}
	now := time.Now().UTC()
	var row assetRow
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		code, err := nextWANAssetCode(tx, in.Kind)
		if err != nil {
			return err
		}
		row = assetRow{
			ID:              valueOrNew(in.ID),
			Kind:            in.Kind,
			Level:           AssetLevelPlatform,
			Name:            in.Name,
			Code:            code,
			Status:          in.Status,
			Copyable:        in.Copyable,
			WeldKind:        weldKind,
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
		if err := tx.Create(&row).Error; err != nil {
			return mapAssetWriteErr(err)
		}
		root, err := ensureFSRootTx(tx, row.Kind, FSTreePlatform, nil)
		if err != nil {
			return err
		}
		return placeAssetFileTx(tx, row.Kind, row.ID, row.Name, row.Code, root.ID)
	})
	if err != nil {
		return Asset{}, err
	}
	return assetFromRow(row), nil
}

// UpdateAsset 按期望修订改平台级当前行。
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
		// 期望修订对不上则原件不变。
		res := tx.Model(&assetRow{}).Where("id = ? AND revision = ?", assetID, expected).Updates(updates)
		if res.Error != nil {
			return mapAssetWriteErr(res.Error)
		}
		if res.RowsAffected == 0 {
			return domain.ErrRevisionConflict
		}
		if err := tx.First(&row, "id = ?", assetID).Error; err != nil {
			return err
		}
		if err := syncFSFileNameTx(tx, assetID, w.Name); err != nil {
			return err
		}
		out = assetFromRow(row)
		return nil
	})
	return out, err
}

// ListAssets 列出平台级当前行，不含正文；按创建时间从新到旧。
func (s *Store) ListAssets(ctx context.Context) ([]Asset, error) {
	var rows []assetRow
	if err := s.db.WithContext(ctx).Omit("Content").Order("created_at DESC").Find(&rows).Error; err != nil {
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

// AssetByCode 按只读编号取当前行。
func (s *Store) AssetByCode(ctx context.Context, code string) (Asset, error) {
	var row assetRow
	if err := s.db.WithContext(ctx).First(&row, "code = ?", code).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Asset{}, domain.ErrNotFound
		}
		return Asset{}, err
	}
	return assetFromRow(row), nil
}

// AssetBySourceID 按升档源身份找当前平台级；同一源只认这一条。
func (s *Store) AssetBySourceID(ctx context.Context, sourceID uuid.UUID) (Asset, error) {
	var row assetRow
	if err := s.db.WithContext(ctx).Where("source_id = ?", sourceID).Order("updated_at DESC").First(&row).Error; err != nil {
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

// AssetIsReferenced 是否仍被某条工程的 deps 引用。
func (s *Store) AssetIsReferenced(ctx context.Context, assetID uuid.UUID) (bool, error) {
	raw, err := json.Marshal([]map[string]string{{"id": assetID.String()}})
	if err != nil {
		return false, err
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&assetRow{}).Where("deps @> ?", raw).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteAsset 物理删除一条平台级；调用方须先清授权并记撤回。
func (s *Store) DeleteAsset(ctx context.Context, assetID uuid.UUID) error {
	res := s.db.WithContext(ctx).Where("id = ?", assetID).Delete(&assetRow{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteAssetAndRetract 清下发授权、记下撤回、再删原件。
func (s *Store) DeleteAssetAndRetract(ctx context.Context, assetID uuid.UUID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("asset_id = ?", assetID).Delete(&distGrantRow{}).Error; err != nil {
			return err
		}
		// 撤回身份幂等记下，厂端用来补送删除。
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&retractionRow{
			AssetID: assetID, CreatedAt: time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", assetID).Delete(&assetRow{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// marshalAssetDeps 工艺不得带依赖；空切片落成 []。
func marshalAssetDeps(kind string, deps []AssetDep) ([]byte, error) {
	if deps == nil {
		deps = []AssetDep{}
	}
	if kind == KindProcess && len(deps) > 0 {
		return nil, domain.ErrAssetDependency
	}
	return json.Marshal(deps)
}

// assertAssetDigest 摘要必须是 32 字节 SHA-256。
func assertAssetDigest(d []byte) error {
	if len(d) != 32 {
		return domain.ErrIntegrity
	}
	return nil
}

// mapAssetWriteErr 库约束收成业务错误，不抛 SQL。
func mapAssetWriteErr(err error) error {
	if domain.IsUniqueViolation(err) {
		return domain.ErrAssetCodeConflict
	}
	if domain.IsCheckViolation(err) {
		return domain.ErrIntegrity
	}
	if domain.IsForeignKeyViolation(err) {
		return domain.ErrNotFound
	}
	return err
}

// valueOrNew 没给身份就现场发号。
func valueOrNew(given uuid.UUID) uuid.UUID {
	if given == uuid.Nil {
		return id.New()
	}
	return given
}

// nonempty 空指针收成空切片，避免 NULL。
func nonempty(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}

// unmarshalAssetDeps 坏 JSON 当没有依赖，不当损坏。
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

// 库行收成当前资产视图。
func assetFromRow(row assetRow) Asset {
	return Asset{
		ID:              row.ID,
		Kind:            row.Kind,
		Level:           row.Level,
		Name:            row.Name,
		Code:            row.Code,
		Status:          row.Status,
		Copyable:        row.Copyable,
		WeldKind:        row.WeldKind,
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
