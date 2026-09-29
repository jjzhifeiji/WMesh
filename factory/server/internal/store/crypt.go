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
	tableAssets   = "assets"         // 本厂工艺/工程正文
	tableReplicas = "asset_replicas" // 已收平台级副本正文
	mkAAD         = "content-mk"     // 包装 MK 用的附加数据
)

// 内容主钥的包装件，全表只留一行。
type contentMasterRow struct {
	ID        int16     `gorm:"primaryKey"`          // 单行
	WrappedMK []byte    `gorm:"type:bytea;not null"` // 用 L 包着的 MK
	UpdatedAt time.Time `gorm:"not null"`            // 最近包装
}

// 指定落库表名，避免查询时按类型名去猜。
func (contentMasterRow) TableName() string { return "content_master" }

// 内存里的内容租约和主钥，不落库。
type cryptBox struct {
	mu       sync.Mutex       // 挡住并发改内存租约
	l        []byte           // 租约钥，只在内存
	mk       []byte           // 内容主钥，只在内存
	deadline time.Time        // 单调时钟到期
	notAfter time.Time        // WAN 给出的墙上到期，只用来算窗口
	online   bool             // 在线时厂钟快了也收下刚签发的租约
	now      func() time.Time // 夹具时钟；空则用系统钟
}

// liveMK 复制当前 MK；调用方用完后 Zero。
func (s *Store) liveMK() ([]byte, error) {
	// 先占住内存租约，避免并发把钥拆散。
	s.crypt.mu.Lock()
	// 放开内存租约，后面的读写才进得来。
	defer s.crypt.mu.Unlock()
	// 先查看内存主钥，结果留给紧跟着的判断。
	mk, err := s.liveMKLocked()
	// 查看内存主钥失败就停，避免带着错误继续。
	if err != nil {
		return nil, err
	}
	// 拷一份再交回，避免和库里的切片共用。
	return append([]byte(nil), mk...), nil
}

// liveMKLocked 到期只按单调时钟清钥；断线不看墙上钟，避免厂钟快误杀。
func (s *Store) liveMKLocked() ([]byte, error) {
	// 内存里没有完整主钥，就当租约已过期。
	if len(s.crypt.mk) != contentcrypt.KeySize {
		return nil, domain.ErrContentLeaseExpired
	}
	// 取当前时刻，时间列和租约用同一个时钟。
	now := s.nowLocked()
	// 单调钟到点就清钥，避免过期后继续解正文。
	if !now.Before(s.crypt.deadline) {
		// 先清掉内存里的钥，结果留给紧跟着的判断。
		s.clearLocked()
		return nil, domain.ErrContentLeaseExpired
	}
	return s.crypt.mk, nil
}

// clearLocked 清内存钥，盘上包装件不动。
func (s *Store) clearLocked() {
	// 擦掉内存里的钥，避免明文钥继续留着。
	contentcrypt.Zero(s.crypt.l)
	// 擦掉内存里的钥，避免明文钥继续留着。
	contentcrypt.Zero(s.crypt.mk)
	// 记下这把租约钥，过站封解还要用。
	s.crypt.l = nil
	// 记下内容主钥，只放在内存里。
	s.crypt.mk = nil
	// 按单调钟记下到期，过点就清钥。
	s.crypt.deadline = time.Time{}
	// 记下墙上到期，只用来算还能给多久。
	s.crypt.notAfter = time.Time{}
}

