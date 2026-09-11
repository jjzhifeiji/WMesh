package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

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

// Client 是本厂已接受的一台现场节点绑定。
type Client struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`       // 与 WAN 相同的 Client 稳定身份
	PublicKey       []byte     `gorm:"type:bytea;not null" json:"publicKey"` // 本机公钥；无私钥
	BindingRevision int64      `gorm:"not null" json:"bindingRevision"`      // 已接受的绑定修订，只向前
	Status          string     `gorm:"not null" json:"status"`               // bound / void
	BoundAt         time.Time  `gorm:"not null" json:"boundAt"`              // 最近一次接受为 bound 的时间
	VoidedAt        *time.Time `json:"voidedAt"`                             // 作废时间；bound 必须为空
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

// OrgOption 是人员离线授权里当时可选的一个组织节点及其祖先路径。
type OrgOption struct {
	OrgUnitID uuid.UUID  `json:"orgUnitId"` // 当时可选节点
	Path      []PathNode `json:"path"`      // 当时从工厂到该节点的祖先快照
}

// RoleSnapshot 是签发时一条角色与作用域，离线不再查厂库。
type RoleSnapshot struct {
	Role      string     `json:"role"`                // 六种固定角色之一
	ScopeKind string     `json:"scopeKind"`           // factory 或 org_unit
	OrgUnitID *uuid.UUID `json:"orgUnitId,omitempty"` // Factory 作用域为空
}

// PersonOfflineGrant 是签给本厂账号、绑定特定 Client 的人员离线授权快照。
type PersonOfflineGrant struct {
	ID            uuid.UUID      // 人员离线授权稳定身份
	PersonID      uuid.UUID      // 本厂账号稳定身份
	ClientID      uuid.UUID      // 绑定到的本厂 Client
	Revision      int64          // 该账号在该 Client 上的授权修订，只向前
	LoginName     string         // 签发时登录名，离线对照用，不是身份
	PasswordHash  string         // 该人口令验证材料副本；不进 JSON
	AllowDirect   bool           // 是否允许 Factory 直属
	OrgSnapshot   []OrgOption    // 当时可选 OrgUnit 及祖先路径
	RolesSnapshot []RoleSnapshot // 当时固定角色与作用域
	NotBefore     time.Time      // 生效时间
	NotAfter      time.Time      // 失效时间
	Payload       []byte         // 被签名的声明原文
	Signature     []byte         // Ed25519 签名 64 字节
	CreatedAt     time.Time      // 写入时间
}

type personOfflineGrantRow struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey"` // 人员离线授权稳定身份
	PersonID      uuid.UUID `gorm:"type:uuid;not null"`   // 本厂账号稳定身份
	ClientID      uuid.UUID `gorm:"type:uuid;not null"`   // 绑定到的本厂 Client
	Revision      int64     `gorm:"not null"`             // 该账号在该 Client 上的授权修订，只向前
	LoginName     string    `gorm:"not null"`             // 签发时登录名，离线对照用，不是身份
	PasswordHash  string    `gorm:"not null"`             // 该人口令验证材料副本，不是全厂账号库
	AllowDirect   bool      `gorm:"not null"`             // 是否允许 Factory 直属
	OrgSnapshot   []byte    `gorm:"type:jsonb;not null"`  // 当时可选 OrgUnit 及祖先路径
	RolesSnapshot []byte    `gorm:"type:jsonb;not null"`  // 当时固定角色与作用域
	NotBefore     time.Time `gorm:"not null"`             // 生效时间
	NotAfter      time.Time `gorm:"not null"`             // 失效时间
	Payload       []byte    `gorm:"type:bytea;not null"`  // 被签名的声明原文
	Signature     []byte    `gorm:"type:bytea;not null"`  // Ed25519 签名 64 字节
	CreatedAt     time.Time `gorm:"not null"`             // 写入时间
}

func (personOfflineGrantRow) TableName() string { return "person_offline_grants" }

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

// AcceptBinding 接受 WAN 送达的绑定；修订必须严格更大，公钥必须一致。
func (s *Store) AcceptBinding(ctx context.Context, clientID uuid.UUID, publicKey []byte, revision int64) (Client, error) {
	if revision < 1 {
		return Client{}, domain.ErrStaleRevision
	}
	now := time.Now().UTC()
	var out Client
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row Client
		err := tx.First(&row, "id = ?", clientID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = Client{
				ID:              clientID,
				PublicKey:       publicKey,
				BindingRevision: revision,
				Status:          ClientStatusBound,
				BoundAt:         now,
			}
			if err := tx.Create(&row).Error; err != nil {
				if domain.IsCheckViolation(err) {
					return domain.ErrInvalidKey
				}
				return err
			}
			out = row
			return nil
		}
		if err != nil {
			return err
		}
		if revision <= row.BindingRevision {
			return domain.ErrStaleRevision
		}
		if !bytes.Equal(row.PublicKey, publicKey) {
			return domain.ErrClientKeyMismatch
		}
		if err := tx.Model(&Client{}).Where("id = ?", clientID).Updates(map[string]any{
			"binding_revision": revision,
			"status":           ClientStatusBound,
			"bound_at":         now,
			"voided_at":        nil,
		}).Error; err != nil {
			if domain.IsCheckViolation(err) {
				return domain.ErrInvalidKey
			}
			return err
		}
		row.BindingRevision = revision
		row.Status = ClientStatusBound
		row.BoundAt = now
		row.VoidedAt = nil
		out = row
		return nil
	})
	return out, err
}

