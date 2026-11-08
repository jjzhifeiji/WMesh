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
	ID         uuid.UUID  `json:"id"`                  // 与 WAN 原件相同
	Kind       string     `json:"kind"`                // process / project
	Name       string     `json:"name"`                // 工程模版名称；工艺为空
	Revision   int64      `json:"revision"`            // 送达修订
	Schema     []byte     `json:"schema"`              // 字段表 JSON
	Digest     []byte     `json:"digest"`              // SHA-256
	ReceivedAt time.Time  `json:"receivedAt"`          // 本厂收到时间
	RetiredAt  *time.Time `json:"retiredAt,omitempty"` // WAN 已删除、本厂撤下的时刻；空表示仍在用
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

// 已收模版修订的落库行。
type contentTemplateReplicaRow struct {
	ID         uuid.UUID       `gorm:"type:uuid;primaryKey"` // 与 WAN 原件相同
	Revision   int64           `gorm:"primaryKey"`           // 送达修订
	Kind       string          `gorm:"not null"`             // process / project
	Name       string          `gorm:"not null"`             // 工程模版名称；工艺为空
	Schema     json.RawMessage `gorm:"type:jsonb;not null"`  // 字段表 JSON
	Digest     []byte          `gorm:"type:bytea;not null"`  // SHA-256
	ReceivedAt time.Time       `gorm:"not null"`             // 本厂收到时间
	RetiredAt  *time.Time      // WAN 已删除、本厂撤下的时刻；空表示仍在用
}

// 指定落库表名，避免查询时按类型名去猜。
func (contentTemplateReplicaRow) TableName() string { return "content_template_replicas" }

// 库行收成已收模版视图。
func templateFromRow(row contentTemplateReplicaRow) ContentTemplate {
	return ContentTemplate{
		ID: row.ID, Kind: row.Kind, Name: row.Name, Revision: row.Revision,
		Schema: []byte(row.Schema), Digest: row.Digest, ReceivedAt: row.ReceivedAt,
		RetiredAt: row.RetiredAt,
	}
}

// InsertTemplateReplica 写入一份模版修订；同身份修订且摘要相同则补名称后返回。
// 这份身份曾被撤下（WAN 删了又按原身份重建）就一并恢复成在用。
// 第二个返回值标明这次是否新建了一行或恢复了撤下的身份，纯重复送达为假，调用方据此省掉重复审计。
func (s *Store) InsertTemplateReplica(ctx context.Context, in ContentTemplate) (ContentTemplate, bool, error) {
	// 摘要必须是三十二字节，否则拒绝写入。
	if err := assertAssetDigest(in.Digest); err != nil {
		return ContentTemplate{}, false, err
	}
	// 按入参组装要写入的行。
	row := contentTemplateReplicaRow{
		ID: in.ID, Revision: in.Revision, Kind: in.Kind, Name: in.Name,
		Schema: json.RawMessage(nonempty(in.Schema)), Digest: in.Digest,
		ReceivedAt: time.Now().UTC(),
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 唯一约束撞了就改成业务冲突，不抛库原文。
		if domain.IsUniqueViolation(err) {
			// 先按修订读模版，结果留给紧跟着的判断。
			got, getErr := s.TemplateReplicaByIDRev(ctx, in.ID, in.Revision)
			// 按修订读模版失败就停，避免带着错误继续。
			if getErr != nil {
				return ContentTemplate{}, false, getErr
			}
			// 同身份修订摘要必须相同，否则当损坏。
			if !bytes.Equal(got.Digest, in.Digest) {
				return ContentTemplate{}, false, domain.ErrIntegrity
			}
			// 同一修订补上名称；摘要未变不当新修订。
			if in.Name != "" && got.Name != in.Name {
				// 接上要操作的表失败就停，避免带着错误继续。
				if err := s.db.WithContext(ctx).Model(&contentTemplateReplicaRow{}).
					Where("id = ? AND revision = ?", in.ID, in.Revision).
					Update("name", in.Name).Error; err != nil {
					return ContentTemplate{}, false, err
				}
				// 把改好的值放进内存行，随后随保存写入。
				got.Name = in.Name
			}
			// 曾被撤下的同身份重新送达，恢复成在用，列表和新建又能用上。
			if got.RetiredAt != nil {
				// 恢复不了就交回，通道下一轮再补。
				if err := s.restoreTemplateReplica(ctx, in.ID); err != nil {
					return ContentTemplate{}, false, err
				}
				// 内存行也清掉撤下时刻，和库里一致。
				got.RetiredAt = nil
				// 恢复算这次有变化，调用方照常记审计。
				return got, true, nil
			}
			// 同修订同摘要是重复送达，标成未新建。
			return got, false, nil
		}
		return ContentTemplate{}, false, err
	}
	// 同身份旧修订曾被撤下的一并恢复，WAN 既然又送来了就是还在。
	if err := s.restoreTemplateReplica(ctx, in.ID); err != nil {
		return ContentTemplate{}, false, err
	}
	// 库行收成对外结果再交回，标成这次新建。
	return templateFromRow(row), true, nil
}

