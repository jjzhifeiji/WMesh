package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
)

// ClientView 是给管理端看的设备行，带焊次里推出的最近使用人和累计焊长，不含私钥。
type ClientView struct {
	Client                     // 设备本体，这个视图不含私钥
	LastUserLogin   string     `json:"lastUserLogin,omitempty"`   // 最近一次焊的人的登录名
	LastUserDisplay string     `json:"lastUserDisplay,omitempty"` // 最近一次焊的人的显示名
	LastUsedAt      *time.Time `json:"lastUsedAt,omitempty"`      // 最近一次焊完时间；没焊过为空
	WeldLengthMM    int64      `json:"weldLengthMm"`              // 累计焊长毫米
	WeldRunCount    int64      `json:"weldRunCount"`              // 累计焊次
}

// RenameClient 由本厂有效超管改昵称；设备名由 WAN 定，不改归属。
func (s *Node) RenameClient(ctx context.Context, token string, clientID uuid.UUID, name string) (Client, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return Client{}, err
	}
	// 只有工厂超管能改显示名。失败一律记拒绝。
	if err := s.can(ctx, acc, permManageAccount); err != nil {
		// 不是工厂超管，记下设备改名被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "rename_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 留空表示去掉昵称，改回显示 WAN 设备名
	if strings.TrimSpace(name) == "" {
		// 清空昵称
		name = ""
		// 有字才核长度
	} else if name, err = normalizeClientName(name); err != nil {
		// 记下设备改名被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "rename_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	// 只改昵称，不改归属。
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

// ListClients 列出本厂已接受的 Client；含当前使用人，不含私钥。
func (s *Node) ListClients(ctx context.Context, token string) ([]ClientView, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return nil, err
	}
	// 只有工厂超管能看设备名录。失败记拒绝。
	if err := s.can(ctx, acc, permManageAccount); err != nil {
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
	// 从焊次推每台的最近使用人和累计焊长
	usage, err := s.store.ListClientWeldUsage(ctx)
	// 焊次汇总读失败，不返回残缺列表
	if err != nil {
		return nil, err
	}
	// 收齐最近使用人，一次读名字
	ids := make([]uuid.UUID, 0, len(usage))
	// 逐台记下最近使用人
	for _, u := range usage {
		// 把这一条收进结果，漏了清单就不齐
		ids = append(ids, u.LastPersonID)
	}
	// 补最近使用人名字，不含密码。
	people, err := s.store.PeopleByIDs(ctx, ids)
	// 人员读失败则设备列表不带使用人
	if err != nil {
		return nil, err
	}
	// 按条数决定是空、超限还是继续
	out := make([]ClientView, 0, len(rows))
	// 逐台补上焊次概况
	for _, row := range rows {
		// 先放上设备本体，焊过再补概况
		view := ClientView{Client: row}
		// 这台焊过才有概况
		if u, ok := usage[row.ID]; ok {
			// 另拷一份时间再取址
			at := u.LastAt
			// 填上最近使用时间
			view.LastUsedAt = &at
			// 填上累计焊长
			view.WeldLengthMM = u.LengthMM
			// 填上累计焊次
			view.WeldRunCount = u.RunCount
			// 对上人就补名字
			if p, ok := people[u.LastPersonID]; ok {
				// 填上最近使用人的登录名
				view.LastUserLogin = p.LoginName
				// 填上最近使用人的显示名
				view.LastUserDisplay = p.DisplayName
			}
		}
		// 把这一条收进结果，漏了清单就不齐
		out = append(out, view)
	}
	// 设备名录是前端每 5 秒的轮询读，成功不记允许，免得撑大审计表
	return out, nil
}
