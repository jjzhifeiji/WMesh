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

// 运行事实桩的落库行，路径按当时快照。
type factRow struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey"` // 事实桩稳定身份
	CreatorID uuid.UUID  `gorm:"type:uuid;not null"`   // 创建账号稳定身份
	FactoryID uuid.UUID  `gorm:"type:uuid;not null"`   // 所属工厂
	OrgUnitID *uuid.UUID `gorm:"type:uuid"`            // 发生节点；直属工厂时为空
	OrgPath   []byte     `gorm:"type:jsonb;not null"`  // 快照原文，迁移不得改写
	CreatedAt time.Time  `gorm:"not null"`             // 发生时间
}

// 指定落库表名，避免查询时按类型名去猜。
func (factRow) TableName() string { return "fact_stubs" }

// 个人资产桩的落库行，只记归属。
type assetRow struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey"` // 个人资产桩稳定身份
	CreatorID uuid.UUID  `gorm:"type:uuid;not null"`   // 创建人；仅本人可读
	FactoryID uuid.UUID  `gorm:"type:uuid;not null"`   // 所属工厂
	OrgUnitID *uuid.UUID `gorm:"type:uuid"`            // 创建时节点；直属时为空
	OrgPath   []byte     `gorm:"type:jsonb;not null"`  // 创建时路径，改分配不改写
	Content   string     `gorm:"not null"`             // 内容正文；不进审计
	CreatedAt time.Time  `gorm:"not null"`             // 创建时间
}

// 指定落库表名，避免查询时按类型名去猜。
func (assetRow) TableName() string { return "personal_asset_stubs" }

// HasActiveAssignment 此人当前是否分在该节点。
func (s *Store) HasActiveAssignment(ctx context.Context, personID, unitID uuid.UUID) (bool, error) {
	// 准备承接计数，用来判断有没有匹配行。
	var n int64
	// 先接上要操作的表，结果留给紧跟着的判断。
	err := s.db.WithContext(ctx).Model(&Assignment{}).
		Where("person_id = ? AND org_unit_id = ? AND status = ?", personID, unitID, StatusActive).
		Count(&n).Error
	return n > 0, err
}

