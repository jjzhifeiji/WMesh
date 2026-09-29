package service

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
)

// normalizeClientName 去掉首尾空白后须 1～64 字。
func normalizeClientName(name string) (string, error) {
	// 去掉多余空白或前后缀。
	name = strings.TrimSpace(name)
	// 按字数看长度，失败就不能继续。
	n := utf8.RuneCountInString(name)
	// 字数不在允许范围就拒绝，避免空名或超长。
	if n < 1 || n > 64 {
		return "", domain.ErrInvalidName
	}
	return name, nil
}

// normalizeDeviceSerial 去掉首尾空白后须 1～128 字。
func normalizeDeviceSerial(serial string) (string, error) {
	// 去掉多余空白或前后缀。
	serial = strings.TrimSpace(serial)
	// 按字数看长度，失败就不能继续。
	n := utf8.RuneCountInString(serial)
	// 字数不在允许范围就拒绝，避免空名或超长。
	if n < 1 || n > 128 {
		return "", domain.ErrDeviceSerialRequired
	}
	return serial, nil
}

// RegisterClient 把自有节点写入名录；识别号必填，可当场分给一厂。公钥可待现场上线再登记。
func (s *Clients) RegisterClient(ctx context.Context, token, name string, clientID, factoryID uuid.UUID, publicKey []byte, deviceSerial string) (Client, error) {
	// 只有 WAN 管理员能登记现场设备。失败一律记拒绝；未登录时操作者为空。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 登记设备被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "register_client", name, audit.Deny)
		return Client{}, err
	}
	// 收成合法值，空或太长不要。
	name, err = normalizeClientName(name)
	// 不合法就拒绝，避免脏数据入库。
	if err != nil {
		// 登记设备被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "register_client", name, audit.Deny)
		return Client{}, err
	}
	// 收成合法值，空或太长不要。
	serial, err := normalizeDeviceSerial(deviceSerial)
	// 不合法就拒绝，避免脏数据入库。
	if err != nil {
		// 登记设备被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "register_client", name, audit.Deny)
		return Client{}, err
	}
	// 未带身份则现场发号。
	if clientID == uuid.Nil {
		// 做一个新的实例。
		clientID = id.New()
	}
	// 写入名录；公钥可空，识别号已规范化。
	c, err := s.store.CreateClient(ctx, clientID, name, publicKey, serial)
	// 新建失败就停，避免留下半截记录。
	if err != nil {
		// 登记设备被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "register_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 当场指定工厂则一并绑定。
	if factoryID != uuid.Nil {
		// 在对得上的修订上写。
		bound, err := s.assignLocked(ctx, admin.ID, c, factoryID)
		// 被别人改过就拒绝这次。
		if err != nil {
			return Client{}, err
		}
		// 用绑完的设备继续，避免还拿旧厂籍。
		c = bound
	}
	// 登记成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, c.FactoryID, "register_client", c.ID.String(), audit.Allow); err != nil {
		return Client{}, err
	}
	// 通知厂端设备绑定有变。
	s.notifyClient(c, nil)
	return c, nil
}

