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
	DeviceSerial    string     `json:"deviceSerial,omitempty"`         // 机械臂识别号；未填为空，不当身份
	CreatedAt       time.Time  `gorm:"not null" json:"createdAt"`       // 身份登记时间
}

func (Client) TableName() string { return "clients" }

// PutFactoryPublicKey 登记该厂签发公钥；一厂一把，不存私钥。
func (s *Store) PutFactoryPublicKey(ctx context.Context, factoryID uuid.UUID, publicKey []byte) error {
	row := FactoryPublicKey{
		FactoryID: factoryID,
		PublicKey: publicKey,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return domain.ErrFactoryKeyExists
		}
		if domain.IsCheckViolation(err) {
			return domain.ErrInvalidKey
		}
		if domain.IsForeignKeyViolation(err) {
			return domain.ErrNotFound
		}
		return err
	}
	return nil
}

// FactoryPublicKey 取该厂签发公钥，不含私钥。
func (s *Store) FactoryPublicKey(ctx context.Context, factoryID uuid.UUID) (FactoryPublicKey, error) {
	var row FactoryPublicKey
	if err := s.db.WithContext(ctx).First(&row, "factory_id = ?", factoryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FactoryPublicKey{}, domain.ErrNotFound
		}
		return FactoryPublicKey{}, err
	}
	return row, nil
}

// normalizePublicKey 空切片收成 nil，避免未上线写成空数组。
func normalizePublicKey(publicKey []byte) []byte {
	if len(publicKey) == 0 {
		return nil
	}
	return publicKey
}

// normalizeStoredSerial 去掉首尾空白；空号允许，超过 128 字拒绝。
func normalizeStoredSerial(serial string) (string, error) {
	serial = strings.TrimSpace(serial)
	if utf8.RuneCountInString(serial) > 128 {
		return "", domain.ErrDeviceSerialRequired
	}
	return serial, nil
}

// CreateClient 登记一台未分配节点；名字必填，公钥可空，识别号可空，身份由调用方给出或现场发号。
func (s *Store) CreateClient(ctx context.Context, clientID uuid.UUID, name string, publicKey []byte, deviceSerial string) (Client, error) {
	if clientID == uuid.Nil {
		clientID = id.New()
	}
	serial, err := normalizeStoredSerial(deviceSerial)
	if err != nil {
		return Client{}, err
	}
	var row Client
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		short, err := nextOriginCode(tx, originKindClient)
		if err != nil {
			return err
		}
		row = Client{
			ID:              clientID,
			Name:            name,
			PublicKey:       normalizePublicKey(publicKey),
			BindingRevision: 0,
			ShortCode:       short,
			DeviceSerial:    serial,
			CreatedAt:       time.Now().UTC(),
		}
		if err := tx.Create(&row).Error; err != nil {
			if domain.IsUniqueViolation(err) {
				if strings.Contains(domain.UniqueConstraint(err), "device_serial") {
					return domain.ErrDeviceSerialTaken
				}
				return domain.ErrClientKeyTaken
			}
			if domain.IsCheckViolation(err) {
				if len(normalizePublicKey(publicKey)) != 0 && len(normalizePublicKey(publicKey)) != 32 {
					return domain.ErrInvalidKey
				}
				return domain.ErrInvalidName
			}
			return err
		}
		return nil
	})
	if err != nil {
		return Client{}, err
	}
	return row, nil
}

// ClientByID 按固定识别号取现场设备。
func (s *Store) ClientByID(ctx context.Context, clientID uuid.UUID) (Client, error) {
	var row Client
	if err := s.db.WithContext(ctx).First(&row, "id = ?", clientID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Client{}, domain.ErrNotFound
		}
		return Client{}, err
	}
	return row, nil
}

// ListClients 列出已登记的现场设备，不含私钥；按创建时间从新到旧。
func (s *Store) ListClients(ctx context.Context) ([]Client, error) {
	var rows []Client
	if err := s.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []Client{}
	}
	return rows, nil
}

// ListClientsByFactory 列出当前分给该厂的节点，供通道补送。
func (s *Store) ListClientsByFactory(ctx context.Context, factoryID uuid.UUID) ([]Client, error) {
	var rows []Client
	if err := s.db.WithContext(ctx).Where("factory_id = ?", factoryID).Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []Client{}
	}
	return rows, nil
}

// RenameClient 只改给人看的名字，不改稳定身份、不升绑定修订。
func (s *Store) RenameClient(ctx context.Context, clientID uuid.UUID, name string) (Client, error) {
	res := s.db.WithContext(ctx).Model(&Client{}).Where("id = ?", clientID).Update("name", name)
	if res.Error != nil {
		if domain.IsCheckViolation(res.Error) {
			return Client{}, domain.ErrInvalidName
		}
		return Client{}, res.Error
	}
	if res.RowsAffected == 0 {
		return Client{}, domain.ErrNotFound
	}
	return s.ClientByID(ctx, clientID)
}

// SetClientPublicKey 给尚未登记公钥的节点补上本机公钥。
func (s *Store) SetClientPublicKey(ctx context.Context, clientID uuid.UUID, publicKey []byte) error {
	publicKey = normalizePublicKey(publicKey)
	if len(publicKey) != 32 {
		return domain.ErrInvalidKey
	}
	res := s.db.WithContext(ctx).Model(&Client{}).
		Where("id = ? AND (public_key IS NULL OR octet_length(public_key) = 0)", clientID).
		Update("public_key", publicKey)
	if res.Error != nil {
		if domain.IsUniqueViolation(res.Error) {
			return domain.ErrClientKeyTaken
		}
		if domain.IsCheckViolation(res.Error) {
			return domain.ErrInvalidKey
		}
		return res.Error
	}
	if res.RowsAffected == 0 {
		cur, err := s.ClientByID(ctx, clientID)
		if err != nil {
			return err
		}
		// 同钥再写算成功；已有另一把不能换。
		if bytes.Equal(cur.PublicKey, publicKey) {
			return nil
		}
		if len(cur.PublicKey) > 0 {
			return domain.ErrInvalidKey
		}
		return domain.ErrNotFound
	}
	return nil
}

// BindClient 把未绑定 Client 绑到一厂，绑定修订从 0 升到 1。
func (s *Store) BindClient(ctx context.Context, clientID, factoryID uuid.UUID) (Client, error) {
	now := time.Now().UTC()
	var out Client
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row Client
		if err := tx.First(&row, "id = ?", clientID).Error; err != nil {
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
		if res.Error != nil {
			if domain.IsCheckViolation(res.Error) {
				return domain.ErrClientBound
			}
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrClientBound
		}
		row.FactoryID = &factoryID
		row.BindingRevision = 1
		row.BoundAt = &now
		out = row
		return nil
	})
	return out, err
}

// RebindClient 把已绑定 Client 改到另一厂，绑定修订必须升高。
func (s *Store) RebindClient(ctx context.Context, clientID, factoryID uuid.UUID) (Client, error) {
	now := time.Now().UTC()
	var out Client
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row Client
		if err := tx.First(&row, "id = ?", clientID).Error; err != nil {
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
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrClientBound
		}
		row.FactoryID = &factoryID
		row.BindingRevision = next
		row.BoundAt = &now
		out = row
		return nil
	})
	return out, err
}

// HasColumn 看某表是否已有该列，给迁移夹具用。
func (s *Store) HasColumn(ctx context.Context, table, column string) (bool, error) {
	var exists bool
	err := s.db.WithContext(ctx).
		Raw("SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = ? AND column_name = ?)", table, column).
		Scan(&exists).Error
	return exists, err
}
