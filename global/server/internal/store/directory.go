package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/domain"
)

const (
	FactoryActive   = "active"   // 有效：可认领、可登录
	FactoryDisabled = "disabled" // 停用：可再启用
	FactoryRetired  = "retired"  // 已注销：不能再启用
)

// Factory 是 WAN 名录里的一家工厂，不含厂内组织或普通账号。
type Factory struct {
	ID                    uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`    // 工厂稳定身份，也用来选厂库
	Name                  string     `gorm:"not null" json:"name"`              // 工厂显示名，不当身份
	Status                string     `gorm:"not null" json:"status"`            // 治理状态：active / disabled / retired
	LifecycleRevision     int64      `gorm:"not null" json:"lifecycleRevision"` // 厂端只接受更高修订
	StatusChangedAt       *time.Time `json:"statusChangedAt,omitempty"`         // 最近一次停用、启用或注销
	EnrollmentTokenHash   *string    `json:"-"`                                 // 一次性建厂码哈希，认领后清空
	EnrolledAt            *time.Time `json:"enrolledAt,omitempty"`              // 厂端认领成功时间；未认领为空
	ChannelConnectedAt    *time.Time `json:"channelConnectedAt,omitempty"`      // 当前 MQTT 会话连上的时间；空表示离线
	ChannelLastSeenAt     *time.Time `json:"channelLastSeenAt,omitempty"`       // 最近一次 MQTT 保活
	ChannelDisconnectedAt *time.Time `json:"channelDisconnectedAt,omitempty"`   // 最近一次断开；在线时为空
	ChannelOnline         bool       `gorm:"-" json:"channelOnline"`            // 当前有没有钉死的厂端通道
	WebVersion            int64      `json:"webVersion"`                        // 厂端前端版本号；离线时名录清成 0
	WebVersionName        string     `json:"webVersionName"`                    // 厂端前端版本名
	ServiceVersion        int64      `json:"serviceVersion"`                    // 厂端服务版本号；离线时名录清成 0
	ServiceVersionName    string     `json:"serviceVersionName"`                // 厂端服务版本名
	ShortCode             string     `gorm:"not null" json:"shortCode"`         // 本厂短码 F01…F99，创建后不改
	CreatedAt             time.Time  `gorm:"not null" json:"createdAt"`         // 名录入库时间
}

// fillPresence 有当前连接时间才算在线，不另存列；离线不把上次版本当正在跑。
func (f *Factory) fillPresence() {
	// 有连接时间才算在线，这个标记不另存一列。
	f.ChannelOnline = f.ChannelConnectedAt != nil
	// 离线时不把上次报的版本当成正在跑。
	if !f.ChannelOnline {
		// 离线时清掉前端版本号，避免名录显示旧包。
		f.WebVersion = 0
		// 离线时前端版本名也清掉，不把旧名字留下。
		f.WebVersionName = ""
		f.ServiceVersion = 0
		// 离线时服务版本名也清掉，不把旧名字留下。
		f.ServiceVersionName = ""
	}
}

// 工厂名录落这张表，不含厂内组织。
func (Factory) TableName() string { return "factories" }

// InitialSuperAdmin 是交付对账用的初始超管身份，不含日常密码。
type InitialSuperAdmin struct {
	FactoryID   uuid.UUID `gorm:"type:uuid;primaryKey" json:"factoryId"` // 一厂只能绑一名初始超管
	PersonID    uuid.UUID `gorm:"type:uuid;not null" json:"personId"`    // 落在目标厂库里的账号身份
	LoginName   string    `gorm:"not null" json:"loginName"`             // 交付时的登录名，不是秘密
	DisplayName string    `gorm:"not null" json:"displayName"`           // 交付时显示名，不是秘密
	CreatedAt   time.Time `gorm:"not null" json:"createdAt"`             // 对账记录写入时间
}

// 初始超管对账落这张表，不含日常密码。
func (InitialSuperAdmin) TableName() string { return "initial_super_admins" }

// RegisterFactory 用调用方给的稳定身份写入名录，并在同一事务绑定初始超管与建厂码哈希，不存密码。
func (s *Store) RegisterFactory(ctx context.Context, factoryID uuid.UUID, name string, personID uuid.UUID, saLogin, saDisplay, enrollHash string) (Factory, error) {
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 建厂码只留哈希，原文不进名录。
	hash := enrollHash
	// 先留出名录行，短码和超管在同一事务里写。
	var fac Factory
	// 名录、短码和初始超管在同一事务里写入。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 行锁取出下一个短码，避免并发发出重号。
		short, err := nextOriginCode(tx, originKindFactory)
		// 短码发不出来就停，避免空号落库。
		if err != nil {
			return err
		}
		// 名录行和短码一起组好，认领前带着建厂码哈希。
		fac = Factory{ID: factoryID, Name: name, Status: FactoryActive, EnrollmentTokenHash: &hash, ShortCode: short, CreatedAt: now}
		// 这一行没写进去就停，不能当成已经落库。
		if err := tx.Create(&fac).Error; err != nil {
			return err
		}
		// 名录写好后绑上初始超管，失败则整笔回滚。
		return bindInitialSuperAdmin(tx, factoryID, personID, saLogin, saDisplay, now)
	})
	// 事务没提交就停，名录和超管不能只留一半。
	if err != nil {
		return Factory{}, err
	}
	return fac, nil
}

// BindInitialSuperAdmin 记下该厂初始超管身份与登录名；一厂只能绑一名。
func (s *Store) BindInitialSuperAdmin(ctx context.Context, factoryID, personID uuid.UUID, loginName, displayName string) error {
	// 在这次连接里绑定初始超管，一厂只能一名。
	return bindInitialSuperAdmin(s.db.WithContext(ctx), factoryID, personID, loginName, displayName, time.Now().UTC())
}

// bindInitialSuperAdmin 一厂只能绑一名初始超管，不含密码。
func bindInitialSuperAdmin(tx *gorm.DB, factoryID, personID uuid.UUID, loginName, displayName string, at time.Time) error {
	// 组好对账行，只留身份和登录名，不含密码。
	row := InitialSuperAdmin{FactoryID: factoryID, PersonID: personID, LoginName: loginName, DisplayName: displayName, CreatedAt: at}
	// 写入失败先停住，再看是重复还是约束没过。
	if err := tx.Create(&row).Error; err != nil {
		// 这家厂已经绑过初始超管。
		if domain.IsUniqueViolation(err) {
			return domain.ErrInitialSAExists
		}
		return err
	}
	return nil
}

// FactoryByID 按稳定身份取名录行。
func (s *Store) FactoryByID(ctx context.Context, factoryID uuid.UUID) (Factory, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row Factory
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", factoryID).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Factory{}, domain.ErrNotFound
		}
		return Factory{}, err
	}
	// 按连接时间补是否在线，离线清掉版本。
	row.fillPresence()
	return row, nil
}

// ListFactories 列出全部工厂名录行，按创建时间从新到旧。
func (s *Store) ListFactories(ctx context.Context) ([]Factory, error) {
	// 先给空表，一家都没有也不交空指针。
	rows := []Factory{}
	// 按创建时间从新到旧读出名录。
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error
	// 每家厂补上是否在线，离线就把版本清掉。
	for i := range rows {
		// 按连接时间补是否在线，离线清掉版本。
		rows[i].fillPresence()
	}
	return rows, err
}

// SetFactoryStatus 写入治理状态并升高修订；相同状态不升修订。
func (s *Store) SetFactoryStatus(ctx context.Context, factoryID uuid.UUID, status string) (Factory, error) {
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 准备接住改完的名录行，事务外再补在线。
	var out Factory
	// 改状态并升高修订放在同一事务里。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 取不到或库出错先停住，再区分没有还是故障。
		if err := tx.First(&out, "id = ?", factoryID).Error; err != nil {
			// 没有这一行就按不存在交回，不当成库故障。
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		// 相同状态不升修订，避免厂端被无意义推送。
		if out.Status == status {
			return nil
		}
		// 已注销不能再启用或停用。
		if out.Status == FactoryRetired && status != FactoryRetired {
			return domain.ErrFactoryRetired
		}
		// 写入新状态，并把生命周期修订加一。
		res := tx.Model(&Factory{}).Where("id = ?", factoryID).Updates(map[string]any{
			"status":             status,
			"lifecycle_revision": out.LifecycleRevision + 1,
			"status_changed_at":  now,
		})
		// 写库报错就停，不能当成已经改成。
		if res.Error != nil {
			return res.Error
		}
		// 状态写完再读出当前行，修订以库为准。
		return tx.First(&out, "id = ?", factoryID).Error
	})
	// 事务没提交就停，调用方拿不到新状态。
	if err != nil {
		return Factory{}, err
	}
	// 按连接时间补是否在线，离线清掉版本。
	out.fillPresence()
	return out, nil
}

// DeleteUnclaimedFactory 尚未认领的工厂从名录拿掉；已认领的不能走这条。
func (s *Store) DeleteUnclaimedFactory(ctx context.Context, factoryID uuid.UUID) error {
	// 未认领才删名录和对账，已认领则整笔不动。
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先取出这家厂，已经认领就不能删。
		var fac Factory
		// 取不到或库出错先停住，再区分没有还是故障。
		if err := tx.First(&fac, "id = ?", factoryID).Error; err != nil {
			// 没有这一行就按不存在交回，不当成库故障。
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		// 已认领不能从名录拿掉。
		if fac.EnrolledAt != nil {
			return domain.ErrReferenced
		}
		// 删除失败就停，不能当成已经清掉。
		if err := tx.Where("factory_id = ?", factoryID).Delete(&InitialSuperAdmin{}).Error; err != nil {
			return err
		}
		// 删掉名录行，还有别的行指着就失败。
		res := tx.Where("id = ?", factoryID).Delete(&Factory{})
		// 写库报错就停，不能当成已经改成。
		if res.Error != nil {
			// 还有别的行指着它，不能从名录拿掉。
			if domain.IsForeignKeyViolation(res.Error) {
				return domain.ErrReferenced
			}
			return res.Error
		}
		return nil
	})
}

// InitialSuperAdmin 读该厂交付对账用的初始超管身份。
func (s *Store) InitialSuperAdmin(ctx context.Context, factoryID uuid.UUID) (InitialSuperAdmin, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row InitialSuperAdmin
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "factory_id = ?", factoryID).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return InitialSuperAdmin{}, domain.ErrNotFound
		}
		return InitialSuperAdmin{}, err
	}
	return row, nil
}

// ListInitialSuperAdmins 一次取全部工厂的初始超管对账行，不含密码。
func (s *Store) ListInitialSuperAdmins(ctx context.Context) ([]InitialSuperAdmin, error) {
	// 先给空表，没有对账行也不交空指针。
	rows := []InitialSuperAdmin{}
	// 按创建时间读出全部初始超管对账，不含密码。
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error
	return rows, err
}
