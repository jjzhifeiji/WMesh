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

type softwareReleaseRow struct {
	Kind        string    `gorm:"primaryKey"`          // wan_service / factory_service / client_apk
	Version     int64     `gorm:"primaryKey"`          // 单调整数
	VersionName string    `gorm:"not null"`            // 给人看的版本名
	Digest      []byte    `gorm:"type:bytea;not null"` // SHA-256
	ObjectKey   string    `gorm:"not null"`            // 对象键
	CreatedAt   time.Time `gorm:"not null"`            // 首次发布
}

func (softwareReleaseRow) TableName() string { return "software_releases" }

// SoftwareDistribution 是向某厂下发过的一份版本。
type SoftwareDistribution struct {
	Kind      string    `json:"kind"`      // 与发布种类相同
	Version   int64     `json:"version"`   // 下发版本
	FactoryID uuid.UUID `json:"factoryId"` // 目标厂
	Signature []byte    `json:"signature"` // WAN 对目标厂的签名
	CreatedAt time.Time `json:"createdAt"` // 首次下发
}

type softwareDistRow struct {
	Kind      string    `gorm:"primaryKey"`           // 种类
	Version   int64     `gorm:"primaryKey"`           // 版本
	FactoryID uuid.UUID `gorm:"type:uuid;primaryKey"` // 目标厂
	Signature []byte    `gorm:"type:bytea;not null"`  // 对目标厂的签名
	CreatedAt time.Time `gorm:"not null"`             // 首次下发
}

func (softwareDistRow) TableName() string { return "software_distributions" }

// SoftwareObjectKey 按种类和版本生成对象键。
func SoftwareObjectKey(kind string, version int64) string {
	return fmt.Sprintf("software/%s/%d", kind, version)
}

// PutWANSigningKey 写入云端唯一签发密钥；已有则拒绝。
func (s *Store) PutWANSigningKey(ctx context.Context, publicKey, privateKey []byte) (WANSigningKey, error) {
	row := WANSigningKey{ID: id.New(), PublicKey: publicKey, PrivateKey: privateKey, CreatedAt: time.Now().UTC()}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return WANSigningKey{}, domain.ErrSigningKeyExists
		}
		if domain.IsCheckViolation(err) {
			return WANSigningKey{}, domain.ErrInvalidKey
		}
		return WANSigningKey{}, err
	}
	return row, nil
}

// WANSigningKey 取云端唯一签发密钥，含私钥。
func (s *Store) WANSigningKey(ctx context.Context) (WANSigningKey, error) {
	var row WANSigningKey
	if err := s.db.WithContext(ctx).First(&row).Error; err != nil {
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
	if err := assertAssetDigest(in.Digest); err != nil {
		return SoftwareRelease{}, err
	}
	got, err := s.SoftwareRelease(ctx, in.Kind, in.Version)
	if err == nil {
		if !bytes.Equal(got.Digest, in.Digest) || got.VersionName != in.VersionName {
			return SoftwareRelease{}, domain.ErrIntegrity
		}
		return got, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return SoftwareRelease{}, err
	}
	row := softwareReleaseRow{
		Kind: in.Kind, Version: in.Version, VersionName: in.VersionName,
		Digest: in.Digest, ObjectKey: in.ObjectKey, CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return s.SoftwareRelease(ctx, in.Kind, in.Version)
		}
		if domain.IsCheckViolation(err) {
			return SoftwareRelease{}, domain.ErrInvalidName
		}
		return SoftwareRelease{}, err
	}
	return releaseFromRow(row), nil
}

// SoftwareRelease 按种类和版本读发布元数据。
func (s *Store) SoftwareRelease(ctx context.Context, kind string, version int64) (SoftwareRelease, error) {
	var row softwareReleaseRow
	if err := s.db.WithContext(ctx).First(&row, "kind = ? AND version = ?", kind, version).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SoftwareRelease{}, domain.ErrNotFound
		}
		return SoftwareRelease{}, err
	}
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
	got, err := s.SoftwareDistribution(ctx, in.Kind, in.Version, in.FactoryID)
	if err == nil {
		return got, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return SoftwareDistribution{}, err
	}
	row := softwareDistRow{
		Kind: in.Kind, Version: in.Version, FactoryID: in.FactoryID,
		Signature: in.Signature, CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return s.SoftwareDistribution(ctx, in.Kind, in.Version, in.FactoryID)
		}
		if domain.IsForeignKeyViolation(err) {
			return SoftwareDistribution{}, domain.ErrNotFound
		}
		if domain.IsCheckViolation(err) {
			return SoftwareDistribution{}, domain.ErrInvalidKey
		}
		return SoftwareDistribution{}, err
	}
	return distFromRow(row), nil
}

