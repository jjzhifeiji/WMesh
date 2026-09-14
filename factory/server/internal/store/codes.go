package store

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"wmesh/factory/internal/platform/assetcode"
	"wmesh/factory/internal/platform/domain"
)

type assetCodeSeqRow struct {
	Kind  string `gorm:"primaryKey"`             // process / project
	NextN int64  `gorm:"column:next_n;not null"` // 下一个本厂序号
}

func (assetCodeSeqRow) TableName() string { return "asset_code_seq" }

type assetCodeRow struct {
	ID   uuid.UUID `gorm:"type:uuid;primaryKey"` // 资产稳定身份
	Code string    `gorm:"not null"`             // 该身份的只读编号
}

func (assetCodeRow) TableName() string { return "asset_codes" }

// nextFactoryAssetCode 行锁取出下一个本厂编号。
func nextFactoryAssetCode(tx *gorm.DB, kind, origin string) (string, error) {
	var row assetCodeSeqRow
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "kind = ?", kind).Error; err != nil {
		return "", err
	}
	code, err := assetcode.Format(kind, origin, row.NextN)
	if err != nil {
		return "", err
	}
	if err := tx.Model(&assetCodeSeqRow{}).Where("kind = ?", kind).Update("next_n", row.NextN+1).Error; err != nil {
		return "", err
	}
	return code, nil
}

// bindAssetCode 把编号钉在身份上；同身份必须同号，同号不得给另一身份。
func bindAssetCode(tx *gorm.DB, id uuid.UUID, code string) error {
	if !assetcode.Valid(code) {
		return domain.ErrAssetCodeConflict
	}
	var existing assetCodeRow
	err := tx.Where("id = ?", id).First(&existing).Error
	if err == nil {
		if existing.Code != code {
			return domain.ErrAssetCodeConflict
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err := tx.Create(&assetCodeRow{ID: id, Code: code}).Error; err != nil {
		if domain.IsUniqueViolation(err) || domain.IsCheckViolation(err) {
			return domain.ErrAssetCodeConflict
		}
		return err
	}
	return nil
}
