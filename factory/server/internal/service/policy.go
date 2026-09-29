package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/store"
)

const (
	CacheScopeAll     = store.CacheScopeAll     // 该人获准的全部工程（仍受上限）
	CacheScopeCurrent = store.CacheScopeCurrent // 只缓存当前激活工程及其成员工艺
)

// GetClientPolicy 由本厂超管读取对本厂全部 Client 生效的策略行。
func (s *Closure) GetClientPolicy(ctx context.Context, token string) (ClientPolicy, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return ClientPolicy{}, err
	}
	// 只有工厂超管能看本厂统一策略。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		return ClientPolicy{}, err
	}
	// 读对本厂全部本机生效的策略，再交回调用方
	return s.store.ClientPolicy(ctx)
}

// SetClientPolicy 由本厂超管改本厂一行策略并升高修订；审计不含钥原文。
func (s *Closure) SetClientPolicy(ctx context.Context, token string, in ClientPolicy) (ClientPolicy, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return ClientPolicy{}, err
	}
	// 读对本厂全部本机生效的策略
	old, err := s.store.ClientPolicy(ctx)
	// 策略读失败，不能改或下发
	if err != nil {
		return ClientPolicy{}, err
	}
	// 审计只记键名和修订，不含钥原文
	target := policyAuditTarget(old, in)
	// 只有工厂超管能改本厂 Client 策略。失败记拒绝，行不变。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		// 不是工厂超管，记下改本机策略被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "set_client_policy", target, audit.Deny)
		return ClientPolicy{}, err
	}
	// 这次没带扩展就沿用旧的，避免清掉
	if len(in.Extra) == 0 {
		// 这次没带扩展就沿用旧值，避免把扩展清空
		in.Extra = old.Extra
	}
	// 非法范围、上限或时效一律拒绝，不升高修订。
	got, err := s.store.SetClientPolicy(ctx, in)
	// 策略写不进去，修订不升高
	if err != nil {
		// 记下改本机策略被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "set_client_policy", target, audit.Deny)
		return ClientPolicy{}, err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &acc.ID, nil, "set_client_policy", policyAuditTarget(old, got), audit.Allow); err != nil {
		return ClientPolicy{}, err
	}
	// 把更高修订策略推给已绑定本机
	s.fanoutPolicy(ctx, got)
	return got, nil
}

// SetCacheLimit 只改缓存上限，仍走本厂一行策略并推给已绑定 Client。
func (s *Closure) SetCacheLimit(ctx context.Context, token string, n int) error {
	// 先读现行策略，只改上限后走统一写入。
	cur, err := s.store.ClientPolicy(ctx)
	// 策略读失败，不能改或下发
	if err != nil {
		return err
	}
	// 只改缓存上限，其它策略字段保持原样
	cur.MaxCachedProjects = n
	// 改本厂一行策略并升高修订
	_, err = s.SetClientPolicy(ctx, token, cur)
	return err
}

// 审计对象只记键名与新旧修订，不含钥原文或扩展值。
func policyAuditTarget(old, neu ClientPolicy) string {
	// 拼审计用的短句，不含钥和扩展值，再交回调用方
	return fmt.Sprintf(
		"revision %d→%d max=%d scope=%s persist=%t ttl=%d encrypt=%t extra=%s",
		old.Revision, neu.Revision, neu.MaxCachedProjects, neu.CacheScope, neu.PersistUnwrapKey, neu.KeyTTLSeconds, neu.EncryptPouch, extraKeyList(neu.Extra),
	)
}

// 扩展键只列名字，值不进审计。
func extraKeyList(raw json.RawMessage) string {
	// 准备接扩展键名，解析失败就当没有扩展
	var obj map[string]any
	// 解析失败则按无效拒绝或忽略
	if err := json.Unmarshal(raw, &obj); err != nil || len(obj) == 0 {
		return "-"
	}
	// 按条数决定是空、超限还是继续
	keys := make([]string, 0, len(obj))
	// 逐条处理，某一条失败不把整批悄悄算成功
	for k := range obj {
		// 把这一条收进结果，漏了清单就不齐
		keys = append(keys, k)
	}
	// 排好键名次序，审计文本才稳定
	sort.Strings(keys)
	// 把键名拼成一行，值不进审计，再交回调用方
	return strings.Join(keys, ",")
}
