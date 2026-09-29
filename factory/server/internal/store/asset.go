package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/assetcode"
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
	WeldKind        string     `json:"weldKind"`        // 作业类型：与源相同
	Status          string     `json:"status"`          // 源状态
	Deps            []AssetDep `json:"deps"`            // 源依赖（身份+修订+摘要）
}

// Asset 是本厂一条工艺或工程的当前行，不含历史正文。
type Asset struct {
	ID             uuid.UUID  `json:"id"`             // 稳定身份
	Kind           string     `json:"kind"`           // process / project
	Level          string     `json:"level"`          // factory / personal
	Name           string     `json:"name"`           // 显示名，不当身份
	Code           string     `json:"code"`           // 只读编号，创建后不改
	Status         string     `json:"status"`         // draft / available / disabled
	Copyable       bool       `json:"copyable"`       // 可否升档
	WeldKind       string     `json:"weldKind"`       // 作业类型：single / multilayer / tbar
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
	Content        []byte     // 新正文；KeepContent 时忽略
	Digest         []byte     // 与正文对应的摘要
	Copyable       bool       // 可复制
	Status         string     // 状态
	Deps           []AssetDep // 工程依赖；工艺必须空
	WeldKind       string     // 作业类型；空则不改
	SourceRevision *int64     // 升档覆盖时更新源修订；空则不改
	KeepContent    bool       // 只改元数据，保留库内原文
}

// 工艺或工程的落库行，正文可按租约封装。
type governedAssetRow struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey"`      // 稳定身份
	Kind           string     `gorm:"not null"`                  // process / project
	Level          string     `gorm:"not null"`                  // factory / personal
	Name           string     `gorm:"not null"`                  // 显示名
	Code           string     `gorm:"not null"`                  // 只读编号
	Status         string     `gorm:"not null"`                  // draft / available / disabled
	Copyable       bool       `gorm:"not null"`                  // 可否升档
	WeldKind       string     `gorm:"column:weld_kind;not null"` // 作业类型
	Revision       int64      `gorm:"not null"`                  // 当前修订
	Content        []byte     `gorm:"type:bytea;not null"`       // 正文
	Digest         []byte     `gorm:"type:bytea;not null"`       // SHA-256
	CreatorID      uuid.UUID  `gorm:"type:uuid;not null"`        // 创建人
	FactoryID      uuid.UUID  `gorm:"type:uuid;not null"`        // 所属本厂
	OrgUnitID      *uuid.UUID `gorm:"type:uuid"`                 // 创建时节点
	OrgPath        []byte     `gorm:"type:jsonb;not null"`       // 路径快照
	SourceID       *uuid.UUID `gorm:"type:uuid"`                 // 升档源
	SourceRevision *int64     `gorm:"column:source_revision"`    // 升档源修订
	Deps           []byte     `gorm:"type:jsonb;not null"`       // 依赖 JSON
	CreatedAt      time.Time  `gorm:"not null"`                  // 创建时间
	UpdatedAt      time.Time  `gorm:"not null"`                  // 最近升高修订的时间
}

// 指定落库表名，避免查询时按类型名去猜。
func (governedAssetRow) TableName() string { return "assets" }

