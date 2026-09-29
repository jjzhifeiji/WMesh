package store

import (
	"bytes"
	"context"
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

// 指定落库表名，避免查询时按类型名去猜。
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
	UnwrapKey       []byte     `json:"-"`                                     // 历史列，焊机不持钥
}

// 指定落库表名，避免查询时按类型名去猜。
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

// 指定落库表名，避免查询时按类型名去猜。
func (RuntimeGrant) TableName() string { return "client_runtime_grants" }

// PutSigningKey 写入本厂唯一签发密钥；已有则拒绝。
func (s *Store) PutSigningKey(ctx context.Context, publicKey, privateKey []byte) (SigningKey, error) {
	// 组装签发行，私钥只留在本厂库。
	row := SigningKey{
		ID:         id.New(),
		PublicKey:  publicKey,
		PrivateKey: privateKey,
		CreatedAt:  time.Now().UTC(),
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 唯一约束撞了就改成业务冲突，不抛库原文。
		if domain.IsUniqueViolation(err) {
			return SigningKey{}, domain.ErrSigningKeyExists
		}
		// 检查约束不通过就改成业务拒绝。
		if domain.IsCheckViolation(err) {
			return SigningKey{}, domain.ErrInvalidKey
		}
		return SigningKey{}, err
	}
	return row, nil
}

// SigningKey 取本厂唯一签发密钥，含私钥。
func (s *Store) SigningKey(ctx context.Context) (SigningKey, error) {
	// 准备承接查到的签发密钥。
	var row SigningKey
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SigningKey{}, domain.ErrNotFound
		}
		return SigningKey{}, err
	}
	return row, nil
}

// AcceptBinding 接受 WAN 送达的绑定；修订只向前，同修订可补名字或补公钥。
func (s *Store) AcceptBinding(ctx context.Context, clientID uuid.UUID, name string, publicKey []byte, revision int64) (Client, error) {
	// 修订不更高就拒绝或忽略，避免旧帧覆盖。
	if revision < 1 {
		return Client{}, domain.ErrStaleRevision
	}
	// 没带公钥就存空，上线前允许还没有。
	if len(publicKey) == 0 {
		// 这里不记这个值，避免串数据或留下空钥。
		publicKey = nil
		// 长度不是三十二字节就拒绝。
	} else if len(publicKey) != 32 {
		return Client{}, domain.ErrInvalidKey
	}
	// 取当前时刻，时间列和租约用同一个时钟。
	now := time.Now().UTC()
	// 准备承接查到的现场设备。
	var out Client
	// 放进同一事务，中途失败就整单回滚。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 准备承接查到的现场设备。
		var row Client
		// 按条件去读，没有行交给后面的分支。
		err := tx.First(&row, "id = ?", clientID).Error
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 去掉空白这一支不成立就换路。
			if strings.TrimSpace(name) == "" {
				// 名字空了就退回备用名，避免空白项。
				name = "Client"
			}
			// 组装现场设备绑定，修订只向前。
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
				// 检查约束不通过就改成业务拒绝。
				if domain.IsCheckViolation(err) {
					// 长度不是三十二字节就拒绝。
					if len(publicKey) != 0 && len(publicKey) != 32 {
						return domain.ErrInvalidKey
					}
					return domain.ErrInvalidName
				}
				return err
			}
			// 把查到的结果交给事务外的调用方。
			out = row
			return nil
		}
		// 出错就停，避免把失败当成已经完成。
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
		// 只收集这次要改的列，避免整行覆盖。
		patch := map[string]any{
			"status":    ClientStatusBound,
			"bound_at":  now,
			"voided_at": nil,
		}
		// 只有更高修订才覆盖绑定，旧帧忽略。
		if revision > row.BindingRevision {
			// 把这一列放进待更新集合，只覆盖带来的字段。
			patch["binding_revision"] = revision
		}
		// 去掉空白这一支不成立就换路。
		if strings.TrimSpace(name) != "" {
			// 把这一列放进待更新集合，只覆盖带来的字段。
			patch["name"] = name
		}
		// 同修订只允许补尚未登记的公钥。
		if len(row.PublicKey) == 0 && len(publicKey) > 0 {
			// 把这一列放进待更新集合，只覆盖带来的字段。
			patch["public_key"] = publicKey
		}
		// 更新指定列失败就停，避免带着错误继续。
		if err := tx.Model(&Client{}).Where("id = ?", clientID).Updates(patch).Error; err != nil {
			if domain.IsCheckViolation(err) {
				return domain.ErrInvalidName
			}
			return err
		}
		// 按条件取一行失败就停，避免带着错误继续。
		if err := tx.First(&row, "id = ?", clientID).Error; err != nil {
			return err
		}
		// 把查到的结果交给事务外的调用方。
		out = row
		return nil
	})
	return out, err
}

