// 登录现场：路径里已核的设备优先，正文自报号不能盖掉它。
package httpapi

import (
	"testing"

	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/service"
)

// 验收本机登录现场记路径那台，自报他机号即失败。
func TestLoginSnapKeepsVerifiedClient(t *testing.T) {
	// 路径里已核对过的本机。
	pinned := id.New()
	// 正文里自报的另一台。
	other := id.New()
	// 本机登录：正文自报另一台，现场仍应记路径那台。
	snap := loginSnap(service.LoginKindClient, 52, "6.1.1", "ARM-1", "", "", "", "", other.String(), &pinned)
	// 设备被自报号盖掉则本测失败。
	if snap.ClientID == nil || *snap.ClientID != pinned {
		// 与预期不符就停测，打出实际记下的设备。
		t.Fatalf("client login device %v", snap.ClientID)
	}
	// 厂网登录没有已核设备，合法自报号照常采用。
	snap = loginSnap(service.LoginKindPad, 52, "6.1.1", "", "", "", "", "", other.String(), nil)
	// 合法自报号没被采用则本测失败。
	if snap.ClientID == nil || *snap.ClientID != other {
		// 与预期不符就停测，打出实际记下的设备。
		t.Fatalf("pad login device %v", snap.ClientID)
	}
	// 没有已核设备且自报号非法，现场设备留空。
	snap = loginSnap(service.LoginKindPad, 52, "6.1.1", "", "", "", "", "", "not-a-uuid", nil)
	// 非法号被记成设备则本测失败。
	if snap.ClientID != nil {
		// 与预期不符就停测，打出实际记下的设备。
		t.Fatalf("bad client id kept %v", snap.ClientID)
	}
}
