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
	Code              string     `json:"code"`           // 只读编号，创建后不改
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
	WeldKind       string     // 作业类型；空则不改
	SourceRevision *int64     // 升档覆盖时更新源修订；空则不改
}

// 平台级工艺或工程的当前库行，正文和依赖都在这。
type assetRow struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey"`      // 稳定身份
	Kind            string     `gorm:"not null"`                  // process / project
	Level           string     `gorm:"not null"`                  // 固定 platform
	Name            string     `gorm:"not null"`                  // 显示名
	Code            string     `gorm:"not null"`                  // 只读编号
	Status          string     `gorm:"not null"`                  // draft / available / disabled
	Copyable        bool       `gorm:"not null"`                  // 可复制
	WeldKind        string     `gorm:"column:weld_kind;not null"` // 作业类型
	Revision        int64      `gorm:"not null"`                  // 当前修订
	Content         []byte     `gorm:"type:bytea;not null"`       // 正文
	Digest          []byte     `gorm:"type:bytea;not null"`       // SHA-256
	CreatorID       uuid.UUID  `gorm:"type:uuid;not null"`        // WAN 管理员
	SourceID        *uuid.UUID `gorm:"type:uuid"`                 // 升档源厂级身份
	SourceRevision  *int64     `gorm:"column:source_revision"`    // 升档源修订
	SourceFactoryID *uuid.UUID `gorm:"type:uuid"`                 // 升档源厂
	Deps            []byte     `gorm:"type:jsonb;not null"`       // 依赖 JSON
	CreatedAt       time.Time  `gorm:"not null"`                  // 创建时间
	UpdatedAt       time.Time  `gorm:"not null"`                  // 最近升高修订的时间
}

// 平台级资产落这张表，不跟默认复数走。
func (assetRow) TableName() string { return "assets" }

// InsertAsset 写入一条平台级工艺或工程；可复制由调用方给定，修订从 1 起。
func (s *Store) InsertAsset(ctx context.Context, in Asset) (Asset, error) {
	// 依赖先收成库里的正文，工艺带依赖会在这里被拒。
	deps, err := marshalAssetDeps(in.Kind, in.Deps)
	// 依赖收不成库里的正文就停，避免写下坏内容。
	if err != nil {
		return Asset{}, err
	}
	// 摘要不是完整的三十二字节就停。
	if err := assertAssetDigest(in.Digest); err != nil {
		return Asset{}, err
	}
	// 作业类型先收成规范值，空的当单层。
	weldKind, err := NormalizeWeldKind(in.WeldKind)
	// 作业类型不认就停，脏值不能入库。
	if err != nil {
		return Asset{}, err
	}
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 准备接住库里的那一行，没有再另作处理。
	var row assetRow
	// 编号、正文和目录挂点同一事务提交，失败一起回滚。
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 行锁取出下一个平台编号，避免并发重号。
		code, err := nextWANAssetCode(tx, in.Kind)
		// 编号发不出来就停，避免空号落库。
		if err != nil {
			return err
		}
		// 平台行组好，修订从一，编号用刚发的那个。
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
		// 这一行没写进去就停，不能当成已经落库。
		if err := tx.Create(&row).Error; err != nil {
			// 库约束收成业务错误再交回，不抛原文。
			return mapAssetWriteErr(err)
		}
		// 没有这类的根就建一棵，已有则直接用。
		root, err := ensureFSRootTx(tx, row.Kind, FSTreePlatform, nil)
		// 根取不到也建不成就停，资产不能没有挂点。
		if err != nil {
			return err
		}
		// 编号写好后把文件挂到该类的根上。
		return placeAssetFileTx(tx, row.Kind, row.ID, row.Name, row.Code, root.ID)
	})
	// 事务没提交就停，编号和挂点不能只留一半。
	if err != nil {
		return Asset{}, err
	}
	// 库行收成资产再交回，依赖从正文解开。
	return assetFromRow(row), nil
}

