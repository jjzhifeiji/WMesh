package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"wmesh/global/internal/platform/domain"
)

const (
	OnboardingEnroll  = "enroll"   // 新厂接入
	OnboardingResetSA = "reset_sa" // 找回初始超管密码

	OnboardingPending  = "pending"  // 等云端批准
	OnboardingApproved = "approved" // 已批准
	OnboardingRejected = "rejected" // 已拒绝
	OnboardingUsed     = "used"     // 找回已用掉

	onboardingKeepRejected = 7 * 24 * time.Hour // 拒绝的申请留七天，现场还能看到结果
)

// OnboardingRequest 是现场发起、等云端批准的一条申请，不含密钥原文和日常密码。
type OnboardingRequest struct {
	ID                 uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`              // 申请稳定身份
	Kind               string     `gorm:"not null" json:"kind"`                        // enroll / reset_sa
	Status             string     `gorm:"not null" json:"status"`                      // pending / approved / rejected / used
	SecretHash         string     `gorm:"not null" json:"-"`                           // 申请密钥哈希
	VerifyCode         string     `gorm:"not null" json:"verifyCode"`                  // 六位核对码
	FactoryID          *uuid.UUID `gorm:"type:uuid" json:"factoryId,omitempty"`        // 批准后或找回时对应的工厂
	FactoryName        string     `gorm:"not null" json:"factoryName"`                 // 工厂显示名
	SALogin            string     `gorm:"column:sa_login;not null" json:"saLogin"`     // 初始超管登录名
	SADisplay          string     `gorm:"column:sa_display;not null" json:"saDisplay"` // 初始超管显示名
	SourceAddr         string     `gorm:"not null" json:"sourceAddr"`                  // 发起申请的来源地址
	ServiceVersionName string     `gorm:"not null" json:"serviceVersionName"`          // 发起方厂端服务版本名
	CreatedAt          time.Time  `gorm:"not null" json:"createdAt"`                   // 申请时间
	ExpiresAt          time.Time  `gorm:"not null" json:"expiresAt"`                   // 到期时间
	DecidedAt          *time.Time `json:"decidedAt,omitempty"`                         // 批准或拒绝的时间
}

// 申请落这张表，不含密钥原文。
func (OnboardingRequest) TableName() string { return "onboarding_requests" }

// CreateOnboarding 写入一条待批准申请；顺手清掉过期的，同类待批准达到上限则拒绝。
func (s *Store) CreateOnboarding(ctx context.Context, row OnboardingRequest, maxPending int64) error {
	// 清理、计数和写入放在同一事务里。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 记下当前时刻，这一笔里的时间都用它。
		now := time.Now().UTC()
		// 过期的待批准和找回单、早已拒绝的申请都不再有用，清掉防止表越积越大。
		purge := tx.Where("(expires_at <= ? AND (status = ? OR kind = ?)) OR (status = ? AND decided_at < ?)",
			now, OnboardingPending, OnboardingResetSA, OnboardingRejected, now.Add(-onboardingKeepRejected))
		// 清理失败就停，不带着旧数据计数。
		if err := purge.Delete(&OnboardingRequest{}).Error; err != nil {
			return err
		}
		// 接住同类待批准的条数。
		var pending int64
		// 数同类还在等批准的申请，匿名入口不能无限写。
		if err := tx.Model(&OnboardingRequest{}).Where("kind = ? AND status = ?", row.Kind, OnboardingPending).Count(&pending).Error; err != nil {
			return err
		}
		// 到上限就拒绝，等管理员处理或申请过期。
		if pending >= maxPending {
			return domain.ErrTooManyRequests
		}
		// 同一家厂的找回只留最新一条，避免管理员批到旧单。
		if row.Kind == OnboardingResetSA {
			// 旧的待批准找回单删掉，失败就停。
			if err := tx.Where("kind = ? AND status = ? AND factory_id = ?", OnboardingResetSA, OnboardingPending, row.FactoryID).Delete(&OnboardingRequest{}).Error; err != nil {
				return err
			}
		}
		// 写入这条申请。
		return tx.Create(&row).Error
	})
}

// OnboardingBySecret 按密钥哈希取申请；对不上交回不存在。
func (s *Store) OnboardingBySecret(ctx context.Context, secretHash string) (OnboardingRequest, error) {
	// 准备接住库里的那一行。
	var row OnboardingRequest
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "secret_hash = ?", secretHash).Error; err != nil {
		// 没有这一行就按不存在交回。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return OnboardingRequest{}, domain.ErrNotFound
		}
		return OnboardingRequest{}, err
	}
	return row, nil
}

// OnboardingByID 按稳定身份取申请。
func (s *Store) OnboardingByID(ctx context.Context, id uuid.UUID) (OnboardingRequest, error) {
	// 准备接住库里的那一行。
	var row OnboardingRequest
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		// 没有这一行就按不存在交回。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return OnboardingRequest{}, domain.ErrNotFound
		}
		return OnboardingRequest{}, err
	}
	return row, nil
}

// ListPendingOnboarding 列出还能批准的申请，先到的在前。
func (s *Store) ListPendingOnboarding(ctx context.Context) ([]OnboardingRequest, error) {
	// 先给空表，一条都没有也不交空指针。
	rows := []OnboardingRequest{}
	// 只读待批准且没过期的。
	err := s.db.WithContext(ctx).Where("status = ? AND expires_at > ?", OnboardingPending, time.Now().UTC()).Order("created_at").Find(&rows).Error
	return rows, err
}

// lockOpenOnboarding 行锁取出还能批准的申请；已处理或已过期都拒绝。
func lockOpenOnboarding(tx *gorm.DB, id uuid.UUID, now time.Time) (OnboardingRequest, error) {
	// 准备接住锁住的那一行。
	var row OnboardingRequest
	// 行锁读出，两个管理员同时点时只有一个能批。
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
		// 没有这一行就按不存在交回。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return OnboardingRequest{}, domain.ErrNotFound
		}
		return OnboardingRequest{}, err
	}
	// 已处理或已过期都不能再批。
	if row.Status != OnboardingPending || !row.ExpiresAt.After(now) {
		return OnboardingRequest{}, domain.ErrOnboardingClosed
	}
	return row, nil
}

// ApproveEnrollRequest 批准新厂接入：同一事务写名录和初始超管，申请密钥哈希直接作建厂码哈希。
func (s *Store) ApproveEnrollRequest(ctx context.Context, id, factoryID, personID uuid.UUID, name string) (OnboardingRequest, error) {
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 准备接住批准后的申请。
	var out OnboardingRequest
	// 名录、初始超管和申请状态一起提交。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 行锁取出还能批准的申请。
		row, err := lockOpenOnboarding(tx, id, now)
		// 已处理或已过期就停。
		if err != nil {
			return err
		}
		// 这条入口只批新厂接入。
		if row.Kind != OnboardingEnroll {
			return domain.ErrOnboardingClosed
		}
		// 管理员没改名就用现场填的厂名。
		if name != "" {
			// 用管理员修正后的厂名。
			row.FactoryName = name
		}
		// 写名录和初始超管，短码在这时才发。
		if _, err := registerFactory(tx, factoryID, row.FactoryName, personID, row.SALogin, row.SADisplay, row.SecretHash, now); err != nil {
			return err
		}
		// 记下批准结果和最终厂名。
		row.Status, row.FactoryID, row.DecidedAt = OnboardingApproved, &factoryID, &now
		// 存回申请，失败整笔回滚。
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		// 交回事务外。
		out = row
		return nil
	})
	return out, err
}

// ApproveResetRequest 批准找回初始超管密码，并把可用时间收短到 until。
func (s *Store) ApproveResetRequest(ctx context.Context, id uuid.UUID, until time.Time) (OnboardingRequest, error) {
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 准备接住批准后的申请。
	var out OnboardingRequest
	// 锁住申请再改状态。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 行锁取出还能批准的申请。
		row, err := lockOpenOnboarding(tx, id, now)
		// 已处理或已过期就停。
		if err != nil {
			return err
		}
		// 这条入口只批找回。
		if row.Kind != OnboardingResetSA {
			return domain.ErrOnboardingClosed
		}
		// 批准后只留短时间给现场设新密码。
		row.Status, row.ExpiresAt, row.DecidedAt = OnboardingApproved, until, &now
		// 存回申请，失败不算批准。
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		// 交回事务外。
		out = row
		return nil
	})
	return out, err
}

// RejectOnboarding 拒绝一条还能批准的申请。
func (s *Store) RejectOnboarding(ctx context.Context, id uuid.UUID) (OnboardingRequest, error) {
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 准备接住拒绝后的申请。
	var out OnboardingRequest
	// 锁住申请再改状态。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 行锁取出还能处理的申请。
		row, err := lockOpenOnboarding(tx, id, now)
		// 已处理或已过期就停。
		if err != nil {
			return err
		}
		// 记下拒绝结果。
		row.Status, row.DecidedAt = OnboardingRejected, &now
		// 存回申请，失败不算拒绝。
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		// 交回事务外。
		out = row
		return nil
	})
	return out, err
}

// ConsumeResetRequest 用掉已批准的找回单：只成功一次，必须是这家厂且没过期。
func (s *Store) ConsumeResetRequest(ctx context.Context, factoryID uuid.UUID, secretHash string) error {
	// 一条条件更新完成检查和用掉，并发时只有一个能成。
	res := s.db.WithContext(ctx).Model(&OnboardingRequest{}).
		Where("secret_hash = ? AND factory_id = ? AND kind = ? AND status = ? AND expires_at > ?",
			secretHash, factoryID, OnboardingResetSA, OnboardingApproved, time.Now().UTC()).
		Update("status", OnboardingUsed)
	// 写库报错就停，不能当成已经用掉。
	if res.Error != nil {
		return res.Error
	}
	// 没改到行说明没批准、已用过、过期或不是这家厂。
	if res.RowsAffected == 0 {
		return domain.ErrInvalidEnrollment
	}
	return nil
}
