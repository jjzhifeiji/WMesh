package store

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"wmesh/global/internal/platform/assetcode"
)

const (
	originKindFactory = "factory" // 厂短码计数
	originKindClient  = "client"  // Client 短码计数
)

type originCodeSeqRow struct {
	Kind  string `gorm:"primaryKey"` // factory / client
	NextN int64  `gorm:"column:next_n;not null"` // 下一个序号
}

func (originCodeSeqRow) TableName() string { return "origin_code_seq" }

type assetCodeSeqRow struct {
	Kind  string `gorm:"primaryKey"` // process / project
	NextN int64  `gorm:"column:next_n;not null"` // 下一个 W 序号
}

func (assetCodeSeqRow) TableName() string { return "asset_code_seq" }

// nextOriginCode 行锁取出下一个厂或 Client 短码。
func nextOriginCode(tx *gorm.DB, kind string) (string, error) {
	var row originCodeSeqRow
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "kind = ?", kind).Error; err != nil {
		return "", err
	}
	var code string
	var err error
	if kind == originKindFactory {
		code, err = assetcode.FormatFactory(row.NextN)
	} else {
		code, err = assetcode.FormatClient(row.NextN)
	}
	if err != nil {
		return "", err
	}
	if err := tx.Model(&originCodeSeqRow{}).Where("kind = ?", kind).Update("next_n", row.NextN+1).Error; err != nil {
		return "", err
	}
	return code, nil
}

// nextWANAssetCode 行锁取出下一个平台级编号。
func nextWANAssetCode(tx *gorm.DB, kind string) (string, error) {
	var row assetCodeSeqRow
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "kind = ?", kind).Error; err != nil {
		return "", err
	}
	code, err := assetcode.Format(kind, assetcode.OriginWAN, row.NextN)
	if err != nil {
		return "", err
	}
	if err := tx.Model(&assetCodeSeqRow{}).Where("kind = ?", kind).Update("next_n", row.NextN+1).Error; err != nil {
		return "", err
	}
	return code, nil
}
