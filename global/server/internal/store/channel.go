package store

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/domain"
)

// EnrollmentOffer 是建厂码对应的待认领身份，不含建厂码原文。
type EnrollmentOffer struct {
	FactoryID  uuid.UUID // 工厂稳定身份
	Name       string    // 工厂显示名
	ShortCode  string    // 本厂短码，认领后用来发编号
	SAPersonID uuid.UUID // 约定的初始超管身份，厂库必须用这个
	SALogin    string    // 初始超管登录名
	SADisplay  string    // 初始超管显示名
}

// LookupEnrollment 按建厂码哈希找出尚未认领的工厂；对不上或已认领都当无效。
func (s *Store) LookupEnrollment(ctx context.Context, tokenHash string) (EnrollmentOffer, error) {
	// 空哈希对不上任何厂，直接当无效码。
	if tokenHash == "" {
		return EnrollmentOffer{}, domain.ErrInvalidEnrollment
	}
	// 准备接住名录行，对不上码就当无效。
	var fac Factory
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&fac, "enrollment_token_hash = ?", tokenHash).Error; err != nil {
		// 对不上建厂码就当无效，不透露厂在不在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return EnrollmentOffer{}, domain.ErrInvalidEnrollment
		}
		return EnrollmentOffer{}, err
	}
	// 认领必须带上约定的初始超管。
	sa, err := s.InitialSuperAdmin(ctx, fac.ID)
	// 约定的初始超管没读到就停，认领码不算有效。
	if err != nil {
		return EnrollmentOffer{}, err
	}
	return EnrollmentOffer{
		FactoryID:  fac.ID,
		Name:       fac.Name,
		ShortCode:  fac.ShortCode,
		SAPersonID: sa.PersonID,
		SALogin:    sa.LoginName,
		SADisplay:  sa.DisplayName,
	}, nil
}

// ConfirmEnrollment 登记厂签发公钥并作废建厂码；同钥再确认视为幂等。
func (s *Store) ConfirmEnrollment(ctx context.Context, factoryID uuid.UUID, publicKey []byte) error {
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 登记公钥并作废建厂码，失败则两步都不留。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先锁住这家厂，没有则认领不能继续。
		var fac Factory
		// 取不到或库出错先停住，再区分没有还是故障。
		if err := tx.First(&fac, "id = ?", factoryID).Error; err != nil {
			// 没有这一行就按不存在交回，不当成库故障。
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		// 准备接住已有的那一行，没有再决定插入。
		var existing FactoryPublicKey
		// 看这厂登记过公钥没有，同钥才算幂等。
		err := tx.First(&existing, "factory_id = ?", factoryID).Error
		// 按公钥在不在分流：同钥算成功，没有则新建。
		switch {
		// 公钥已经登记过，接着核对是不是同一把。
		case err == nil:
			// 同一把钥再确认算成功；换钥拒绝。
			if !bytes.Equal(existing.PublicKey, publicKey) {
				return domain.ErrFactoryKeyExists
			}
		// 还没有公钥，下面补登记这一把。
		case errors.Is(err, gorm.ErrRecordNotFound):
			// 组好这厂的签发公钥，私钥不在这行。
			row := FactoryPublicKey{FactoryID: factoryID, PublicKey: publicKey, CreatedAt: now}
			// 写入失败先停住，再看是重复还是约束没过。
			if err := tx.Create(&row).Error; err != nil {
				// 长度或格式不合格，按坏钥拒绝。
				if domain.IsCheckViolation(err) {
					return domain.ErrInvalidKey
				}
				return err
			}
		// 读公钥失败就停，不当成还没登记。
		default:
			return err
		}
		// 建厂码一次性，认领后清空。
		res := tx.Model(&Factory{}).Where("id = ?", factoryID).Updates(map[string]any{
			"enrollment_token_hash": nil,
			"enrolled_at":           now,
		})
		return res.Error
	})
}

// MarkChannelOnline 记下这条厂端通道刚连上；上一根的离线时间清掉。
func (s *Store) MarkChannelOnline(ctx context.Context, factoryID uuid.UUID) error {
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 记下刚连上的时刻，并清掉上次的断开。
	res := s.db.WithContext(ctx).Model(&Factory{}).Where("id = ?", factoryID).Updates(map[string]any{
		"channel_connected_at":    now,
		"channel_last_seen_at":    now,
		"channel_disconnected_at": nil,
	})
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

// TouchChannel 刷新最近心跳；已经离线的行不改。
func (s *Store) TouchChannel(ctx context.Context, factoryID uuid.UUID) error {
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 只刷新仍在线的心跳，离线行保持原样。
	return s.db.WithContext(ctx).Model(&Factory{}).
		Where("id = ? AND channel_connected_at IS NOT NULL", factoryID).
		Update("channel_last_seen_at", now).Error
}

// MarkChannelOffline 只在仍标记为在线时记下断开时间。
func (s *Store) MarkChannelOffline(ctx context.Context, factoryID uuid.UUID) error {
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 只给仍标记在线的厂记下断开时刻。
	return s.db.WithContext(ctx).Model(&Factory{}).
		Where("id = ? AND channel_connected_at IS NOT NULL", factoryID).
		Updates(map[string]any{
			"channel_connected_at":    nil,
			"channel_disconnected_at": now,
			"channel_last_seen_at":    now,
		}).Error
}

// ResetChannelPresence WAN 重启时把残留的在线标成离线，避免幽灵在线。
func (s *Store) ResetChannelPresence(ctx context.Context) error {
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 重启时把残留的在线全部收成离线。
	return s.db.WithContext(ctx).Model(&Factory{}).
		Where("channel_connected_at IS NOT NULL").
		Updates(map[string]any{
			"channel_connected_at":    nil,
			"channel_disconnected_at": now,
		}).Error
}

// PutFactoryRelease 记下该厂自报的前端和服务版本。
func (s *Store) PutFactoryRelease(ctx context.Context, factoryID uuid.UUID, webCode int64, webName string, svcCode int64, svcName string) error {
	// 记下该厂自报的前端和服务版本。
	res := s.db.WithContext(ctx).Model(&Factory{}).Where("id = ?", factoryID).Updates(map[string]any{
		"web_version":          webCode,
		"web_version_name":     webName,
		"service_version":      svcCode,
		"service_version_name": svcName,
	})
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
