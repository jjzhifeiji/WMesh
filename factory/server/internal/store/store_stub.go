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

func sameOptUUID(a, b *uuid.UUID) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func pathEqual(a, b []PathNode) bool {
	ra, errA := marshalPath(a)
	rb, errB := marshalPath(b)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(ra, rb)
}

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

func (s *Store) FactsByCreator(ctx context.Context, creatorID uuid.UUID) ([]FactStub, error) {
	var rows []factRow
	if err := s.db.WithContext(ctx).Where("creator_id = ?", creatorID).Order("created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]FactStub, 0, len(rows))
	for _, r := range rows {
		out = append(out, factFromRow(r))
	}
	return out, nil
}

func marshalPath(path []PathNode) ([]byte, error) {
	if path == nil {
		path = []PathNode{}
	}
	return json.Marshal(path)
}

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
