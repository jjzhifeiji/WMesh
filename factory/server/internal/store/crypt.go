package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/domain"
)

const (
	tableAssets   = "assets"
	tableReplicas = "asset_replicas"
	mkAAD         = "content-mk"
)

type contentMasterRow struct {
	ID        int16     `gorm:"primaryKey"` // 单行
	WrappedMK []byte    `gorm:"type:bytea;not null"` // 用 L 包着的 MK
	UpdatedAt time.Time `gorm:"not null"`            // 最近包装
}

func (contentMasterRow) TableName() string { return "content_master" }

type cryptBox struct {
	mu       sync.Mutex
	l        []byte    // 租约钥，只在内存
	mk       []byte    // 内容主钥，只在内存
	deadline time.Time // 单调时钟到期
	notAfter time.Time // WAN 给出的墙上到期
	online   bool      // 通道在线时只看单调钟
}

// liveMK 复制当前 MK；调用方用完后 Zero。
func (s *Store) liveMK() ([]byte, error) {
	s.crypt.mu.Lock()
	defer s.crypt.mu.Unlock()
	mk, err := s.liveMKLocked()
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), mk...), nil
}

// liveMKLocked 到期按单调时钟清钥；离线才兼看墙上 notAfter。
func (s *Store) liveMKLocked() ([]byte, error) {
	if len(s.crypt.mk) != contentcrypt.KeySize {
		return nil, domain.ErrContentLeaseExpired
	}
	now := time.Now()
	if !now.Before(s.crypt.deadline) {
		s.clearLocked()
		return nil, domain.ErrContentLeaseExpired
	}
	// 离线才看本机钟上的 notAfter；在线跟单调时限。
	if !s.crypt.online && !now.Before(s.crypt.notAfter) {
		s.clearLocked()
		return nil, domain.ErrContentLeaseExpired
	}
	return s.crypt.mk, nil
}

func (s *Store) clearLocked() {
	contentcrypt.Zero(s.crypt.l)
	contentcrypt.Zero(s.crypt.mk)
	s.crypt.l = nil
	s.crypt.mk = nil
	s.crypt.deadline = time.Time{}
	s.crypt.notAfter = time.Time{}
}

// ApplyContentLease 用 WAN 租约解开或新建 MK；钥只进内存。
func (s *Store) ApplyContentLease(ctx context.Context, lease []byte, notAfter time.Time) error {
	if len(lease) != contentcrypt.KeySize {
		return domain.ErrInvalidKey
	}
	now := time.Now()
	s.crypt.mu.Lock()
	defer s.crypt.mu.Unlock()
	dur := notAfter.Sub(now)
	if dur > contentcrypt.MaxLease {
		dur = contentcrypt.MaxLease
	}
	if dur <= 0 {
		// 在线刚收到的租约信 WAN：厂钟快了也给单调 24 小时，不拆通道。
		if !s.crypt.online {
			return domain.ErrContentLeaseExpired
		}
		dur = contentcrypt.MaxLease
	}
	var row contentMasterRow
	err := s.db.WithContext(ctx).First(&row, "id = 1").Error
	var mk []byte
	switch {
	case err == nil:
		// 必须是签发这把包装件的同一把 L。
		mk, err = contentcrypt.Open(lease, row.WrappedMK, []byte(mkAAD))
		if err != nil {
			// 解不开不覆盖内存里还能用的旧钥。
			if _, liveErr := s.liveMKLocked(); liveErr == nil {
				return nil
			}
			return domain.ErrContentLeaseExpired
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		mk, err = contentcrypt.RandomKey()
		if err != nil {
			return err
		}
		// 磁盘只存 wrap(L, MK)。
		wrapped, err := contentcrypt.Seal(lease, mk, []byte(mkAAD))
		if err != nil {
			contentcrypt.Zero(mk)
			return err
		}
		row = contentMasterRow{ID: 1, WrappedMK: wrapped, UpdatedAt: now.UTC()}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			contentcrypt.Zero(mk)
			return err
		}
	default:
		return err
	}
	contentcrypt.Zero(s.crypt.l)
	contentcrypt.Zero(s.crypt.mk)
	s.crypt.l = append([]byte(nil), lease...)
	s.crypt.mk = mk
	s.crypt.deadline = now.Add(dur)
	s.crypt.notAfter = notAfter.UTC()
	return nil
}

