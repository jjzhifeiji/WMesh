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
	ID        int16     `gorm:"primaryKey"`          // 单行
	WrappedMK []byte    `gorm:"type:bytea;not null"` // 用 L 包着的 MK
	UpdatedAt time.Time `gorm:"not null"`            // 最近包装
}

func (contentMasterRow) TableName() string { return "content_master" }

type cryptBox struct {
	mu       sync.Mutex
	l        []byte           // 租约钥，只在内存
	mk       []byte           // 内容主钥，只在内存
	deadline time.Time        // 单调时钟到期
	notAfter time.Time        // WAN 给出的墙上到期，只用来算窗口
	online   bool             // 在线时厂钟快了也收下刚签发的租约
	now      func() time.Time // 夹具时钟；空则用系统钟
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

// liveMKLocked 到期只按单调时钟清钥；断线不看墙上钟，避免厂钟快误杀。
func (s *Store) liveMKLocked() ([]byte, error) {
	if len(s.crypt.mk) != contentcrypt.KeySize {
		return nil, domain.ErrContentLeaseExpired
	}
	now := s.nowLocked()
	if !now.Before(s.crypt.deadline) {
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
	s.crypt.mu.Lock()
	unlocked := false
	defer func() {
		if !unlocked {
			s.crypt.mu.Unlock()
		}
	}()
	now := s.nowLocked()
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
			live, liveErr := s.liveMKLocked()
			if liveErr != nil {
				return domain.ErrContentLeaseExpired
			}
			// 内存还有 MK：用新 L 重包，避免 WAN 换钥后盘上包装件作废。
			wrapped, werr := contentcrypt.Seal(lease, live, []byte(mkAAD))
			if werr != nil {
				return werr
			}
			row.WrappedMK = wrapped
			row.UpdatedAt = now.UTC()
			if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
				return err
			}
			mk = append([]byte(nil), live...)
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
	s.crypt.mu.Unlock()
	unlocked = true
	// 领到钥匙立刻把遗留明文就地封上，厂库不许明文过夜。
	return s.sealPlainRows(ctx)
}

// SetContentChannelOnline 标记厂↔WAN 通道是否钉死；在线只看单调钟。
func (s *Store) SetContentChannelOnline(online bool) {
	s.crypt.mu.Lock()
	defer s.crypt.mu.Unlock()
	s.crypt.online = online
}

// SetContentClock 夹具用：替换租约判定用的时钟。
func (s *Store) SetContentClock(now func() time.Time) {
	s.crypt.mu.Lock()
	defer s.crypt.mu.Unlock()
	s.crypt.now = now
}

func (s *Store) nowLocked() time.Time {
	if s.crypt.now != nil {
		return s.crypt.now()
	}
	return time.Now()
}

// GrantLocalLease 夹具用：本进程自签一把租约，不连 WAN。已有有效租约则不换钥。
func (s *Store) GrantLocalLease(ctx context.Context) error {
	if _, err := s.liveMK(); err == nil {
		return s.sealPlainRows(ctx)
	}
	l, err := contentcrypt.RandomKey()
	if err != nil {
		return err
	}
	defer contentcrypt.Zero(l)
	s.crypt.mu.Lock()
	now := s.nowLocked()
	s.crypt.mu.Unlock()
	return s.ApplyContentLease(ctx, l, now.Add(contentcrypt.MaxLease))
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

// persistBody 正文必须封 WM2；没有解包钥匙就拒绝写入。
func (s *Store) persistBody(id uuid.UUID, rev int64, table string, plain []byte) ([]byte, error) {
	mk, err := s.liveMK()
	if err != nil {
		return nil, err
	}
	defer contentcrypt.Zero(mk)
	// 每写一把新 DEK，绑到这一行身份和修订。
	return contentcrypt.Seal(mk, nonempty(plain), contentcrypt.AssetAAD(id, rev, table))
}

func (s *Store) openAsset(id uuid.UUID, rev int64, table string, blob []byte) ([]byte, error) {
	if len(blob) == 0 || !contentcrypt.IsEnvelope(blob) {
		// 遗留明文只在租约有效时可读，随后由 sealPlainRows 封掉。
		if _, err := s.liveMK(); err != nil {
			return nil, err
		}
		return append([]byte(nil), nonempty(blob)...), nil
	}
	mk, err := s.liveMK()
	if err != nil {
		return nil, err
	}
	defer contentcrypt.Zero(mk)
	// 改名升高修订可能没重封，信封仍绑当时改正文的修订。
	for r := rev; r >= 1; r-- {
		body, err := contentcrypt.Open(mk, blob, contentcrypt.AssetAAD(id, r, table))
		if err == nil {
			return body, nil
		}
	}
	return nil, domain.ErrIntegrity
}

// rewrapIfSealed 把正文绑到新修订：明文也要封，厂库不许再落明文。
func (s *Store) rewrapIfSealed(id uuid.UUID, oldRev, newRev int64, table string, blob []byte) ([]byte, bool, error) {
	if !contentcrypt.IsEnvelope(blob) {
		env, err := s.persistBody(id, newRev, table, blob)
		if err != nil {
			return nil, false, err
		}
		return env, true, nil
	}
	body, err := s.openAsset(id, oldRev, table, blob)
	if errors.Is(err, domain.ErrContentLeaseExpired) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	env, err := s.persistBody(id, newRev, table, body)
	if err != nil {
		return nil, false, err
	}
	return env, true, nil
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

// RequireContentLease 租约有效才允许厂内业务；探活和 WAN 通道不走这里。
func (s *Store) RequireContentLease() error {
	s.crypt.mu.Lock()
	defer s.crypt.mu.Unlock()
	_, err := s.liveMKLocked()
	return err
}

// sealPlainRows 把厂库里还没封的正文就地封成 WM2，不改修订和摘要。
func (s *Store) sealPlainRows(ctx context.Context) error {
	var assets []governedAssetRow
	if err := s.db.WithContext(ctx).Select("id", "revision", "content").Find(&assets).Error; err != nil {
		return err
	}
	for _, row := range assets {
		if contentcrypt.IsEnvelope(row.Content) {
			continue
		}
		env, err := s.persistBody(row.ID, row.Revision, tableAssets, nonempty(row.Content))
		if err != nil {
			return err
		}
		if err := s.db.WithContext(ctx).Model(&governedAssetRow{}).Where("id = ? AND revision = ?", row.ID, row.Revision).Update("content", env).Error; err != nil {
			return err
		}
	}
	var replicas []replicaRow
	if err := s.db.WithContext(ctx).Select("id", "revision", "content").Find(&replicas).Error; err != nil {
		return err
	}
	for _, row := range replicas {
		if contentcrypt.IsEnvelope(row.Content) {
			continue
		}
		env, err := s.persistBody(row.ID, row.Revision, tableReplicas, nonempty(row.Content))
		if err != nil {
			return err
		}
		if err := s.db.WithContext(ctx).Model(&replicaRow{}).Where("id = ? AND revision = ?", row.ID, row.Revision).Update("content", env).Error; err != nil {
			return err
		}
	}
	return nil
}
