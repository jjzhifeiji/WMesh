// 现场认领：WAN 回的拒绝原因按业务错误回给认领页。
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/testpg"
)

// WAN 按来源限频回 429 时，认领页也拿到 429 和限频原因，不是 500 内部错误。
func TestClaimWANTooManyRequests(t *testing.T) {
	// 假平台对认领一律回请求过多，模拟同一出口 IP 名额用完。
	wan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 标明是结构化正文，和 WAN 一样。
		w.Header().Set("Content-Type", "application/json")
		// 先写请求过多的状态码。
		w.WriteHeader(http.StatusTooManyRequests)
		// 正文和 WAN 限频时一样。
		_, _ = w.Write([]byte(`{"error":"too many requests"}`))
	}))
	// 测完关掉假平台，避免端口一直占住。
	t.Cleanup(wan.Close)
	// 连上测试库并建枢纽，连不上就停测。
	h, err := hub.New(testpg.AdminDSN())
	// 测试库连不上则停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("hub: %v", err)
	}
	// 测完关掉枢纽，释放测试占用的连接。
	t.Cleanup(h.Close)
	// 拉起指向假平台的厂端服务，测完由清理关掉。
	srv := httptest.NewServer(httpapi.New(h, "boot-secret", wan.URL).Router())
	// 测完关闭临时服务，把端口让出来。
	t.Cleanup(srv.Close)
	// 用建厂码认领，WAN 第一步就限频。
	code, body := do(t, srv, "POST", "/v1/site/claim", "", `{"enrollmentCode":"code","password":"secret-123"}`)
	// 不是 429 就停测，页面会误报服务异常。
	if code != http.StatusTooManyRequests {
		// 带上状态和正文便于定位。
		t.Fatalf("claim %d %s", code, body)
	}
	// 原因不是限频就停测，前端靠这句翻成中文。
	if got := gjson(t, body, "error"); got != "too many requests" {
		// 带上实际原因便于定位。
		t.Fatalf("claim error %q", got)
	}
}
