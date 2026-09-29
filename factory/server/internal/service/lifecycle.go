package service

import (
	"context"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// ApplyLifecycle 按 WAN 修订落地停用/启用/注销；低修订忽略。
func (s *Auth) ApplyLifecycle(ctx context.Context, status string, revision int64) (store.Lifecycle, error) {
	// 只按启用、停用、注销三种厂况落地
	switch status {
	// 只接受启用、停用、注销这三种厂况
	case store.FactoryActive, store.FactoryDisabled, store.FactoryRetired:
	// 其它厂况一律拒绝，避免写成非法状态
	default:
		return store.Lifecycle{}, domain.ErrForbidden
	}
	// 读本厂启停状态和修订
	before, err := s.store.Lifecycle(ctx)
	// 厂况读失败，不能改登录门
	if err != nil {
		return store.Lifecycle{}, err
	}
	// 按 WAN 修订落地；修订没涨就不写审计。
	out, err := s.store.ApplyLifecycle(ctx, status, revision)
	// 厂况写失败，登录门仍按旧状态
	if err != nil {
		// 记下落地厂况被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "apply_lifecycle", s.store.FactoryID().String(), audit.Deny)
		return store.Lifecycle{}, err
	}
	// 修订没涨则不写审计，避免重复记账
	if before.Revision == out.Revision {
		return out, nil
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, nil, nil, "apply_lifecycle", status, audit.Allow); err != nil {
		return store.Lifecycle{}, err
	}
	return out, nil
}

// CloseFromWAN 名录已注销或已从 WAN 消失时，本厂立即拒绝登录。
func (s *Auth) CloseFromWAN(ctx context.Context) error {
	// 已注销则幂等；否则抬修订立刻关掉登录。
	cur, err := s.store.Lifecycle(ctx)
	// 厂况读失败，不能改登录门
	if err != nil {
		return err
	}
	// 已经注销则不必再抬修订
	if cur.Status == store.FactoryRetired {
		return nil
	}
	// 按更高修订落地启停或注销
	_, err = s.ApplyLifecycle(ctx, store.FactoryRetired, cur.Revision+1)
	return err
}

// requireFactoryOpen 停用、注销或解包租约到期后，拒绝登录和新操作。
func (k *kernel) requireFactoryOpen(ctx context.Context) error {
	// 读本厂启停状态和修订
	lc, err := k.store.Lifecycle(ctx)
	// 厂况读失败，不能改登录门
	if err != nil {
		return err
	}
	// 停用和注销直接拒绝，有效再看租约
	switch lc.Status {
	// 厂已停用，拒绝新登录和新操作
	case store.FactoryDisabled:
		return domain.ErrFactoryDisabled
	// 厂已注销，立即拒绝登录
	case store.FactoryRetired:
		return domain.ErrFactoryRetired
	}
	// 超时整厂业务不可用；探活和 WAN 通道仍可把钥匙领回来。
	return k.store.RequireContentLease()
}
