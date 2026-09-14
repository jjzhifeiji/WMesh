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
	Revision        int64           `json:"revision"`        // 修订
	Schema          json.RawMessage `json:"schema"`          // 字段表
	Digest          []byte          `json:"digest"`          // SHA-256
	TargetFactoryID *uuid.UUID      `json:"targetFactoryId"` // 目标工厂
}

type contentTemplateRow struct {
	ID        uuid.UUID       `gorm:"type:uuid;primaryKey"` // 稳定身份
	Kind      string          `gorm:"not null"`             // process / project
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
		ID: row.ID, Kind: row.Kind, Revision: row.Revision,
		Schema: []byte(row.Schema), Digest: row.Digest,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

// TemplateByKind 读该类型当前模版。
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

// ListTemplates 列出两份当前模版。
func (s *Store) ListTemplates(ctx context.Context) ([]ContentTemplate, error) {
	var rows []contentTemplateRow
	if err := s.db.WithContext(ctx).Order("kind").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ContentTemplate, 0, len(rows))
	for _, row := range rows {
		out = append(out, templateFromRow(row))
	}
	return out, nil
}

// InsertTemplate 写入一份类型的首份模版。
func (s *Store) InsertTemplate(ctx context.Context, in ContentTemplate) (ContentTemplate, error) {
	if err := assertAssetDigest(in.Digest); err != nil {
		return ContentTemplate{}, err
	}
	now := time.Now().UTC()
	row := contentTemplateRow{
		ID: valueOrNew(in.ID), Kind: in.Kind, Revision: 1,
		Schema: json.RawMessage(nonempty(in.Schema)), Digest: in.Digest,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 同类型已有则返回当前行，不另开一份。
		if domain.IsUniqueViolation(err) {
			return s.TemplateByKind(ctx, in.Kind)
		}
		return ContentTemplate{}, err
	}
	return templateFromRow(row), nil
}

// UpdateTemplate 按期望修订改字段表。
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