// BindClient 把未分配节点分给一厂；节点尚不存在则连同名字、公钥一并建档。
func (s *Clients) BindClient(ctx context.Context, token string, clientID, factoryID uuid.UUID, name string, publicKey []byte) (Client, error) {
	// 只有 WAN 管理员能分厂；工厂必须已在名录。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 分厂被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 身份是空就按未设置处理，避免写空号。
	if factoryID == uuid.Nil {
		// 分厂被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "bind_client", clientID.String(), audit.Deny)
		return Client{}, domain.ErrNotFound
	}
	// 工厂必须已在名录。
	if _, err := s.store.FactoryByID(ctx, factoryID); err != nil {
		// 分厂被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 按现场设备处理。
	c, err := s.store.ClientByID(ctx, clientID)
	// 没有这条就按不存在处理，不当成别的故障。
	if errors.Is(err, domain.ErrNotFound) {
		// 收成合法值，空或太长不要。
		nm, nerr := normalizeClientName(name)
		// 不合法就拒绝，避免脏数据入库。
		if nerr != nil {
			// 分厂被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
			return Client{}, nerr
		}
		// 尚无档则连同名字、公钥建档。
		c, err = s.store.CreateClient(ctx, clientID, nm, publicKey, "")
		// 新建失败就停，避免留下半截记录。
		if err != nil {
			// 分厂被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
			return Client{}, err
		}
		// 这一步失败就停，避免留下半截。
	} else if err != nil {
		// 分厂被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
		// 冲突或类型不同就拒绝。
	} else if err := s.mergePublicKey(ctx, c, publicKey); err != nil {
		// 分厂被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 在对得上的修订上写。
	bound, err := s.assignLocked(ctx, admin.ID, c, factoryID)
	// 被别人改过就拒绝这次。
	if err != nil {
		return Client{}, err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String()+" "+factoryID.String(), audit.Allow); err != nil {
		return Client{}, err
	}
	// 通知厂端设备绑定有变。
	s.notifyClient(bound, nil)
	return bound, nil
}

// mergePublicKey 空钥则补登；已有且不一致则拒绝改钥。
func (s *Clients) mergePublicKey(ctx context.Context, c Client, publicKey []byte) error {
	// 空的就按没有处理，避免交出空壳当成功。
	if len(publicKey) == 0 {
		return nil
	}
	// 空的就按没有处理，避免交出空壳当成功。
	if len(c.PublicKey) == 0 {
		// 空钥则补登，已有且不一致则拒绝改钥。
		return s.store.SetClientPublicKey(ctx, c.ID, publicKey)
	}
	// 公钥变了才要重登，没变就保持原样。
	if !bytes.Equal(c.PublicKey, publicKey) {
		return domain.ErrInvalidKey
	}
	return nil
}

// assignLocked 已属该厂则幂等返回；已属别厂则拒绝，不在这里改绑。
func (s *Clients) assignLocked(ctx context.Context, adminID uuid.UUID, c Client, factoryID uuid.UUID) (Client, error) {
	// 还没有准备好就停，避免空着往下用。
	if c.FactoryID != nil && *c.FactoryID == factoryID {
		return c, nil
	}
	// 未分配才能绑；已属别厂由库拒绝。
	bound, err := s.store.BindClient(ctx, c.ID, factoryID)
	// 已属别的厂就拒绝。
	if err != nil {
		// 分厂被拒就留审计。
		_ = s.audit(ctx, &adminID, nil, &factoryID, "bind_client", c.ID.String(), audit.Deny)
		return Client{}, err
	}
	return bound, nil
}

// AssignClient 把已建档、尚未分厂的节点分给一厂。
func (s *Clients) AssignClient(ctx context.Context, token string, clientID, factoryID uuid.UUID) (Client, error) {
	// 只有 WAN 管理员能分厂。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 分厂被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 按现场设备处理。
	c, err := s.store.ClientByID(ctx, clientID)
	// 未登记或厂籍不对就拒绝。
	if err != nil {
		// 分厂被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 在对得上的修订上写。
	bound, err := s.assignLocked(ctx, admin.ID, c, factoryID)
	// 被别人改过就拒绝这次。
	if err != nil {
		return Client{}, err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String()+" "+factoryID.String(), audit.Allow); err != nil {
		return Client{}, err
	}
	// 通知厂端设备绑定有变。
	s.notifyClient(bound, nil)
	return bound, nil
}

// RenameClient 只改给人看的名字，不改归属。
func (s *Clients) RenameClient(ctx context.Context, token string, clientID uuid.UUID, name string) (Client, error) {
	// 只有 WAN 管理员能改名。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 改设备名被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "rename_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 收成合法值，空或太长不要。
	name, err = normalizeClientName(name)
	// 不合法就拒绝，避免脏数据入库。
	if err != nil {
		// 改设备名被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "rename_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 只改显示名，不升绑定修订。
	row, err := s.store.RenameClient(ctx, clientID, name)
	// 改名失败就停，避免名字和身份错位。
	if err != nil {
		// 改设备名被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "rename_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, row.FactoryID, "rename_client", clientID.String(), audit.Allow); err != nil {
		return Client{}, err
	}
	// 通知厂端设备绑定有变。
	s.notifyClient(row, nil)
	return row, nil
}

// RebindClient 把已分配节点改到另一厂，绑定修订升高；旧厂不再是当前所属。
func (s *Clients) RebindClient(ctx context.Context, token string, clientID, factoryID uuid.UUID) (Client, error) {
	// 只有 WAN 管理员能改绑。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 改绑被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "rebind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 按现场设备处理。
	prev, _ := s.store.ClientByID(ctx, clientID)
	// 已绑定才能改到另一厂，修订必须升高。
	bound, err := s.store.RebindClient(ctx, clientID, factoryID)
	// 原来没有绑定就拒绝。
	if err != nil {
		// 改绑被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "rebind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, &factoryID, "rebind_client", clientID.String()+" "+factoryID.String(), audit.Allow); err != nil {
		return Client{}, err
	}
	// 通知厂端设备绑定有变。
	s.notifyClient(bound, prev.FactoryID)
	return bound, nil
}

// ClientByID 按稳定身份取名录行，不含私钥。
func (s *Clients) ClientByID(ctx context.Context, clientID uuid.UUID) (Client, error) {
	// 按现场设备处理。
	return s.store.ClientByID(ctx, clientID)
}

// ListClients 列出 WAN 已登记的现场设备，不含私钥。
func (s *Clients) ListClients(ctx context.Context, token string) ([]Client, error) {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		return nil, err
	}
	// 列出这一批供后面筛选。
	rows, err := s.store.ListClients(ctx)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return nil, err
	}
	// 列出后才记允许。
	return rows, s.audit(ctx, &admin.ID, nil, nil, "list_clients", "clients", audit.Allow)
}

// ListPersonOfflineGrants 从 WAN 查厂内人员，一律拒绝。
func (s *Clients) ListPersonOfflineGrants(ctx context.Context, token string, factoryID uuid.UUID) error {
	// 拒绝代管厂内人员、组织和角色。
	return s.denyFactoryManage(ctx, token, factoryID, "list_person_offline_grants", factoryID.String())
}
