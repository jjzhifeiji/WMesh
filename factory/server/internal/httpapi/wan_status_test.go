package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
)

// 通道现状要登录才能看；没连过 WAN 时报断开，不带最近连上和租约到期。
func TestWANStatusHTTP(t *testing.T) {
	// 连上测试库并建枢纽，连不上就停测。
	h, err := hub.New(testpg.AdminDSN())
	// 测试库连不上则停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("hub: %v", err)
	}
	// 为本测单独分配工厂，避免和别的库串。
	fid := id.New()
	// 测完删掉临时工厂并关掉枢纽。
	t.Cleanup(func() {
		// 删掉这个测试工厂，避免库一直留着。
		_ = h.Drop(fid)
		// 关掉枢纽，释放测试占用的连接。
		h.Close()
	})
	// 拉起临时服务，测完由清理关掉。
	srv := httptest.NewServer(httpapi.New(h, "").Router())
	// 测完关闭临时服务，把端口让出来。
	t.Cleanup(srv.Close)
	// 拼出本厂前缀，后面的路径都接在后面。
	base := "/v1/factories/" + fid.String()
	// 按约定身份建厂，初始超管当场有效。
	enrollSA(t, h, fid)
	// 未登录看通道现状应被拒。
	code, body := do(t, srv, "GET", base+"/wan-status", "", "")
	// 匿名放行就停测。
	if code != http.StatusUnauthorized {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("anon wan-status %d %s", code, body)
	}
	// 登录拿会话，后面带上它看现状。
	tok := loginWeb(t, srv, base, "sa", "secret")
	// 登录后看通道现状。
	code, body = do(t, srv, "GET", base+"/wan-status", tok, "")
	// 没连过 WAN：应报未配置、断开，不带最近连上和租约到期。
	if code != http.StatusOK || !strings.Contains(body, `"configured":false`) || !strings.Contains(body, `"connected":false`) || strings.Contains(body, "lastConnectedAt") || strings.Contains(body, "leaseExpiresAt") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("wan-status %d %s", code, body)
	}
}
