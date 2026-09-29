package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
)

const (
	SoftwareWANService     = "wan_service"     // 云端 API 与管理台一包
	SoftwareFactoryService = "factory_service" // 厂端 API 与管理台一包
	SoftwareClientAPK      = "client_apk"      // 当前 Android 客户端包
)

// WANSigningKey 是云端签发软件包的密钥；全表一行，私钥不进审计。
type WANSigningKey struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`       // 行身份
	PublicKey  []byte    `gorm:"type:bytea;not null" json:"publicKey"` // Ed25519 公钥 32 字节
	PrivateKey []byte    `gorm:"type:bytea;not null" json:"-"`         // 云端签发私钥
	CreatedAt  time.Time `gorm:"not null" json:"createdAt"`            // 写入时间
}

// 云端签发密钥落这张表，全表只留一行。
func (WANSigningKey) TableName() string { return "wan_signing_keys" }

// SoftwareRelease 是一条已发布软件版本的元数据，不含包字节。
type SoftwareRelease struct {
	Kind        string    `json:"kind"`        // wan_service / factory_service / client_apk
	Version     int64     `json:"version"`     // 单调整数
	VersionName string    `json:"versionName"` // 给人看的版本名
	Digest      []byte    `json:"digest"`      // SHA-256
	ObjectKey   string    `json:"objectKey"`   // 对象键
	CreatedAt   time.Time `json:"createdAt"`   // 首次发布
}

// 一条已发布软件版本的库行，不含包字节。
type softwareReleaseRow struct {
	Kind        string    `gorm:"primaryKey"`          // wan_service / factory_service / client_apk
	Version     int64     `gorm:"primaryKey"`          // 单调整数
	VersionName string    `gorm:"not null"`            // 给人看的版本名
	Digest      []byte    `gorm:"type:bytea;not null"` // SHA-256
	ObjectKey   string    `gorm:"not null"`            // 对象键
	CreatedAt   time.Time `gorm:"not null"`            // 首次发布
}

// 软件发布元数据落这张表，不含包字节。
func (softwareReleaseRow) TableName() string { return "software_releases" }

// SoftwareDistribution 是向某厂下发过的一份版本。
type SoftwareDistribution struct {
	Kind      string    `json:"kind"`      // 与发布种类相同
	Version   int64     `json:"version"`   // 下发版本
	FactoryID uuid.UUID `json:"factoryId"` // 目标厂
	Signature []byte    `json:"signature"` // WAN 对目标厂的签名
	CreatedAt time.Time `json:"createdAt"` // 首次下发
}

// 向某厂下发过的一份软件版本库行。
type softwareDistRow struct {
	Kind      string    `gorm:"primaryKey"`           // 种类
	Version   int64     `gorm:"primaryKey"`           // 版本
	FactoryID uuid.UUID `gorm:"type:uuid;primaryKey"` // 目标厂
	Signature []byte    `gorm:"type:bytea;not null"`  // 对目标厂的签名
	CreatedAt time.Time `gorm:"not null"`             // 首次下发
}

// 软件下发记录落这张表，按厂和版本区分。
func (softwareDistRow) TableName() string { return "software_distributions" }

// SoftwareObjectKey 按种类和版本生成对象键。
func SoftwareObjectKey(kind string, version int64) string {
	// 对象键按种类和版本拼死，同一版永远同一处。
	return fmt.Sprintf("software/%s/%d", kind, version)
}

// PutWANSigningKey 写入云端唯一签发密钥；已有则拒绝。
func (s *Store) PutWANSigningKey(ctx context.Context, publicKey, privateKey []byte) (WANSigningKey, error) {
	// 组好唯一签发密钥，身份现发，私钥只留这里。
	row := WANSigningKey{ID: id.New(), PublicKey: publicKey, PrivateKey: privateKey, CreatedAt: time.Now().UTC()}
	// 写入失败先停住，再看是重复还是约束没过。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 签发密钥只能有一行，已有就拒绝。
		if domain.IsUniqueViolation(err) {
			return WANSigningKey{}, domain.ErrSigningKeyExists
		}
		// 长度或格式不合格，按坏钥拒绝。
		if domain.IsCheckViolation(err) {
			return WANSigningKey{}, domain.ErrInvalidKey
		}
		return WANSigningKey{}, err
	}
	return row, nil
}

// WANSigningKey 取云端唯一签发密钥，含私钥。
func (s *Store) WANSigningKey(ctx context.Context) (WANSigningKey, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row WANSigningKey
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return WANSigningKey{}, domain.ErrNotFound
		}
		return WANSigningKey{}, err
	}
	return row, nil
}

// 库行收成发布视图。
func releaseFromRow(row softwareReleaseRow) SoftwareRelease {
	return SoftwareRelease{
		Kind: row.Kind, Version: row.Version, VersionName: row.VersionName,
		Digest: row.Digest, ObjectKey: row.ObjectKey, CreatedAt: row.CreatedAt,
	}
}

// InsertSoftwareRelease 写入一条发布；同种类同版本同摘要则原样返回。
func (s *Store) InsertSoftwareRelease(ctx context.Context, in SoftwareRelease) (SoftwareRelease, error) {
	// 摘要不是完整的三十二字节就停。
	if err := assertAssetDigest(in.Digest); err != nil {
		return SoftwareRelease{}, err
	}
	// 先看这个版本在不在，在则核对是不是同一包。
	got, err := s.SoftwareRelease(ctx, in.Kind, in.Version)
	// 这一版已经在，先核对是不是同一包。
	if err == nil {
		// 同版本的摘要或名字对不上，不能悄悄换包。
		if !bytes.Equal(got.Digest, in.Digest) || got.VersionName != in.VersionName {
			return SoftwareRelease{}, domain.ErrIntegrity
		}
		return got, nil
	}
	// 不是还没有，读取失败就不能当成可新增。
	if !errors.Is(err, domain.ErrNotFound) {
		return SoftwareRelease{}, err
	}
	// 组一条发布元数据，摘要必须和包一致。
	row := softwareReleaseRow{
		Kind: in.Kind, Version: in.Version, VersionName: in.VersionName,
		Digest: in.Digest, ObjectKey: in.ObjectKey, CreatedAt: time.Now().UTC(),
	}
	// 写入失败先停住，再看是重复还是约束没过。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 已经有同一份就交回原行，不插第二份。
		if domain.IsUniqueViolation(err) {
			// 并发下已经写下，交回库里那一条发布。
			return s.SoftwareRelease(ctx, in.Kind, in.Version)
		}
		// 名字不合格，不把库检查原文抛出去。
		if domain.IsCheckViolation(err) {
			return SoftwareRelease{}, domain.ErrInvalidName
		}
		return SoftwareRelease{}, err
	}
	// 库行收成发布元数据再交回。
	return releaseFromRow(row), nil
}

// SoftwareRelease 按种类和版本读发布元数据。
func (s *Store) SoftwareRelease(ctx context.Context, kind string, version int64) (SoftwareRelease, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row softwareReleaseRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "kind = ? AND version = ?", kind, version).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SoftwareRelease{}, domain.ErrNotFound
		}
		return SoftwareRelease{}, err
	}
	// 库行收成发布元数据再交回。
	return releaseFromRow(row), nil
}

// 库行收成下发记录。
func distFromRow(row softwareDistRow) SoftwareDistribution {
	return SoftwareDistribution{
		Kind: row.Kind, Version: row.Version, FactoryID: row.FactoryID,
		Signature: row.Signature, CreatedAt: row.CreatedAt,
	}
}

// InsertSoftwareDistribution 记下向某厂下发；同键幂等。
func (s *Store) InsertSoftwareDistribution(ctx context.Context, in SoftwareDistribution) (SoftwareDistribution, error) {
	// 先看这份下发在不在，在则直接交回。
	got, err := s.SoftwareDistribution(ctx, in.Kind, in.Version, in.FactoryID)
	// 这一份已经下过，直接交回原记录。
	if err == nil {
		return got, nil
	}
	// 不是还没有，读取失败就不能当成可新增。
	if !errors.Is(err, domain.ErrNotFound) {
		return SoftwareDistribution{}, err
	}
	// 组一条下发记录，签名是给这家厂的。
	row := softwareDistRow{
		Kind: in.Kind, Version: in.Version, FactoryID: in.FactoryID,
		Signature: in.Signature, CreatedAt: time.Now().UTC(),
	}
	// 写入失败先停住，再看是重复还是约束没过。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 已经有同一份就交回原行，不插第二份。
		if domain.IsUniqueViolation(err) {
			// 这一份已经下过，交回原来的记录。
			return s.SoftwareDistribution(ctx, in.Kind, in.Version, in.FactoryID)
		}
		// 引用的厂或资产不在名录，按不存在拒绝。
		if domain.IsForeignKeyViolation(err) {
			return SoftwareDistribution{}, domain.ErrNotFound
		}
		// 长度或格式不合格，按坏钥拒绝。
		if domain.IsCheckViolation(err) {
			return SoftwareDistribution{}, domain.ErrInvalidKey
		}
		return SoftwareDistribution{}, err
	}
	// 库行收成软件下发记录再交回。
	return distFromRow(row), nil
}

// SoftwareDistribution 读向某厂下发过的一份版本。
func (s *Store) SoftwareDistribution(ctx context.Context, kind string, version int64, factoryID uuid.UUID) (SoftwareDistribution, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row softwareDistRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "kind = ? AND version = ? AND factory_id = ?", kind, version, factoryID).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SoftwareDistribution{}, domain.ErrNotFound
		}
		return SoftwareDistribution{}, err
	}
	// 库行收成软件下发记录再交回。
	return distFromRow(row), nil
}

// MaxSoftwareDistributed 该厂该种类已下发的最高版本；没有则为 0。
func (s *Store) MaxSoftwareDistributed(ctx context.Context, kind string, factoryID uuid.UUID) (int64, error) {
	// 准备接住行数，不能事先把零当成没有。
	var n int64
	// 取这家厂该种类已下发的最高版本，没有则当零。
	err := s.db.WithContext(ctx).Model(&softwareDistRow{}).
		Where("kind = ? AND factory_id = ?", kind, factoryID).
		Select("COALESCE(MAX(version), 0)").Scan(&n).Error
	return n, err
}

// ListSoftwareReleases 列出已发布版本；kind 空则两类都给，高版本在前。
func (s *Store) ListSoftwareReleases(ctx context.Context, kind string) ([]SoftwareRelease, error) {
	// 高版本排在前面，没指定种类就两类都给。
	q := s.db.WithContext(ctx).Model(&softwareReleaseRow{}).Order("kind ASC, version DESC")
	// 指定了种类就只列这一类发布。
	if kind != "" {
		// 指定了种类就只留这一类发布。
		q = q.Where("kind = ?", kind)
	}
	// 准备接住查出来的列表，空的也要能交回。
	var rows []softwareReleaseRow
	// 列表没读出来就停，故障不能当成空表。
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按行数预留结果，查空也是空表不是空指针。
	out := make([]SoftwareRelease, 0, len(rows))
	// 逐行收成对外结果，顺序保持查询原来的样子。
	for _, row := range rows {
		// 这一行收成发布元数据，放进结果。
		out = append(out, releaseFromRow(row))
	}
	return out, nil
}

// DeleteSoftwareRelease 删掉该（种类, 版本）发布行。
func (s *Store) DeleteSoftwareRelease(ctx context.Context, kind string, version int64) error {
	// 删掉这个种类和版本的发布行。
	res := s.db.WithContext(ctx).Where("kind = ? AND version = ?", kind, version).Delete(&softwareReleaseRow{})
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

// MaxSoftwareRelease 该种类已发布最高版本；没有则为 0。
func (s *Store) MaxSoftwareRelease(ctx context.Context, kind string) (int64, error) {
	// 准备接住行数，不能事先把零当成没有。
	var n int64
	// 取该种类已发布的最高版本，没有则当零。
	err := s.db.WithContext(ctx).Model(&softwareReleaseRow{}).
		Where("kind = ?", kind).
		Select("COALESCE(MAX(version), 0)").Scan(&n).Error
	return n, err
}

// 某类软件已确认落地的版本。
type softwareStateRow struct {
	Kind             string    `gorm:"primaryKey"` // 种类
	InstalledVersion int64     `gorm:"not null"`   // 已装版本
	UpdatedAt        time.Time `gorm:"not null"`   // 最近确认
}

// 已落地版本落这张表，按种类各一行。
func (softwareStateRow) TableName() string { return "software_state" }

// InstalledSoftware 读该种类已确认落地版本；没有行则为 0。
func (s *Store) InstalledSoftware(ctx context.Context, kind string) (int64, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row softwareStateRow
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "kind = ?", kind).Error; err != nil {
		// 还没有落地记录就当版本零，不算故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return row.InstalledVersion, nil
}

// PutInstalledSoftware 记下落地成功的版本，只向前。
func (s *Store) PutInstalledSoftware(ctx context.Context, kind string, version int64) error {
	// 先读已落地的版本，只允许往更高走。
	cur, err := s.InstalledSoftware(ctx, kind)
	// 已落地版本没读到就停，不能判断能不能往前装。
	if err != nil {
		return err
	}
	// 不能往回装，只接受比现在更高的版本。
	if version <= cur {
		return domain.ErrStaleRevision
	}
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 还没落地过就新插一行，不走覆盖更新。
	if cur == 0 {
		// 第一次落地，把版本和时刻一起写下。
		row := softwareStateRow{Kind: kind, InstalledVersion: version, UpdatedAt: now}
		// 第一次写入失败就停，检查没过再收成业务错误。
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			// 检查约束没过就停，不把库原文抛出去。
			if domain.IsCheckViolation(err) {
				return domain.ErrInvalidName
			}
			return err
		}
		return nil
	}
	// 只在仍是刚才读到的版本时往前推。
	res := s.db.WithContext(ctx).Model(&softwareStateRow{}).Where("kind = ? AND installed_version = ?", kind, cur).
		Updates(map[string]any{"installed_version": version, "updated_at": now})
	// 写库报错就停，不能当成已经改成。
	if res.Error != nil {
		return res.Error
	}
	// 版本已被别人推进，这次不覆盖。
	if res.RowsAffected == 0 {
		return domain.ErrStaleRevision
	}
	return nil
}

// ListSoftwareDistributions 列出已下到该厂的各份版本，按种类和版本升序。
func (s *Store) ListSoftwareDistributions(ctx context.Context, factoryID uuid.UUID) ([]SoftwareDistribution, error) {
	// 准备接住查出来的列表，空的也要能交回。
	var rows []softwareDistRow
	// 列表没读出来就停，故障不能当成空表。
	if err := s.db.WithContext(ctx).Where("factory_id = ?", factoryID).Order("kind ASC, version ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按行数预留结果，查空也是空表不是空指针。
	out := make([]SoftwareDistribution, 0, len(rows))
	// 逐行收成对外结果，顺序保持查询原来的样子。
	for _, row := range rows {
		// 这一行收成下发记录，放进结果。
		out = append(out, distFromRow(row))
	}
	return out, nil
}
