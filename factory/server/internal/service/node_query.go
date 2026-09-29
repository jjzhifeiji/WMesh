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
	Client                 // 设备本体，这个视图不含私钥
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
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return Client{}, err
	}
	// 只有工厂超管能登记 WAN 已送达的绑定。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 不是工厂超管，记下登记绑定被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "accept_binding", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 名字或识别号为空则拒绝或去名录补
	if strings.TrimSpace(name) != "" {
		// 去掉空白并限制名字长度
		name, err = normalizeClientName(name)
		// 名字空或太长，拒绝改名
		if err != nil {
			// 记下登记绑定被拒绝，写失败不改变结果
			_ = s.audit(ctx, &acc.ID, nil, "accept_binding", clientID.String(), audit.Deny)
			return Client{}, err
		}
	}
	// 修订只向前。
	row, err := s.store.AcceptBinding(ctx, clientID, name, publicKey, revision)
	// 落库失败则本厂仍不认这台
	if err != nil {
		// 记下登记绑定被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "accept_binding", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 登记绑定成功后记审计，再把结果交回
	return row, s.audit(ctx, &acc.ID, nil, "accept_binding", clientID.String(), audit.Allow)
}

// RenameClient 由本厂有效超管改给人看的名字，不改归属。
func (s *Node) RenameClient(ctx context.Context, token string, clientID uuid.UUID, name string) (Client, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return Client{}, err
	}
	// 只有工厂超管能改显示名。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 不是工厂超管，记下设备改名被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "rename_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 去掉空白并限制名字长度
	name, err = normalizeClientName(name)
	// 名字空或太长，拒绝改名
	if err != nil {
		// 记下设备改名被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "rename_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 只改显示名，不改归属。
	row, err := s.store.RenameClient(ctx, clientID, name)
	// 改名写不进去，原来的名字保持不变
	if err != nil {
		// 记下设备改名被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "rename_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 设备改名成功后记审计，再把结果交回
	return row, s.audit(ctx, &acc.ID, nil, "rename_client", clientID.String(), audit.Allow)
}

// VoidClientBinding 由本厂有效超管把本厂绑定标作废，之后不得再签发。
func (s *Node) VoidClientBinding(ctx context.Context, token string, clientID uuid.UUID) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 只有工厂超管能作废绑定。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 不是工厂超管，记下作废绑定被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "void_binding", clientID.String(), audit.Deny)
		return err
	}
	// 作废写失败，设备仍可能被签发
	if err := s.store.VoidBinding(ctx, clientID); err != nil {
		// 记下作废绑定被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "void_binding", clientID.String(), audit.Deny)
		return err
	}
	// 作废绑定成功后记审计，再把结果交回
	return s.audit(ctx, &acc.ID, nil, "void_binding", clientID.String(), audit.Allow)
}

// ListClients 列出本厂已接受的 Client；含当前使用人，不含私钥。
func (s *Node) ListClients(ctx context.Context, token string) ([]ClientView, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return nil, err
	}
	// 只有工厂超管能看设备名录。失败记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 不是工厂超管，记下列出设备被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "list_clients", "clients", audit.Deny)
		return nil, err
	}
	// 列出本厂已接受的设备，不含私钥
	rows, err := s.store.ListClients(ctx)
	// 设备名录读失败，不返回残缺列表
	if err != nil {
		return nil, err
	}
	// 按条数决定是空、超限还是继续
	ids := make([]uuid.UUID, 0, len(rows))
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, row := range rows {
		// 这台没有使用人或使用人不是当前登录者
		if row.OperatorID != nil {
			// 把这一条收进结果，漏了清单就不齐
			ids = append(ids, *row.OperatorID)
		}
	}
	// 补当前使用人名字，不含密码。
	people, err := s.store.PeopleByIDs(ctx, ids)
	// 人员读失败则设备列表不带使用人
	if err != nil {
		return nil, err
	}
	// 按条数决定是空、超限还是继续
	out := make([]ClientView, 0, len(rows))
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, row := range rows {
		// 先放上设备本体，使用人有则再补
		view := ClientView{Client: row}
		// 这台没有使用人或使用人不是当前登录者
		if row.OperatorID != nil {
			// 对上使用人就补登录名，对不上就留空
			if p, ok := people[*row.OperatorID]; ok {
				// 填上当前使用人的登录名，没有则空
				view.OperatorLogin = p.LoginName
				// 填上当前使用人的显示名
				view.OperatorDisplay = p.DisplayName
			}
		}
		// 把这一条收进结果，漏了清单就不齐
		out = append(out, view)
	}
	// 列出设备成功后记审计，再把结果交回
	return out, s.audit(ctx, &acc.ID, nil, "list_clients", "clients", audit.Allow)
}

// ListRuntimeGrants 每台 Client 只回最高修订，不含声明原文和签名。
func (s *Node) ListRuntimeGrants(ctx context.Context, token string) ([]RuntimeGrantView, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return nil, err
	}
	// 只有工厂超管能看节点凭证摘要。失败记拒绝。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 不是工厂超管，记下列出运行凭证被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "list_runtime", "runtime", audit.Deny)
		return nil, err
	}
	// 每台只取最高修订，不含声明原文
	rows, err := s.store.ListRuntimeGrants(ctx)
	// 凭证列表读失败，不返回残缺摘要
	if err != nil {
		return nil, err
	}
	// 用来挡重复，同一份只收一次
	seen := map[uuid.UUID]struct{}{}
	// 按已知条数预留位置
	out := make([]RuntimeGrantView, 0)
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, r := range rows {
		// 已经收过则跳过，保证一份只出现一次
		if _, ok := seen[r.ClientID]; ok {
			continue
		}
		// 记下已经收过，后面的重复直接跳过
		seen[r.ClientID] = struct{}{}
		// 把这一条收进结果，漏了清单就不齐
		out = append(out, RuntimeGrantView{
			ClientID: r.ClientID, Revision: r.Revision, CanRun: r.CanRun,
			NotBefore: r.NotBefore, NotAfter: r.NotAfter, CreatedAt: r.CreatedAt,
		})
	}
	// 列出运行凭证成功后记审计，再把结果交回
	return out, s.audit(ctx, &acc.ID, nil, "list_runtime", "runtime", audit.Allow)
}
