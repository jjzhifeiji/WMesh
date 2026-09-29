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

// 厂或设备短码的下一个序号，取出时加行锁。
type originCodeSeqRow struct {
	Kind  string `gorm:"primaryKey"`             // factory / client
	NextN int64  `gorm:"column:next_n;not null"` // 下一个序号
}

// 短码序号落这张表，按来源各留一行。
func (originCodeSeqRow) TableName() string { return "origin_code_seq" }

// 平台级编号的下一个序号，取出时加行锁。
type assetCodeSeqRow struct {
	Kind  string `gorm:"primaryKey"`             // process / project
	NextN int64  `gorm:"column:next_n;not null"` // 下一个 W 序号
}

// 平台编号序号落这张表，按种类一行。
func (assetCodeSeqRow) TableName() string { return "asset_code_seq" }

// nextOriginCode 行锁取出下一个厂或 Client 短码。
func nextOriginCode(tx *gorm.DB, kind string) (string, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row originCodeSeqRow
	// 锁不住计数行就停，避免没锁住还发号。
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "kind = ?", kind).Error; err != nil {
		return "", err
	}
	// 短码先留空，编成功再填进去。
	var code string
	// 编号失败的原因单独留着，成功时它是空的。
	var err error
	// 厂来源用厂短码的格式来编。
	if kind == originKindFactory {
		// 按当前序号编出厂短码。
		code, err = assetcode.FormatFactory(row.NextN)
		// 其余来源按设备短码的格式来编。
	} else {
		// 按当前序号编出设备短码。
		code, err = assetcode.FormatClient(row.NextN)
	}
	// 短码编不出来就停，序号也不能往下拨。
	if err != nil {
		return "", err
	}
	// 序号没拨上去就停，否则下一个号会重。
	if err := tx.Model(&originCodeSeqRow{}).Where("kind = ?", kind).Update("next_n", row.NextN+1).Error; err != nil {
		return "", err
	}
	return code, nil
}

// nextWANAssetCode 行锁取出下一个平台级编号。
func nextWANAssetCode(tx *gorm.DB, kind string) (string, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row assetCodeSeqRow
	// 锁不住计数行就停，避免没锁住还发号。
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "kind = ?", kind).Error; err != nil {
		return "", err
	}
	// 按平台来源和序号编出编号。
	code, err := assetcode.Format(kind, assetcode.OriginWAN, row.NextN)
	// 编号编不出来就停，序号也不能往下拨。
	if err != nil {
		return "", err
	}
	// 序号没拨上去就停，否则下一个号会重。
	if err := tx.Model(&assetCodeSeqRow{}).Where("kind = ?", kind).Update("next_n", row.NextN+1).Error; err != nil {
		return "", err
	}
	return code, nil
}
