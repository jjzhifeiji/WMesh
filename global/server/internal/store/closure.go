package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
)

// ClosureMember 是闭包里的一条资产快照，含正文。
type ClosureMember struct {
	ID         uuid.UUID      `json:"id"`                   // 稳定身份
	Kind       string         `json:"kind"`                 // process / project
	Level      string         `json:"level"`                // 固定 platform
	Name       string         `json:"name"`                 // 显示名
	Code       string         `json:"code,omitempty"`       // 只读编号，跟身份走
	Status     string         `json:"status"`               // 组包时状态
	Copyable   bool           `json:"copyable"`             // 与源相同
	WeldKind   string         `json:"weldKind"`             // 作业类型：与源相同
	Revision   int64          `json:"revision"`             // 钉死修订
	Content    []byte         `json:"content"`              // 正文
	Digest     []byte         `json:"digest"`               // 内容 SHA-256
	Deps       []AssetDep     `json:"deps"`                 // 工艺必须空
	FSParentID *uuid.UUID     `json:"fsParentId,omitempty"` // 文件所在文件夹；空表示平台根
	FSPath     []FSFolderHint `json:"fsPath,omitempty"`     // 从靠近根到父文件夹，不含根；不进摘要
}

// ClosureSnapshot 是一份平台级工程或工艺的完整快照，不是新身份。
type ClosureSnapshot struct {
	Kind            string          `json:"kind"`            // process / project
	AssetID         uuid.UUID       `json:"assetId"`         // 根资产身份
	Revision        int64           `json:"revision"`        // 根修订
	Level           string          `json:"level"`           // 固定 platform
	Copyable        bool            `json:"copyable"`        // 与源相同
	Status          string          `json:"status"`          // 组包时状态
	TargetFactoryID *uuid.UUID      `json:"targetFactoryId"` // 目标工厂
	TargetClientID  *uuid.UUID      `json:"targetClientId"`  // WAN→厂为空
	Members         []ClosureMember `json:"members"`         // 根在前，其余按 deps 顺序
	Digest          []byte          `json:"digest"`          // 整包 SHA-256
}

// DistributionGrant 是某平台级资产可否下发到某厂。
type DistributionGrant struct {
	ID        uuid.UUID `json:"id"`        // 授权记录身份
	AssetID   uuid.UUID `json:"assetId"`   // 平台级资产
	FactoryID uuid.UUID `json:"factoryId"` // 目标工厂
	Active    bool      `json:"active"`    // 是否仍有效
	CreatedAt time.Time `json:"createdAt"` // 授权时间
	UpdatedAt time.Time `json:"updatedAt"` // 最近变更
}

// DistributionRecord 是向某厂下发过的一份修订，不含正文。
type DistributionRecord struct {
	ID            uuid.UUID  `json:"id"`            // 记录身份
	AssetID       uuid.UUID  `json:"assetId"`       // 根资产
	Revision      int64      `json:"revision"`      // 下发修订
	FactoryID     uuid.UUID  `json:"factoryId"`     // 目标工厂
	Kind          string     `json:"kind"`          // process / project
	ClosureDigest []byte     `json:"closureDigest"` // 整包摘要
	Members       []AssetDep `json:"members"`       // 成员身份+修订+摘要
	CreatedAt     time.Time  `json:"createdAt"`     // 首次下发时间
}

// 某资产对某厂是否仍可下发的库行。
type distGrantRow struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"` // 授权身份
	AssetID   uuid.UUID `gorm:"type:uuid;not null"`   // 平台级资产
	FactoryID uuid.UUID `gorm:"type:uuid;not null"`   // 工厂
	Active    bool      `gorm:"not null"`             // 是否有效
	CreatedAt time.Time `gorm:"not null"`             // 授权时间
	UpdatedAt time.Time `gorm:"not null"`             // 最近变更
}

// 下发授权落这张表，不跟默认复数走。
func (distGrantRow) TableName() string { return "distribution_grants" }

// 向某厂下发过的一份修订，不含正文。
type distRecordRow struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey"` // 记录身份
	AssetID       uuid.UUID `gorm:"type:uuid;not null"`   // 根资产
	Revision      int64     `gorm:"not null"`             // 修订
	FactoryID     uuid.UUID `gorm:"type:uuid;not null"`   // 工厂
	Kind          string    `gorm:"not null"`             // process / project
	ClosureDigest []byte    `gorm:"type:bytea;not null"`  // 整包摘要
	Members       []byte    `gorm:"type:jsonb;not null"`  // 成员 JSON
	CreatedAt     time.Time `gorm:"not null"`             // 首次下发
}

// 下发记录落这张表，不含包正文。
func (distRecordRow) TableName() string { return "distribution_records" }

// UpsertFactoryGrant 授予或重新激活某平台级资产到某厂。
func (s *Store) UpsertFactoryGrant(ctx context.Context, assetID, factoryID uuid.UUID) (DistributionGrant, error) {
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 准备接住已有的那一行，没有再决定插入。
	var existing distGrantRow
	// 先找这对资产和厂的授权，没有再插入。
	err := s.db.WithContext(ctx).First(&existing, "asset_id = ? AND factory_id = ?", assetID, factoryID).Error
	// 已经有这条授权，就地重新打开。
	if err == nil {
		// 已有授权则重新激活，不另开一行。
		existing.Active = true
		// 记下这次重新打开授权的时刻。
		existing.UpdatedAt = now
		// 写回失败就停，刚才改过的字段不算数。
		if err := s.db.WithContext(ctx).Save(&existing).Error; err != nil {
			return DistributionGrant{}, err
		}
		// 重新打开后的授权收成视图交回。
		return grantFromRow(existing), nil
	}
	// 不是没有这行，是读取失败，不能当成该新建。
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return DistributionGrant{}, err
	}
	// 组一条新的下发授权，一开始就是有效的。
	row := distGrantRow{
		ID: id.New(), AssetID: assetID, FactoryID: factoryID,
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	// 这一行没写进去就停，不能当成已经落库。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 引用的厂或资产不在名录，按不存在拒绝。
		if domain.IsForeignKeyViolation(err) {
			return DistributionGrant{}, domain.ErrNotFound
		}
		return DistributionGrant{}, err
	}
	// 新授权收成视图再交回。
	return grantFromRow(row), nil
}

