// HTTP 适配：厂钥上送焊汇总，管理员读跨厂报表。
package httpapi_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 厂钥上送汇总，管理员能读，带人员则拒绝。
func TestWeldSummaryHTTP(t *testing.T) {
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
	// 生成厂钥，私钥只留在本测。
	pub, priv, err := nodekey.Generate()
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
	// 起临时云端，只在本测里访问。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)

	// 留下正文供后面断言，读失败就停测。
	body := `{"rows":[{"day":"2026-09-17","projectName":"单层-舷侧分段","weldKind":"single","runCount":5,"lengthMm":4000,"durationSec":200}]}`
	code, raw := doFactory(t, srv, created.Factory.ID, priv, "POST", "/v1/channel/weld-summaries", body)
	// 成功却不是无正文，调用方会误判。
	if code != http.StatusNoContent {
		// 上送没被收下，带上现场停测。
		t.Fatalf("put %d %s", code, raw)
	}
	// 发出请求并记下状态，对不上就停测。
	code, raw = do(t, srv, "GET", "/v1/weld-reports", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || !strings.Contains(raw, `"projectName":"单层-舷侧分段"`) || strings.Contains(raw, "loginName") {
		// 列表结果不对，带上现场停测。
		t.Fatalf("list %d %s", code, raw)
	}
	// 发出请求并记下状态，对不上就停测。
	code, raw = doFactory(t, srv, created.Factory.ID, priv, "POST", "/v1/channel/weld-summaries", `{"rows":[{"day":"2026-09-17","projectName":"x","weldKind":"single","runCount":1,"lengthMm":1,"durationSec":1,"personId":"no"}]}`)
	// 非法请求没被拒绝，就停测。
	if code != http.StatusBadRequest {
		// 带了人员字段却没被拒绝，就停测。
		t.Fatalf("person field %d %s", code, raw)
	}
	// 发出请求并记下状态，对不上就停测。
	code, _ = do(t, srv, "GET", "/v1/weld-reports", "", "")
	// 该拒绝的匿名或错凭证却通过了，就停测。
	if code != http.StatusUnauthorized {
		// 匿名调用没被拒绝，带上现场停测。
		t.Fatalf("anon %d", code)
	}
}

// 带厂钥签名发厂端请求，签错应被拒绝。
func doFactory(t *testing.T, srv *httptest.Server, factoryID uuid.UUID, priv []byte, method, path, body string) (int, string) {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 组好请求，组失败就停测。
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 标明 JSON，前端才按错误体解析。
	req.Header.Set("Content-Type", "application/json")
	// 用当前时刻签名，过期会被拒绝。
	unix := time.Now().Unix()
	// 用厂钥签时刻，云端用来认厂。
	sig := nodekey.Sign(priv, nodekey.MQTTConnectPayload(factoryID, unix))
	// 补上厂钥或会话，缺了应被拒绝。
	req.Header.Set("X-WMesh-Factory", factoryID.String())
	// 带上签名时刻，过期会被拒绝。
	req.Header.Set("X-WMesh-Time", strconv.FormatInt(unix, 10))
	// 带上厂钥签名，对不上就未授权。
	req.Header.Set("X-WMesh-Sign", base64.StdEncoding.EncodeToString(sig))
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
	raw, err := io.ReadAll(res.Body)
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 把状态和正文交回调用它的测试。
	return res.StatusCode, string(raw)
}
