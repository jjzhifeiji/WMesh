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

// 指定落库表名，避免查询时按类型名去猜。
func (WANTrust) TableName() string { return "wan_trust" }

// SoftwareReplica 是已送达本厂的一份软件版本元数据，不含字节。
type SoftwareReplica struct {
	Kind        string    `json:"kind"`                // factory_service / client_apk
	Version     int64     `json:"version"`             // 送达版本
	VersionName string    `json:"versionName"`         // 给人看的版本名
	Digest      []byte    `json:"digest"`              // SHA-256
	ObjectKey   string    `json:"objectKey"`           // 本厂对象键
	Signature   []byte    `json:"signature,omitempty"` // 旧行可能有签名；新拉入为空
	ReceivedAt  time.Time `json:"receivedAt"`          // 收到时间
}

// 已收软件包的落库行，不含字节。
type softwareReplicaRow struct {
	Kind        string    `gorm:"primaryKey"`          // 种类
	Version     int64     `gorm:"primaryKey"`          // 版本
	VersionName string    `gorm:"not null"`            // 版本名
	Digest      []byte    `gorm:"type:bytea;not null"` // SHA-256
	ObjectKey   string    `gorm:"not null"`            // 对象键
	Signature   []byte    `gorm:"type:bytea"`          // 旧签名可空；新拉入不写
	ReceivedAt  time.Time `gorm:"not null"`            // 收到时间
}

// 指定落库表名，避免查询时按类型名去猜。
func (softwareReplicaRow) TableName() string { return "software_replicas" }

// SoftwareState 是本厂已确认安装的厂服务版本。
type SoftwareState struct {
	Kind             string    `json:"kind"`             // 固定 factory_service
	InstalledVersion int64     `json:"installedVersion"` // 已装版本；0 未装
	UpdatedAt        time.Time `json:"updatedAt"`        // 最近确认安装
}

// 本厂已确认安装版本的落库行。
type softwareStateRow struct {
	Kind             string    `gorm:"primaryKey"` // 种类
	InstalledVersion int64     `gorm:"not null"`   // 已装版本
	UpdatedAt        time.Time `gorm:"not null"`   // 最近确认
}

// 指定落库表名，避免查询时按类型名去猜。
func (softwareStateRow) TableName() string { return "software_state" }

// SoftwareObjectKey 按种类和版本生成本厂对象键。
func SoftwareObjectKey(kind string, version int64) string {
	// 按种类和版本生成本厂对象键。
	return fmt.Sprintf("software/%s/%d", kind, version)
}

// PutWANTrust 写入或核对 WAN 公钥；换钥拒绝。
func (s *Store) PutWANTrust(ctx context.Context, publicKey []byte) error {
	// 先读云端公钥，结果留给紧跟着的判断。
	got, err := s.WANTrust(ctx)
	// 已经有记录就核对摘要，不一致不能覆盖。
	if err == nil {
		// 比对原文这一支不成立就换路。
		if !bytes.Equal(got.PublicKey, publicKey) {
			return domain.ErrIntegrity
		}
		return nil
	}
	// 不是找不到的错误要原样交回。
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	// 取当前时刻，时间列和租约用同一个时钟。
	row := WANTrust{ID: 1, PublicKey: publicKey, CreatedAt: time.Now().UTC()}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 检查约束不通过就改成业务拒绝。
		if domain.IsCheckViolation(err) {
			return domain.ErrInvalidKey
		}
		return err
	}
	return nil
}

// WANTrust 读本厂已登记的 WAN 公钥。
func (s *Store) WANTrust(ctx context.Context) (WANTrust, error) {
	// 准备承接查到的那一行。
	var row WANTrust
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = 1").Error; err != nil {
		// 没有这一行就当成不存在。
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
	// 摘要必须是三十二字节，否则拒绝收下。
	if len(in.Digest) != 32 {
		return SoftwareReplica{}, domain.ErrIntegrity
	}
	// 先读软件副本，结果留给紧跟着的判断。
	got, err := s.SoftwareReplica(ctx, in.Kind, in.Version)
	// 已经有记录就核对摘要，不一致不能覆盖。
	if err == nil {
		// 比对原文这一支不成立就换路。
		if !bytes.Equal(got.Digest, in.Digest) {
			return SoftwareReplica{}, domain.ErrIntegrity
		}
		return got, nil
	}
	// 不是找不到的错误要原样交回。
	if !errors.Is(err, domain.ErrNotFound) {
		return SoftwareReplica{}, err
	}
	// 按入参组装要写入的行。
	row := softwareReplicaRow{
		Kind: in.Kind, Version: in.Version, VersionName: in.VersionName,
		Digest: in.Digest, ObjectKey: in.ObjectKey, Signature: in.Signature, ReceivedAt: time.Now().UTC(),
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 唯一约束撞了就改成业务冲突，不抛库原文。
		if domain.IsUniqueViolation(err) {
			// 做完读软件副本后把结果交回。
			return s.SoftwareReplica(ctx, in.Kind, in.Version)
		}
		// 检查约束不通过就改成业务拒绝。
		if domain.IsCheckViolation(err) {
			return SoftwareReplica{}, domain.ErrInvalidName
		}
		return SoftwareReplica{}, err
	}
	// 库行收成对外结果再交回。
	return softwareReplicaFromRow(row), nil
}

