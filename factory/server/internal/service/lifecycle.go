package service

import (
	"context"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// ApplyLifecycle 按 WAN 修订落地停用/启用/注销；低修订忽略。
func (s *Auth) ApplyLifecycle(ctx context.Context, status string, revision int64) (store.Lifecycle, error) {
	switch status {
	case store.FactoryActive, store.FactoryDisabled, store.FactoryRetired:
	default:
		return store.Lifecycle{}, domain.ErrForbidden
	}
	before, err := s.store.Lifecycle(ctx)
	if err != nil {
		return store.Lifecycle{}, err
	}
	// 按 WAN 修订落地；修订没涨就不写审计。
	out, err := s.store.ApplyLifecycle(ctx, status, revision)
	if err != nil {
		_ = s.audit(ctx, nil, nil, "apply_lifecycle", s.store.FactoryID().String(), audit.Deny)
		return store.Lifecycle{}, err
	}
	if before.Revision == out.Revision {
		return out, nil
	}
	if err := s.audit(ctx, nil, nil, "apply_lifecycle", status, audit.Allow); err != nil {
		return store.Lifecycle{}, err
	}
	return out, nil
}

// CloseFromWAN 名录已注销或已从 WAN 消失时，本厂立即拒绝登录。
func (s *Auth) CloseFromWAN(ctx context.Context) error {
	// 已注销则幂等；否则抬修订立刻关掉登录。
	cur, err := s.store.Lifecycle(ctx)
	if err != nil {
		return err
	}
	if cur.Status == store.FactoryRetired {
		return nil
	}
	_, err = s.ApplyLifecycle(ctx, store.FactoryRetired, cur.Revision+1)
	return err
}

// requireFactoryOpen 停用、注销或解包租约到期后，拒绝登录和新操作。
func (k *kernel) requireFactoryOpen(ctx context.Context) error {
	lc, err := k.store.Lifecycle(ctx)
	if err != nil {
		return err
	}
	switch lc.Status {
	case store.FactoryDisabled:
		return domain.ErrFactoryDisabled
	case store.FactoryRetired:
		return domain.ErrFactoryRetired
	}
	// 超时整厂业务不可用；探活和 WAN 通道仍可把钥匙领回来。
	return k.store.RequireContentLease()
}
