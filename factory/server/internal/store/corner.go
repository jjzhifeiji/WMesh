package store

import (
	"context"

	"github.com/google/uuid"
)

// 补判包角用的窄行，只取主键、种类和正文。
type cornerRow struct {
	ID       uuid.UUID `gorm:"type:uuid"` // 稳定身份
	Revision int64     // 修订；副本表里和身份组成主键
	Kind     string    // process / project
	Content  []byte    `gorm:"type:bytea"` // 库内正文，多半是信封
}

// fillCornerRows 把还没判过包角的行按正文补上；迁移时厂库正文是信封判不了，要等领到租约才解得开。
// 解不开的行先留空，下次领约再试，不挡领约。
func (s *Store) fillCornerRows(ctx context.Context) error {
	// 工艺工程表和副本表都要补，表名同时是信封绑定的标签。
	for _, table := range []string{tableAssets, tableReplicas} {
		// 这张表补不下去就停，下次领约再补。
		if err := s.fillCornerTable(ctx, table); err != nil {
			return err
		}
	}
	return nil
}

// fillCornerTable 按 (id, revision) 键集分批补判一张表里包角还空着的行。
func (s *Store) fillCornerTable(ctx context.Context, table string) error {
	// 游标从最小身份起，首批从表头取。
	afterID, afterRev := uuid.Nil, int64(0)
	// 一批一批往后翻，取不满一批就说明翻到底了。
	for {
		// 准备承接这一批还没判过的行。
		var rows []cornerRow
		// 只取包角还空着的行。
		err := s.db.WithContext(ctx).Model(sealModel(table)).Select("id", "revision", "kind", "content").
			Where("corner IS NULL AND (id, revision) > (?, ?)", afterID, afterRev).
			Order("id").Order("revision").Limit(sealBatch).Find(&rows).Error
		// 取这一批失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 逐条按正文判。
		for _, row := range rows {
			// 工程不用解正文，恒为否。
			var plain []byte
			// 只有工艺要解开正文看 kind。
			if row.Kind == KindProcess {
				// 解开正文，遗留明文也认。
				body, err := s.openAsset(row.ID, row.Revision, table, row.Content)
				// 解不开先留空，下次领约再试。
				if err != nil {
					continue
				}
				// 拿明文去判。
				plain = body
			}
			// 只补仍空着的，并发写入已经判过的不动。
			if err := s.db.WithContext(ctx).Model(sealModel(table)).
				Where("id = ? AND revision = ? AND corner IS NULL", row.ID, row.Revision).
				Update("corner", cornerOf(row.Kind, plain)).Error; err != nil {
				return err
			}
		}
		// 不满一批说明已到表尾，这张表补完了。
		if len(rows) < sealBatch {
			return nil
		}
		// 游标挪到这一批最后一行，下一批从它之后取。
		afterID, afterRev = rows[len(rows)-1].ID, rows[len(rows)-1].Revision
	}
}