// PutClientShortCode 写入 Client 短码；已有则必须相同。
func (s *Store) PutClientShortCode(ctx context.Context, clientID uuid.UUID, code string) error {
	// 核对编号这一支不成立就换路。
	if !assetcode.ValidClientOrigin(code) {
		return domain.ErrAssetCodeConflict
	}
	// 准备承接查到的现场设备。
	var row Client
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", clientID).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrNotFound
		}
		return err
	}
	// 已有不同短码就拒绝，一家厂只能留一个。
	if row.ShortCode != "" && row.ShortCode != code {
		return domain.ErrAssetCodeConflict
	}
	// 把库操作的错误原样交回，好让调用方处理。
	return s.db.WithContext(ctx).Model(&Client{}).Where("id = ?", clientID).Update("short_code", code).Error
}

// RenameClient 只改给人看的名字，不改绑定修订。
func (s *Store) RenameClient(ctx context.Context, clientID uuid.UUID, name string) (Client, error) {
	// 只改点名的列，避免把别的字段写成零值。
	res := s.db.WithContext(ctx).Model(&Client{}).Where("id = ?", clientID).Update("name", name)
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		// 检查约束不通过就改成业务拒绝。
		if domain.IsCheckViolation(res.Error) {
			return Client{}, domain.ErrInvalidName
		}
		return Client{}, res.Error
	}
	// 没有改到行就当目标不存在。
	if res.RowsAffected == 0 {
		return Client{}, domain.ErrNotFound
	}
	// 做完按身份读设备后把结果交回。
	return s.ClientByID(ctx, clientID)
}

// VoidBinding 把本厂绑定标作废；之后不得再签发。
func (s *Store) VoidBinding(ctx context.Context, clientID uuid.UUID) error {
	// 取当前时刻，时间列和租约用同一个时钟。
	now := time.Now().UTC()
	// 执行这次写入，行数和错误分开看。
	res := s.db.WithContext(ctx).Model(&Client{}).
		Where("id = ? AND status = ?", clientID, ClientStatusBound).
		Updates(map[string]any{"status": ClientStatusVoid, "voided_at": now, "operator_id": gorm.Expr("NULL")})
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		return res.Error
	}
	// 真的改到了才继续，没改到就当没有。
	if res.RowsAffected > 0 {
		return nil
	}
	// 已作废再调不算错。
	var n int64
	// 计数失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Model(&Client{}).Where("id = ?", clientID).Count(&n).Error; err != nil {
		return err
	}
	// 计数是零就按没有处理，避免误判已存在。
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ClientByID 按固定识别号取本厂绑定。
func (s *Store) ClientByID(ctx context.Context, clientID uuid.UUID) (Client, error) {
	// 准备承接查到的现场设备。
	var row Client
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", clientID).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Client{}, domain.ErrNotFound
		}
		return Client{}, err
	}
	return row, nil
}

// SetClientOperator 记下谁在这台已绑定设备上登录；设备未落库则忽略。
func (s *Store) SetClientOperator(ctx context.Context, clientID, personID uuid.UUID) error {
	// 执行这次写入，行数和错误分开看。
	res := s.db.WithContext(ctx).Model(&Client{}).
		Where("id = ? AND status = ?", clientID, ClientStatusBound).
		Update("operator_id", personID)
	// 更新失败则交回库错误，不能当成已经改完。
	if res.Error != nil {
		// 外键对不上就当被指的对象不存在。
		if domain.IsForeignKeyViolation(res.Error) {
			return domain.ErrNotFound
		}
		return res.Error
	}
	return nil
}

