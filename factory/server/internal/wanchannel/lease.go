package wanchannel

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
)

var leaseTimeout = 30 * time.Second // 单次领租约的时限，回包很小，卡住就退避重试

var errLeaseNotApplied = errors.New("content lease not applied") // 领到了但钥坏或落不上，按退避重领

// KeepLease 独立经 HTTPS 续内容租约，不依赖 MQTT；成功按点再续，失败退避重试，注销或取消才返回。
func KeepLease(ctx context.Context, wanHTTP string, factoryID uuid.UUID, priv []byte, applyLease LeaseHandler) error {
	// 没有签发钥就不续，避免匿名去领。
	if len(priv) == 0 {
		return domain.ErrNotFound
	}
	// 没有平台地址就当不可达，避免去拨空主机。
	if strings.TrimSpace(wanHTTP) == "" {
		return domain.ErrWANUnreachable
	}
	// 没人接租约就不必续，空打平台没有意义。
	if applyLease == nil {
		return nil
	}
	// 用厂钥签每次领取，没有签名会被拒绝。
	pull := newPuller(wanHTTP, factoryID, priv)
	// 失败后的等待从起步时长算，成功就收回。
	wait := retryBase
	// 领一次、等一段，取消或注销才离开。
	for {
		// 领一次并落地，交回是否成功。
		err := renewLease(ctx, pull, applyLease)
		// 已经取消就离开，不再退避。
		if ctx.Err() != nil {
			// 把取消原因交回外层。
			return ctx.Err()
		}
		// 注销后不再续，交回外层停掉通道。
		if errors.Is(err, domain.ErrFactoryRetired) {
			return err
		}
		// 成功就隔一个续租间隔再领。
		delay := leaseEvery
		// 失败按退避等，不能干等一小时。
		if err != nil {
			// 这次按当前退避时长等。
			delay = wait
			// 记下续租失败和下次等待，便于排查弱网。
			slog.Warn("renew content lease failed, will retry", "factory", factoryID, "wait", delay, "err", err)
			// 下一次等待翻倍，弱网下不打满平台。
			wait = min(wait*2, retryCap)
		} else { // 领到并落地就把退避收回起步。
			// 退避回到起步，下次失败从头算。
			wait = retryBase
		}
		// 拨好这一段等待的节拍。
		timer := time.NewTimer(delay)
		// 等到点或取消，谁先到走谁。
		select {
		// 取消就停节拍离开。
		case <-ctx.Done():
			// 停掉节拍，不留计时器。
			timer.Stop()
			// 把取消原因交回外层。
			return ctx.Err()
		// 到点就再领一次。
		case <-timer.C:
		}
	}
}

// 领一次租约并交给本厂：拉不到交回原因，钥坏或落不上也算失败，好按退避重领。
func renewLease(ctx context.Context, pull *puller, applyLease LeaseHandler) error {
	// 单次领取限时，弱网卡住也能退回重试。
	rctx, cancel := context.WithTimeout(ctx, leaseTimeout)
	// 领完就放掉时限。
	defer cancel()
	// 预备装新的解包钥。
	var lr leaseResp
	// 签名 GET 领租约，拿不到就交回原因。
	if err := pull.get(rctx, "/v1/channel/lease", &lr); err != nil {
		return err
	}
	// 交给本厂落地，没落上就交回，按退避重领。
	if !applyPulledLease(applyLease, lr) {
		return errLeaseNotApplied
	}
	return nil
}