// PathSnapshot 按当前树从工厂走到该节点，记下当时身份和名称；写入后当原文。
func (s *Store) PathSnapshot(ctx context.Context, unitID uuid.UUID) ([]PathNode, error) {
	// 准备承接这次要交回的结果。
	var chain []PathNode
	// 从当前节点往上收集，直到厂根。
	current := unitID
	// 准备去重或保留集合，避免重复和误删。
	seen := map[uuid.UUID]struct{}{}
	// 一直往上走，遇到环或到根就停。
	for {
		// 这个节点走过就停，避免组织环死循环。
		if _, ok := seen[current]; ok {
			return nil, domain.ErrCycle
		}
		// 记下走过的节点，下一圈用来认环。
		seen[current] = struct{}{}
		// 先拿到这一步的结果，后面断言还要用。
		u, err := s.getUnit(ctx, current)
		// 读取组织节点失败就停，避免带着错误继续。
		if err != nil {
			return nil, err
		}
		// 收进结果，保持原来的先后顺序。
		chain = append(chain, PathNode{ID: u.ID, Name: u.Name})
		// 已经是根就停，根不能改父级或删除。
		if u.ParentID == nil {
			break
		}
		// 继续走向父级，直到根或发现环。
		current = *u.ParentID
	}
	// 把自叶到根的顺序倒成自根到叶。
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		// 交换两截，完成从叶到根的倒序。
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

// InsertFact 写入运行事实并钉死当时路径。
func (s *Store) InsertFact(ctx context.Context, creatorID uuid.UUID, unitID *uuid.UUID, path []PathNode) (FactStub, error) {
	// 先收成路径，结果留给紧跟着的判断。
	raw, err := marshalPath(path)
	// 收成路径失败就停，避免带着错误继续。
	if err != nil {
		return FactStub{}, err
	}
	// 组装事实桩，路径按当时快照冻结。
	row := factRow{
		ID:        id.New(),
		CreatorID: creatorID,
		FactoryID: s.factoryID,
		OrgUnitID: unitID,
		OrgPath:   raw,
		CreatedAt: time.Now().UTC(),
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return FactStub{}, err
	}
	// 库行收成对外结果再交回。
	return factFromRow(row), nil
}

// MergeFact 按产生端身份写入；已有且字段相同则原样返回，不同则拒绝覆盖。
func (s *Store) MergeFact(ctx context.Context, in FactStub) (FactStub, error) {
	// 没有身份就不能当有效目标。
	if in.ID == uuid.Nil {
		return FactStub{}, domain.ErrNotFound
	}
	// 先按身份读事实，结果留给紧跟着的判断。
	got, err := s.FactByID(ctx, in.ID)
	// 已经查到就按现有结果核对，不再插入。
	if err == nil {
		// 已有行字段不同不能覆盖。
		if got.CreatorID != in.CreatorID || !sameOptUUID(got.OrgUnitID, in.OrgUnitID) || !pathEqual(got.OrgPath, in.OrgPath) {
			return FactStub{}, domain.ErrIntegrity
		}
		return got, nil
	}
	// 不是找不到的错误要原样交回。
	if !errors.Is(err, domain.ErrNotFound) {
		return FactStub{}, err
	}
	// 先收成路径，结果留给紧跟着的判断。
	raw, err := marshalPath(in.OrgPath)
	// 收成路径失败就停，避免带着错误继续。
	if err != nil {
		return FactStub{}, err
	}
	// 组装事实桩，路径按当时快照冻结。
	row := factRow{
		ID:        in.ID,
		CreatorID: in.CreatorID,
		FactoryID: s.factoryID,
		OrgUnitID: in.OrgUnitID,
		OrgPath:   raw,
		CreatedAt: time.Now().UTC(),
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 唯一约束撞了就改成业务冲突，不抛库原文。
		if domain.IsUniqueViolation(err) {
			// 做完汇聚事实桩后把结果交回。
			return s.MergeFact(ctx, in)
		}
		// 外键对不上就当被指的对象不存在。
		if domain.IsForeignKeyViolation(err) {
			return FactStub{}, domain.ErrNotFound
		}
		return FactStub{}, err
	}
	// 库行收成对外结果再交回。
	return factFromRow(row), nil
}

// 两边可空身份是否指向同一条。
func sameOptUUID(a, b *uuid.UUID) bool {
	// 两边都没有就视为相同。
	if a == nil && b == nil {
		return true
	}
	// 只有一边有就视为已经变化。
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// pathEqual 两条路径快照是否同一原文。
func pathEqual(a, b []PathNode) bool {
	// 先收成路径，结果留给紧跟着的判断。
	ra, errA := marshalPath(a)
	// 先收成路径，结果留给紧跟着的判断。
	rb, errB := marshalPath(b)
	// 收成路径这一支不成立就换路。
	if errA != nil || errB != nil {
		return false
	}
	// 两边原文一致才算没有变化。
	return bytes.Equal(ra, rb)
}

// InsertAsset 写入个人资产桩并钉死当时路径。
func (s *Store) InsertAsset(ctx context.Context, creatorID uuid.UUID, unitID *uuid.UUID, path []PathNode, content string) (PersonalAsset, error) {
	// 先收成路径，结果留给紧跟着的判断。
	raw, err := marshalPath(path)
	// 收成路径失败就停，避免带着错误继续。
	if err != nil {
		return PersonalAsset{}, err
	}
	// 组装要落库的行，正文或元数据按入参填。
	row := assetRow{
		ID:        id.New(),
		CreatorID: creatorID,
		FactoryID: s.factoryID,
		OrgUnitID: unitID,
		OrgPath:   raw,
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return PersonalAsset{}, err
	}
	// 库行收成对外结果再交回。
	return assetFromRow(row), nil
}

// FactByID 按身份取事实桩。
func (s *Store) FactByID(ctx context.Context, factID uuid.UUID) (FactStub, error) {
	// 准备承接查到的事实桩。
	var row factRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", factID).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FactStub{}, domain.ErrNotFound
		}
		return FactStub{}, err
	}
	// 库行收成对外结果再交回。
	return factFromRow(row), nil
}

// AssetByID 只返回个人资产元数据和创建时路径，不含内容。
func (s *Store) AssetByID(ctx context.Context, assetID uuid.UUID) (PersonalAsset, error) {
	// 准备承接查到的个人资产桩。
	var row assetRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", assetID).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PersonalAsset{}, domain.ErrNotFound
		}
		return PersonalAsset{}, err
	}
	// 库行收成对外结果再交回。
	return assetFromRow(row), nil
}

// AssetContent 取出创建人与内容；是否给看由应用服务判定。
func (s *Store) AssetContent(ctx context.Context, assetID uuid.UUID) (creatorID uuid.UUID, content string, err error) {
	// 准备承接查到的个人资产桩。
	var row assetRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", assetID).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return uuid.Nil, "", domain.ErrNotFound
		}
		return uuid.Nil, "", err
	}
	return row.CreatorID, row.Content, nil
}

// FactsByCreator 列出某人产生的事实桩。
func (s *Store) FactsByCreator(ctx context.Context, creatorID uuid.UUID) ([]FactStub, error) {
	// 准备承接查到的多条事实桩。
	var rows []factRow
	// 按条件取多行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Where("creator_id = ?", creatorID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]FactStub, 0, len(rows))
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, r := range rows {
		// 收进结果，保持原来的先后顺序。
		out = append(out, factFromRow(r))
	}
	return out, nil
}

// marshalPath 路径收成 JSON；空当空数组。
func marshalPath(path []PathNode) ([]byte, error) {
	// 没有内容就当空列表，避免后面空指针。
	if path == nil {
		// 没有就交回空列表，避免调用方拿到空指针。
		path = []PathNode{}
	}
	// 收成正文再交回，空结构不落半截。
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
	// 收成路径这一支不成立就换路。
	if len(raw) == 0 {
		return []PathNode{}
	}
	// 准备承接查到的那一行。
	var path []PathNode
	// 没有内容就当空列表，避免后面空指针。
	if err := json.Unmarshal(raw, &path); err != nil || path == nil {
		return []PathNode{}
	}
	return path
}
