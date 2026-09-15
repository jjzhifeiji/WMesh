package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/assetcode"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
)

const (
	ClientStatusBound = "bound" // 本厂有效绑定，可以签发
	ClientStatusVoid  = "void"  // 已作废，不得再签发
)

// SigningKey 是本厂签发密钥；全表一行，私钥不进审计。
type SigningKey struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`       // 本厂签发密钥行身份
	PublicKey  []byte    `gorm:"type:bytea;not null" json:"publicKey"` // Ed25519 公钥 32 字节
	PrivateKey []byte    `gorm:"type:bytea;not null" json:"-"`         // 本厂签发私钥，不是 Client 私钥
	CreatedAt  time.Time `gorm:"not null" json:"createdAt"`            // 写入时间
}

func (SigningKey) TableName() string { return "signing_keys" }

// Client 是本厂已接受的一台现场设备绑定。
type Client struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`        // 固定识别号，与 WAN 相同的全局身份
	Name            string     `gorm:"not null" json:"name"`                  // 给人看的设备名，可改，不当身份
	PublicKey       []byte     `gorm:"type:bytea" json:"publicKey"`           // 本机公钥；未上线为空，无私钥
	BindingRevision int64      `gorm:"not null" json:"bindingRevision"`       // 已接受的绑定修订，只向前
	Status          string     `gorm:"not null" json:"status"`                // bound / void
	BoundAt         time.Time  `gorm:"not null" json:"boundAt"`               // 最近一次接受为 bound 的时间
	VoidedAt        *time.Time `json:"voidedAt"`                              // 作废时间；bound 必须为空
	OperatorID      *uuid.UUID `gorm:"type:uuid" json:"operatorId,omitempty"` // 当前在本机登录的本厂账号；无人则为空
	ShortCode       string     `json:"shortCode,omitempty"`                   // Client 短码，随绑定补齐
	DeviceSerial    string     `json:"deviceSerial,omitempty"`                // 从设备读到的机械臂识别号；未登记为空
	UnwrapKey       []byte     `json:"-"`                                     // 到站重封解封钥；不进 JSON
}

func (Client) TableName() string { return "clients" }

// RuntimeGrant 是签给某 Client 的一版节点运行凭证。
type RuntimeGrant struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`       // 节点运行凭证稳定身份
	ClientID  uuid.UUID `gorm:"type:uuid;not null" json:"clientId"`   // 签给本厂这台 Client
	Revision  int64     `gorm:"not null" json:"revision"`             // 该 Client 的节点授权修订，只向前
	CanRun    bool      `gorm:"not null" json:"canRun"`               // 本修订是否允许运行
	NotBefore time.Time `gorm:"not null" json:"notBefore"`            // 生效时间
	NotAfter  time.Time `gorm:"not null" json:"notAfter"`             // 失效时间
	Payload   []byte    `gorm:"type:bytea;not null" json:"payload"`   // 被签名的声明原文
	Signature []byte    `gorm:"type:bytea;not null" json:"signature"` // Ed25519 签名 64 字节
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`            // 写入时间
}

func (RuntimeGrant) TableName() string { return "client_runtime_grants" }

// PutSigningKey 写入本厂唯一签发密钥；已有则拒绝。
func (s *Store) PutSigningKey(ctx context.Context, publicKey, privateKey []byte) (SigningKey, error) {
	row := SigningKey{
		ID:         id.New(),
		PublicKey:  publicKey,
		PrivateKey: privateKey,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return SigningKey{}, domain.ErrSigningKeyExists
		}
		if domain.IsCheckViolation(err) {
			return SigningKey{}, domain.ErrInvalidKey
		}
		return SigningKey{}, err
	}
	return row, nil
}

// SigningKey 取本厂唯一签发密钥，含私钥。
func (s *Store) SigningKey(ctx context.Context) (SigningKey, error) {
	var row SigningKey
	if err := s.db.WithContext(ctx).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SigningKey{}, domain.ErrNotFound
		}
		return SigningKey{}, err
	}
	return row, nil
}