// ApplyContentLease 用 WAN 租约解开或新建 MK；钥只进内存。
func (s *Store) ApplyContentLease(ctx context.Context, lease []byte, notAfter time.Time) error {
	// 租约长度不对就拒绝，不能当密钥用。
	if len(lease) != contentcrypt.KeySize {
		return domain.ErrInvalidKey
	}
	// 先占住内存租约，避免并发把钥拆散。
	s.crypt.mu.Lock()
	// 先标成还占着锁，成功放开后再改掉。
	unlocked := false
	// 离开前若锁还占着就放开，避免堵住后面。
	defer func() {
		// 锁还占着才解，避免解两次把后面堵住。
		if !unlocked {
			// 放开内存租约，后面的读写才进得来。
			s.crypt.mu.Unlock()
		}
	}()
	// 取当前时刻，时间列和租约用同一个时钟。
	now := s.nowLocked()
	// 先计算剩余时长，结果留给紧跟着的判断。
	dur := notAfter.Sub(now)
	// 租约不能长过上限，多出来的裁掉。
	if dur > contentcrypt.MaxLease {
		// 把窗口收到允许的上限，避免无限期。
		dur = contentcrypt.MaxLease
	}
	// 墙上钟已经过了，在线才给满窗。
	if dur <= 0 {
		// 在线刚收到的租约信 WAN：厂钟快了也给单调 24 小时，不拆通道。
		if !s.crypt.online {
			return domain.ErrContentLeaseExpired
		}
		// 把窗口收到允许的上限，避免无限期。
		dur = contentcrypt.MaxLease
	}
	// 准备承接查到的主钥包装。
	var row contentMasterRow
	// 按条件去读，没有行交给后面的分支。
	err := s.db.WithContext(ctx).First(&row, "id = 1").Error
	// 准备承接查到的那一行。
	var mk []byte
	// 按当前取值分派，各支不要混用结果。
	switch {
	// 库里已有包装件，就用这把租约解开主钥。
	case err == nil:
		// 必须是签发这把包装件的同一把 L。
		mk, err = contentcrypt.Open(lease, row.WrappedMK, []byte(mkAAD))
		// 解开信封失败就停，避免带着错误继续。
		if err != nil {
			// 先查看内存主钥，结果留给紧跟着的判断。
			live, liveErr := s.liveMKLocked()
			// 查看内存主钥失败就停，避免带着错误继续。
			if liveErr != nil {
				return domain.ErrContentLeaseExpired
			}
			// 内存还有 MK：用新 L 重包，避免 WAN 换钥后盘上包装件作废。
			wrapped, werr := contentcrypt.Seal(lease, live, []byte(mkAAD))
			// 封成信封失败就停，避免带着错误继续。
			if werr != nil {
				return werr
			}
			// 把这一列放进内存行，随后随保存写入。
			row.WrappedMK = wrapped
			// 把改好的值放进内存行，随后随保存写入。
			row.UpdatedAt = now.UTC()
			// 保存整行失败就停，避免带着错误继续。
			if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
				return err
			}
			// 收进结果，保持原来的先后顺序。
			mk = append([]byte(nil), live...)
		}
	// 还没有包装件就新生成主钥并包上。
	case errors.Is(err, gorm.ErrRecordNotFound):
		// 先生成随机钥，结果留给紧跟着的判断。
		mk, err = contentcrypt.RandomKey()
		// 生成随机钥失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 磁盘只存 wrap(L, MK)。
		wrapped, err := contentcrypt.Seal(lease, mk, []byte(mkAAD))
		// 封成信封失败就停，避免带着错误继续。
		if err != nil {
			// 擦掉内存里的钥，避免明文钥继续留着。
			contentcrypt.Zero(mk)
			return err
		}
		// 记下包好的主钥，明文主钥不进库。
		row = contentMasterRow{ID: 1, WrappedMK: wrapped, UpdatedAt: now.UTC()}
		// 写入一行失败就停，避免带着错误继续。
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			// 擦掉内存里的钥，避免明文钥继续留着。
			contentcrypt.Zero(mk)
			return err
		}
	// 别的库错误原样交回，不当成缺行。
	default:
		return err
	}
	// 擦掉内存里的钥，避免明文钥继续留着。
	contentcrypt.Zero(s.crypt.l)
	// 擦掉内存里的钥，避免明文钥继续留着。
	contentcrypt.Zero(s.crypt.mk)
	// 收进结果，保持原来的先后顺序。
	s.crypt.l = append([]byte(nil), lease...)
	// 记下内容主钥，只放在内存里。
	s.crypt.mk = mk
	// 按单调钟记下到期，过点就清钥。
	s.crypt.deadline = now.Add(dur)
	// 记下墙上到期，只用来算还能给多久。
	s.crypt.notAfter = notAfter.UTC()
	// 放开内存租约，后面的读写才进得来。
	s.crypt.mu.Unlock()
	// 锁已经放开，收尾不要再解一次。
	unlocked = true
	// 领到钥匙立刻把遗留明文就地封上，厂库不许明文过夜。
	return s.sealPlainRows(ctx)
}