// ClearOperatorForPersonExcept 同一人只留这一台设备的登录标记。
func (s *Store) ClearOperatorForPersonExcept(ctx context.Context, personID, keepClient uuid.UUID) error {
	// 做完接上要操作的表后把结果交回。
	return s.db.WithContext(ctx).Model(&Client{}).
		Where("operator_id = ? AND id <> ?", personID, keepClient).
		Update("operator_id", nil).Error
}

// PinDeviceSerial 把机械臂号钉到已绑定 Client；换臂覆盖本机旧号。本厂未作废号不得重复。
func (s *Store) PinDeviceSerial(ctx context.Context, clientID uuid.UUID, serial string) (Client, error) {
	// 去掉首尾空白，纯空白不当有效内容。
	serial = strings.TrimSpace(serial)
	// 去掉空白这一支不成立就换路。
	if serial == "" {
		return Client{}, domain.ErrDeviceSerialRequired
	}
	// 准备承接查到的现场设备。
	var out Client
	// 放进同一事务，中途失败就整单回滚。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 准备承接查到的现场设备。
		var row Client
		// 按条件取一行失败就停，避免带着错误继续。
		if err := tx.First(&row, "id = ?", clientID).Error; err != nil {
			// 没有这一行就当成不存在。
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		// 不是有效绑定就不能继续签发或改登录人。
		if row.Status != ClientStatusBound {
			return domain.ErrBindingVoid
		}
		// 准备承接计数或合计。
		var taken int64
		// 接上要操作的表失败就停，避免带着错误继续。
		if err := tx.Model(&Client{}).
			Where("status = ? AND device_serial = ? AND id <> ?", ClientStatusBound, serial, clientID).
			Count(&taken).Error; err != nil {
			return err
		}
		// 这个识别号已经被别的设备占用。
		if taken > 0 {
			return domain.ErrDeviceSerialTaken
		}
		// 只收集这次要改的列，避免整行覆盖。
		patch := map[string]any{"device_serial": serial}
		// 更新指定列失败就停，避免带着错误继续。
		if err := tx.Model(&Client{}).Where("id = ?", clientID).Updates(patch).Error; err != nil {
			if domain.IsUniqueViolation(err) {
				return domain.ErrDeviceSerialTaken
			}
			return err
		}
		// 按条件取一行失败就停，避免带着错误继续。
		if err := tx.First(&row, "id = ?", clientID).Error; err != nil {
			return err
		}
		// 把查到的结果交给事务外的调用方。
		out = row
		return nil
	})
	return out, err
}

// BoundClientByDeviceSerial 按已钉机械臂号找本厂未作废 Client；空号或不存在则没有。
func (s *Store) BoundClientByDeviceSerial(ctx context.Context, serial string) (Client, error) {
	// 去掉首尾空白，纯空白不当有效内容。
	serial = strings.TrimSpace(serial)
	// 去掉空白这一支不成立就换路。
	if serial == "" {
		return Client{}, domain.ErrDeviceSerialRequired
	}
	// 准备承接查到的现场设备。
	var row Client
	// 按条件去读，没有行交给后面的分支。
	err := s.db.WithContext(ctx).First(&row, "status = ? AND device_serial = ?", ClientStatusBound, serial).Error
	// 按条件取一行失败就停，避免带着错误继续。
	if err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Client{}, domain.ErrNotFound
		}
		return Client{}, err
	}
	return row, nil
}

// ListClients 列出本厂已接受的 Client，按接受时间从新到旧。
func (s *Store) ListClients(ctx context.Context) ([]Client, error) {
	// 准备承接查到的多条现场设备。
	var rows []Client
	// 按条件取多行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Order("bound_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 没有行也交回空列表，避免调用方拿到空指针。
	if rows == nil {
		// 没有就交回空列表，避免调用方拿到空指针。
		rows = []Client{}
	}
	return rows, nil
}