// AcceptBinding 接受 WAN 送达的绑定；修订只向前，同修订可补名字或补公钥。
func (s *Store) AcceptBinding(ctx context.Context, clientID uuid.UUID, name string, publicKey []byte, revision int64) (Client, error) {
	if revision < 1 {
		return Client{}, domain.ErrStaleRevision
	}
	if len(publicKey) == 0 {
		publicKey = nil
	} else if len(publicKey) != 32 {
		return Client{}, domain.ErrInvalidKey
	}
	now := time.Now().UTC()
	var out Client
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row Client
		err := tx.First(&row, "id = ?", clientID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if strings.TrimSpace(name) == "" {
				name = "Client"
			}
			row = Client{
				ID:              clientID,
				Name:            name,
				PublicKey:       publicKey,
				BindingRevision: revision,
				Status:          ClientStatusBound,
				BoundAt:         now,
			}
			// 短码稍后随绑定帧补齐，空串不能落库。
			if err := tx.Omit("ShortCode").Create(&row).Error; err != nil {
				if domain.IsCheckViolation(err) {
					if len(publicKey) != 0 && len(publicKey) != 32 {
						return domain.ErrInvalidKey
					}
					return domain.ErrInvalidName
				}
				return err
			}
			out = row
			return nil
		}
		if err != nil {
			return err
		}
		// 修订只向前，迟到的帧丢掉。
		if revision < row.BindingRevision {
			return domain.ErrStaleRevision
		}
		// 已有公钥不能换成另一把。
		if len(publicKey) > 0 && len(row.PublicKey) > 0 && !bytes.Equal(row.PublicKey, publicKey) {
			return domain.ErrClientKeyMismatch
		}
		patch := map[string]any{
			"status":    ClientStatusBound,
			"bound_at":  now,
			"voided_at": nil,
		}
		if revision > row.BindingRevision {
			patch["binding_revision"] = revision
		}
		if strings.TrimSpace(name) != "" {
			patch["name"] = name
		}
		// 同修订只允许补尚未登记的公钥。
		if len(row.PublicKey) == 0 && len(publicKey) > 0 {
			patch["public_key"] = publicKey
		}
		if err := tx.Model(&Client{}).Where("id = ?", clientID).Updates(patch).Error; err != nil {
			if domain.IsCheckViolation(err) {
				return domain.ErrInvalidName
			}
			return err
		}
		if err := tx.First(&row, "id = ?", clientID).Error; err != nil {
			return err
		}
		out = row
		return nil
	})
	return out, err
}

// PutClientShortCode 写入 Client 短码；已有则必须相同。
func (s *Store) PutClientShortCode(ctx context.Context, clientID uuid.UUID, code string) error {
	if !assetcode.ValidClientOrigin(code) {
		return domain.ErrAssetCodeConflict
	}
	var row Client
	if err := s.db.WithContext(ctx).First(&row, "id = ?", clientID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrNotFound
		}
		return err
	}
	if row.ShortCode != "" && row.ShortCode != code {
		return domain.ErrAssetCodeConflict
	}
	return s.db.WithContext(ctx).Model(&Client{}).Where("id = ?", clientID).Update("short_code", code).Error
}

// RenameClient 只改给人看的名字，不改绑定修订。
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

// VoidBinding 把本厂绑定标作废；之后不得再签发。
func (s *Store) VoidBinding(ctx context.Context, clientID uuid.UUID) error {
	now := time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&Client{}).
		Where("id = ? AND status = ?", clientID, ClientStatusBound).
		Updates(map[string]any{"status": ClientStatusVoid, "voided_at": now, "operator_id": gorm.Expr("NULL")})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	// 已作废再调不算错。
	var n int64
	if err := s.db.WithContext(ctx).Model(&Client{}).Where("id = ?", clientID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ClientByID 按固定识别号取本厂绑定。
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

// SetClientOperator 记下谁在这台已绑定设备上登录；设备未落库则忽略。
func (s *Store) SetClientOperator(ctx context.Context, clientID, personID uuid.UUID) error {
	res := s.db.WithContext(ctx).Model(&Client{}).
		Where("id = ? AND status = ?", clientID, ClientStatusBound).
		Update("operator_id", personID)
	if res.Error != nil {
		if domain.IsForeignKeyViolation(res.Error) {
			return domain.ErrNotFound
		}
		return res.Error
	}
	return nil
}

// ClearOperatorForPersonExcept 同一人只留这一台设备的登录标记。
func (s *Store) ClearOperatorForPersonExcept(ctx context.Context, personID, keepClient uuid.UUID) error {
	return s.db.WithContext(ctx).Model(&Client{}).
		Where("operator_id = ? AND id <> ?", personID, keepClient).
		Update("operator_id", nil).Error
}

// PinDeviceSerial 把机械臂号钉到已绑定 Client；换臂覆盖本机旧号。本厂未作废号不得重复。
func (s *Store) PinDeviceSerial(ctx context.Context, clientID uuid.UUID, serial string) (Client, error) {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return Client{}, domain.ErrDeviceSerialRequired
	}
	var out Client
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row Client
		if err := tx.First(&row, "id = ?", clientID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		if row.Status != ClientStatusBound {
			return domain.ErrBindingVoid
		}
		var taken int64
		if err := tx.Model(&Client{}).
			Where("status = ? AND device_serial = ? AND id <> ?", ClientStatusBound, serial, clientID).
			Count(&taken).Error; err != nil {
			return err
		}
		if taken > 0 {
			return domain.ErrDeviceSerialTaken
		}
		patch := map[string]any{"device_serial": serial}
		if len(row.UnwrapKey) != 32 {
			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return err
			}
			patch["unwrap_key"] = key
		}
		if err := tx.Model(&Client{}).Where("id = ?", clientID).Updates(patch).Error; err != nil {
			if domain.IsUniqueViolation(err) {
				return domain.ErrDeviceSerialTaken
			}
			return err
		}
		if err := tx.First(&row, "id = ?", clientID).Error; err != nil {
			return err
		}
		out = row
		return nil
	})
	return out, err
}

