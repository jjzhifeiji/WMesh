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
	"wmesh/factory/internal/platform/id"
)

// PathNode 是事实发生时路径上的一截：当时的身份和名称。
type PathNode struct {
	ID     uuid.UUID  `json:"id"`               // 节点稳定身份
	TypeID *uuid.UUID `json:"typeId,omitempty"` // 旧快照可能带类型身份；新写入不再写
	Name   string     `json:"name"`             // 当时的显示名，之后改名也不改这里
}

// WorkContext 是产生事实或个人资产时必须明确选的一个上下文。
type WorkContext struct {
	Direct    bool       // true 表示 Factory 直属，路径为空
	OrgUnitID *uuid.UUID // 与 Direct 互斥；必须是本人当前分配的有效节点
}

// FactStub 是最小运行事实：只记创建人和发生时的组织路径，供统计口径验收。
type FactStub struct {
	ID        uuid.UUID  // 事实桩稳定身份
	CreatorID uuid.UUID  // 创建账号稳定身份
	FactoryID uuid.UUID  // 所属工厂
	OrgUnitID *uuid.UUID // 发生节点；直属工厂时为空
	OrgPath   []PathNode // 当时从工厂到该节点的祖先快照；直属为空
	CreatedAt time.Time  // 发生时间；路径快照此后不得改写
}

// PersonalAsset 是最小个人级资产桩，不是真实工艺/工程；内容不因超管身份打开。
type PersonalAsset struct {
	ID        uuid.UUID  // 个人资产桩稳定身份
	CreatorID uuid.UUID  // 创建人；仅本人可读
	FactoryID uuid.UUID  // 所属工厂
	OrgUnitID *uuid.UUID // 创建时节点；直属时为空
	OrgPath   []PathNode // 创建时路径，改分配不改写
	CreatedAt time.Time  // 创建时间
}

type factRow struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey"` // 事实桩稳定身份
	CreatorID uuid.UUID  `gorm:"type:uuid;not null"`   // 创建账号稳定身份
	FactoryID uuid.UUID  `gorm:"type:uuid;not null"`   // 所属工厂
	OrgUnitID *uuid.UUID `gorm:"type:uuid"`            // 发生节点；直属工厂时为空
	OrgPath   []byte     `gorm:"type:jsonb;not null"`  // 快照原文，迁移不得改写
	CreatedAt time.Time  `gorm:"not null"`             // 发生时间
}

func (factRow) TableName() string { return "fact_stubs" }

type assetRow struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey"` // 个人资产桩稳定身份
	CreatorID uuid.UUID  `gorm:"type:uuid;not null"`   // 创建人；仅本人可读
	FactoryID uuid.UUID  `gorm:"type:uuid;not null"`   // 所属工厂
	OrgUnitID *uuid.UUID `gorm:"type:uuid"`            // 创建时节点；直属时为空
	OrgPath   []byte     `gorm:"type:jsonb;not null"`  // 创建时路径，改分配不改写
	Content   string     `gorm:"not null"`             // 内容正文；不进审计
	CreatedAt time.Time  `gorm:"not null"`             // 创建时间
}

func (assetRow) TableName() string { return "personal_asset_stubs" }

// HasActiveAssignment 此人当前是否分在该节点。
func (s *Store) HasActiveAssignment(ctx context.Context, personID, unitID uuid.UUID) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Assignment{}).
		Where("person_id = ? AND org_unit_id = ? AND status = ?", personID, unitID, StatusActive).
		Count(&n).Error
	return n > 0, err
}

