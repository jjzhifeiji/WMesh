package store

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"wmesh/factory/internal/platform/assetcode"
	"wmesh/factory/internal/platform/domain"
)

// 本厂编号发号行，每个种类一行。
type assetCodeSeqRow struct {
	Kind  string `gorm:"primaryKey"`             // process / project
	NextN int64  `gorm:"column:next_n;not null"` // 下一个本厂序号
}

// 指定落库表名，避免查询时按类型名去猜。
func (assetCodeSeqRow) TableName() string { return "asset_code_seq" }

// 资产身份与只读编号的对应行。
type assetCodeRow struct {
	ID   uuid.UUID `gorm:"type:uuid;primaryKey"` // 资产稳定身份
	Code string    `gorm:"not null"`             // 该身份的只读编号
}

// 指定落库表名，避免查询时按类型名去猜。
func (assetCodeRow) TableName() string { return "asset_codes" }

// nextFactoryAssetCode 行锁取出下一个本厂编号。
func nextFactoryAssetCode(tx *gorm.DB, kind, origin string) (string, error) {
	// 准备承接查到的发号行。
	var row assetCodeSeqRow
	// 行锁读发号行失败就停，避免带着错误继续。
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "kind = ?", kind).Error; err != nil {
		return "", err
	}
	// 按种类、本厂短码和序号拼出编号。
	code, err := assetcode.Format(kind, origin, row.NextN)
	// 拼出编号失败就停，避免带着错误继续。
	if err != nil {
		return "", err
	}
	// 更新指定列失败就停，避免带着错误继续。
	if err := tx.Model(&assetCodeSeqRow{}).Where("kind = ?", kind).Update("next_n", row.NextN+1).Error; err != nil {
		return "", err
	}
	return code, nil
}

// bindAssetCode 把编号钉在身份上；同身份必须同号，同号不得给另一身份。
func bindAssetCode(tx *gorm.DB, id uuid.UUID, code string) error {
	// 核对编号这一支不成立就换路。
	if !assetcode.Valid(code) {
		return domain.ErrAssetCodeConflict
	}
	// 准备承接查到的编号对应。
	var existing assetCodeRow
	// 按条件去读，没有行交给后面的分支。
	err := tx.Where("id = ?", id).First(&existing).Error
	// 已经查到就按现有结果核对，不再插入。
	if err == nil {
		// 同一身份不能改成另一个号。
		if existing.Code != code {
			return domain.ErrAssetCodeConflict
		}
		return nil
	}
	// 不是缺行的库错误要原样交回。
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := tx.Create(&assetCodeRow{ID: id, Code: code}).Error; err != nil {
		if domain.IsUniqueViolation(err) || domain.IsCheckViolation(err) {
			return domain.ErrAssetCodeConflict
		}
		return err
	}
	return nil
}