// UpdateAsset 按期望修订改平台级当前行。
func (s *Store) UpdateAsset(ctx context.Context, assetID uuid.UUID, expected int64, w AssetWrite) (Asset, error) {
	// 摘要不是完整的三十二字节就停。
	if err := assertAssetDigest(w.Digest); err != nil {
		return Asset{}, err
	}
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 准备接住事务里写好的结果，失败则不用它。
	var out Asset
	// 改当前行、对修订、同步目录名，失败整笔不动。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 准备接住库里的那一行，没有再另作处理。
		var row assetRow
		// 取不到或库出错先停住，再区分没有还是故障。
		if err := tx.First(&row, "id = ?", assetID).Error; err != nil {
			// 没有这一行就按不存在交回，不当成库故障。
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		// 依赖先收成库里的正文，工艺带依赖会在这里被拒。
		depsJSON, err := marshalAssetDeps(row.Kind, w.Deps)
		// 依赖收不成库里的正文就停，避免写下坏内容。
		if err != nil {
			return err
		}
		// 先组好要改的列，修订在期望值上加一。
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
		// 这次带了作业类型才改，空着就保持原样。
		if w.WeldKind != "" {
			// 作业类型先收成规范值，空的当单层。
			kind, err := NormalizeWeldKind(w.WeldKind)
			// 作业类型不认就停，脏值不能入库。
			if err != nil {
				return err
			}
			// 作业类型收成规范值后再写入。
			updates["weld_kind"] = kind
		}
		// 升档覆盖时才改源修订，平时不动来源。
		if w.SourceRevision != nil {
			// 升档覆盖时把源修订一起推进。
			updates["source_revision"] = *w.SourceRevision
		}
		// 期望修订对不上则原件不变。
		res := tx.Model(&assetRow{}).Where("id = ? AND revision = ?", assetID, expected).Updates(updates)
		// 更新报错就收成业务错误，原件保持不动。
		if res.Error != nil {
			// 库约束收成业务错误再交回，不抛原文。
			return mapAssetWriteErr(res.Error)
		}
		// 期望修订已经变了，原件保持不动。
		if res.RowsAffected == 0 {
			return domain.ErrRevisionConflict
		}
		// 取不到或库出错先停住，再区分没有还是故障。
		if err := tx.First(&row, "id = ?", assetID).Error; err != nil {
			return err
		}
		// 目录名没同步上就停，资产和文件会各叫各的。
		if err := syncFSFileNameTx(tx, assetID, w.Name); err != nil {
			return err
		}
		// 改完的行收成资产，事务成功才用它。
		out = assetFromRow(row)
		return nil
	})
	return out, err
}

// ListAssets 列出平台级当前行，不含正文；按创建时间从新到旧。
func (s *Store) ListAssets(ctx context.Context) ([]Asset, error) {
	// 准备接住查出来的列表，空的也要能交回。
	var rows []assetRow
	// 列表没读出来就停，故障不能当成空表。
	if err := s.db.WithContext(ctx).Omit("Content").Order("created_at DESC").Find(&rows).Error; err != nil {
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

// AssetByID 读平台级当前行，含正文。
func (s *Store) AssetByID(ctx context.Context, assetID uuid.UUID) (Asset, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row assetRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", assetID).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Asset{}, domain.ErrNotFound
		}
		return Asset{}, err
	}
	// 库行收成资产再交回，依赖从正文解开。
	return assetFromRow(row), nil
}

// AssetByCode 按只读编号取当前行。
func (s *Store) AssetByCode(ctx context.Context, code string) (Asset, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row assetRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "code = ?", code).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Asset{}, domain.ErrNotFound
		}
		return Asset{}, err
	}
	// 库行收成资产再交回，依赖从正文解开。
	return assetFromRow(row), nil
}

// AssetBySourceID 按升档源身份找当前平台级；同一源只认这一条。
func (s *Store) AssetBySourceID(ctx context.Context, sourceID uuid.UUID) (Asset, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row assetRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).Where("source_id = ?", sourceID).Order("updated_at DESC").First(&row).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Asset{}, domain.ErrNotFound
		}
		return Asset{}, err
	}
	// 库行收成资产再交回，依赖从正文解开。
	return assetFromRow(row), nil
}

// AssetBySource 按升档源身份和源修订找平台级当前行。
func (s *Store) AssetBySource(ctx context.Context, sourceID uuid.UUID, sourceRevision int64) (Asset, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row assetRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "source_id = ? AND source_revision = ?", sourceID, sourceRevision).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Asset{}, domain.ErrNotFound
		}
		return Asset{}, err
	}
	// 库行收成资产再交回，依赖从正文解开。
	return assetFromRow(row), nil
}