// SoftwareDistribution 读向某厂下发过的一份版本。
func (s *Store) SoftwareDistribution(ctx context.Context, kind string, version int64, factoryID uuid.UUID) (SoftwareDistribution, error) {
	var row softwareDistRow
	if err := s.db.WithContext(ctx).First(&row, "kind = ? AND version = ? AND factory_id = ?", kind, version, factoryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SoftwareDistribution{}, domain.ErrNotFound
		}
		return SoftwareDistribution{}, err
	}
	return distFromRow(row), nil
}

// MaxSoftwareDistributed 该厂该种类已下发的最高版本；没有则为 0。
func (s *Store) MaxSoftwareDistributed(ctx context.Context, kind string, factoryID uuid.UUID) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&softwareDistRow{}).
		Where("kind = ? AND factory_id = ?", kind, factoryID).
		Select("COALESCE(MAX(version), 0)").Scan(&n).Error
	return n, err
}

// ListSoftwareReleases 列出已发布版本；kind 空则两类都给，高版本在前。
func (s *Store) ListSoftwareReleases(ctx context.Context, kind string) ([]SoftwareRelease, error) {
	q := s.db.WithContext(ctx).Model(&softwareReleaseRow{}).Order("kind ASC, version DESC")
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	var rows []softwareReleaseRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]SoftwareRelease, 0, len(rows))
	for _, row := range rows {
		out = append(out, releaseFromRow(row))
	}
	return out, nil
}

// DeleteSoftwareRelease 删掉该（种类, 版本）发布行。
func (s *Store) DeleteSoftwareRelease(ctx context.Context, kind string, version int64) error {
	res := s.db.WithContext(ctx).Where("kind = ? AND version = ?", kind, version).Delete(&softwareReleaseRow{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// MaxSoftwareRelease 该种类已发布最高版本；没有则为 0。
func (s *Store) MaxSoftwareRelease(ctx context.Context, kind string) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&softwareReleaseRow{}).
		Where("kind = ?", kind).
		Select("COALESCE(MAX(version), 0)").Scan(&n).Error
	return n, err
}

type softwareStateRow struct {
	Kind             string    `gorm:"primaryKey"` // 种类
	InstalledVersion int64     `gorm:"not null"`   // 已装版本
	UpdatedAt        time.Time `gorm:"not null"`   // 最近确认
}

func (softwareStateRow) TableName() string { return "software_state" }

// InstalledSoftware 读该种类已确认落地版本；没有行则为 0。
func (s *Store) InstalledSoftware(ctx context.Context, kind string) (int64, error) {
	var row softwareStateRow
	if err := s.db.WithContext(ctx).First(&row, "kind = ?", kind).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return row.InstalledVersion, nil
}

// PutInstalledSoftware 记下落地成功的版本，只向前。
func (s *Store) PutInstalledSoftware(ctx context.Context, kind string, version int64) error {
	cur, err := s.InstalledSoftware(ctx, kind)
	if err != nil {
		return err
	}
	if version <= cur {
		return domain.ErrStaleRevision
	}
	now := time.Now().UTC()
	if cur == 0 {
		row := softwareStateRow{Kind: kind, InstalledVersion: version, UpdatedAt: now}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			if domain.IsCheckViolation(err) {
				return domain.ErrInvalidName
			}
			return err
		}
		return nil
	}
	res := s.db.WithContext(ctx).Model(&softwareStateRow{}).Where("kind = ? AND installed_version = ?", kind, cur).
		Updates(map[string]any{"installed_version": version, "updated_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrStaleRevision
	}
	return nil
}

// ListSoftwareDistributions 列出已下到该厂的各份版本，按种类和版本升序。
func (s *Store) ListSoftwareDistributions(ctx context.Context, factoryID uuid.UUID) ([]SoftwareDistribution, error) {
	var rows []softwareDistRow
	if err := s.db.WithContext(ctx).Where("factory_id = ?", factoryID).Order("kind ASC, version ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]SoftwareDistribution, 0, len(rows))
	for _, row := range rows {
		out = append(out, distFromRow(row))
	}
	return out, nil
}
