package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
)

// RuntimeGrantView 是给管理端看的节点凭证摘要，不含声明原文和签名。
type RuntimeGrantView struct {
	ClientID  uuid.UUID `json:"clientId"`  // 签给哪台 Client
	Revision  int64     `json:"revision"`  // 该 Client 最高修订
	CanRun    bool      `json:"canRun"`    // 本修订是否允许运行
	NotBefore time.Time `json:"notBefore"` // 生效时间
	NotAfter  time.Time `json:"notAfter"`  // 失效时间
	CreatedAt time.Time `json:"createdAt"` // 写入时间
}

// PersonGrantView 是给管理端看的人员离线授权摘要，不含口令哈希。
type PersonGrantView struct {
	PersonID      uuid.UUID      `json:"personId"`      // 本厂账号
	ClientID      uuid.UUID      `json:"clientId"`      // 绑定 Client
	LoginName     string         `json:"loginName"`     // 签发时登录名
	AllowDirect   bool           `json:"allowDirect"`   // 是否允许 Factory 直属
	OrgSnapshot   []OrgOption    `json:"orgSnapshot"`   // 当时可选节点
	RolesSnapshot []RoleSnapshot `json:"rolesSnapshot"` // 当时角色
	Active        bool           `json:"active"`        // 签发时账号是否有效
	NotBefore     time.Time      `json:"notBefore"`     // 生效时间
	NotAfter      time.Time      `json:"notAfter"`      // 失效时间
	Revision      int64          `json:"revision"`      // 人员授权修订
}

// RegisterBinding 由本厂有效超管登记 WAN 已送达的 Client 绑定。
func (s *Node) RegisterBinding(ctx context.Context, token string, clientID uuid.UUID, publicKey []byte, revision int64) (Client, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Client{}, err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "accept_binding", clientID.String(), audit.Deny)
		return Client{}, err
	}
	row, err := s.store.AcceptBinding(ctx, clientID, publicKey, revision)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "accept_binding", clientID.String(), audit.Deny)
		return Client{}, err
	}
	return row, s.audit(ctx, &acc.ID, nil, "accept_binding", clientID.String(), audit.Allow)
}

// VoidClientBinding 由本厂有效超管把本厂绑定标作废，之后不得再签发。
func (s *Node) VoidClientBinding(ctx context.Context, token string, clientID uuid.UUID) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "void_binding", clientID.String(), audit.Deny)
		return err
	}
	if err := s.store.VoidBinding(ctx, clientID); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "void_binding", clientID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, &acc.ID, nil, "void_binding", clientID.String(), audit.Allow)
}

// ListClients 列出本厂已接受的 Client；不含私钥。
func (s *Node) ListClients(ctx context.Context, token string) ([]Client, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "list_clients", "clients", audit.Deny)
		return nil, err
	}
	rows, err := s.store.ListClients(ctx)
	if err != nil {
		return nil, err
	}
	return rows, s.audit(ctx, &acc.ID, nil, "list_clients", "clients", audit.Allow)
}

// ListRuntimeGrants 每台 Client 只回最高修订，不含声明原文和签名。
func (s *Node) ListRuntimeGrants(ctx context.Context, token string) ([]RuntimeGrantView, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "list_runtime", "runtime", audit.Deny)
		return nil, err
	}
	rows, err := s.store.ListRuntimeGrants(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[uuid.UUID]struct{}{}
	out := make([]RuntimeGrantView, 0)
	for _, r := range rows {
		if _, ok := seen[r.ClientID]; ok {
			continue
		}
		seen[r.ClientID] = struct{}{}
		out = append(out, RuntimeGrantView{
			ClientID: r.ClientID, Revision: r.Revision, CanRun: r.CanRun,
			NotBefore: r.NotBefore, NotAfter: r.NotAfter, CreatedAt: r.CreatedAt,
		})
	}
	return out, s.audit(ctx, &acc.ID, nil, "list_runtime", "runtime", audit.Allow)
}

// ListPersonGrantViews 列出每人每 Client 的最高修订，不含口令哈希。
func (s *Node) ListPersonGrantViews(ctx context.Context, token string) ([]PersonGrantView, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "list_person_offline", "person_offline", audit.Deny)
		return nil, err
	}
	rows, err := s.store.ListPersonOfflineGrants(ctx)
	if err != nil {
		return nil, err
	}
	type pair struct{ p, c uuid.UUID }
	seen := map[pair]struct{}{}
	out := make([]PersonGrantView, 0)
	for _, r := range rows {
		k := pair{r.PersonID, r.ClientID}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, personViewFromRow(r))
	}
	return out, s.audit(ctx, &acc.ID, nil, "list_person_offline", "person_offline", audit.Allow)
}

func runtimeView(c RuntimeCred) RuntimeGrantView {
	return RuntimeGrantView{
		ClientID: c.ClientID, Revision: c.Revision, CanRun: c.CanRun,
		NotBefore: c.NotBefore, NotAfter: c.NotAfter,
	}
}

func personGrantView(c PersonCred) PersonGrantView {
	orgs, roles := c.OrgSnapshot, c.RolesSnapshot
	if orgs == nil {
		orgs = []OrgOption{}
	}
	if roles == nil {
		roles = []RoleSnapshot{}
	}
	return PersonGrantView{
		PersonID: c.PersonID, ClientID: c.ClientID, LoginName: c.LoginName,
		AllowDirect: c.AllowDirect, OrgSnapshot: orgs, RolesSnapshot: roles,
		Active: c.Active, NotBefore: c.NotBefore, NotAfter: c.NotAfter, Revision: c.Revision,
	}
}

func personViewFromRow(row PersonOfflineGrant) PersonGrantView {
	cred, err := decodePerson(row.Payload)
	if err != nil {
		orgs, roles := row.OrgSnapshot, row.RolesSnapshot
		if orgs == nil {
			orgs = []OrgOption{}
		}
		if roles == nil {
			roles = []RoleSnapshot{}
		}
		return PersonGrantView{
			PersonID: row.PersonID, ClientID: row.ClientID, LoginName: row.LoginName,
			AllowDirect: row.AllowDirect, OrgSnapshot: orgs, RolesSnapshot: roles,
			Active: true, NotBefore: row.NotBefore, NotAfter: row.NotAfter, Revision: row.Revision,
		}
	}
	return personGrantView(cred)
}
