package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/domain"
)

// ContentTemplate 是一份当前字段模版。
type ContentTemplate struct {
	ID        uuid.UUID `json:"id"`        // 稳定身份
	Kind      string    `json:"kind"`      // process / project
	Name      string    `json:"name"`      // 工程模版名称；工艺为空
	Revision  int64     `json:"revision"`  // 当前修订
	Schema    []byte    `json:"schema"`    // 字段表 JSON
	Digest    []byte    `json:"digest"`    // SHA-256
	CreatedAt time.Time `json:"createdAt"` // 首次写入
	UpdatedAt time.Time `json:"updatedAt"` // 最近升高修订
}

// TemplateSnapshot 是下发给厂的一份模版修订。
type TemplateSnapshot struct {
	ID              uuid.UUID       `json:"id"`              // 与 WAN 原件相同
	Kind            string          `json:"kind"`            // process / project
	Name            string          `json:"name"`            // 工程模版名称；工艺为空
	Revision        int64           `json:"revision"`        // 修订
	Schema          json.RawMessage `json:"schema"`          // 字段表
	Digest          []byte          `json:"digest"`          // SHA-256
	TargetFactoryID *uuid.UUID      `json:"targetFactoryId"` // 目标工厂
}

type contentTemplateRow struct {
	ID        uuid.UUID       `gorm:"type:uuid;primaryKey"` // 稳定身份
	Kind      string          `gorm:"not null"`             // process / project
	Name      string          `gorm:"not null"`             // 工程模版名称；工艺为空
	Revision  int64           `gorm:"not null"`             // 当前修订
	Schema    json.RawMessage `gorm:"type:jsonb;not null"`  // 字段表 JSON
	Digest    []byte          `gorm:"type:bytea;not null"`  // SHA-256
	CreatedAt time.Time       `gorm:"not null"`             // 首次写入
	UpdatedAt time.Time       `gorm:"not null"`             // 最近升高修订
}

func (contentTemplateRow) TableName() string { return "content_templates" }

// 库行收成当前模版视图。
func templateFromRow(row contentTemplateRow) ContentTemplate {
	return ContentTemplate{
		ID: row.ID, Kind: row.Kind, Name: row.Name, Revision: row.Revision,
		Schema: []byte(row.Schema), Digest: row.Digest,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

// TemplateByKind 读该类型当前模版（工艺仍一行）。
func (s *Store) TemplateByKind(ctx context.Context, kind string) (ContentTemplate, error) {
	var row contentTemplateRow
	if err := s.db.WithContext(ctx).Where("kind = ?", kind).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ContentTemplate{}, domain.ErrNotFound
		}
		return ContentTemplate{}, err
	}
	return templateFromRow(row), nil
}

// TemplateByID 按身份读当前模版。
func (s *Store) TemplateByID(ctx context.Context, id uuid.UUID) (ContentTemplate, error) {
	var row contentTemplateRow
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ContentTemplate{}, domain.ErrNotFound
		}
		return ContentTemplate{}, err
	}
	return templateFromRow(row), nil
}

// TemplatesByKind 列出该类型全部当前模版。
func (s *Store) TemplatesByKind(ctx context.Context, kind string) ([]ContentTemplate, error) {
	var rows []contentTemplateRow
	if err := s.db.WithContext(ctx).Where("kind = ?", kind).Order("name, created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ContentTemplate, 0, len(rows))
	for _, row := range rows {
		out = append(out, templateFromRow(row))
	}
	return out, nil
}

// ListTemplates 列出全部当前模版。
func (s *Store) ListTemplates(ctx context.Context) ([]ContentTemplate, error) {
	var rows []contentTemplateRow
	if err := s.db.WithContext(ctx).Order("kind, name").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ContentTemplate, 0, len(rows))
	for _, row := range rows {
		out = append(out, templateFromRow(row))
	}
	return out, nil
}

// InsertTemplate 写入一份当前模版。
func (s *Store) InsertTemplate(ctx context.Context, in ContentTemplate) (ContentTemplate, error) {
	if err := assertAssetDigest(in.Digest); err != nil {
		return ContentTemplate{}, err
	}
	now := time.Now().UTC()
	row := contentTemplateRow{
		ID: valueOrNew(in.ID), Kind: in.Kind, Name: in.Name, Revision: 1,
		Schema: json.RawMessage(nonempty(in.Schema)), Digest: in.Digest,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			if in.Kind == KindProcess {
				return s.TemplateByKind(ctx, in.Kind)
			}
			// 身份已有则收回已有行；名称冲突仍拒绝。
			if in.ID != uuid.Nil {
				got, getErr := s.TemplateByID(ctx, in.ID)
				if getErr == nil {
					return got, nil
				}
			}
			return ContentTemplate{}, domain.ErrTemplateInvalid
		}
		return ContentTemplate{}, err
	}
	return templateFromRow(row), nil
}

// UpdateTemplate 按类型和期望修订改工艺字段表。
func (s *Store) UpdateTemplate(ctx context.Context, kind string, expected int64, schema, digest []byte) (ContentTemplate, error) {
	if err := assertAssetDigest(digest); err != nil {
		return ContentTemplate{}, err
	}
	now := time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&contentTemplateRow{}).
		Where("kind = ? AND revision = ?", kind, expected).
		Updates(map[string]any{
			"schema": json.RawMessage(nonempty(schema)), "digest": digest,
			"revision": expected + 1, "updated_at": now,
		})
	if res.Error != nil {
		return ContentTemplate{}, res.Error
	}
	if res.RowsAffected == 0 {
		return ContentTemplate{}, domain.ErrRevisionConflict
	}
	return s.TemplateByKind(ctx, kind)
}

// UpdateTemplateByID 按身份和期望修订改名称与字段表。
func (s *Store) UpdateTemplateByID(ctx context.Context, id uuid.UUID, expected int64, name string, schema, digest []byte) (ContentTemplate, error) {
	if err := assertAssetDigest(digest); err != nil {
		return ContentTemplate{}, err
	}
	now := time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&contentTemplateRow{}).
		Where("id = ? AND revision = ?", id, expected).
		Updates(map[string]any{
			"name": name, "schema": json.RawMessage(nonempty(schema)), "digest": digest,
			"revision": expected + 1, "updated_at": now,
		})
	if res.Error != nil {
		if domain.IsUniqueViolation(res.Error) {
			return ContentTemplate{}, domain.ErrTemplateInvalid
		}
		return ContentTemplate{}, res.Error
	}
	if res.RowsAffected == 0 {
		return ContentTemplate{}, domain.ErrRevisionConflict
	}
	return s.TemplateByID(ctx, id)
}

// DeleteTemplate 删掉一份当前工程模版。
func (s *Store) DeleteTemplate(ctx context.Context, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Where("id = ? AND kind = ?", id, KindProject).Delete(&contentTemplateRow{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// AssetsByKind 列出该类型当前行，含正文。
func (s *Store) AssetsByKind(ctx context.Context, kind string) ([]Asset, error) {
	var rows []assetRow
	if err := s.db.WithContext(ctx).Where("kind = ?", kind).Order("created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Asset, 0, len(rows))
	for _, row := range rows {
		out = append(out, assetFromRow(row))
	}
	return out, nil
}