// InsertGovernedAsset 写入本厂一条工艺或工程，修订从 1 起；正文必须封成 WM2。
func (s *Store) InsertGovernedAsset(ctx context.Context, in Asset) (Asset, error) {
	// 人必须已经在本厂，否则不继续写。
	if err := s.assertPersonExists(ctx, in.CreatorID); err != nil {
		return Asset{}, err
	}
	// 挂了节点就要确认节点还在，避免悬空归属。
	if in.OrgUnitID != nil {
		// 读取组织节点失败就停，避免带着错误继续。
		if _, err := s.getUnit(ctx, *in.OrgUnitID); err != nil {
			return Asset{}, err
		}
	}
	// 先收成依赖，结果留给紧跟着的判断。
	deps, err := marshalAssetDeps(in.Kind, in.Deps)
	// 收成依赖失败就停，避免带着错误继续。
	if err != nil {
		return Asset{}, err
	}
	// 先收成路径，结果留给紧跟着的判断。
	path, err := marshalPath(in.OrgPath)
	// 收成路径失败就停，避免带着错误继续。
	if err != nil {
		return Asset{}, err
	}
	// 摘要必须是三十二字节，否则拒绝写入。
	if err := assertAssetDigest(in.Digest); err != nil {
		return Asset{}, err
	}
	// 先规范作业类型，结果留给紧跟着的判断。
	weldKind, err := NormalizeWeldKind(in.WeldKind)
	// 规范作业类型失败就停，避免带着错误继续。
	if err != nil {
		return Asset{}, err
	}
	// 先接调用方给的身份，空的再另发。
	id := valueOrNew(in.ID)
	// 先封装正文，结果留给紧跟着的判断。
	env, err := s.persistBody(id, 1, tableAssets, nonempty(in.Content))
	// 封装正文失败就停，避免带着错误继续。
	if err != nil {
		return Asset{}, err
	}
	// 取当前时刻，时间列和租约用同一个时钟。
	now := time.Now().UTC()
	// 准备承接查到的工艺或工程。
	var row governedAssetRow
	// 放进同一事务，中途失败就整单回滚。
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先接调用方带来的编号，空的再现发。
		code := in.Code
		// 没带编号就按本厂短码现发，不接收空号。
		if code == "" {
			// 先读本厂短码，结果留给紧跟着的判断。
			origin, err := s.FactoryShortCode(ctx)
			// 读本厂短码失败就停，避免带着错误继续。
			if err != nil {
				return err
			}
			// 先取下一个编号，结果留给紧跟着的判断。
			code, err = nextFactoryAssetCode(tx, in.Kind, origin)
			// 取下一个编号失败就停，避免带着错误继续。
			if err != nil {
				return err
			}
			// 自带编号必须对上种类，否则拒绝写入。
		} else if !assetcode.MatchKind(in.Kind, code) {
			return domain.ErrAssetCodeConflict
		}
		// 钉住编号失败就停，避免带着错误继续。
		if err := bindAssetCode(tx, id, code); err != nil {
			return err
		}
		// 组装要落库的行，编号和摘要已经核对。
		row = governedAssetRow{
			ID:             id,
			Kind:           in.Kind,
			Level:          in.Level,
			Name:           in.Name,
			Code:           code,
			Status:         in.Status,
			Copyable:       in.Copyable,
			WeldKind:       weldKind,
			Revision:       1,
			Content:        env,
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
		// 写入一行失败就停，避免带着错误继续。
		if err := tx.Create(&row).Error; err != nil {
			// 先把库错误译成业务错误再交回。
			return mapAssetWriteErr(err)
		}
		// 做完挂上文件节点后把结果交回。
		return placeAssetFileTx(tx, row.Kind, row.Level, ownerPtr(row.Level, row.CreatorID), row.ID, row.Name, row.Code, uuid.Nil)
	})
	// 出错就停，避免把失败当成已经完成。
	if err != nil {
		return Asset{}, err
	}
	// 库行收成对外结果再交回。
	return s.decodeGoverned(row)
}

// UpdateGovernedAsset 按期望修订改当前行；对不上则原件不变。
func (s *Store) UpdateGovernedAsset(ctx context.Context, assetID uuid.UUID, expected int64, w AssetWrite) (Asset, error) {
	// 摘要必须是三十二字节，否则拒绝写入。
	if err := assertAssetDigest(w.Digest); err != nil {
		return Asset{}, err
	}
	// 取当前时刻，时间列和租约用同一个时钟。
	now := time.Now().UTC()
	// 准备承接这次要交回的结果。
	var out Asset
	// 放进同一事务，中途失败就整单回滚。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 准备承接查到的工艺或工程。
		var row governedAssetRow
		// 按条件取一行失败就停，避免带着错误继续。
		if err := tx.First(&row, "id = ?", assetID).Error; err != nil {
			// 没有这一行就当成不存在。
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		// 期望修订对不上则原件不变。
		if row.Revision != expected {
			return domain.ErrRevisionConflict
		}
		// 先收成依赖，结果留给紧跟着的判断。
		depsJSON, err := marshalAssetDeps(row.Kind, w.Deps)
		// 收成依赖失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 只收集这次要改的列，避免整行覆盖。
		updates := map[string]any{
			"name":       w.Name,
			"digest":     w.Digest,
			"copyable":   w.Copyable,
			"status":     w.Status,
			"deps":       depsJSON,
			"revision":   expected + 1,
			"updated_at": now,
		}
		// 这次只改元数据，正文保持库里的原样。
		if !w.KeepContent {
			// 改正文必须封 WM2。
			env, err := s.persistBody(assetID, expected+1, tableAssets, nonempty(w.Content))
			// 封装正文失败就停，避免带着错误继续。
			if err != nil {
				return err
			}
			// 把这一列放进本次更新。
			updates["content"] = env
			// 按新修订重封失败就回滚，不能留半截。
		} else if env, changed, err := s.rewrapIfSealed(assetID, expected, expected+1, tableAssets, row.Content); err != nil {
			return err
			// 重封确实变了才写回，没变就保持原信封。
		} else if changed {
			// 把这一列放进本次更新。
			updates["content"] = env
		}
		// 带了作业类型才改，空着表示保持原样。
		if w.WeldKind != "" {
			// 先规范作业类型，结果留给紧跟着的判断。
			kind, err := NormalizeWeldKind(w.WeldKind)
			// 规范作业类型失败就停，避免带着错误继续。
			if err != nil {
				return err
			}
			// 把这一列放进本次更新。
			updates["weld_kind"] = kind
		}
		// 带了源修订才覆盖，避免把升档来源抹掉。
		if w.SourceRevision != nil {
			// 把这一列放进本次更新。
			updates["source_revision"] = *w.SourceRevision
		}
		// 只改点名的列，避免把别的字段写成零值。
		res := tx.Model(&governedAssetRow{}).Where("id = ? AND revision = ?", assetID, expected).Updates(updates)
		// 更新失败则交回库错误，不能当成已经改完。
		if res.Error != nil {
			// 先把库错误译成业务错误再交回。
			return mapAssetWriteErr(res.Error)
		}
		// 并发下期望修订对不上则原件不变。
		if res.RowsAffected == 0 {
			return domain.ErrRevisionConflict
		}
		// 按条件取一行失败就停，避免带着错误继续。
		if err := tx.First(&row, "id = ?", assetID).Error; err != nil {
			return err
		}
		// 同步文件名失败就停，避免带着错误继续。
		if err := syncFSFileNameTx(tx, assetID, w.Name); err != nil {
			return err
		}
		// 这次只改元数据，正文保持库里的原样。
		if w.KeepContent {
			// 先收成工艺视图，结果留给紧跟着的判断。
			out = assetFromGoverned(row)
			// 这次不带正文，调用方只要元数据。
			out.Content = nil
			return nil
		}
		// 先解开工艺正文，结果留给紧跟着的判断。
		out, err = s.decodeGoverned(row)
		return err
	})
	return out, err
}

