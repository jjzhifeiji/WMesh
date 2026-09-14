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
	Name       string    `json:"name"`       // 工程模版名称；工艺为空
	Revision   int64     `json:"revision"`   // 送达修订
	Schema     []byte    `json:"schema"`     // 字段表 JSON
	Digest     []byte    `json:"digest"`     // SHA-256
	ReceivedAt time.Time `json:"receivedAt"` // 本厂收到时间
}

// TemplateSnapshot 是 WAN 推来的一份模版修订。
type TemplateSnapshot struct {
	ID              uuid.UUID       `json:"id"`              // 与 WAN 原件相同
	Kind            string          `json:"kind"`            // process / project
	Name            string          `json:"name"`            // 工程模版名称；工艺为空
	Revision        int64           `json:"revision"`        // 修订
	Schema          json.RawMessage `json:"schema"`          // 字段表
	Digest          []byte          `json:"digest"`          // SHA-256
	TargetFactoryID *uuid.UUID      `json:"targetFactoryId"` // 目标工厂
}

type contentTemplateReplicaRow struct {
	ID         uuid.UUID       `gorm:"type:uuid;primaryKey"` // 与 WAN 原件相同
	Revision   int64           `gorm:"primaryKey"`           // 送达修订
	Kind       string          `gorm:"not null"`             // process / project
	Name       string          `gorm:"not null"`             // 工程模版名称；工艺为空
	Schema     json.RawMessage `gorm:"type:jsonb;not null"`  // 字段表 JSON
	Digest     []byte          `gorm:"type:bytea;not null"`  // SHA-256
	ReceivedAt time.Time       `gorm:"not null"`             // 本厂收到时间
}

func (contentTemplateReplicaRow) TableName() string { return "content_template_replicas" }

// 库行收成已收模版视图。
func templateFromRow(row contentTemplateReplicaRow) ContentTemplate {
	return ContentTemplate{
		ID: row.ID, Kind: row.Kind, Name: row.Name, Revision: row.Revision,
		Schema: []byte(row.Schema), Digest: row.Digest, ReceivedAt: row.ReceivedAt,
	}
}

// InsertTemplateReplica 写入一份模版修订；同身份修订且摘要相同则补名称后返回。
func (s *Store) InsertTemplateReplica(ctx context.Context, in ContentTemplate) (ContentTemplate, error) {
	if err := assertAssetDigest(in.Digest); err != nil {
		return ContentTemplate{}, err
	}
	row := contentTemplateReplicaRow{
		ID: in.ID, Revision: in.Revision, Kind: in.Kind, Name: in.Name,
		Schema: json.RawMessage(nonempty(in.Schema)), Digest: in.Digest,
		ReceivedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			got, getErr := s.TemplateReplicaByIDRev(ctx, in.ID, in.Revision)
			if getErr != nil {
				return ContentTemplate{}, getErr
			}
			// 同身份修订摘要必须相同，否则当损坏。
			if !bytes.Equal(got.Digest, in.Digest) {
				return ContentTemplate{}, domain.ErrIntegrity
			}
			// 同一修订补上名称；摘要未变不当新修订。
			if in.Name != "" && got.Name != in.Name {
				if err := s.db.WithContext(ctx).Model(&contentTemplateReplicaRow{}).
					Where("id = ? AND revision = ?", in.ID, in.Revision).
					Update("name", in.Name).Error; err != nil {
					return ContentTemplate{}, err
				}
				got.Name = in.Name
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

// LatestTemplateByKind 读该类型最高修订副本（工艺仍一行）。
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

// LatestProjectTemplates 各工程模版身份取最高修订。
func (s *Store) LatestProjectTemplates(ctx context.Context) ([]ContentTemplate, error) {
	var rows []contentTemplateReplicaRow
	if err := s.db.WithContext(ctx).Raw(
		`SELECT DISTINCT ON (id) * FROM content_template_replicas WHERE kind = ? ORDER BY id, revision DESC`,
		KindProject,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ContentTemplate, 0, len(rows))
	for _, row := range rows {
		out = append(out, templateFromRow(row))
	}
	return out, nil
}

// GovernedAssetsByKind 列出该类型本厂当前行，含正文。
func (s *Store) GovernedAssetsByKind(ctx context.Context, kind string) ([]Asset, error) {
	var rows []governedAssetRow
	if err := s.db.WithContext(ctx).Where("kind = ?", kind).Order("created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Asset, 0, len(rows))
	for _, row := range rows {
		a, err := s.decodeGoverned(row)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}