// ListClients 列出本厂已接受的 Client，按接受时间从新到旧。
func (s *Store) ListClients(ctx context.Context) ([]Client, error) {
	var rows []Client
	if err := s.db.WithContext(ctx).Order("bound_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []Client{}
	}
	return rows, nil
}

// ListRuntimeGrants 列出节点运行凭证，按 Client、修订从新到旧。
func (s *Store) ListRuntimeGrants(ctx context.Context) ([]RuntimeGrant, error) {
	var rows []RuntimeGrant
	if err := s.db.WithContext(ctx).Order("client_id, revision DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []RuntimeGrant{}
	}
	return rows, nil
}

// assertClientBound 作废后不得再签发。
func (s *Store) assertClientBound(ctx context.Context, tx *gorm.DB, clientID uuid.UUID) error {
	var row Client
	db := s.db.WithContext(ctx)
	if tx != nil {
		db = tx
	}
	if err := db.First(&row, "id = ?", clientID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrNotFound
		}
		return err
	}
	if row.Status != ClientStatusBound {
		return domain.ErrBindingVoid
	}
	return nil
}

// maxRevision 该对象当前最大修订；没有则为 0。
func maxRevision(tx *gorm.DB, model any, query string, args ...any) (int64, error) {
	var n sql.NullInt64
	if err := tx.Model(model).Where(query, args...).Select("MAX(revision)").Scan(&n).Error; err != nil {
		return 0, err
	}
	if !n.Valid {
		return 0, nil
	}
	return n.Int64, nil
}

// InsertRuntimeGrant 写入一版节点运行凭证；Client 必须有效绑定，修订必须严格更大。
func (s *Store) InsertRuntimeGrant(ctx context.Context, g RuntimeGrant) (RuntimeGrant, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.assertClientBound(ctx, tx, g.ClientID); err != nil {
			return err
		}
		max, err := maxRevision(tx, &RuntimeGrant{}, "client_id = ?", g.ClientID)
		if err != nil {
			return err
		}
		// 修订必须严格更大。
		if g.Revision <= max {
			return domain.ErrStaleRevision
		}
		if g.ID == uuid.Nil {
			g.ID = id.New()
		}
		if g.CreatedAt.IsZero() {
			g.CreatedAt = time.Now().UTC()
		}
		if err := tx.Select("ID", "ClientID", "Revision", "CanRun", "NotBefore", "NotAfter", "Payload", "Signature", "CreatedAt").Create(&g).Error; err != nil {
			if domain.IsUniqueViolation(err) {
				return domain.ErrStaleRevision
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
	})
	return g, err
}

// LatestRuntimeGrant 取该 Client 最新一版运行凭证。
func (s *Store) LatestRuntimeGrant(ctx context.Context, clientID uuid.UUID) (RuntimeGrant, error) {
	var row RuntimeGrant
	if err := s.db.WithContext(ctx).Where("client_id = ?", clientID).Order("revision DESC").First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return RuntimeGrant{}, domain.ErrNotFound
		}
		return RuntimeGrant{}, err
	}
	return row, nil
}