// ListGovernedAssets 列出本厂工艺/工程当前行，不含正文；按创建时间从新到旧。
func (s *Store) ListGovernedAssets(ctx context.Context) ([]Asset, error) {
	// 准备承接查到的多条工艺或工程。
	var rows []governedAssetRow
	// 按条件取多行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Omit("Content").Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]Asset, 0, len(rows))
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, row := range rows {
		// 收进结果，保持原来的先后顺序。
		out = append(out, assetFromGoverned(row))
	}
	return out, nil
}

// GovernedAssetByID 读本厂一条工艺/工程当前行，含正文。
func (s *Store) GovernedAssetByID(ctx context.Context, assetID uuid.UUID) (Asset, error) {
	// 准备承接查到的工艺或工程。
	var row governedAssetRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", assetID).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Asset{}, domain.ErrNotFound
		}
		return Asset{}, err
	}
	// 库行收成对外结果再交回。
	return s.decodeGoverned(row)
}

// GovernedAssetMetaByID 读本厂当前行元数据，不解包正文。
func (s *Store) GovernedAssetMetaByID(ctx context.Context, assetID uuid.UUID) (Asset, error) {
	// 准备承接查到的工艺或工程。
	var row governedAssetRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Omit("Content").First(&row, "id = ?", assetID).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Asset{}, domain.ErrNotFound
		}
		return Asset{}, err
	}
	// 库行收成对外结果再交回。
	return assetFromGoverned(row), nil
}

// GovernedAssetByCode 按只读编号取本厂当前行元数据。
func (s *Store) GovernedAssetByCode(ctx context.Context, code string) (Asset, error) {
	// 准备承接查到的工艺或工程。
	var row governedAssetRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Omit("Content").First(&row, "code = ?", code).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Asset{}, domain.ErrNotFound
		}
		return Asset{}, err
	}
	// 库行收成对外结果再交回。
	return assetFromGoverned(row), nil
}

// GovernedAssetBySourceID 按升档源身份找本厂已升档条目。
func (s *Store) GovernedAssetBySourceID(ctx context.Context, sourceID uuid.UUID) (Asset, error) {
	// 准备承接查到的工艺或工程。
	var row governedAssetRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Where("source_id = ?", sourceID).Order("updated_at DESC").First(&row).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Asset{}, domain.ErrNotFound
		}
		return Asset{}, err
	}
	// 库行收成对外结果再交回。
	return s.decodeGoverned(row)
}