// SetContentChannelOnline 标记厂↔WAN 通道是否钉死；在线只看单调钟。
func (s *Store) SetContentChannelOnline(online bool) {
	// 先占住内存租约，避免并发把钥拆散。
	s.crypt.mu.Lock()
	// 放开内存租约，后面的读写才进得来。
	defer s.crypt.mu.Unlock()
	// 记下通道是否在线，续租要看这个。
	s.crypt.online = online
}

// SetContentClock 夹具用：替换租约判定用的时钟。
func (s *Store) SetContentClock(now func() time.Time) {
	// 先占住内存租约，避免并发把钥拆散。
	s.crypt.mu.Lock()
	// 放开内存租约，后面的读写才进得来。
	defer s.crypt.mu.Unlock()
	// 换上夹具钟，租约窗口按它计算。
	s.crypt.now = now
}

// nowLocked 夹具钟优先，否则用系统钟。
func (s *Store) nowLocked() time.Time {
	// 夹具换过钟就用夹具，否则用系统钟。
	if s.crypt.now != nil {
		// 把这一步的结果交回给调用方。
		return s.crypt.now()
	}
	// 取当前时刻，时间列和租约用同一个时钟。
	return time.Now()
}

// GrantLocalLease 夹具用：本进程自签一把租约，不连 WAN。已有有效租约则不换钥。
func (s *Store) GrantLocalLease(ctx context.Context) error {
	// 租约还在就只去封遗留明文，不换钥。
	if _, err := s.liveMK(); err == nil {
		// 领到钥后把库里的明文就地封上。
		return s.sealPlainRows(ctx)
	}
	// 准备密钥或摘要，后面套租约或写入要用。
	l, err := contentcrypt.RandomKey()
	// 生成随机钥失败就停，避免带着错误继续。
	if err != nil {
		return err
	}
	// 用完就擦掉临时钥，避免留在内存里。
	defer contentcrypt.Zero(l)
	// 先占住内存租约，避免并发把钥拆散。
	s.crypt.mu.Lock()
	// 取当前时刻，时间列和租约用同一个时钟。
	now := s.nowLocked()
	// 放开内存租约，后面的读写才进得来。
	s.crypt.mu.Unlock()
	// 用这把新钥去套租约，内存里才有主钥。
	return s.ApplyContentLease(ctx, l, now.Add(contentcrypt.MaxLease))
}

// ClearContentLease 清掉内存钥，密文行不动。
func (s *Store) ClearContentLease() {
	// 先占住内存租约，避免并发把钥拆散。
	s.crypt.mu.Lock()
	// 放开内存租约，后面的读写才进得来。
	defer s.crypt.mu.Unlock()
	// 先清掉内存里的钥，结果留给紧跟着的判断。
	s.clearLocked()
}

// TransitKey 复制当前租约钥，给过站封/解。
func (s *Store) TransitKey() ([]byte, error) {
	// 先占住内存租约，避免并发把钥拆散。
	s.crypt.mu.Lock()
	// 放开内存租约，后面的读写才进得来。
	defer s.crypt.mu.Unlock()
	// 查看内存主钥失败就停，避免带着错误继续。
	if _, err := s.liveMKLocked(); err != nil {
		return nil, err
	}
	// 拷一份再交回，避免和库里的切片共用。
	return append([]byte(nil), s.crypt.l...), nil
}