// PathSnapshot 按当前树从工厂走到该节点，记下当时身份和名称；写入后当原文。
func (s *Store) PathSnapshot(ctx context.Context, unitID uuid.UUID) ([]PathNode, error) {
	var chain []PathNode
	current := unitID
	seen := map[uuid.UUID]struct{}{}
	for {
		if _, ok := seen[current]; ok {
			return nil, domain.ErrCycle
		}
		seen[current] = struct{}{}
		u, err := s.getUnit(ctx, current)
		if err != nil {
			return nil, err
		}
		chain = append(chain, PathNode{ID: u.ID, Name: u.Name})
		if u.ParentID == nil {
			break
		}
		current = *u.ParentID
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

// InsertFact 写入运行事实并钉死当时路径。
func (s *Store) InsertFact(ctx context.Context, creatorID uuid.UUID, unitID *uuid.UUID, path []PathNode) (FactStub, error) {
	raw, err := marshalPath(path)
	if err != nil {
		return FactStub{}, err
	}
	row := factRow{
		ID:        id.New(),
		CreatorID: creatorID,
		FactoryID: s.factoryID,
		OrgUnitID: unitID,
		OrgPath:   raw,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return FactStub{}, err
	}
	return factFromRow(row), nil
}

// MergeFact 按产生端身份写入；已有且字段相同则原样返回，不同则拒绝覆盖。
func (s *Store) MergeFact(ctx context.Context, in FactStub) (FactStub, error) {
	if in.ID == uuid.Nil {
		return FactStub{}, domain.ErrNotFound
	}
	got, err := s.FactByID(ctx, in.ID)
	if err == nil {
		// 已有行字段不同不能覆盖。
		if got.CreatorID != in.CreatorID || !sameOptUUID(got.OrgUnitID, in.OrgUnitID) || !pathEqual(got.OrgPath, in.OrgPath) {
			return FactStub{}, domain.ErrIntegrity
		}
		return got, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return FactStub{}, err
	}
	raw, err := marshalPath(in.OrgPath)
	if err != nil {
		return FactStub{}, err
	}
	row := factRow{
		ID:        in.ID,
		CreatorID: in.CreatorID,
		FactoryID: s.factoryID,
		OrgUnitID: in.OrgUnitID,
		OrgPath:   raw,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return s.MergeFact(ctx, in)
		}
		if domain.IsForeignKeyViolation(err) {
			return FactStub{}, domain.ErrNotFound
		}
		return FactStub{}, err
	}
	return factFromRow(row), nil
}

// 两边可空身份是否指向同一条。
func sameOptUUID(a, b *uuid.UUID) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// pathEqual 两条路径快照是否同一原文。
func pathEqual(a, b []PathNode) bool {
	ra, errA := marshalPath(a)
	rb, errB := marshalPath(b)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(ra, rb)
}

// InsertAsset 写入个人资产桩并钉死当时路径。
func (s *Store) InsertAsset(ctx context.Context, creatorID uuid.UUID, unitID *uuid.UUID, path []PathNode, content string) (PersonalAsset, error) {
	raw, err := marshalPath(path)
	if err != nil {
		return PersonalAsset{}, err
	}
	row := assetRow{
		ID:        id.New(),
		CreatorID: creatorID,
		FactoryID: s.factoryID,
		OrgUnitID: unitID,
		OrgPath:   raw,
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return PersonalAsset{}, err
	}
	return assetFromRow(row), nil
}

// FactByID 按身份取事实桩。
func (s *Store) FactByID(ctx context.Context, factID uuid.UUID) (FactStub, error) {
	var row factRow
	if err := s.db.WithContext(ctx).First(&row, "id = ?", factID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FactStub{}, domain.ErrNotFound
		}
		return FactStub{}, err
	}
	return factFromRow(row), nil
}

// AssetByID 只返回个人资产元数据和创建时路径，不含内容。
func (s *Store) AssetByID(ctx context.Context, assetID uuid.UUID) (PersonalAsset, error) {
	var row assetRow
	if err := s.db.WithContext(ctx).First(&row, "id = ?", assetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PersonalAsset{}, domain.ErrNotFound
		}
		return PersonalAsset{}, err
	}
	return assetFromRow(row), nil
}

// AssetContent 取出创建人与内容；是否给看由应用服务判定。
func (s *Store) AssetContent(ctx context.Context, assetID uuid.UUID) (creatorID uuid.UUID, content string, err error) {
	var row assetRow
	if err := s.db.WithContext(ctx).First(&row, "id = ?", assetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return uuid.Nil, "", domain.ErrNotFound
		}
		return uuid.Nil, "", err
	}
	return row.CreatorID, row.Content, nil
}

// FactsByCreator 列出某人产生的事实桩。
func (s *Store) FactsByCreator(ctx context.Context, creatorID uuid.UUID) ([]FactStub, error) {
	var rows []factRow
	if err := s.db.WithContext(ctx).Where("creator_id = ?", creatorID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]FactStub, 0, len(rows))
	for _, r := range rows {
		out = append(out, factFromRow(r))
	}
	return out, nil
}

// marshalPath 路径收成 JSON；空当空数组。
func marshalPath(path []PathNode) ([]byte, error) {
	if path == nil {
		path = []PathNode{}
	}
	return json.Marshal(path)
}

// 库行收成事实桩视图。
func factFromRow(row factRow) FactStub {
	return FactStub{
		ID:        row.ID,
		CreatorID: row.CreatorID,
		FactoryID: row.FactoryID,
		OrgUnitID: row.OrgUnitID,
		OrgPath:   unmarshalPath(row.OrgPath),
		CreatedAt: row.CreatedAt,
	}
}

// 库行收成个人资产桩元数据。
func assetFromRow(row assetRow) PersonalAsset {
	return PersonalAsset{
		ID:        row.ID,
		CreatorID: row.CreatorID,
		FactoryID: row.FactoryID,
		OrgUnitID: row.OrgUnitID,
		OrgPath:   unmarshalPath(row.OrgPath),
		CreatedAt: row.CreatedAt,
	}
}

// unmarshalPath 坏 JSON 当空路径，不当损坏。
func unmarshalPath(raw []byte) []PathNode {
	if len(raw) == 0 {
		return []PathNode{}
	}
	var path []PathNode
	if err := json.Unmarshal(raw, &path); err != nil || path == nil {
		return []PathNode{}
	}
	return path
}
