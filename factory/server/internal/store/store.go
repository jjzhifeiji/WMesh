// Package store 只读写本厂库：人员、组织、角色、会话、归属桩、本厂 Client 凭证、本厂工艺/工程、已收平台级副本、内容模版副本和上传记录。
// 不判定允许/拒绝，也不回调应用服务。
// 文件按域拆：account / org / attr / node / asset / template / closure / sync / lifecycle。
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
)

// Store 只打开这一家工厂的库，按工厂稳定身份选库，不搞单库多厂。
type Store struct {
	db        *gorm.DB
	factoryID uuid.UUID // 本厂稳定身份，写入事实时带上
}

// Open 打开这一家工厂的库；factoryID 是选库用的稳定身份，不从名称推导。
func Open(db *gorm.DB, factoryID uuid.UUID) *Store {
	return &Store{db: db, factoryID: factoryID}
}

func (s *Store) FactoryID() uuid.UUID { return s.factoryID }

func (s *Store) AppendAudit(ctx context.Context, e audit.Event) error {
	if e.ID == uuid.Nil {
		e.ID = id.New()
	}
	if e.FactoryID == nil {
		fid := s.factoryID
		e.FactoryID = &fid
	}
	return s.db.WithContext(ctx).Create(audit.RowFrom(e)).Error
}

// ListAudit 按发生时间从新到旧列出审计行。
func (s *Store) ListAudit(ctx context.Context) ([]audit.Row, error) {
	var rows []audit.Row
	err := s.db.WithContext(ctx).Order("occurred_at DESC").Find(&rows).Error
	return rows, err
}

func hasRows(db *gorm.DB, model any, query string, args ...any) (bool, error) {
	var n int64
	err := db.Model(model).Where(query, args...).Count(&n).Error
	return n > 0, err
}

// pathMentions 看事实/资产路径快照里是否出现过该身份。
func pathMentions(db *gorm.DB, key string, id uuid.UUID) (bool, error) {
	payload := fmt.Sprintf(`[{"%s":"%s"}]`, key, id)
	for _, table := range []string{"fact_stubs", "personal_asset_stubs", "assets"} {
		var n int64
		err := db.Raw("SELECT COUNT(*) FROM "+table+" WHERE org_path @> ?::jsonb", payload).Scan(&n).Error
		if err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

// setStatus 只改状态列，找不到行就当不存在。
func (s *Store) setStatus(ctx context.Context, model any, id uuid.UUID, status string) error {
	res := s.db.WithContext(ctx).Model(model).Where("id = ?", id).Update("status", status)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) getUnit(ctx context.Context, unitID uuid.UUID) (OrgUnit, error) {
	var u OrgUnit
	if err := s.db.WithContext(ctx).First(&u, "id = ?", unitID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return OrgUnit{}, domain.ErrNotFound
		}
		return OrgUnit{}, err
	}
	return u, nil
}

func (s *Store) assertUnitActive(ctx context.Context, unitID uuid.UUID) error {
	u, err := s.getUnit(ctx, unitID)
	if err != nil {
		return err
	}
	if u.Status != StatusActive {
		return domain.ErrDisabledOrgUnit
	}
	return nil
}

func (s *Store) assertPersonExists(ctx context.Context, personID uuid.UUID) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&Person{}).Where("id = ?", personID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
