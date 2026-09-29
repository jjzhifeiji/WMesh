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

// 一份当前字段模版的库行。
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

// 字段模版落这张表，修订升高时覆盖。
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
	// 准备接住库里的那一行，没有再另作处理。
	var row contentTemplateRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).Where("kind = ?", kind).First(&row).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ContentTemplate{}, domain.ErrNotFound
		}
		return ContentTemplate{}, err
	}
	// 库行收成当前模版再交回。
	return templateFromRow(row), nil
}

// TemplateByID 按身份读当前模版。
func (s *Store) TemplateByID(ctx context.Context, id uuid.UUID) (ContentTemplate, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row contentTemplateRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ContentTemplate{}, domain.ErrNotFound
		}
		return ContentTemplate{}, err
	}
	// 库行收成当前模版再交回。
	return templateFromRow(row), nil
}

// TemplatesByKind 列出该类型全部当前模版。
func (s *Store) TemplatesByKind(ctx context.Context, kind string) ([]ContentTemplate, error) {
	// 准备接住查出来的列表，空的也要能交回。
	var rows []contentTemplateRow
	// 列表没读出来就停，故障不能当成空表。
	if err := s.db.WithContext(ctx).Where("kind = ?", kind).Order("name, created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按行数预留结果，查空也是空表不是空指针。
	out := make([]ContentTemplate, 0, len(rows))
	// 逐行收成对外结果，顺序保持查询原来的样子。
	for _, row := range rows {
		// 这一行收成模版视图，放进结果。
		out = append(out, templateFromRow(row))
	}
	return out, nil
}

// ListTemplates 列出全部当前模版。
func (s *Store) ListTemplates(ctx context.Context) ([]ContentTemplate, error) {
	// 准备接住查出来的列表，空的也要能交回。
	var rows []contentTemplateRow
	// 列表没读出来就停，故障不能当成空表。
	if err := s.db.WithContext(ctx).Order("kind, name").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按行数预留结果，查空也是空表不是空指针。
	out := make([]ContentTemplate, 0, len(rows))
	// 逐行收成对外结果，顺序保持查询原来的样子。
	for _, row := range rows {
		// 这一行收成模版视图，放进结果。
		out = append(out, templateFromRow(row))
	}
	return out, nil
}

// InsertTemplate 写入一份当前模版。
func (s *Store) InsertTemplate(ctx context.Context, in ContentTemplate) (ContentTemplate, error) {
	// 摘要不是完整的三十二字节就停。
	if err := assertAssetDigest(in.Digest); err != nil {
		return ContentTemplate{}, err
	}
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 组一份新模版，修订从一开始。
	row := contentTemplateRow{
		ID: valueOrNew(in.ID), Kind: in.Kind, Name: in.Name, Revision: 1,
		Schema: json.RawMessage(nonempty(in.Schema)), Digest: in.Digest,
		CreatedAt: now, UpdatedAt: now,
	}
	// 写入失败先停住，再看是重复还是约束没过。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 唯一撞了就不另插，工艺交回已有，名称冲突才拒绝。
		if domain.IsUniqueViolation(err) {
			// 工艺模版全库只留一行，撞了就交回已有的。
			if in.Kind == KindProcess {
				// 工艺模版已经有一行，交回那一份。
				return s.TemplateByKind(ctx, in.Kind)
			}
			// 身份已有则收回已有行；名称冲突仍拒绝。
			if in.ID != uuid.Nil {
				// 按身份再读一次，还在就当这次已经写过。
				got, getErr := s.TemplateByID(ctx, in.ID)
				// 原行还在就交回它，当作这次已经写过。
				if getErr == nil {
					return got, nil
				}
			}
			return ContentTemplate{}, domain.ErrTemplateInvalid
		}
		return ContentTemplate{}, err
	}
	// 库行收成当前模版再交回。
	return templateFromRow(row), nil
}

// UpdateTemplate 按类型和期望修订改工艺字段表。
func (s *Store) UpdateTemplate(ctx context.Context, kind string, expected int64, schema, digest []byte) (ContentTemplate, error) {
	// 摘要不是完整的三十二字节就停。
	if err := assertAssetDigest(digest); err != nil {
		return ContentTemplate{}, err
	}
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 按期望修订改字段表，对不上则一行不改。
	res := s.db.WithContext(ctx).Model(&contentTemplateRow{}).
		Where("kind = ? AND revision = ?", kind, expected).
		Updates(map[string]any{
			"schema": json.RawMessage(nonempty(schema)), "digest": digest,
			"revision": expected + 1, "updated_at": now,
		})
	// 写库报错就停，不能当成已经改成。
	if res.Error != nil {
		return ContentTemplate{}, res.Error
	}
	// 期望修订已经变了，原件保持不动。
	if res.RowsAffected == 0 {
		return ContentTemplate{}, domain.ErrRevisionConflict
	}
	// 改完再读当前模版，修订以库为准。
	return s.TemplateByKind(ctx, kind)
}

// UpdateTemplateByID 按身份和期望修订改名称与字段表。
func (s *Store) UpdateTemplateByID(ctx context.Context, id uuid.UUID, expected int64, name string, schema, digest []byte) (ContentTemplate, error) {
	// 摘要不是完整的三十二字节就停。
	if err := assertAssetDigest(digest); err != nil {
		return ContentTemplate{}, err
	}
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 按身份和期望修订改名称与字段表。
	res := s.db.WithContext(ctx).Model(&contentTemplateRow{}).
		Where("id = ? AND revision = ?", id, expected).
		Updates(map[string]any{
			"name": name, "schema": json.RawMessage(nonempty(schema)), "digest": digest,
			"revision": expected + 1, "updated_at": now,
		})
	// 写库报错就停，不能当成已经改成。
	if res.Error != nil {
		// 名称冲突不能再插一份。
		if domain.IsUniqueViolation(res.Error) {
			return ContentTemplate{}, domain.ErrTemplateInvalid
		}
		return ContentTemplate{}, res.Error
	}
	// 期望修订已经变了，原件保持不动。
	if res.RowsAffected == 0 {
		return ContentTemplate{}, domain.ErrRevisionConflict
	}
	// 改完再读这一份，名称和修订以库为准。
	return s.TemplateByID(ctx, id)
}

// DeleteTemplate 删掉一份当前工程模版。
func (s *Store) DeleteTemplate(ctx context.Context, id uuid.UUID) error {
	// 只删工程模版，工艺那一行不走这里。
	res := s.db.WithContext(ctx).Where("id = ? AND kind = ?", id, KindProject).Delete(&contentTemplateRow{})
	// 写库报错就停，不能当成已经改成。
	if res.Error != nil {
		return res.Error
	}
	// 一行都没碰到，按不存在拒绝。
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// AssetsByKind 列出该类型当前行，含正文。
func (s *Store) AssetsByKind(ctx context.Context, kind string) ([]Asset, error) {
	// 准备接住查出来的列表，空的也要能交回。
	var rows []assetRow
	// 列表没读出来就停，故障不能当成空表。
	if err := s.db.WithContext(ctx).Where("kind = ?", kind).Order("created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按行数预留结果，查空也是空表不是空指针。
	out := make([]Asset, 0, len(rows))
	// 逐行收成对外结果，顺序保持查询原来的样子。
	for _, row := range rows {
		// 这一行收成资产视图，放进结果。
		out = append(out, assetFromRow(row))
	}
	return out, nil
}