// VoidBinding 把本厂绑定标作废；之后不得再签发。
func (s *Store) VoidBinding(ctx context.Context, clientID uuid.UUID) error {
	now := time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&Client{}).
		Where("id = ? AND status = ?", clientID, ClientStatusBound).
		Updates(map[string]any{"status": ClientStatusVoid, "voided_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&Client{}).Where("id = ?", clientID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

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

// ListClients 列出本厂已接受的 Client，按最近接受时间倒序。
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

// ListPersonOfflineGrants 列出人员离线授权各修订，按人、Client、修订从新到旧。
func (s *Store) ListPersonOfflineGrants(ctx context.Context) ([]PersonOfflineGrant, error) {
	var rows []personOfflineGrantRow
	if err := s.db.WithContext(ctx).Order("person_id, client_id, revision DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]PersonOfflineGrant, 0, len(rows))
	for _, r := range rows {
		out = append(out, personGrantFromRow(r))
	}
	return out, nil
}

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

// InsertPersonOfflineGrant 写入一版人员离线授权快照；Client 必须有效绑定，修订必须严格更大。
func (s *Store) InsertPersonOfflineGrant(ctx context.Context, g PersonOfflineGrant) (PersonOfflineGrant, error) {
	if g.OrgSnapshot == nil {
		g.OrgSnapshot = []OrgOption{}
	}
	if g.RolesSnapshot == nil {
		g.RolesSnapshot = []RoleSnapshot{}
	}
	orgRaw, err := json.Marshal(g.OrgSnapshot)
	if err != nil {
		return PersonOfflineGrant{}, err
	}
	rolesRaw, err := json.Marshal(g.RolesSnapshot)
	if err != nil {
		return PersonOfflineGrant{}, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.assertClientBound(ctx, tx, g.ClientID); err != nil {
			return err
		}
		max, err := maxRevision(tx, &personOfflineGrantRow{}, "person_id = ? AND client_id = ?", g.PersonID, g.ClientID)
		if err != nil {
			return err
		}
		if g.Revision <= max {
			return domain.ErrStaleRevision
		}
		if g.ID == uuid.Nil {
			g.ID = id.New()
		}
		if g.CreatedAt.IsZero() {
			g.CreatedAt = time.Now().UTC()
		}
		row := personOfflineGrantRow{
			ID:            g.ID,
			PersonID:      g.PersonID,
			ClientID:      g.ClientID,
			Revision:      g.Revision,
			LoginName:     g.LoginName,
			PasswordHash:  g.PasswordHash,
			AllowDirect:   g.AllowDirect,
			OrgSnapshot:   orgRaw,
			RolesSnapshot: rolesRaw,
			NotBefore:     g.NotBefore,
			NotAfter:      g.NotAfter,
			Payload:       g.Payload,
			Signature:     g.Signature,
			CreatedAt:     g.CreatedAt,
		}
		if err := tx.Select("ID", "PersonID", "ClientID", "Revision", "LoginName", "PasswordHash", "AllowDirect", "OrgSnapshot", "RolesSnapshot", "NotBefore", "NotAfter", "Payload", "Signature", "CreatedAt").Create(&row).Error; err != nil {
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

func (s *Store) LatestPersonOfflineGrant(ctx context.Context, personID, clientID uuid.UUID) (PersonOfflineGrant, error) {
	var row personOfflineGrantRow
	if err := s.db.WithContext(ctx).Where("person_id = ? AND client_id = ?", personID, clientID).
		Order("revision DESC").First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PersonOfflineGrant{}, domain.ErrNotFound
		}
		return PersonOfflineGrant{}, err
	}
	return personGrantFromRow(row), nil
}

func personGrantFromRow(row personOfflineGrantRow) PersonOfflineGrant {
	orgs := []OrgOption{}
	if len(row.OrgSnapshot) > 0 {
		_ = json.Unmarshal(row.OrgSnapshot, &orgs)
	}
	roles := []RoleSnapshot{}
	if len(row.RolesSnapshot) > 0 {
		_ = json.Unmarshal(row.RolesSnapshot, &roles)
	}
	return PersonOfflineGrant{
		ID:            row.ID,
		PersonID:      row.PersonID,
		ClientID:      row.ClientID,
		Revision:      row.Revision,
		LoginName:     row.LoginName,
		PasswordHash:  row.PasswordHash,
		AllowDirect:   row.AllowDirect,
		OrgSnapshot:   orgs,
		RolesSnapshot: roles,
		NotBefore:     row.NotBefore,
		NotAfter:      row.NotAfter,
		Payload:       row.Payload,
		Signature:     row.Signature,
		CreatedAt:     row.CreatedAt,
	}
}