// AssetIsReferenced 是否仍被某条工程的 deps 引用。
func (s *Store) AssetIsReferenced(ctx context.Context, assetID uuid.UUID) (bool, error) {
	// 编成包含条件，看有没有工程钉着它。
	raw, err := json.Marshal([]map[string]string{{"id": assetID.String()}})
	// 编不成正文就停，避免把坏内容写入。
	if err != nil {
		return false, err
	}
	// 准备接住行数，不能事先把零当成没有。
	var n int64
	// 数量没数出来就停，不能把零当成没有。
	if err := s.db.WithContext(ctx).Model(&assetRow{}).Where("deps @> ?", raw).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteAsset 物理删除一条平台级；调用方须先清授权并记撤回。
func (s *Store) DeleteAsset(ctx context.Context, assetID uuid.UUID) error {
	// 按身份删掉这条平台级资产。
	res := s.db.WithContext(ctx).Where("id = ?", assetID).Delete(&assetRow{})
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

// DeleteAssetAndRetract 清下发授权、记下撤回、再删原件。
func (s *Store) DeleteAssetAndRetract(ctx context.Context, assetID uuid.UUID) error {
	// 清授权、记撤回、删原件必须一起成功。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 删除失败就停，不能当成已经清掉。
		if err := tx.Where("asset_id = ?", assetID).Delete(&distGrantRow{}).Error; err != nil {
			return err
		}
		// 撤回身份幂等记下，厂端用来补送删除。
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&retractionRow{
			AssetID: assetID, CreatedAt: time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		// 按身份删掉原件，授权和撤回已经清过。
		res := tx.Where("id = ?", assetID).Delete(&assetRow{})
		// 写库报错就停，不能当成已经改成。
		if res.Error != nil {
			return res.Error
		}
		// 一行都没碰到，按不存在拒绝。
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// marshalAssetDeps 工艺不得带依赖；空切片落成 []。
func marshalAssetDeps(kind string, deps []AssetDep) ([]byte, error) {
	// 没带依赖就落成空表，避免写成空值。
	if deps == nil {
		// 没带依赖就落成空表，不写空值。
		deps = []AssetDep{}
	}
	// 工艺不能钉别的工艺，带了就拒绝。
	if kind == KindProcess && len(deps) > 0 {
		return nil, domain.ErrAssetDependency
	}
	// 依赖收成一段正文再落库。
	return json.Marshal(deps)
}

// assertAssetDigest 摘要必须是 32 字节 SHA-256。
func assertAssetDigest(d []byte) error {
	// 不是三十二字节就拒绝，摘要必须写全。
	if len(d) != 32 {
		return domain.ErrIntegrity
	}
	return nil
}

// mapAssetWriteErr 库约束收成业务错误，不抛 SQL。
func mapAssetWriteErr(err error) error {
	// 编号撞了就按冲突拒绝，不另插一行。
	if domain.IsUniqueViolation(err) {
		return domain.ErrAssetCodeConflict
	}
	// 库检查没过，按内容不完整拒绝。
	if domain.IsCheckViolation(err) {
		return domain.ErrIntegrity
	}
	// 引用的厂或资产不在名录，按不存在拒绝。
	if domain.IsForeignKeyViolation(err) {
		return domain.ErrNotFound
	}
	return err
}

// valueOrNew 没给身份就现场发号。
func valueOrNew(given uuid.UUID) uuid.UUID {
	// 调用方没给身份就现场发一个。
	if given == uuid.Nil {
		// 调用方没给身份，这里现场发一个。
		return id.New()
	}
	return given
}

// nonempty 空指针收成空切片，避免 NULL。
func nonempty(b []byte) []byte {
	// 空指针收成空切片，避免正文落成空值。
	if b == nil {
		return []byte{}
	}
	return b
}

// unmarshalAssetDeps 坏 JSON 当没有依赖，不当损坏。
func unmarshalAssetDeps(raw []byte) []AssetDep {
	// 没有依赖正文就当空表，不当成损坏。
	if len(raw) == 0 {
		return []AssetDep{}
	}
	// 依赖先留空，坏正文就当没有。
	var deps []AssetDep
	// 坏依赖或空值都当没有，不当成库损坏。
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
	// 只改正文，故意不动摘要，供完整性夹具。
	res := s.db.WithContext(ctx).Model(&assetRow{}).Where("id = ?", assetID).Update("content", nonempty(content))
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
