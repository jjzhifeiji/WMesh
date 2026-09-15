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
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if n < 1 || n > 64 {
		return "", domain.ErrInvalidName
	}
	return name, nil
}

// RegisterClient 把自有节点写入名录；可当场分给一厂。公钥可待现场上线再登记。
func (s *Clients) RegisterClient(ctx context.Context, token, name string, clientID, factoryID uuid.UUID, publicKey []byte) (Client, error) {
	// 只有 WAN 管理员能登记现场设备。失败一律记拒绝；未登录时操作者为空。
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "register_client", name, audit.Deny)
		return Client{}, err
	}
	name, err = normalizeClientName(name)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "register_client", name, audit.Deny)
		return Client{}, err
	}
	// 未带身份则现场发号。
	if clientID == uuid.Nil {
		clientID = id.New()
	}
	// 写入名录；公钥可空。
	c, err := s.store.CreateClient(ctx, clientID, name, publicKey)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "register_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 当场指定工厂则一并绑定。
	if factoryID != uuid.Nil {
		bound, err := s.assignLocked(ctx, admin.ID, c, factoryID)
		if err != nil {
			return Client{}, err
		}
		c = bound
	}
	// 登记成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, c.FactoryID, "register_client", c.ID.String(), audit.Allow); err != nil {
		return Client{}, err
	}
	s.notifyClient(c, nil)
	return c, nil
}

// BindClient 把未分配节点分给一厂；节点尚不存在则连同名字、公钥一并建档。
func (s *Clients) BindClient(ctx context.Context, token string, clientID, factoryID uuid.UUID, name string, publicKey []byte) (Client, error) {
	// 只有 WAN 管理员能分厂；工厂必须已在名录。
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	if factoryID == uuid.Nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "bind_client", clientID.String(), audit.Deny)
		return Client{}, domain.ErrNotFound
	}
	// 工厂必须已在名录。
	if _, err := s.store.FactoryByID(ctx, factoryID); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	c, err := s.store.ClientByID(ctx, clientID)
	if errors.Is(err, domain.ErrNotFound) {
		nm, nerr := normalizeClientName(name)
		if nerr != nil {
			_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
			return Client{}, nerr
		}
		// 尚无档则连同名字、公钥建档。
		c, err = s.store.CreateClient(ctx, clientID, nm, publicKey)
		if err != nil {
			_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
			return Client{}, err
		}
	} else if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	} else if err := s.mergePublicKey(ctx, c, publicKey); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	bound, err := s.assignLocked(ctx, admin.ID, c, factoryID)
	if err != nil {
		return Client{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String()+" "+factoryID.String(), audit.Allow); err != nil {
		return Client{}, err
	}
	s.notifyClient(bound, nil)
	return bound, nil
}

// mergePublicKey 空钥则补登；已有且不一致则拒绝改钥。
func (s *Clients) mergePublicKey(ctx context.Context, c Client, publicKey []byte) error {
	if len(publicKey) == 0 {
		return nil
	}
	if len(c.PublicKey) == 0 {
		// 空钥则补登，已有且不一致则拒绝改钥。
		return s.store.SetClientPublicKey(ctx, c.ID, publicKey)
	}
	if !bytes.Equal(c.PublicKey, publicKey) {
		return domain.ErrInvalidKey
	}
	return nil
}

// assignLocked 已属该厂则幂等返回；已属别厂则拒绝，不在这里改绑。
func (s *Clients) assignLocked(ctx context.Context, adminID uuid.UUID, c Client, factoryID uuid.UUID) (Client, error) {
	if c.FactoryID != nil && *c.FactoryID == factoryID {
		return c, nil
	}
	// 未分配才能绑；已属别厂由库拒绝。
	bound, err := s.store.BindClient(ctx, c.ID, factoryID)
	if err != nil {
		_ = s.audit(ctx, &adminID, nil, &factoryID, "bind_client", c.ID.String(), audit.Deny)
		return Client{}, err
	}
	return bound, nil
}

// AssignClient 把已建档、尚未分厂的节点分给一厂。
func (s *Clients) AssignClient(ctx context.Context, token string, clientID, factoryID uuid.UUID) (Client, error) {
	// 只有 WAN 管理员能分厂。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	c, err := s.store.ClientByID(ctx, clientID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	bound, err := s.assignLocked(ctx, admin.ID, c, factoryID)
	if err != nil {
		return Client{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String()+" "+factoryID.String(), audit.Allow); err != nil {
		return Client{}, err
	}
	s.notifyClient(bound, nil)
	return bound, nil
}

// RenameClient 只改给人看的名字，不改归属。
func (s *Clients) RenameClient(ctx context.Context, token string, clientID uuid.UUID, name string) (Client, error) {
	// 只有 WAN 管理员能改名。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "rename_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	name, err = normalizeClientName(name)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "rename_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 只改显示名，不升绑定修订。
	row, err := s.store.RenameClient(ctx, clientID, name)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "rename_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, row.FactoryID, "rename_client", clientID.String(), audit.Allow); err != nil {
		return Client{}, err
	}
	s.notifyClient(row, nil)
	return row, nil
}

// RebindClient 把已分配节点改到另一厂，绑定修订升高；旧厂不再是当前所属。
func (s *Clients) RebindClient(ctx context.Context, token string, clientID, factoryID uuid.UUID) (Client, error) {
	// 只有 WAN 管理员能改绑。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "rebind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	prev, _ := s.store.ClientByID(ctx, clientID)
	// 已绑定才能改到另一厂，修订必须升高。
	bound, err := s.store.RebindClient(ctx, clientID, factoryID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "rebind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, &factoryID, "rebind_client", clientID.String()+" "+factoryID.String(), audit.Allow); err != nil {
		return Client{}, err
	}
	s.notifyClient(bound, prev.FactoryID)
	return bound, nil
}

// ClientByID 按稳定身份取名录行，不含私钥。
func (s *Clients) ClientByID(ctx context.Context, clientID uuid.UUID) (Client, error) {
	return s.store.ClientByID(ctx, clientID)
}

// ListClients 列出 WAN 已登记的现场设备，不含私钥。
func (s *Clients) ListClients(ctx context.Context, token string) ([]Client, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListClients(ctx)
	if err != nil {
		return nil, err
	}
	return rows, s.audit(ctx, &admin.ID, nil, nil, "list_clients", "clients", audit.Allow)
}

// ListPersonOfflineGrants 从 WAN 查厂内人员，一律拒绝。
func (s *Clients) ListPersonOfflineGrants(ctx context.Context, token string, factoryID uuid.UUID) error {
	return s.denyFactoryManage(ctx, token, factoryID, "list_person_offline_grants", factoryID.String())
}