// RevokeFactoryGrant 收回某资产对某厂的下发授权。
func (s *Store) RevokeFactoryGrant(ctx context.Context, assetID, factoryID uuid.UUID) error {
	// 把对该厂的授权收成无效，行先留着。
	res := s.db.WithContext(ctx).Model(&distGrantRow{}).Where("asset_id = ? AND factory_id = ?", assetID, factoryID).Updates(map[string]any{
		"active":     false,
		"updated_at": time.Now().UTC(),
	})
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

// FactoryGrant 读某资产对某厂的授权。
func (s *Store) FactoryGrant(ctx context.Context, assetID, factoryID uuid.UUID) (DistributionGrant, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row distGrantRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "asset_id = ? AND factory_id = ?", assetID, factoryID).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DistributionGrant{}, domain.ErrNotFound
		}
		return DistributionGrant{}, err
	}
	// 新授权收成视图再交回。
	return grantFromRow(row), nil
}

// InsertDistributionRecord 写下发记录；同一资产修订对同一厂幂等。
func (s *Store) InsertDistributionRecord(ctx context.Context, rec DistributionRecord) (DistributionRecord, error) {
	// 摘要不是完整的三十二字节就停。
	if err := assertAssetDigest(rec.ClosureDigest); err != nil {
		return DistributionRecord{}, err
	}
	// 成员收成正文再入库，失败就不记这次下发。
	members, err := json.Marshal(rec.Members)
	// 编不成正文就停，避免把坏内容写入。
	if err != nil {
		return DistributionRecord{}, err
	}
	// 组一条下发记录，成员正文已经编好。
	row := distRecordRow{
		ID: id.New(), AssetID: rec.AssetID, Revision: rec.Revision, FactoryID: rec.FactoryID,
		Kind: rec.Kind, ClosureDigest: rec.ClosureDigest, Members: members, CreatedAt: time.Now().UTC(),
	}
	// 写入失败先停住，再看是重复还是约束没过。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 同一资产修订对同一厂只记一次。
		if domain.IsUniqueViolation(err) {
			// 同一修订已经记过，交回原来的记录。
			return s.DistributionRecord(ctx, rec.AssetID, rec.Revision, rec.FactoryID)
		}
		// 引用的厂或资产不在名录，按不存在拒绝。
		if domain.IsForeignKeyViolation(err) {
			return DistributionRecord{}, domain.ErrNotFound
		}
		return DistributionRecord{}, err
	}
	// 库行收成下发记录，成员从正文解开。
	return recordFromRow(row), nil
}

// DistributionRecord 读向某厂下发过的一份修订。
func (s *Store) DistributionRecord(ctx context.Context, assetID uuid.UUID, revision int64, factoryID uuid.UUID) (DistributionRecord, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row distRecordRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "asset_id = ? AND revision = ? AND factory_id = ?", assetID, revision, factoryID).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DistributionRecord{}, domain.ErrNotFound
		}
		return DistributionRecord{}, err
	}
	// 库行收成下发记录，成员从正文解开。
	return recordFromRow(row), nil
}

// HasDistributionTo 是否曾向该厂下发过该资产任一修订。
func (s *Store) HasDistributionTo(ctx context.Context, assetID, factoryID uuid.UUID) (bool, error) {
	// 准备接住行数，不能事先把零当成没有。
	var n int64
	// 数量没数出来就停，不能把零当成没有。
	if err := s.db.WithContext(ctx).Model(&distRecordRow{}).Where("asset_id = ? AND factory_id = ?", assetID, factoryID).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// 库行收成下发授权视图。
func grantFromRow(row distGrantRow) DistributionGrant {
	return DistributionGrant{
		ID: row.ID, AssetID: row.AssetID, FactoryID: row.FactoryID, Active: row.Active,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

// 库行收成下发记录视图。
func recordFromRow(row distRecordRow) DistributionRecord {
	return DistributionRecord{
		ID: row.ID, AssetID: row.AssetID, Revision: row.Revision, FactoryID: row.FactoryID,
		Kind: row.Kind, ClosureDigest: row.ClosureDigest, Members: unmarshalAssetDeps(row.Members),
		CreatedAt: row.CreatedAt,
	}
}

// 已删平台级资产的身份，供厂端补送删除。
type retractionRow struct {
	AssetID   uuid.UUID `gorm:"type:uuid;primaryKey"` // 已删除身份
	CreatedAt time.Time `gorm:"not null"`             // 删除时间
}

// 已删身份落这张表，供厂端补送删除。
func (retractionRow) TableName() string { return "asset_retractions" }

// ListRetractions 列出须补送给厂的已删平台级身份。
func (s *Store) ListRetractions(ctx context.Context) ([]uuid.UUID, error) {
	// 准备接住查出来的列表，空的也要能交回。
	var rows []retractionRow
	// 列表没读出来就停，故障不能当成空表。
	if err := s.db.WithContext(ctx).Order("created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按行数预留结果，查空也是空表不是空指针。
	out := make([]uuid.UUID, 0, len(rows))
	// 逐行收成对外结果，顺序保持查询原来的样子。
	for _, row := range rows {
		// 只交出已删资产的身份，供厂端补送。
		out = append(out, row.AssetID)
	}
	return out, nil
}
