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
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return ClientPolicy{}, err
	}
	// 只有工厂超管能看本厂统一策略。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		return ClientPolicy{}, err
	}
	return s.store.ClientPolicy(ctx)
}

// SetClientPolicy 由本厂超管改本厂一行策略并升高修订；审计不含钥原文。
func (s *Closure) SetClientPolicy(ctx context.Context, token string, in ClientPolicy) (ClientPolicy, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return ClientPolicy{}, err
	}
	old, err := s.store.ClientPolicy(ctx)
	if err != nil {
		return ClientPolicy{}, err
	}
	target := policyAuditTarget(old, in)
	// 只有工厂超管能改本厂 Client 策略。失败记拒绝，行不变。
	if err := s.can(ctx, acc, permManageAccount, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "set_client_policy", target, audit.Deny)
		return ClientPolicy{}, err
	}
	if len(in.Extra) == 0 {
		in.Extra = old.Extra
	}
	// 非法范围、上限或时效一律拒绝，不升高修订。
	got, err := s.store.SetClientPolicy(ctx, in)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "set_client_policy", target, audit.Deny)
		return ClientPolicy{}, err
	}
	if err := s.audit(ctx, &acc.ID, nil, "set_client_policy", policyAuditTarget(old, got), audit.Allow); err != nil {
		return ClientPolicy{}, err
	}
	s.fanoutPolicy(ctx, got)
	return got, nil
}

// SetCacheLimit 只改缓存上限，仍走本厂一行策略并推给已绑定 Client。
func (s *Closure) SetCacheLimit(ctx context.Context, token string, n int) error {
	// 先读现行策略，只改上限后走统一写入。
	cur, err := s.store.ClientPolicy(ctx)
	if err != nil {
		return err
	}
	cur.MaxCachedProjects = n
	_, err = s.SetClientPolicy(ctx, token, cur)
	return err
}

// 审计对象只记键名与新旧修订，不含钥原文或扩展值。
func policyAuditTarget(old, neu ClientPolicy) string {
	return fmt.Sprintf(
		"revision %d→%d max=%d scope=%s persist=%t ttl=%d encrypt=%t extra=%s",
		old.Revision, neu.Revision, neu.MaxCachedProjects, neu.CacheScope, neu.PersistUnwrapKey, neu.KeyTTLSeconds, neu.EncryptPouch, extraKeyList(neu.Extra),
	)
}

// 扩展键只列名字，值不进审计。
func extraKeyList(raw json.RawMessage) string {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || len(obj) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}
