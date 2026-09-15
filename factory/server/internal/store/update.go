package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"wmesh/factory/internal/platform/domain"
)

const (
	SoftwareFactoryService = "factory_service" // 厂端 API 与管理台一包
	SoftwareClientAPK      = "client_apk"      // 当前 Android 客户端包
)

// WANTrust 是本厂验云端软件包用的 WAN 公钥。
type WANTrust struct {
	ID        int16     `gorm:"primaryKey" json:"id"`                 // 固定 1
	PublicKey []byte    `gorm:"type:bytea;not null" json:"publicKey"` // WAN 公钥 32 字节
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`            // 写入时间
}

func (WANTrust) TableName() string { return "wan_trust" }

// SoftwareReplica 是已送达本厂的一份软件版本元数据，不含字节。
type SoftwareReplica struct {
	Kind        string    `json:"kind"`        // factory_service / client_apk
	Version     int64     `json:"version"`     // 送达版本
	VersionName string    `json:"versionName"` // 给人看的版本名
	Digest      []byte    `json:"digest"`      // SHA-256
	ObjectKey   string    `json:"objectKey"`   // 本厂对象键
	Signature   []byte    `json:"signature"`   // WAN 对目标本厂的签名
	ReceivedAt  time.Time `json:"receivedAt"`  // 收到时间
}

type softwareReplicaRow struct {
	Kind        string    `gorm:"primaryKey"`          // 种类
	Version     int64     `gorm:"primaryKey"`          // 版本
	VersionName string    `gorm:"not null"`            // 版本名
	Digest      []byte    `gorm:"type:bytea;not null"` // SHA-256
	ObjectKey   string    `gorm:"not null"`            // 对象键
	Signature   []byte    `gorm:"type:bytea;not null"` // WAN 签名
	ReceivedAt  time.Time `gorm:"not null"`            // 收到时间
}

func (softwareReplicaRow) TableName() string { return "software_replicas" }

// SoftwareState 是本厂已确认安装的厂服务版本。
type SoftwareState struct {
	Kind             string    `json:"kind"`             // 固定 factory_service
	InstalledVersion int64     `json:"installedVersion"` // 已装版本；0 未装
	UpdatedAt        time.Time `json:"updatedAt"`        // 最近确认安装
}

type softwareStateRow struct {
	Kind             string    `gorm:"primaryKey"` // 种类
	InstalledVersion int64     `gorm:"not null"`   // 已装版本
	UpdatedAt        time.Time `gorm:"not null"`   // 最近确认
}

func (softwareStateRow) TableName() string { return "software_state" }

// SoftwareObjectKey 按种类和版本生成本厂对象键。
func SoftwareObjectKey(kind string, version int64) string {
	return fmt.Sprintf("software/%s/%d", kind, version)
}

// PutWANTrust 写入或核对 WAN 公钥；换钥拒绝。
func (s *Store) PutWANTrust(ctx context.Context, publicKey []byte) error {
	got, err := s.WANTrust(ctx)
	if err == nil {
		if !bytes.Equal(got.PublicKey, publicKey) {
			return domain.ErrIntegrity
		}
		return nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	row := WANTrust{ID: 1, PublicKey: publicKey, CreatedAt: time.Now().UTC()}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsCheckViolation(err) {
			return domain.ErrInvalidKey
		}
		return err
	}
	return nil
}

// WANTrust 读本厂已登记的 WAN 公钥。
func (s *Store) WANTrust(ctx context.Context) (WANTrust, error) {
	var row WANTrust
	if err := s.db.WithContext(ctx).First(&row, "id = 1").Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return WANTrust{}, domain.ErrNotFound
		}
		return WANTrust{}, err
	}
	return row, nil
}

// 库行收成软件副本视图。
func softwareReplicaFromRow(row softwareReplicaRow) SoftwareReplica {
	return SoftwareReplica{
		Kind: row.Kind, Version: row.Version, VersionName: row.VersionName,
		Digest: row.Digest, ObjectKey: row.ObjectKey, Signature: row.Signature, ReceivedAt: row.ReceivedAt,
	}
}

// InsertSoftwareReplica 按种类+版本收下副本；同摘要幂等，摘要不同则完整性失败。
func (s *Store) InsertSoftwareReplica(ctx context.Context, in SoftwareReplica) (SoftwareReplica, error) {
	if len(in.Digest) != 32 {
		return SoftwareReplica{}, domain.ErrIntegrity
	}
	got, err := s.SoftwareReplica(ctx, in.Kind, in.Version)
	if err == nil {
		if !bytes.Equal(got.Digest, in.Digest) || !bytes.Equal(got.Signature, in.Signature) {
			return SoftwareReplica{}, domain.ErrIntegrity
		}
		return got, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return SoftwareReplica{}, err
	}
	row := softwareReplicaRow{
		Kind: in.Kind, Version: in.Version, VersionName: in.VersionName,
		Digest: in.Digest, ObjectKey: in.ObjectKey, Signature: in.Signature, ReceivedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return s.SoftwareReplica(ctx, in.Kind, in.Version)
		}
		if domain.IsCheckViolation(err) {
			return SoftwareReplica{}, domain.ErrInvalidName
		}
		return SoftwareReplica{}, err
	}
	return softwareReplicaFromRow(row), nil
}

// SoftwareReplica 按种类和版本读已收副本。
func (s *Store) SoftwareReplica(ctx context.Context, kind string, version int64) (SoftwareReplica, error) {
	var row softwareReplicaRow
	if err := s.db.WithContext(ctx).First(&row, "kind = ? AND version = ?", kind, version).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SoftwareReplica{}, domain.ErrNotFound
		}
		return SoftwareReplica{}, err
	}
	return softwareReplicaFromRow(row), nil
}

// MaxSoftwareReplica 该种类已收最高版本；没有则为 0。
func (s *Store) MaxSoftwareReplica(ctx context.Context, kind string) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&softwareReplicaRow{}).
		Where("kind = ?", kind).
		Select("COALESCE(MAX(version), 0)").Scan(&n).Error
	return n, err
}

// InstalledSoftware 读厂服务已确认安装版本；没有行则为 0。
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

// PutInstalledSoftware 记下确认安装成功的版本，只向前。
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
