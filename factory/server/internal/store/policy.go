package store

import (
	"bytes"
	"context"
	"encoding/json"

	"gorm.io/gorm"

	"wmesh/factory/internal/platform/domain"
)

const (
	CacheScopeAll     = "all"     // 该人获准的全部工程（仍受上限）
	CacheScopeCurrent = "current" // 只缓存当前激活工程及其成员工艺
)

// ClientPolicy 是本厂一行 Client 策略，对本厂全部 Client 生效。
type ClientPolicy struct {
	Revision          int64           `json:"revision"`          // 策略修订；Client 只接受更高
	MaxCachedProjects int             `json:"maxCachedProjects"` // 每 Client 工程份上限，≥1
	CacheScope        string          `json:"cacheScope"`        // current / all
	PersistUnwrapKey  bool            `json:"persistUnwrapKey"`  // 解封钥可否落盘；退出或登录到期必清
	KeyTTLSeconds     int64           `json:"keyTtlSeconds"`     // 登录时效秒；0 表示直到退出，进程内不踢
	EncryptPouch      bool            `json:"encryptPouch"`      // 本机袋是否 SQLCipher；关则明文 SQLite
	Extra             json.RawMessage `json:"extra"`             // 本厂扩展键；Client 忽略未知
}

type factorySettingsRow struct {
	ID                int16           `gorm:"primaryKey"`                          // 固定 1
	MaxCachedProjects int             `gorm:"column:max_cached_projects;not null"`  // 缓存上限
	Revision          int64           `gorm:"column:revision;not null"`             // 策略修订
	CacheScope        string          `gorm:"column:cache_scope;not null"`          // current / all
	PersistUnwrapKey  bool            `gorm:"column:persist_unwrap_key;not null"`   // 解封钥可否落盘
	KeyTTLSeconds     int64           `gorm:"column:key_ttl_seconds;not null"`      // 登录时效秒
	EncryptPouch      bool            `gorm:"column:encrypt_pouch;not null"`        // 本机袋是否加密
	Extra             json.RawMessage `gorm:"column:extra;type:jsonb;not null"`     // 扩展键
}

func (factorySettingsRow) TableName() string { return "factory_settings" }

// ClientPolicy 读本厂这一行策略。
func (s *Store) ClientPolicy(ctx context.Context) (ClientPolicy, error) {
	var row factorySettingsRow
	if err := s.db.WithContext(ctx).First(&row, "id = 1").Error; err != nil {
		return ClientPolicy{}, err
	}
	return policyFromRow(row)
}

// SetClientPolicy 写入本厂策略并升高修订；上限须 ≥1，范围只认 current/all。
func (s *Store) SetClientPolicy(ctx context.Context, in ClientPolicy) (ClientPolicy, error) {
	if err := assertClientPolicy(in); err != nil {
		return ClientPolicy{}, err
	}
	extra, err := normalizeExtra(in.Extra)
	if err != nil {
		return ClientPolicy{}, err
	}
	// 每次超管写入都加修订，Client 之后只收更高修订。
	res := s.db.WithContext(ctx).Model(&factorySettingsRow{}).Where("id = 1").Updates(map[string]any{
		"max_cached_projects": in.MaxCachedProjects,
		"cache_scope":         in.CacheScope,
		"persist_unwrap_key":  in.PersistUnwrapKey,
		"key_ttl_seconds":     in.KeyTTLSeconds,
		"encrypt_pouch":       in.EncryptPouch,
		"extra":               []byte(extra),
		"revision":            gorm.Expr("revision + 1"),
	})
	if res.Error != nil {
		return ClientPolicy{}, res.Error
	}
	if res.RowsAffected == 0 {
		return ClientPolicy{}, domain.ErrNotFound
	}
	return s.ClientPolicy(ctx)
}

// CacheLimit 读本厂 Client 工程缓存上限。
func (s *Store) CacheLimit(ctx context.Context) (int, error) {
	p, err := s.ClientPolicy(ctx)
	if err != nil {
		return 0, err
	}
	return p.MaxCachedProjects, nil
}

// SetCacheLimit 只改上限并升高策略修订，须 ≥1。
func (s *Store) SetCacheLimit(ctx context.Context, n int) error {
	cur, err := s.ClientPolicy(ctx)
	if err != nil {
		return err
	}
	cur.MaxCachedProjects = n
	_, err = s.SetClientPolicy(ctx, cur)
	return err
}

// 库行收成策略视图，扩展键缺省当空对象。
func policyFromRow(row factorySettingsRow) (ClientPolicy, error) {
	extra, err := normalizeExtra(row.Extra)
	if err != nil {
		return ClientPolicy{}, err
	}
	return ClientPolicy{
		Revision:          row.Revision,
		MaxCachedProjects: row.MaxCachedProjects,
		CacheScope:        row.CacheScope,
		PersistUnwrapKey:  row.PersistUnwrapKey,
		KeyTTLSeconds:     row.KeyTTLSeconds,
		EncryptPouch:      row.EncryptPouch,
		Extra:             extra,
	}, nil
}

// 上限、范围和时效不合法则拒绝写入。
func assertClientPolicy(in ClientPolicy) error {
	if in.MaxCachedProjects < 1 {
		return domain.ErrForbidden
	}
	if in.CacheScope != CacheScopeAll && in.CacheScope != CacheScopeCurrent {
		return domain.ErrForbidden
	}
	if in.KeyTTLSeconds < 0 {
		return domain.ErrForbidden
	}
	return nil
}

// 扩展键必须是 JSON 对象；空或 null 当 {}。
func normalizeExtra(raw json.RawMessage) (json.RawMessage, error) {
	trim := bytes.TrimSpace(raw)
	if len(trim) == 0 || string(trim) == "null" {
		return json.RawMessage(`{}`), nil
	}
	var obj map[string]any
	if err := json.Unmarshal(trim, &obj); err != nil {
		return nil, domain.ErrForbidden
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(out), nil
}