// RawGovernedContent 读库内信封原文，不解包；夹具用来看是不是 WM2。
func (s *Store) RawGovernedContent(ctx context.Context, assetID uuid.UUID) ([]byte, error) {
	// 准备承接查到的工艺或工程。
	var row governedAssetRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Select("content").First(&row, "id = ?", assetID).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	// 拷一份再交回，避免和库里的切片共用。
	return append([]byte(nil), row.Content...), nil
}

// RawReplicaContent 读副本信封原文，不解包。
func (s *Store) RawReplicaContent(ctx context.Context, assetID uuid.UUID, revision int64) ([]byte, error) {
	// 准备承接查到的平台副本。
	var row replicaRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Select("content").First(&row, "id = ? AND revision = ?", assetID, revision).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	// 拷一份再交回，避免和库里的切片共用。
	return append([]byte(nil), row.Content...), nil
}

// persistBody 正文必须封 WM2；没有解包钥匙就拒绝写入。
func (s *Store) persistBody(id uuid.UUID, rev int64, table string, plain []byte) ([]byte, error) {
	// 先复制内存主钥，结果留给紧跟着的判断。
	mk, err := s.liveMK()
	// 复制内存主钥失败就停，避免带着错误继续。
	if err != nil {
		return nil, err
	}
	// 用完就擦掉临时钥，避免留在内存里。
	defer contentcrypt.Zero(mk)
	// 每写一把新 DEK，绑到这一行身份和修订。
	return contentcrypt.Seal(mk, nonempty(plain), contentcrypt.AssetAAD(id, rev, table))
}

// openAsset 解 WM2；遗留明文只在租约有效时可读。
func (s *Store) openAsset(id uuid.UUID, rev int64, table string, blob []byte) ([]byte, error) {
	// 空正文或还不是信封，就按明文处理。
	if len(blob) == 0 || !contentcrypt.IsEnvelope(blob) {
		// 遗留明文只在租约有效时可读，随后由 sealPlainRows 封掉。
		if _, err := s.liveMK(); err != nil {
			return nil, err
		}
		// 拷一份再交回，避免和库里的切片共用。
		return append([]byte(nil), nonempty(blob)...), nil
	}
	// 先复制内存主钥，结果留给紧跟着的判断。
	mk, err := s.liveMK()
	// 复制内存主钥失败就停，避免带着错误继续。
	if err != nil {
		return nil, err
	}
	// 用完就擦掉临时钥，避免留在内存里。
	defer contentcrypt.Zero(mk)
	// 改名升高修订可能没重封，信封仍绑当时改正文的修订。
	for r := rev; r >= 1; r-- {
		// 先解开信封，结果留给紧跟着的判断。
		body, err := contentcrypt.Open(mk, blob, contentcrypt.AssetAAD(id, r, table))
		// 已经查到就按现有结果核对，不再插入。
		if err == nil {
			return body, nil
		}
	}
	return nil, domain.ErrIntegrity
}

// rewrapIfSealed 把正文绑到新修订：明文也要封，厂库不许再落明文。
func (s *Store) rewrapIfSealed(id uuid.UUID, oldRev, newRev int64, table string, blob []byte) ([]byte, bool, error) {
	// 还是明文就先封上，库里不能再留明文。
	if !contentcrypt.IsEnvelope(blob) {
		// 先封装正文，结果留给紧跟着的判断。
		env, err := s.persistBody(id, newRev, table, blob)
		// 封装正文失败就停，避免带着错误继续。
		if err != nil {
			return nil, false, err
		}
		return env, true, nil
	}
	// 先解开正文，结果留给紧跟着的判断。
	body, err := s.openAsset(id, oldRev, table, blob)
	// 租约过期就先不动正文，也不当成损坏。
	if errors.Is(err, domain.ErrContentLeaseExpired) {
		return nil, false, nil
	}
	// 出错就停，避免把失败当成已经完成。
	if err != nil {
		return nil, false, err
	}
	// 先封装正文，结果留给紧跟着的判断。
	env, err := s.persistBody(id, newRev, table, body)
	// 封装正文失败就停，避免带着错误继续。
	if err != nil {
		return nil, false, err
	}
	return env, true, nil
}