// SetContentChannelOnline 标记厂↔WAN 通道是否钉死；在线只看单调钟。
func (s *Store) SetContentChannelOnline(online bool) {
	s.crypt.mu.Lock()
	defer s.crypt.mu.Unlock()
	s.crypt.online = online
}

// GrantLocalLease 夹具用：本进程自签一把租约，不连 WAN。已有有效租约则不换钥。
func (s *Store) GrantLocalLease(ctx context.Context) error {
	if _, err := s.liveMK(); err == nil {
		return nil
	}
	l, err := contentcrypt.RandomKey()
	if err != nil {
		return err
	}
	defer contentcrypt.Zero(l)
	return s.ApplyContentLease(ctx, l, time.Now().Add(contentcrypt.MaxLease))
}

// ClearContentLease 清掉内存钥，密文行不动。
func (s *Store) ClearContentLease() {
	s.crypt.mu.Lock()
	defer s.crypt.mu.Unlock()
	s.clearLocked()
}

// TransitKey 复制当前租约钥，给过站封/解。
func (s *Store) TransitKey() ([]byte, error) {
	s.crypt.mu.Lock()
	defer s.crypt.mu.Unlock()
	if _, err := s.liveMKLocked(); err != nil {
		return nil, err
	}
	return append([]byte(nil), s.crypt.l...), nil
}

// RawGovernedContent 读库内信封原文，不解包；夹具用来看是不是 WM2。
func (s *Store) RawGovernedContent(ctx context.Context, assetID uuid.UUID) ([]byte, error) {
	var row governedAssetRow
	if err := s.db.WithContext(ctx).Select("content").First(&row, "id = ?", assetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return append([]byte(nil), row.Content...), nil
}

// RawReplicaContent 读副本信封原文，不解包。
func (s *Store) RawReplicaContent(ctx context.Context, assetID uuid.UUID, revision int64) ([]byte, error) {
	var row replicaRow
	if err := s.db.WithContext(ctx).Select("content").First(&row, "id = ? AND revision = ?", assetID, revision).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return append([]byte(nil), row.Content...), nil
}

func (s *Store) sealAsset(id uuid.UUID, rev int64, table string, plain []byte) ([]byte, error) {
	mk, err := s.liveMK()
	if err != nil {
		return nil, err
	}
	defer contentcrypt.Zero(mk)
	// 每写一把新 DEK，绑到这一行身份和修订。
	return contentcrypt.Seal(mk, plain, contentcrypt.AssetAAD(id, rev, table))
}

func (s *Store) openAsset(id uuid.UUID, rev int64, table string, blob []byte) ([]byte, error) {
	// 旧行没有 WM2：当明文读，不绑租约。
	if len(blob) == 0 || !contentcrypt.IsEnvelope(blob) {
		return append([]byte(nil), nonempty(blob)...), nil
	}
	mk, err := s.liveMK()
	if err != nil {
		return nil, err
	}
	defer contentcrypt.Zero(mk)
	// 解包失败当损坏，不提密钥。
	return contentcrypt.Open(mk, blob, contentcrypt.AssetAAD(id, rev, table))
}

func (s *Store) decodeGoverned(row governedAssetRow) (Asset, error) {
	a := assetFromGoverned(row)
	body, err := s.openAsset(row.ID, row.Revision, tableAssets, row.Content)
	if err != nil {
		return Asset{}, err
	}
	a.Content = body
	return a, nil
}

func (s *Store) decodeReplica(row replicaRow) (AssetReplica, error) {
	a := replicaFromRow(row)
	body, err := s.openAsset(row.ID, row.Revision, tableReplicas, row.Content)
	if err != nil {
		return AssetReplica{}, err
	}
	a.Content = body
	return a, nil
}
