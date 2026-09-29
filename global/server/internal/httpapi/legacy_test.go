// HTTP 适配：WAN 旧文件导入。
package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 空导入成功，没登录则拒绝。
func TestLegacyImportHTTP(t *testing.T) {
	// 打开测试库管理员，用来建一块独立库。
	admin := testpg.Open(t)
	// 建一块独立库，不碰别的测试数据。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_wan")
	// 把库迁到当前结构，再交给服务。
	svc := service.NewService(store.Open(testpg.OpenMigrated(t, dsn)))
	// 初始管理员没建起来，后面的测试不能做。
	if err := svc.BootstrapAdmin(context.Background(), "w", "wan-secret"); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 用初始管理员登录，拿会话做后面的调用。
	tok, err := svc.Login(context.Background(), "w", "wan-secret")
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 起临时云端，只在本测里访问。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)

	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/legacy-import", tok, `{"processes":[],"projects":[]}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || !strings.Contains(body, `"processes":[]`) || !strings.Contains(body, `"rejected":[]`) || !strings.Contains(body, `"skipped":[]`) {
		// 空导入结果不对，带上现场停测。
		t.Fatalf("empty %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, _ = do(t, srv, "POST", "/v1/legacy-import", "", `{"processes":[],"projects":[]}`)
	// 该拒绝的匿名或错凭证却通过了，就停测。
	if code != http.StatusUnauthorized {
		// 匿名调用没被拒绝，带上现场停测。
		t.Fatalf("anon %d", code)
	}
}