// AssetIsReferenced 是否仍被某条工程的 deps 引用。
func (s *Store) AssetIsReferenced(ctx context.Context, assetID uuid.UUID) (bool, error) {
	// 收成正文再往下用，避免半截结构落库。
	raw, err := json.Marshal([]map[string]string{{"id": assetID.String()}})
	// 收成正文失败就停，避免带着错误继续。
	if err != nil {
		return false, err
	}
	// 准备承接计数，用来判断有没有匹配行。
	var n int64
	// 计数失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Model(&governedAssetRow{}).Where("deps @> ?", raw).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteGovernedAsset 物理删除本厂一条；调用方须先确认未被依赖。
func (s *Store) DeleteGovernedAsset(ctx context.Context, assetID uuid.UUID) error {
	// 放进同一事务，中途失败就整单回滚。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 删除匹配行失败就停，避免带着错误继续。
		if err := tx.Where("asset_id = ? AND node_kind = ?", assetID, FSNodeFile).Delete(&fsNodeRow{}).Error; err != nil {
			return err
		}
		// 删掉匹配行，没有行交给后面判断。
		res := tx.Where("id = ?", assetID).Delete(&governedAssetRow{})
		// 更新失败则交回库错误，不能当成已经改完。
		if res.Error != nil {
			return res.Error
		}
		// 没有改到行就当目标不存在。
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// ExportAssetSnapshot 导出升平台用快照；不改原件。
func (s *Store) ExportAssetSnapshot(ctx context.Context, assetID uuid.UUID) (AssetSnapshot, error) {
	// 先拿到这一步的结果，后面断言还要用。
	a, err := s.GovernedAssetByID(ctx, assetID)
	// 按身份读正文失败就停，避免带着错误继续。
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
		WeldKind:        a.WeldKind,
		Status:          a.Status,
		Deps:            a.Deps,
	}, nil
}

// marshalAssetDeps 工艺不得带依赖；空切片落成 []。
func marshalAssetDeps(kind string, deps []AssetDep) ([]byte, error) {
	// 空内容换成空切片，避免库里留下空指针。
	if deps == nil {
		// 没有就交回空列表，避免调用方拿到空指针。
		deps = []AssetDep{}
	}
	// 工艺不允许带依赖，有就拒绝。
	if kind == KindProcess && len(deps) > 0 {
		return nil, domain.ErrAssetDependency
	}
	// 收成正文再交回，空结构不落半截。
	return json.Marshal(deps)
}

// unmarshalAssetDeps 坏 JSON 当没有依赖，不当损坏。
func unmarshalAssetDeps(raw []byte) []AssetDep {
	// 收成依赖这一支不成立就换路。
	if len(raw) == 0 {
		return []AssetDep{}
	}
	// 准备承接查到的那一行。
	var deps []AssetDep
	// 空内容换成空切片，避免库里留下空指针。
	if err := json.Unmarshal(raw, &deps); err != nil || deps == nil {
		return []AssetDep{}
	}
	return deps
}

// assertAssetDigest 摘要必须是 32 字节 SHA-256。
func assertAssetDigest(d []byte) error {
	// 长度不是三十二字节就拒绝。
	if len(d) != 32 {
		return domain.ErrIntegrity
	}
	return nil
}

// mapAssetWriteErr 库约束收成业务错误，不抛 SQL。
func mapAssetWriteErr(err error) error {
	// 唯一约束撞了就改成业务冲突，不抛库原文。
	if domain.IsUniqueViolation(err) {
		return domain.ErrAssetCodeConflict
	}
	// 检查约束不通过就改成业务拒绝。
	if domain.IsCheckViolation(err) {
		return domain.ErrIntegrity
	}
	// 外键对不上就当被指的对象不存在。
	if domain.IsForeignKeyViolation(err) {
		return domain.ErrNotFound
	}
	return err
}

// valueOrNew 没给身份就现场发号。
func valueOrNew(given uuid.UUID) uuid.UUID {
	// 没有身份就不能当有效目标。
	if given == uuid.Nil {
		// 没有指定身份就现发一个。
		return id.New()
	}
	return given
}

// nonempty 空指针收成空切片，避免 NULL。
func nonempty(b []byte) []byte {
	// 空内容换成空切片，避免库里留下空指针。
	if b == nil {
		return []byte{}
	}
	return b
}

// 库行收成本厂资产视图。
func assetFromGoverned(row governedAssetRow) Asset {
	return Asset{
		ID:             row.ID,
		Kind:           row.Kind,
		Level:          row.Level,
		Name:           row.Name,
		Code:           row.Code,
		Status:         row.Status,
		Copyable:       row.Copyable,
		WeldKind:       row.WeldKind,
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
	// 只改点名的列，避免把别的字段写成零值。
	res := s.db.WithContext(ctx).Model(&governedAssetRow{}).Where("id = ?", assetID).Update("content", nonempty(content))
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// TamperAssetDeps 只改依赖 JSON 不升修订，供缺失夹具使用。
func (s *Store) TamperAssetDeps(ctx context.Context, assetID uuid.UUID, deps []AssetDep) error {
	// 收成正文再往下用，避免半截结构落库。
	raw, err := json.Marshal(deps)
	// 收成正文失败就停，避免带着错误继续。
	if err != nil {
		return err
	}
	// 只改点名的列，避免把别的字段写成零值。
	res := s.db.WithContext(ctx).Model(&governedAssetRow{}).Where("id = ?", assetID).Update("deps", raw)
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
