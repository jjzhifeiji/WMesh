package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/testpg"
)

// 现场申请转给 WAN：密码不出厂，版本带上，进度和凭据原样回给页面。
func TestSiteOnboardingRelay(t *testing.T) {
	// 记下 WAN 收到的申请。
	var got map[string]any
	// 假平台回凭据和进度。
	wan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 标明是结构化正文。
		w.Header().Set("Content-Type", "application/json")
		// 按路径回包。
		switch r.URL.Path {
		// 申请入口。
		case "/v1/channel/onboarding":
			// 记下收到的正文，解不开就当没收到。
			_ = json.NewDecoder(r.Body).Decode(&got)
			// 创建成功。
			w.WriteHeader(http.StatusCreated)
			// 回凭据。
			_, _ = w.Write([]byte(`{"requestId":"r1","secret":"s1","verifyCode":"654321","expiresAt":"2030-01-01T00:00:00Z"}`))
		// 进度入口。
		case "/v1/channel/onboarding/status":
			// 回已批准。
			_, _ = w.Write([]byte(`{"status":"approved","factoryName":"现场厂"}`))
		// 其余路径拒绝。
		default:
			// 别的请求不当成功。
			http.NotFound(w, r)
		}
	}))
	// 测完关掉假平台。
	t.Cleanup(wan.Close)
	// 建枢纽。
	h, err := hub.New(testpg.AdminDSN())
	// 测试库连不上则停测。
	if err != nil {
		// 带上现场。
		t.Fatalf("hub: %v", err)
	}
	// 测完关掉枢纽。
	t.Cleanup(h.Close)
	// 拉起指向假平台的厂端服务。
	srv := httptest.NewServer(httpapi.New(h, wan.URL).Router())
	// 测完关闭临时服务。
	t.Cleanup(srv.Close)
	// 提交申请。
	code, body := do(t, srv, "POST", "/v1/site/onboarding", "", `{"factoryName":"现场厂","saLogin":"boss","saDisplay":"老板"}`)
	// 必须创建成功且带回核对码。
	if code != http.StatusCreated || gjson(t, body, "verifyCode") != "654321" {
		// 带上现场。
		t.Fatalf("apply %d %s", code, body)
	}
	// WAN 必须收到厂名、登录名和版本。
	if got["factoryName"] != "现场厂" || got["saLogin"] != "boss" || got["serviceVersionName"] == "" {
		// 带上现场。
		t.Fatalf("wan got %v", got)
	}
	// 申请里不能有密码字段。
	if _, leaked := got["password"]; leaked {
		// 带上现场。
		t.Fatalf("password sent to wan: %v", got)
	}
	// 查进度。
	code, body = do(t, srv, "POST", "/v1/site/onboarding/status", "", `{"secret":"s1"}`)
	// 进度原样回给页面。
	if code != http.StatusOK || gjson(t, body, "status") != "approved" || gjson(t, body, "factoryName") != "现场厂" {
		// 带上现场。
		t.Fatalf("status %d %s", code, body)
	}
}