// SoftwareReplica 按种类和版本读已收副本。
func (s *Store) SoftwareReplica(ctx context.Context, kind string, version int64) (SoftwareReplica, error) {
	// 准备承接查到的软件副本。
	var row softwareReplicaRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "kind = ? AND version = ?", kind, version).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SoftwareReplica{}, domain.ErrNotFound
		}
		return SoftwareReplica{}, err
	}
	// 库行收成对外结果再交回。
	return softwareReplicaFromRow(row), nil
}

// ListSoftwareReplicas 列出已收副本；kind 空则两类都给，高版本在前。
func (s *Store) ListSoftwareReplicas(ctx context.Context, kind string) ([]SoftwareReplica, error) {
	// 补上筛选或限定列，避免动到不该动的字段。
	q := s.db.WithContext(ctx).Model(&softwareReplicaRow{}).Order("kind ASC, version DESC")
	// 排好序这一支不成立就换路。
	if kind != "" {
		// 补上筛选或限定列，避免动到不该动的字段。
		q = q.Where("kind = ?", kind)
	}
	// 准备承接查到的多条软件副本。
	var rows []softwareReplicaRow
	// 按条件取多行失败就停，避免带着错误继续。
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]SoftwareReplica, 0, len(rows))
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, row := range rows {
		// 收进结果，保持原来的先后顺序。
		out = append(out, softwareReplicaFromRow(row))
	}
	return out, nil
}

// DeleteSoftwareReplica 删掉该（种类, 版本）副本行。
func (s *Store) DeleteSoftwareReplica(ctx context.Context, kind string, version int64) error {
	// 删掉匹配行，没有行交给后面判断。
	res := s.db.WithContext(ctx).Where("kind = ? AND version = ?", kind, version).Delete(&softwareReplicaRow{})
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// MaxSoftwareReplica 该种类已收最高版本；没有则为 0。
func (s *Store) MaxSoftwareReplica(ctx context.Context, kind string) (int64, error) {
	// 准备承接计数，用来判断有没有匹配行。
	var n int64
	// 先接上要操作的表，结果留给紧跟着的判断。
	err := s.db.WithContext(ctx).Model(&softwareReplicaRow{}).
		Where("kind = ?", kind).
		Select("COALESCE(MAX(version), 0)").Scan(&n).Error
	return n, err
}

// InstalledSoftware 读厂服务已确认安装版本；没有行则为 0。
func (s *Store) InstalledSoftware(ctx context.Context, kind string) (int64, error) {
	// 准备承接查到的安装状态。
	var row softwareStateRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "kind = ?", kind).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return row.InstalledVersion, nil
}

// PutInstalledSoftware 记下确认安装成功的版本，只向前。
func (s *Store) PutInstalledSoftware(ctx context.Context, kind string, version int64) error {
	// 先读已装版本，结果留给紧跟着的判断。
	cur, err := s.InstalledSoftware(ctx, kind)
	// 读已装版本失败就停，避免带着错误继续。
	if err != nil {
		return err
	}
	// 不比已装版本新就拒绝确认安装。
	if version <= cur {
		return domain.ErrStaleRevision
	}
	// 取当前时刻，时间列和租约用同一个时钟。
	now := time.Now().UTC()
	// 取当前时刻这一支不成立就换路。
	if cur == 0 {
		// 按入参组装要写入的行。
		row := softwareStateRow{Kind: kind, InstalledVersion: version, UpdatedAt: now}
		// 写入一行失败就停，避免带着错误继续。
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			// 检查约束不通过就改成业务拒绝。
			if domain.IsCheckViolation(err) {
				return domain.ErrInvalidName
			}
			return err
		}
		return nil
	}
	// 执行这次写入，行数和错误分开看。
	res := s.db.WithContext(ctx).Model(&softwareStateRow{}).Where("kind = ? AND installed_version = ?", kind, cur).
		Updates(map[string]any{"installed_version": version, "updated_at": now})
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		return res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return domain.ErrStaleRevision
	}
	return nil
}
