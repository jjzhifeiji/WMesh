// HTTP 适配：厂出站认领 HTTPS，日常 MQTT 指令加 HTTPS 拉正文。
package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 建厂码换身份，再交钥完成认领。
func TestChannelEnroll(t *testing.T) {
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
	// 建一家厂，后面用建厂码或身份继续。
	created, err := svc.CreateFactory(context.Background(), tok, "厂A", "sa-a", "超管A")
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 起临时云端，只在本测里访问。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)

	// 把建厂码收成请求，码错应被拒绝。
	enrollBody, err := json.Marshal(map[string]string{"enrollmentCode": created.EnrollmentToken})
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/channel/enroll", "", string(enrollBody))
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 认领结果不对，带上现场停测。
		t.Fatalf("enroll %d %s", code, body)
	}
	// 读出的字段和预期不符，就停测。
	if gjson(t, body, "factoryId") != created.Factory.ID.String() || gjson(t, body, "saLogin") != "sa-a" {
		// 认领准备失败，就停测。
		t.Fatalf("enroll: %s", body)
	}
	// 生成厂钥，私钥只留在本测。
	pub, _, err := nodekey.Generate()
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 把建厂码收成请求，码错应被拒绝。
	claimBody, err := json.Marshal(map[string]any{"enrollmentCode": created.EnrollmentToken, "factoryPublicKey": pub})
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/channel/claim", "", string(claimBody))
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 交钥认领结果不对，带上现场停测。
		t.Fatalf("claim %d %s", code, body)
	}
	// 读出的字段和预期不符，就停测。
	if gjson(t, body, "factoryId") != created.Factory.ID.String() || gjson(t, body, "status") != service.FactoryActive {
		// 交钥结果和预期不符，就停测。
		t.Fatalf("claim: %s", body)
	}
}

// 停用和注销之后，认领必须被拒绝。
func TestChannelEnrollAfterDelete(t *testing.T) {
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
	// 建一家厂，后面用建厂码或身份继续。
	created, err := svc.CreateFactory(context.Background(), tok, "厂A", "sa-a", "超管A")
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 先停用，用来断言停用厂不能认领。
	if _, err := svc.DisableFactory(context.Background(), tok, created.Factory.ID); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 起临时云端，只在本测里访问。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)

	// 把建厂码收成请求，码错应被拒绝。
	enrollBody, err := json.Marshal(map[string]string{"enrollmentCode": created.EnrollmentToken})
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/channel/enroll", "", string(enrollBody))
	// 不该放行的操作没被拦住，就停测。
	if code != http.StatusForbidden || !strings.Contains(body, "factory is disabled") {
		// 停用厂仍能换身份，就停测。
		t.Fatalf("enroll disabled %d %s", code, body)
	}
	// 重新启用，才能继续认领和注销。
	if _, err := svc.EnableFactory(context.Background(), tok, created.Factory.ID); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 生成厂钥，私钥只留在本测。
	pub, _, err := nodekey.Generate()
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 交钥没登记成功，认领就不能算完成。
	if err := svc.ConfirmEnroll(context.Background(), created.Factory.ID, pub); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "DELETE", "/v1/factories/"+created.Factory.ID.String(), tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 删除结果不对，带上现场停测。
		t.Fatalf("delete %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/channel/enroll", "", string(enrollBody))
	// 该拒绝的匿名或错凭证却通过了，就停测。
	if code != http.StatusUnauthorized || !strings.Contains(body, "invalid enrollment") {
		// 注销后仍能换身份，就停测。
		t.Fatalf("enroll after delete %d %s", code, body)
	}
	// 把建厂码收成请求，码错应被拒绝。
	claimBody, err := json.Marshal(map[string]any{"enrollmentCode": created.EnrollmentToken, "factoryPublicKey": pub})
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/channel/claim", "", string(claimBody))
	// 该拒绝的匿名或错凭证却通过了，就停测。
	if code != http.StatusUnauthorized || !strings.Contains(body, "invalid enrollment") {
		// 注销后仍能交钥，就停测。
		t.Fatalf("claim after delete %d %s", code, body)
	}
}

// 等到名录上的在线状态符合预期，超时失败。
func assertOnline(t *testing.T, srv *httptest.Server, tok string, want bool) {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 只等一小段，超时就当没有变化。
	deadline := time.Now().Add(3 * time.Second)
	// 在时限内反复看，超时再判失败。
	for time.Now().Before(deadline) {
		// 在线状态已经符合预期，不必再等。
		if channelOnline(t, srv, tok) == want {
			return
		}
		// 稍等再看，避免通知还没写进名录。
		time.Sleep(50 * time.Millisecond)
	}
	// 在线状态没在时限内符合预期。
	t.Fatalf("want online=%v", want)
}

// 问名录，看该厂通道现在是否在线。
func channelOnline(t *testing.T, srv *httptest.Server, tok string) bool {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 组好请求，组失败就停测。
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/directory", nil)
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 带上管理员会话，匿名请求会被拒绝。
	req.Header.Set("Authorization", "Bearer "+tok)
	// 发出去，连不上就停测。
	res, err := http.DefaultClient.Do(req)
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 用完就关上，避免连接或文件一直占着。
	defer res.Body.Close()
	// 读完全部正文，读失败就停测。
	body, err := io.ReadAll(res.Body)
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 没成功就停测，避免把失败响应当成数据。
	if res.StatusCode != http.StatusOK {
		// 名录没读成，带上现场停测。
		t.Fatalf("directory %d %s", res.StatusCode, body)
	}
	// 名录里标了在线，才算这厂通道还连着。
	return strings.Contains(string(body), `"channelOnline":true`)
}