// restoreTemplateReplica 把这份身份所有被撤下的修订恢复成在用；没有撤下的不动。
func (s *Store) restoreTemplateReplica(ctx context.Context, id uuid.UUID) error {
	// 只清撤下时刻一列，库错误原样交回。
	return s.db.WithContext(ctx).Model(&contentTemplateReplicaRow{}).
		Where("id = ? AND retired_at IS NOT NULL", id).
		Update("retired_at", nil).Error
}

// TemplateReplicaByIDRev 按身份和修订读模版副本。
func (s *Store) TemplateReplicaByIDRev(ctx context.Context, id uuid.UUID, revision int64) (ContentTemplate, error) {
	// 准备承接查到的模版副本。
	var row contentTemplateReplicaRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = ? AND revision = ?", id, revision).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ContentTemplate{}, domain.ErrNotFound
		}
		return ContentTemplate{}, err
	}
	// 库行收成对外结果再交回。
	return templateFromRow(row), nil
}

// LatestTemplateByKind 读该类型最高修订副本（工艺仍一行）。
func (s *Store) LatestTemplateByKind(ctx context.Context, kind string) (ContentTemplate, error) {
	// 准备承接查到的模版副本。
	var row contentTemplateReplicaRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Where("kind = ?", kind).Order("revision DESC").First(&row).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ContentTemplate{}, domain.ErrNotFound
		}
		return ContentTemplate{}, err
	}
	// 库行收成对外结果再交回。
	return templateFromRow(row), nil
}

// LatestProjectTemplates 各工程模版身份取最高修订；WAN 已删除、本厂已撤下的不列。
func (s *Store) LatestProjectTemplates(ctx context.Context) ([]ContentTemplate, error) {
	// 准备承接查到的多条模版副本。
	var rows []contentTemplateReplicaRow
	// 走手写查询失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Raw(
		`SELECT DISTINCT ON (id) * FROM content_template_replicas WHERE kind = ? AND retired_at IS NULL ORDER BY id, revision DESC`,
		KindProject,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]ContentTemplate, 0, len(rows))
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, row := range rows {
		// 收进结果，保持原来的先后顺序。
		out = append(out, templateFromRow(row))
	}
	return out, nil
}

// GovernedAssetsByKind 列出该类型本厂当前行，含正文。
func (s *Store) GovernedAssetsByKind(ctx context.Context, kind string) ([]Asset, error) {
	// 准备承接查到的多条工艺或工程。
	var rows []governedAssetRow
	// 按条件取多行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Where("kind = ?", kind).Order("created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]Asset, 0, len(rows))
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, row := range rows {
		// 先拿到这一步的结果，后面断言还要用。
		a, err := s.decodeGoverned(row)
		// 解开工艺正文失败就停，避免带着错误继续。
		if err != nil {
			return nil, err
		}
		// 收进结果，保持原来的先后顺序。
		out = append(out, a)
	}
	return out, nil
}

// SyncTemplateReplicas 按 WAN 全量索引对齐某类模版：不在 keep 里的整份身份标成撤下，
// 在 keep 里却被撤下过的恢复；副本不物理删除。交回这次新撤下的身份数。
func (s *Store) SyncTemplateReplicas(ctx context.Context, kind string, keep []uuid.UUID) (int64, error) {
	// 记下新撤下的行数，事务成功才交回。
	var retired int64
	// 撤下与恢复放进同一事务，不留半对齐的状态。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 只动这一类仍在用的行，已撤下的不重复改时刻。
		q := tx.Model(&contentTemplateReplicaRow{}).Where("kind = ? AND retired_at IS NULL", kind)
		// 有保留名单才排除名单里的身份，空名单表示这一类全部撤下。
		if len(keep) > 0 {
			// 名单里的身份仍在 WAN，不撤。
			q = q.Where("id NOT IN ?", keep)
		}
		// 不在名单里的标成撤下，写失败整单回滚。
		res := q.Update("retired_at", time.Now().UTC())
		// 撤下写失败就交回，事务随之回滚。
		if res.Error != nil {
			return res.Error
		}
		// 记下这次新撤下了几行。
		retired = res.RowsAffected
		// 名单为空就没有要恢复的。
		if len(keep) == 0 {
			return nil
		}
		// 名单里曾被撤下的恢复成在用，WAN 仍列着它。
		return tx.Model(&contentTemplateReplicaRow{}).
			Where("kind = ? AND retired_at IS NOT NULL AND id IN ?", kind, keep).
			Update("retired_at", nil).Error
	})
	// 事务失败就交回，不报撤下了多少。
	if err != nil {
		return 0, err
	}
	return retired, nil
}
