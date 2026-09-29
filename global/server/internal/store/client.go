package store

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
)

// FactoryPublicKey 是某厂签发用的公钥，WAN 不存对应私钥。
type FactoryPublicKey struct {
	FactoryID uuid.UUID `gorm:"type:uuid;primaryKey" json:"factoryId"` // 该厂签发公钥，一对一
	PublicKey []byte    `gorm:"type:bytea;not null" json:"publicKey"`  // Ed25519 公钥 32 字节，无私钥
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`             // 登记时间
}

// 厂签发公钥落这张表，对应私钥不在这里。
func (FactoryPublicKey) TableName() string { return "factory_public_keys" }

// Client 是一台现场设备：固定识别号加名字，公钥可待上线再登记，同一时刻只属一个工厂。
type Client struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`  // 固定识别号，全局唯一
	Name            string     `gorm:"not null" json:"name"`            // 给人看的设备名，可改，不当身份
	PublicKey       []byte     `gorm:"type:bytea" json:"publicKey"`     // 本机公钥，上线时登记；未上线为空，无私钥
	FactoryID       *uuid.UUID `gorm:"type:uuid" json:"factoryId"`      // 当前所属工厂；空表示未分配
	BindingRevision int64      `gorm:"not null" json:"bindingRevision"` // 绑定修订；未分配为 0，改分必须升高
	BoundAt         *time.Time `json:"boundAt"`                         // 当前这次分配生效时间；未分配为空
	ShortCode       string     `gorm:"not null" json:"shortCode"`       // Client 短码 C0001…C9999，登记后不改
	DeviceSerial    string     `json:"deviceSerial,omitempty"`          // 机械臂识别号；未填为空，不当身份
	CreatedAt       time.Time  `gorm:"not null" json:"createdAt"`       // 身份登记时间
}

// 现场设备落这张表，不跟默认复数走。
func (Client) TableName() string { return "clients" }

// PutFactoryPublicKey 登记该厂签发公钥；一厂一把，不存私钥。
func (s *Store) PutFactoryPublicKey(ctx context.Context, factoryID uuid.UUID, publicKey []byte) error {
	// 组好这厂的签发公钥，私钥不在这行。
	row := FactoryPublicKey{
		FactoryID: factoryID,
		PublicKey: publicKey,
		CreatedAt: time.Now().UTC(),
	}
	// 写入失败先停住，再看是重复还是约束没过。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 这家厂的签发公钥已经登记过。
		if domain.IsUniqueViolation(err) {
			return domain.ErrFactoryKeyExists
		}
		// 长度或格式不合格，按坏钥拒绝。
		if domain.IsCheckViolation(err) {
			return domain.ErrInvalidKey
		}
		// 引用的厂或资产不在名录，按不存在拒绝。
		if domain.IsForeignKeyViolation(err) {
			return domain.ErrNotFound
		}
		return err
	}
	return nil
}

// FactoryPublicKey 取该厂签发公钥，不含私钥。
func (s *Store) FactoryPublicKey(ctx context.Context, factoryID uuid.UUID) (FactoryPublicKey, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row FactoryPublicKey
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "factory_id = ?", factoryID).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FactoryPublicKey{}, domain.ErrNotFound
		}
		return FactoryPublicKey{}, err
	}
	return row, nil
}

// normalizePublicKey 空切片收成 nil，避免未上线写成空数组。
func normalizePublicKey(publicKey []byte) []byte {
	// 还没上线就存空，不写成零长度数组。
	if len(publicKey) == 0 {
		return nil
	}
	return publicKey
}

// normalizeStoredSerial 去掉首尾空白；空号允许，超过 128 字拒绝。
func normalizeStoredSerial(serial string) (string, error) {
	// 去掉首尾空白，空号允许留下。
	serial = strings.TrimSpace(serial)
	// 识别号超长就拒绝，避免截断以后撞号。
	if utf8.RuneCountInString(serial) > 128 {
		return "", domain.ErrDeviceSerialRequired
	}
	return serial, nil
}

