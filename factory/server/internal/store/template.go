package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/domain"
)

// ContentTemplate 是本厂已收的一份模版修订。
type ContentTemplate struct {
	ID         uuid.UUID `json:"id"`         // 与 WAN 原件相同
	Kind       string    `json:"kind"`       // process / project
	Revision   int64     `json:"revision"`   // 送达修订
	Schema     []byte    `json:"schema"`     // 字段表 JSON
	Digest     []byte    `json:"digest"`     // SHA-256
	ReceivedAt time.Time `json:"receivedAt"` // 本厂收到时间
}

// TemplateSnapshot 是 WAN 推来的一份模版修订。
type TemplateSnapshot struct {
	ID              uuid.UUID       `json:"id"`              // 与 WAN 原件相同
	Kind            string          `json:"kind"`            // process / project
	Revision        int64           `json:"revision"`        // 修订
	Schema          json.RawMessage `json:"schema"`          // 字段表
	Digest          []byte          `json:"digest"`          // SHA-256
	TargetFactoryID *uuid.UUID      `json:"targetFactoryId"` // 目标工厂
}

type contentTemplateReplicaRow struct {
	ID         uuid.UUID       `gorm:"type:uuid;primaryKey"`
	Revision   int64           `gorm:"primaryKey"`
	Kind       string          `gorm:"not null"`
	Schema     json.RawMessage `gorm:"type:jsonb;not null"`
	Digest     []byte          `gorm:"type:bytea;not null"`
	ReceivedAt time.Time       `gorm:"not null"`
}

func (contentTemplateReplicaRow) TableName() string { return "content_template_replicas" }

func templateFromRow(row contentTemplateReplicaRow) ContentTemplate {
	return ContentTemplate{
		ID: row.ID, Kind: row.Kind, Revision: row.Revision,
		Schema: []byte(row.Schema), Digest: row.Digest, ReceivedAt: row.ReceivedAt,
	}
}

// InsertTemplateReplica 写入一份模版修订；同身份修订且摘要相同则原样返回。
func (s *Store) InsertTemplateReplica(ctx context.Context, in ContentTemplate) (ContentTemplate, error) {
	if err := assertAssetDigest(in.Digest); err != nil {
		return ContentTemplate{}, err
	}
	row := contentTemplateReplicaRow{
		ID: in.ID, Revision: in.Revision, Kind: in.Kind,
		Schema: json.RawMessage(nonempty(in.Schema)), Digest: in.Digest,
		ReceivedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			got, getErr := s.TemplateReplicaByIDRev(ctx, in.ID, in.Revision)
			if getErr != nil {
				return ContentTemplate{}, getErr
			}
			if !bytes.Equal(got.Digest, in.Digest) {
				return ContentTemplate{}, domain.ErrIntegrity
			}
			return got, nil
		}
		return ContentTemplate{}, err
	}
	return templateFromRow(row), nil
}

// TemplateReplicaByIDRev 按身份和修订读模版副本。
func (s *Store) TemplateReplicaByIDRev(ctx context.Context, id uuid.UUID, revision int64) (ContentTemplate, error) {
	var row contentTemplateReplicaRow
	if err := s.db.WithContext(ctx).First(&row, "id = ? AND revision = ?", id, revision).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ContentTemplate{}, domain.ErrNotFound
		}
		return ContentTemplate{}, err
	}
	return templateFromRow(row), nil
}

// LatestTemplateByKind 读该类型最高修订副本。
func (s *Store) LatestTemplateByKind(ctx context.Context, kind string) (ContentTemplate, error) {
	var row contentTemplateReplicaRow
	if err := s.db.WithContext(ctx).Where("kind = ?", kind).Order("revision DESC").First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ContentTemplate{}, domain.ErrNotFound
		}
		return ContentTemplate{}, err
	}
	return templateFromRow(row), nil
}

// GovernedAssetsByKind 列出该类型本厂当前行，含正文。
func (s *Store) GovernedAssetsByKind(ctx context.Context, kind string) ([]Asset, error) {
	var rows []governedAssetRow
	if err := s.db.WithContext(ctx).Where("kind = ?", kind).Order("created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Asset, 0, len(rows))
	for _, row := range rows {
		out = append(out, assetFromGoverned(row))
	}
	return out, nil
}
