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
	"gorm.io/gorm/clause"

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
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`  // 固定识别号，与 WAN 相同的全局身份
	Name            string     `gorm:"not null" json:"name"`            // WAN 下发的设备名，厂端只读，不当身份
	Nickname        string     `gorm:"not null" json:"nickname"`        // 厂端起的昵称；空表示没起
	PublicKey       []byte     `gorm:"type:bytea" json:"publicKey"`     // 本机公钥；未上线为空，无私钥
	BindingRevision int64      `gorm:"not null" json:"bindingRevision"` // 已接受的绑定修订，只向前
	Status          string     `gorm:"not null" json:"status"`          // bound / void
	BoundAt         time.Time  `gorm:"not null" json:"boundAt"`         // 最近一次接受为 bound 的时间
	VoidedAt        *time.Time `json:"voidedAt"`                        // 作废时间；bound 必须为空
	ShortCode       string     `json:"shortCode,omitempty"`             // Client 短码，随绑定补齐
	DeviceSerial    string     `json:"deviceSerial,omitempty"`          // 从设备读到的机械臂识别号；未登记为空
	UnwrapKey       []byte     `json:"-"`                               // 历史列，焊机不持钥
	WANName         *string    `gorm:"column:wan_name" json:"-"`        // 上次收到的 WAN 设备名；旧行为空，只用来判断 WAN 是否改名
}

// 指定落库表名，避免查询时按类型名去猜。
func (Client) TableName() string { return "clients" }

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

// AcceptBinding 接受 WAN 送达的绑定；修订只向前，同修订可补公钥。
// 同修订只在 WAN 名与上次记下的不同时才改名；同修订不复活已作废的行，原样交回。
func (s *Store) AcceptBinding(ctx context.Context, clientID uuid.UUID, name string, publicKey []byte, revision int64) (Client, error) {
	// 修订不更高就拒绝或忽略，避免旧帧覆盖。
	if revision < 1 {
		return Client{}, domain.ErrStaleRevision
	}
	// 先留空，WAN 带了名字才记下，供同修订重放时比对。
	var wanName *string
	// WAN 带了非空名字才记，空名不当 WAN 改了名。
	if strings.TrimSpace(name) != "" {
		// 另存一份，避免后面退回备用名时连带改掉。
		n := name
		// 记下这次收到的 WAN 名。
		wanName = &n
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
				WANName:         wanName,
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
		// 同修订碰到已作废的行原样交回，本厂作废不被 WAN 重推复活。
		if revision == row.BindingRevision && row.Status == ClientStatusVoid {
			// 把库里原样的行交给调用方，不改任何列。
			out = row
			return nil
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
		// WAN 带了名字就照收，设备名只由 WAN 定；厂端自己的叫法在昵称里。
		if wanName != nil {
			// 把这一列放进待更新集合，只覆盖带来的字段。
			patch["name"] = *wanName
			// 记下这次收到的 WAN 名。
			patch["wan_name"] = *wanName
		}
		// 同修订只允许补尚未登记的公钥。
		if len(row.PublicKey) == 0 && len(publicKey) > 0 {
			// 把这一列放进待更新集合，只覆盖带来的字段。
			patch["public_key"] = publicKey
		}
		// 作废设备恢复绑定时，旧臂号可能已被别的有效设备钉走。
		if row.Status == ClientStatusVoid && row.DeviceSerial != "" {
			// 准备承接占着这个旧号的有效设备数。
			var taken int64
			// 数别的有效设备有没有钉这个号，数不出就停。
			if err := tx.Model(&Client{}).
				Where("status = ? AND device_serial = ? AND id <> ?", ClientStatusBound, row.DeviceSerial, clientID).
				Count(&taken).Error; err != nil {
				return err
			}
			// 旧号已被占用就清空，等 WAN 改号或厂端另行钉号。
			if taken > 0 {
				// 把臂号清成空串，恢复绑定不再撞唯一索引。
				patch["device_serial"] = ""
			}
		}
		// 更新指定列失败就停，避免带着错误继续。
		if err := tx.Model(&Client{}).Where("id = ?", clientID).Updates(patch).Error; err != nil {
			// 并发下仍撞臂号唯一索引就收成业务冲突，不抛库原文。
			if domain.IsUniqueViolation(err) {
				return domain.ErrDeviceSerialTaken
			}
			if domain.IsCheckViolation(err) {
				return domain.ErrInvalidName
			}
			return err
		}
		// 读回到新的空行里，旧行残留的作废时间不会带到结果。
		var fresh Client
		// 按条件取一行失败就停，避免带着错误继续。
		if err := tx.First(&fresh, "id = ?", clientID).Error; err != nil {
			return err
		}
		// 把查到的结果交给事务外的调用方。
		out = fresh
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

// RenameClient 只改厂端昵称，不改 WAN 设备名和绑定修订。
func (s *Store) RenameClient(ctx context.Context, clientID uuid.UUID, nickname string) (Client, error) {
	// 只改昵称，设备名由 WAN 定。
	res := s.db.WithContext(ctx).Model(&Client{}).Where("id = ?", clientID).Update("nickname", nickname)
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
		Updates(map[string]any{"status": ClientStatusVoid, "voided_at": now})
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

// VoidBindingByRevision 按 WAN 作废指令的修订作废本厂绑定；本地修订更低才作废。
// 已作废再调不算错；本地已在同修订或更高修订有效时交回 ErrStaleRevision，当迟到帧。
func (s *Store) VoidBindingByRevision(ctx context.Context, clientID uuid.UUID, revision int64) error {
	// 取当前时刻，作废时间用同一个时钟。
	now := time.Now().UTC()
	// 修订条件写进同一条更新，迟到的旧作废并发也作废不了新绑定。
	res := s.db.WithContext(ctx).Model(&Client{}).
		Where("id = ? AND status = ? AND binding_revision < ?", clientID, ClientStatusBound, revision).
		Updates(map[string]any{"status": ClientStatusVoid, "voided_at": now})
	// 更新失败则交回库错误，不能当成已经作废。
	if res.Error != nil {
		return res.Error
	}
	// 真的改到了就是作废成功。
	if res.RowsAffected > 0 {
		return nil
	}
	// 没改到就读回这台，分清没有、已作废还是迟到帧。
	row, err := s.ClientByID(ctx, clientID)
	// 读不到就交回，没有这台时是 ErrNotFound。
	if err != nil {
		return err
	}
	// 已作废再调不算错。
	if row.Status == ClientStatusVoid {
		return nil
	}
	// 仍有效且修订不更低，说明作废指令是迟到的旧帧。
	return domain.ErrStaleRevision
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

// PinDeviceSerial 把机械臂号钉到已绑定 Client；本厂未作废号不得重复。
// 已钉别的号时 replace=false 拒绝；replace=true 才覆盖，现只给测试模拟现场换臂。
// WAN 下发不走这里，改走 FillDeviceSerial，只给还没号的设备补号。
func (s *Store) PinDeviceSerial(ctx context.Context, clientID uuid.UUID, serial string, replace bool) (Client, error) {
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
		// 行锁读这台设备，免得两个请求同时看到空号都写进去。
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", clientID).Error; err != nil {
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
		// replace=false 时已钉别的号就拒绝，不许悄悄换号；同号重钉算幂等。
		if !replace && row.DeviceSerial != "" && row.DeviceSerial != serial {
			return domain.ErrDeviceSerialMismatch
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

// FillDeviceSerial 只给还没钉号的有效绑定补上 WAN 登记的臂号；本机已有号不动。
// 调用方按读回的行判断补上没有；别的有效设备占着这个号时返回 ErrDeviceSerialTaken。
func (s *Store) FillDeviceSerial(ctx context.Context, clientID uuid.UUID, serial string) (Client, error) {
	// 去掉首尾空白，纯空白不当有效内容。
	serial = strings.TrimSpace(serial)
	// 空号没有可补的，直接拒绝。
	if serial == "" {
		return Client{}, domain.ErrDeviceSerialRequired
	}
	// 条件写进同一条更新：有效且本机没号才补，并发也覆盖不了本机号。
	res := s.db.WithContext(ctx).Model(&Client{}).
		Where("id = ? AND status = ? AND device_serial = ''", clientID, ClientStatusBound).
		Update("device_serial", serial)
	// 更新失败则交回库错误，不能当成已经补上。
	if res.Error != nil {
		// 别的有效设备已钉这个号，收成业务冲突。
		if domain.IsUniqueViolation(res.Error) {
			return Client{}, domain.ErrDeviceSerialTaken
		}
		return Client{}, res.Error
	}
	// 读回这台，调用方据此看本机留的是哪个号。
	return s.ClientByID(ctx, clientID)
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