// CreateClient 登记一台未分配节点；名字必填，公钥可空，识别号可空，身份由调用方给出或现场发号。
func (s *Store) CreateClient(ctx context.Context, clientID uuid.UUID, name string, publicKey []byte, deviceSerial string) (Client, error) {
	// 没给识别号就现场发号，登记必须有稳定身份。
	if clientID == uuid.Nil {
		// 没给识别号就现场发一个稳定身份。
		clientID = id.New()
	}
	// 识别号去掉空白，超长在这里拒绝。
	serial, err := normalizeStoredSerial(deviceSerial)
	// 识别号过长就停，不截断后硬写进库。
	if err != nil {
		return Client{}, err
	}
	// 准备接住库里的那一行，没有再另作处理。
	var row Client
	// 短码和设备行一起写入，失败不留半条。
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 行锁取出下一个短码，避免并发发出重号。
		short, err := nextOriginCode(tx, originKindClient)
		// 短码发不出来就停，避免空号落库。
		if err != nil {
			return err
		}
		// 未分配的设备行组好，修订先是零。
		row = Client{
			ID:              clientID,
			Name:            name,
			PublicKey:       normalizePublicKey(publicKey),
			BindingRevision: 0,
			ShortCode:       short,
			DeviceSerial:    serial,
			CreatedAt:       time.Now().UTC(),
		}
		// 写入失败先停住，再看是重复还是约束没过。
		if err := tx.Create(&row).Error; err != nil {
			// 唯一约束撞了，下面再分是识别号还是公钥。
			if domain.IsUniqueViolation(err) {
				// 撞上的是机械臂识别号，按号被占用拒绝。
				if strings.Contains(domain.UniqueConstraint(err), "device_serial") {
					return domain.ErrDeviceSerialTaken
				}
				return domain.ErrClientKeyTaken
			}
			// 检查没过，下面再分是坏钥还是坏名字。
			if domain.IsCheckViolation(err) {
				// 带了公钥但长度不对，按坏钥而不是坏名字拒绝。
				if len(normalizePublicKey(publicKey)) != 0 && len(normalizePublicKey(publicKey)) != 32 {
					return domain.ErrInvalidKey
				}
				return domain.ErrInvalidName
			}
			return err
		}
		return nil
	})
	// 事务没提交就停，短码和设备不能只留一半。
	if err != nil {
		return Client{}, err
	}
	return row, nil
}

// ClientByID 按固定识别号取现场设备。
func (s *Store) ClientByID(ctx context.Context, clientID uuid.UUID) (Client, error) {
	// 准备接住库里的那一行，没有再另作处理。
	var row Client
	// 取不到或库出错先停住，再区分没有还是故障。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", clientID).Error; err != nil {
		// 没有这一行就按不存在交回，不当成库故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Client{}, domain.ErrNotFound
		}
		return Client{}, err
	}
	return row, nil
}

// ListClients 列出已登记的现场设备，不含私钥；按创建时间从新到旧。
func (s *Store) ListClients(ctx context.Context) ([]Client, error) {
	// 准备接住查出来的列表，空的也要能交回。
	var rows []Client
	// 列表没读出来就停，故障不能当成空表。
	if err := s.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 空列表不要交空指针，改成空表再返回。
	if rows == nil {
		// 查询交回空指针时改成空表，调用方好遍历。
		rows = []Client{}
	}
	return rows, nil
}

// ListClientsByFactory 列出当前分给该厂的节点，供通道补送。
func (s *Store) ListClientsByFactory(ctx context.Context, factoryID uuid.UUID) ([]Client, error) {
	// 准备接住查出来的列表，空的也要能交回。
	var rows []Client
	// 列表没读出来就停，故障不能当成空表。
	if err := s.db.WithContext(ctx).Where("factory_id = ?", factoryID).Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 空列表不要交空指针，改成空表再返回。
	if rows == nil {
		// 查询交回空指针时改成空表，调用方好遍历。
		rows = []Client{}
	}
	return rows, nil
}

// RenameClient 只改给人看的名字，不改稳定身份、不升绑定修订。
func (s *Store) RenameClient(ctx context.Context, clientID uuid.UUID, name string) (Client, error) {
	// 只改给人看的名字，不升绑定修订。
	res := s.db.WithContext(ctx).Model(&Client{}).Where("id = ?", clientID).Update("name", name)
	// 写库报错就停，不能当成已经改成。
	if res.Error != nil {
		// 名字不合格，不把库检查原文抛出去。
		if domain.IsCheckViolation(res.Error) {
			return Client{}, domain.ErrInvalidName
		}
		return Client{}, res.Error
	}
	// 一行都没碰到，按不存在拒绝。
	if res.RowsAffected == 0 {
		return Client{}, domain.ErrNotFound
	}
	// 名字改完再读当前行，交回库里的结果。
	return s.ClientByID(ctx, clientID)
}

// SetClientPublicKey 给尚未登记公钥的节点补上本机公钥。
func (s *Store) SetClientPublicKey(ctx context.Context, clientID uuid.UUID, publicKey []byte) error {
	// 空钥收成空值，避免未上线写成空数组。
	publicKey = normalizePublicKey(publicKey)
	// 公钥必须是三十二字节，短的直接拒绝。
	if len(publicKey) != 32 {
		return domain.ErrInvalidKey
	}
	// 只给还没有公钥的行补上这一把。
	res := s.db.WithContext(ctx).Model(&Client{}).
		Where("id = ? AND (public_key IS NULL OR octet_length(public_key) = 0)", clientID).
		Update("public_key", publicKey)
	// 写库报错就停，不能当成已经改成。
	if res.Error != nil {
		// 这把公钥已经属于别的设备。
		if domain.IsUniqueViolation(res.Error) {
			return domain.ErrClientKeyTaken
		}
		// 长度或格式不合格，按坏钥拒绝。
		if domain.IsCheckViolation(res.Error) {
			return domain.ErrInvalidKey
		}
		return res.Error
	}
	// 一行都没改到，这次不能算成功。
	if res.RowsAffected == 0 {
		// 没改到行就读出现状，分同钥、换钥和没有这台。
		cur, err := s.ClientByID(ctx, clientID)
		// 设备没读到就停，分不清是同钥、换钥还是没有这台。
		if err != nil {
			return err
		}
		// 同钥再写算成功；已有另一把不能换。
		if bytes.Equal(cur.PublicKey, publicKey) {
			return nil
		}
		// 已经有另一把公钥，不能换成新的。
		if len(cur.PublicKey) > 0 {
			return domain.ErrInvalidKey
		}
		return domain.ErrNotFound
	}
	return nil
}

