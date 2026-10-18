package store

import (
	"context"
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

// callerAssetCodeTx 核对示教器带来的身份和编号，交回要落的号；空串表示由本厂现发。
func (s *Store) callerAssetCodeTx(ctx context.Context, tx *gorm.DB, id uuid.UUID, kind, code string) (string, error) {
	// 先查身份是否已是本厂原件或已收副本。
	taken, err := assetIDTakenTx(tx, id)
	// 查身份失败就停，避免带着错误继续。
	if err != nil {
		return "", err
	}
	// 已有原件或副本的身份不能再建，免得冒充平台工艺。
	if taken {
		return "", domain.ErrAssetCodeConflict
	}
	// 再读这个身份以前登记过的编号。
	prev, err := registeredAssetCodeTx(tx, id)
	// 读登记编号失败就停，避免带着错误继续。
	if err != nil {
		return "", err
	}
	// 本厂删过的身份只能沿用原号重建。
	if prev != "" {
		// 带了别的号就拒绝，同一身份不能换号。
		if code != "" && code != prev {
			return "", domain.ErrAssetCodeConflict
		}
		// 读本厂短码，用来认出他厂 F 段。
		own, err := s.FactoryShortCode(ctx)
		// 短码还没齐时按空串比对，只放过 F00 和 C 段。
		if errors.Is(err, domain.ErrAssetCodeMissing) {
			// 没有本厂短码就当空串，错误不再往外交。
			own, err = "", nil
		}
		// 读短码遇到别的库错误就停。
		if err != nil {
			return "", err
		}
		// 原号是外来号段或种类对不上，只能是历史污染，拒绝沿用。
		if foreignOrigin(assetcode.Origin(prev), own) || !assetcode.MatchKind(kind, prev) {
			return "", domain.ErrAssetCodeConflict
		}
		return prev, nil
	}
	// 新身份不带号就交给本厂按短码现发。
	if code == "" {
		return "", nil
	}
	// 取出自带号的创建端短码。
	origin := assetcode.Origin(code)
	// 自带号必须对上种类且是 C 段，F 段和 W 段不收。
	if !assetcode.MatchKind(kind, code) || !assetcode.ValidClientOrigin(origin) {
		return "", domain.ErrAssetCodeConflict
	}
	// 再查这个 C 段是否在本厂登记过。
	known, err := clientOriginKnownTx(tx, origin)
	// 查登记失败就停，避免带着错误继续。
	if err != nil {
		return "", err
	}
	// 没登记过的短码不收，免得占死别人的号。
	if !known {
		return "", domain.ErrAssetCodeConflict
	}
	return code, nil
}

// assetIDTakenTx 身份是否已在本厂原件或副本里；副本禁删，行不会消失。
func assetIDTakenTx(tx *gorm.DB, id uuid.UUID) (bool, error) {
	// 准备承接查询结果。
	var taken bool
	// 两张表任一有这个身份就算已占用。
	err := tx.Raw("SELECT EXISTS (SELECT 1 FROM assets WHERE id = ?) OR EXISTS (SELECT 1 FROM asset_replicas WHERE id = ?)", id, id).Scan(&taken).Error
	return taken, err
}

// registeredAssetCodeTx 读该身份登记过的编号；没登记回空串。
func registeredAssetCodeTx(tx *gorm.DB, id uuid.UUID) (string, error) {
	// 准备承接查到的编号对应。
	var rows []assetCodeRow
	// 按身份去读，没有行就是空切片。
	if err := tx.Where("id = ?", id).Limit(1).Find(&rows).Error; err != nil {
		return "", err
	}
	// 没有登记过就回空串，不算出错。
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].Code, nil
}

// clientOriginKnownTx 本厂是否登记过这个 Client 短码；作废的设备也认。
func clientOriginKnownTx(tx *gorm.DB, origin string) (bool, error) {
	// 准备承接计数。
	var n int64
	// 不看状态，作废前离线发出的号仍是真号。
	err := tx.Model(&Client{}).Where("short_code = ?", origin).Count(&n).Error
	return n > 0, err
}

// foreignOrigin 短码是云端或他厂 F 段时为真；F00 是本厂迁移旧号。
func foreignOrigin(origin, own string) bool {
	// 云端号只随副本落，原件持有就是外来；F 段只认本厂。
	return origin == assetcode.OriginWAN || (assetcode.ValidFactoryOrigin(origin) && origin != own)
}