// decodeGoverned 解包当前行正文。
func (s *Store) decodeGoverned(row governedAssetRow) (Asset, error) {
	// 先收成工艺视图，结果留给紧跟着的判断。
	a := assetFromGoverned(row)
	// 先解开正文，结果留给紧跟着的判断。
	body, err := s.openAsset(row.ID, row.Revision, tableAssets, row.Content)
	// 解开正文失败就停，避免带着错误继续。
	if err != nil {
		return Asset{}, err
	}
	// 把正文填回去，调用方才能看到内容。
	a.Content = body
	return a, nil
}

// decodeReplica 解包副本正文。
func (s *Store) decodeReplica(row replicaRow) (AssetReplica, error) {
	// 先收成副本视图，结果留给紧跟着的判断。
	a := replicaFromRow(row)
	// 先解开正文，结果留给紧跟着的判断。
	body, err := s.openAsset(row.ID, row.Revision, tableReplicas, row.Content)
	// 解开正文失败就停，避免带着错误继续。
	if err != nil {
		return AssetReplica{}, err
	}
	// 把正文填回去，调用方才能看到内容。
	a.Content = body
	return a, nil
}

// RequireContentLease 租约有效才允许厂内业务；探活和 WAN 通道不走这里。
func (s *Store) RequireContentLease() error {
	// 先占住内存租约，避免并发把钥拆散。
	s.crypt.mu.Lock()
	// 放开内存租约，后面的读写才进得来。
	defer s.crypt.mu.Unlock()
	// 先查看内存主钥，结果留给紧跟着的判断。
	_, err := s.liveMKLocked()
	return err
}

// sealPlainRows 把厂库里还没封的正文就地封成 WM2，不改修订和摘要。
func (s *Store) sealPlainRows(ctx context.Context) error {
	// 准备承接查到的多条工艺或工程。
	var assets []governedAssetRow
	// 按条件取多行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Select("id", "revision", "content").Find(&assets).Error; err != nil {
		return err
	}
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, row := range assets {
		// 已经是信封就跳过，避免重复封装。
		if contentcrypt.IsEnvelope(row.Content) {
			continue
		}
		// 先封装正文，结果留给紧跟着的判断。
		env, err := s.persistBody(row.ID, row.Revision, tableAssets, nonempty(row.Content))
		// 封装正文失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 更新指定列失败就停，避免带着错误继续。
		if err := s.db.WithContext(ctx).Model(&governedAssetRow{}).Where("id = ? AND revision = ?", row.ID, row.Revision).Update("content", env).Error; err != nil {
			return err
		}
	}
	// 准备承接查到的多条平台副本。
	var replicas []replicaRow
	// 按条件取多行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Select("id", "revision", "content").Find(&replicas).Error; err != nil {
		return err
	}
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, row := range replicas {
		// 已经是信封就跳过，避免重复封装。
		if contentcrypt.IsEnvelope(row.Content) {
			continue
		}
		// 先封装正文，结果留给紧跟着的判断。
		env, err := s.persistBody(row.ID, row.Revision, tableReplicas, nonempty(row.Content))
		// 封装正文失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 更新指定列失败就停，避免带着错误继续。
		if err := s.db.WithContext(ctx).Model(&replicaRow{}).Where("id = ? AND revision = ?", row.ID, row.Revision).Update("content", env).Error; err != nil {
			return err
		}
	}
	return nil
}