// BindClient 把未绑定 Client 绑到一厂，绑定修订从 0 升到 1。
func (s *Store) BindClient(ctx context.Context, clientID, factoryID uuid.UUID) (Client, error) {
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 准备接住事务里写好的结果，失败则不用它。
	var out Client
	// 确认尚未绑定再改归属，修订从零升到一。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 准备接住库里的那一行，没有再另作处理。
		var row Client
		// 取不到或库出错先停住，再区分没有还是故障。
		if err := tx.First(&row, "id = ?", clientID).Error; err != nil {
			// 没有这一行就按不存在交回，不当成库故障。
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		// 已属一厂不能再绑。
		if row.FactoryID != nil {
			return domain.ErrClientBound
		}
		// 目标厂必须已在名录。
		if err := tx.First(&Factory{}, "id = ?", factoryID).Error; err != nil {
			// 目标厂不在名录就按不存在拒绝。
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		// 只改尚未分配的行，修订从 0 升到 1。
		res := tx.Model(&Client{}).Where("id = ? AND factory_id IS NULL", clientID).Updates(map[string]any{
			"factory_id":       factoryID,
			"binding_revision": int64(1),
			"bound_at":         now,
		})
		// 写库报错就停，不能当成已经改成。
		if res.Error != nil {
			// 绑定约束没过，按已经绑定拒绝。
			if domain.IsCheckViolation(res.Error) {
				return domain.ErrClientBound
			}
			return res.Error
		}
		// 没改到符合条件的行，绑定状态不动。
		if res.RowsAffected == 0 {
			return domain.ErrClientBound
		}
		// 内存里的归属改到目标厂，和库保持一致。
		row.FactoryID = &factoryID
		// 内存里的修订一起升高，交出去才是新值。
		row.BindingRevision = 1
		// 记下这次分配生效的时刻。
		row.BoundAt = &now
		// 事务成功时才把这一行交出去。
		out = row
		return nil
	})
	return out, err
}

// RebindClient 把已绑定 Client 改到另一厂，绑定修订必须升高。
func (s *Store) RebindClient(ctx context.Context, clientID, factoryID uuid.UUID) (Client, error) {
	// 记下当前时刻，这一笔里的时间都用它。
	now := time.Now().UTC()
	// 准备接住事务里写好的结果，失败则不用它。
	var out Client
	// 确认已绑定且换了厂，再把修订升高。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 准备接住库里的那一行，没有再另作处理。
		var row Client
		// 取不到或库出错先停住，再区分没有还是故障。
		if err := tx.First(&row, "id = ?", clientID).Error; err != nil {
			// 没有这一行就按不存在交回，不当成库故障。
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		// 未绑定不能改绑。
		if row.FactoryID == nil {
			return domain.ErrUnbound
		}
		// 仍是同一厂不算改分。
		if *row.FactoryID == factoryID {
			return domain.ErrClientBound
		}
		// 目标厂必须已在名录。
		if err := tx.First(&Factory{}, "id = ?", factoryID).Error; err != nil {
			// 目标厂不在名录就按不存在拒绝。
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		// 改分必须升高修订，厂端只认更高修订。
		next := row.BindingRevision + 1
		// 只改已经分给别厂的行。
		res := tx.Model(&Client{}).Where("id = ? AND factory_id IS NOT NULL AND factory_id <> ?", clientID, factoryID).
			Updates(map[string]any{
				"factory_id":       factoryID,
				"binding_revision": next,
				"bound_at":         now,
			})
		// 写库报错就停，不能当成已经改成。
		if res.Error != nil {
			return res.Error
		}
		// 没改到符合条件的行，绑定状态不动。
		if res.RowsAffected == 0 {
			return domain.ErrClientBound
		}
		// 内存里的归属改到目标厂，和库保持一致。
		row.FactoryID = &factoryID
		// 内存里的修订一起升高，交出去才是新值。
		row.BindingRevision = next
		// 记下这次分配生效的时刻。
		row.BoundAt = &now
		// 事务成功时才把这一行交出去。
		out = row
		return nil
	})
	return out, err
}

// HasColumn 看某表是否已有该列，给迁移夹具用。
func (s *Store) HasColumn(ctx context.Context, table, column string) (bool, error) {
	// 准备接住在不在，零值先不当成没有。
	var exists bool
	// 问这张表有没有这一列，给迁移夹具用。
	err := s.db.WithContext(ctx).
		Raw("SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = ? AND column_name = ?)", table, column).
		Scan(&exists).Error
	return exists, err
}
