package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
)

// ClientView 是给管理端看的设备行，带当前使用人，不含私钥。
type ClientView struct {
	Client
	OperatorLogin   string `json:"operatorLogin,omitempty"`   // 当前使用人登录名
	OperatorDisplay string `json:"operatorDisplay,omitempty"` // 当前使用人显示名
}

// RuntimeGrantView 是给管理端看的节点凭证摘要，不含声明原文和签名。
type RuntimeGrantView struct {
	ClientID  uuid.UUID `json:"clientId"`  // 签给哪台 Client
	Revision  int64     `json:"revision"`  // 该 Client 最高修订
	CanRun    bool      `json:"canRun"`    // 本修订是否允许运行
	NotBefore time.Time `json:"notBefore"` // 生效时间
	NotAfter  time.Time `json:"notAfter"`  // 失效时间
	CreatedAt time.Time `json:"createdAt"` // 写入时间
}

// RegisterBinding 由本厂有效超管登记 WAN 已送达的 Client 绑定。
func (s *Node) RegisterBinding(ctx context.Context, token string, clientID uuid.UUID, name string, publicKey []byte, revision int64) (Client, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Client{}, err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "accept_binding", clientID.String(), audit.Deny)
		return Client{}, err
	}
	if strings.TrimSpace(name) != "" {
		name, err = normalizeClientName(name)
		if err != nil {
			_ = s.audit(ctx, &acc.ID, nil, "accept_binding", clientID.String(), audit.Deny)
			return Client{}, err
		}
	}
	row, err := s.store.AcceptBinding(ctx, clientID, name, publicKey, revision)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "accept_binding", clientID.String(), audit.Deny)
		return Client{}, err
	}
	return row, s.audit(ctx, &acc.ID, nil, "accept_binding", clientID.String(), audit.Allow)
}

// RenameClient 由本厂有效超管改给人看的名字，不改归属。
func (s *Node) RenameClient(ctx context.Context, token string, clientID uuid.UUID, name string) (Client, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return Client{}, err
	}
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "rename_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	name, err = normalizeClientName(name)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "rename_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	row, err := s.store.RenameClient(ctx, clientID, name)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "rename_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	return row, s.audit(ctx, &acc.ID, nil, "rename_client", clientID.String(), audit.Allow)
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

// ListClients 列出本厂已接受的 Client；含当前使用人，不含私钥。
func (s *Node) ListClients(ctx context.Context, token string) ([]ClientView, error) {
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
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if row.OperatorID != nil {
			ids = append(ids, *row.OperatorID)
		}
	}
	people, err := s.store.PeopleByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]ClientView, 0, len(rows))
	for _, row := range rows {
		view := ClientView{Client: row}
		if row.OperatorID != nil {
			if p, ok := people[*row.OperatorID]; ok {
				view.OperatorLogin = p.LoginName
				view.OperatorDisplay = p.DisplayName
			}
		}
		out = append(out, view)
	}
	return out, s.audit(ctx, &acc.ID, nil, "list_clients", "clients", audit.Allow)
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
