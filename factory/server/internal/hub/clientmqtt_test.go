package hub

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/clientmqtt"
	"wmesh/factory/internal/platform/domain"
)

// 示教器 CONNECT 被拒分两类：令牌、账号、设备、厂停用注销照原样拒（示教器登出）；
// 租约没了、库连不上包成暂不可用（示教器只重试），WAN 断开时厂端重启不再把人踢下线（D-89、D-47）。
func TestMQTTAuthErrSplitsDeadFromUnavailable(t *testing.T) {
	// 这些是真不能再用，不能包成暂不可用。
	for _, dead := range []error{
		// 令牌、账号、设备、厂停用注销这几类。
		domain.ErrUnauthorized, domain.ErrSessionExpired, domain.ErrAccountDisabled, domain.ErrForbidden,
		domain.ErrBindingVoid, domain.ErrFactoryDisabled, domain.ErrFactoryRetired,
	} {
		// 包成暂不可用就停住。
		if got := mqttAuthErr(dead); errors.Is(got, clientmqtt.ErrUnavailable) || !errors.Is(got, dead) {
			// 把错误打出来并停住。
			t.Fatalf("%v -> %v", dead, got)
		}
	}
	// 这些是厂暂时不能服务，要包成暂不可用，原因仍认得出。
	for _, busy := range []error{domain.ErrContentLeaseExpired, context.DeadlineExceeded, errors.New("dial tcp: connection refused")} {
		// 换算一次。
		got := mqttAuthErr(busy)
		// 没包成暂不可用或丢了原因就停住。
		if !errors.Is(got, clientmqtt.ErrUnavailable) || !errors.Is(got, busy) {
			// 把错误打出来并停住。
			t.Fatalf("%v -> %v", busy, got)
		}
	}
}

// 令牌钉的不是这台设备（D-59）包成设备不符，不当令牌失效也不算厂暂不可用；原因仍认得出。
func TestMQTTAuthErrMarksClientMismatch(t *testing.T) {
	// 换算一次。
	got := mqttAuthErr(domain.ErrSessionClientMismatch)
	// 没包成设备不符、丢了原因或被当成暂不可用就停住。
	if !errors.Is(got, clientmqtt.ErrClientMismatch) || !errors.Is(got, domain.ErrSessionClientMismatch) || errors.Is(got, clientmqtt.ErrUnavailable) {
		// 把错误打出来并停住。
		t.Fatalf("mismatch -> %v", got)
	}
}