// ListRuntimeGrants 列出节点运行凭证，按 Client、修订从新到旧。
func (s *Store) ListRuntimeGrants(ctx context.Context) ([]RuntimeGrant, error) {
	// 准备承接查到的多条运行凭证。
	var rows []RuntimeGrant
	// 按条件取多行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Order("client_id, revision DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	// 没有行也交回空列表，避免调用方拿到空指针。
	if rows == nil {
		// 没有就交回空列表，避免调用方拿到空指针。
		rows = []RuntimeGrant{}
	}
	return rows, nil
}

// assertClientBound 作废后不得再签发。
func (s *Store) assertClientBound(ctx context.Context, tx *gorm.DB, clientID uuid.UUID) error {
	// 准备承接查到的现场设备。
	var row Client
	// 先带上请求上下文查库，结果留给紧跟着的判断。
	db := s.db.WithContext(ctx)
	// 调用方给了事务就沿用，避免另起一单。
	if tx != nil {
		// 沿用调用方给的事务，避免另起一单。
		db = tx
	}
	// 按条件取一行失败就停，避免带着错误继续。
	if err := db.First(&row, "id = ?", clientID).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrNotFound
		}
		return err
	}
	// 不是有效绑定就不能继续签发或改登录人。
	if row.Status != ClientStatusBound {
		return domain.ErrBindingVoid
	}
	return nil
}

// maxRevision 该对象当前最大修订；没有则为 0。
func maxRevision(tx *gorm.DB, model any, query string, args ...any) (int64, error) {
	// 准备承接计数，用来判断有没有匹配行。
	var n sql.NullInt64
	// 扫描查询结果失败就停，避免带着错误继续。
	if err := tx.Model(model).Where(query, args...).Select("MAX(revision)").Scan(&n).Error; err != nil {
		return 0, err
	}
	// 还没有任何修订就当零，后面从一开始加。
	if !n.Valid {
		return 0, nil
	}
	return n.Int64, nil
}

// InsertRuntimeGrant 写入一版节点运行凭证；Client 必须有效绑定，修订必须严格更大。
func (s *Store) InsertRuntimeGrant(ctx context.Context, g RuntimeGrant) (RuntimeGrant, error) {
	// 放进同一事务，中途失败就整单回滚。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 带上请求上下文查库失败就停，避免带着错误继续。
		if err := s.assertClientBound(ctx, tx, g.ClientID); err != nil {
			return err
		}
		// 先取已有最大修订，结果留给紧跟着的判断。
		max, err := maxRevision(tx, &RuntimeGrant{}, "client_id = ?", g.ClientID)
		// 取已有最大修订失败就停，避免带着错误继续。
		if err != nil {
			return err
		}
		// 修订必须严格更大。
		if g.Revision <= max {
			return domain.ErrStaleRevision
		}
		// 还没有凭证行就新建，不把空身份写入。
		if g.ID == uuid.Nil {
			// 现发一个新身份，不沿用空值。
			g.ID = id.New()
		}
		// 还没有更新时间就补上现在，避免零时间。
		if g.CreatedAt.IsZero() {
			// 取当前时刻，时间列和租约用同一个时钟。
			g.CreatedAt = time.Now().UTC()
		}
		// 写入一行失败就停，避免带着错误继续。
		if err := tx.Select("ID", "ClientID", "Revision", "CanRun", "NotBefore", "NotAfter", "Payload", "Signature", "CreatedAt").Create(&g).Error; err != nil {
			// 唯一约束撞了就改成业务冲突，不抛库原文。
			if domain.IsUniqueViolation(err) {
				return domain.ErrStaleRevision
			}
			// 检查约束不通过就改成业务拒绝。
			if domain.IsCheckViolation(err) {
				return domain.ErrInvalidKey
			}
			// 外键对不上就当被指的对象不存在。
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
	// 准备承接查到的运行凭证。
	var row RuntimeGrant
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Where("client_id = ?", clientID).Order("revision DESC").First(&row).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return RuntimeGrant{}, domain.ErrNotFound
		}
		return RuntimeGrant{}, err
	}
	return row, nil
}
