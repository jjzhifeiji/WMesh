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
	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 厂钥上送汇总，坏行进回执，管理员能读，带人员则拒绝。
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

	// 一行好一行日期坏，好行入库，坏行只跳过。
	body := `{"rows":[{"day":"2026-09-17","projectName":"单层-舷侧分段","weldKind":"single","runCount":5,"lengthMm":4000,"durationSec":200},{"day":"2026-13-01","projectName":"坏","weldKind":"single","runCount":1,"lengthMm":1,"durationSec":1}]}`
	// 用厂钥上送，收下回执留着断言。
	code, raw := doFactory(t, srv, created.Factory.ID, priv, "POST", "/v1/channel/weld-summaries", body)
	// 应回 200，且回执里跳过一行、原因是日期坏。
	if code != http.StatusOK || !strings.Contains(raw, `"skippedCount":1`) || !strings.Contains(raw, `{"index":1,"reason":"invalid day"}`) {
		// 上送没被收下或回执不对，带上现场停测。
		t.Fatalf("put %d %s", code, raw)
	}
	// 发出请求并记下状态，对不上就停测。
	code, raw = do(t, srv, "GET", "/v1/weld-reports", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || !strings.Contains(raw, `"projectName":"单层-舷侧分段"`) || strings.Contains(raw, `"projectName":"坏"`) || strings.Contains(raw, "loginName") {
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

// 注销后的厂用厂钥上送空表回 403，云端汇总不动并记拒绝；停用照常上送。
func TestWeldSummaryRetiredFactory(t *testing.T) {
	// 准备本测上下文，服务调用都用它。
	ctx := context.Background()
	// 打开测试库管理员，用来建一块独立库。
	admin := testpg.Open(t)
	// 建一块独立库，不碰别的测试数据。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_wan")
	// 把库迁到当前结构，再交给服务。
	svc := service.NewService(store.Open(testpg.OpenMigrated(t, dsn)))
	// 立初始管理员，失败后面没人能注销厂。
	if err := svc.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立初始管理员失败。
		t.Fatal(err)
	}
	// 用初始管理员登录，停用、注销和查汇总都要会话。
	tok, err := svc.Login(ctx, "w", "wan-secret")
	// 登录失败就停，没有会话后面做不了。
	if err != nil {
		// 不符即停：登录失败。
		t.Fatal(err)
	}
	// 建一家厂，后面认领、上送和注销都指它。
	created, err := svc.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 建厂失败就停，没有厂可上送。
	if err != nil {
		// 不符即停：建厂失败。
		t.Fatal(err)
	}
	// 记下这家厂，后面反复用。
	fid := created.Factory.ID
	// 生成厂钥，私钥只留在本测。
	pub, priv, err := nodekey.Generate()
	// 生成失败就停，没有钥签不了名。
	if err != nil {
		// 不符即停：生成厂钥失败。
		t.Fatal(err)
	}
	// 交钥认领，注销时才走只改状态那条路。
	if err := svc.ConfirmEnroll(ctx, fid, pub); err != nil {
		// 不符即停：认领失败。
		t.Fatal(err)
	}
	// 起临时云端，只在本测里访问。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)
	// 先上送一行好汇总，作为注销前留在云端的历史。
	code, raw := doFactory(t, srv, fid, priv, "POST", "/v1/channel/weld-summaries", `{"rows":[{"day":"2026-09-17","projectName":"单层-舷侧分段","weldKind":"single","runCount":5,"lengthMm":4000,"durationSec":200}]}`)
	// 正常厂上送应回 200。
	if code != http.StatusOK {
		// 不符即停：正常上送没被收下。
		t.Fatalf("put active %d %s", code, raw)
	}
	// 停用这家厂，停用仍应能上送。
	if _, err := svc.DisableFactory(ctx, tok, fid); err != nil {
		// 不符即停：停用失败。
		t.Fatal(err)
	}
	// 停用后再送两行，应照常整表替换。
	code, raw = doFactory(t, srv, fid, priv, "POST", "/v1/channel/weld-summaries", `{"rows":[{"day":"2026-09-17","projectName":"单层-舷侧分段","weldKind":"single","runCount":5,"lengthMm":4000,"durationSec":200},{"day":"2026-09-18","projectName":"多层-箱体","weldKind":"multilayer","runCount":1,"lengthMm":100,"durationSec":10}]}`)
	// 停用厂上送应回 200。
	if code != http.StatusOK {
		// 不符即停：停用厂上送被拒。
		t.Fatalf("put disabled %d %s", code, raw)
	}
	// 注销这家厂，已认领只改状态，历史保留。
	if _, err := svc.DeleteFactory(ctx, tok, fid); err != nil {
		// 不符即停：注销失败。
		t.Fatal(err)
	}
	// 注销后上送空表，旧代码会借此清空历史。
	code, raw = doFactory(t, srv, fid, priv, "POST", "/v1/channel/weld-summaries", `{"rows":[]}`)
	// 已注销应回 403。
	if code != http.StatusForbidden {
		// 不符即停：注销厂上送没被拒。
		t.Fatalf("put retired %d %s", code, raw)
	}
	// 查这家厂的汇总，应仍是停用时那两行。
	rows, err := svc.Stats.ListWeldReports(ctx, tok, fid.String(), nil, nil)
	// 查询失败或行数变了，说明空表还是覆盖了。
	if err != nil || len(rows) != 2 {
		// 不符即停：注销后云端汇总被改动。
		t.Fatalf("rows after retired put %d %v", len(rows), err)
	}
	// 读审计，核对这次拒绝留了痕。
	logs, err := svc.ListAudit(ctx)
	// 读审计失败就停，读不到就无法核对。
	if err != nil {
		// 不符即停：读审计失败。
		t.Fatal(err)
	}
	// 数出这家厂上送汇总被拒的条数。
	denies := 0
	// 逐行看审计，只数本厂的上送拒绝。
	for _, row := range logs {
		// 动作、结果和对象都对上才算这一条。
		if row.Action == "put_weld_summaries" && row.Result == audit.Deny && row.Target == fid.String() {
			// 对上就记一条。
			denies++
		}
	}
	// 只有注销后那一次被拒，应恰好一条。
	if denies != 1 {
		// 不符即停：拒绝审计条数不对。
		t.Fatalf("put_weld_summaries deny audits %d, want 1", denies)
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
