package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/service"
)

// 验收焊事实上报和厂端报表权限。
func TestWeldFactsHTTP(t *testing.T) {
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
	srv := httptest.NewServer(httpapi.New(h, "boot-secret", "").Router())
	// 测完关闭临时服务，把端口让出来。
	t.Cleanup(srv.Close)
	// 拼出本厂前缀，后面的路径都接在后面。
	base := "/v1/factories/" + fid.String()
	// 用共享密码建厂，激活码只出现在回包。
	code, body := do(t, srv, "POST", "/internal/bootstrap", "boot-secret", `{"factoryId":"`+fid.String()+`","saLogin":"sa","saDisplay":"超管"}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("bootstrap %d %s", code, body)
	}
	// 按工厂打开已有库，没有库就当未初始化。
	svc, err := h.Service(context.Background(), fid)
	// 这一步失败则停测，避免在坏结果上继续。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("service: %v", err)
	}
	// 租约写不进去则停测，后面读正文会失败。
	if err := svc.Store().GrantLocalLease(context.Background()); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("lease: %v", err)
	}
	// 取出激活码，启用账号时要用。
	act := gjson(t, body, "activationToken")
	// 用激活码启用账号，错码应当被拒绝。
	code, body = do(t, srv, "POST", base+"/activate", "", `{"loginName":"sa","activationToken":"`+act+`","password":"secret"}`)
	// 不是无内容则本测失败。
	if code != http.StatusNoContent {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("activate %d %s", code, body)
	}
	// 登录网页并取回会话，失败则停测。
	saTok := loginWeb(t, srv, base, "sa", "secret")
	// 办理组织节点，无上级则挂在工厂下。
	code, body = do(t, srv, "POST", base+"/org-units", saTok, `{"name":"车间"}`)
	// 状态不在允许的几种里则本测失败。
	if code != http.StatusOK && code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("unit %d %s", code, body)
	}
	// 建人走服务，避免 HTTP 建人响应差一档。
	ctx := context.Background()
	// 超管新建账号，默认口令立即可用。
	pa, err := svc.CreatePerson(ctx, saTok, "op-a", "焊工A")
	// 这一步失败则停测，避免在坏结果上继续。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 超管新建账号，默认口令立即可用。
	pb, err := svc.CreatePerson(ctx, saTok, "op-b", "焊工B")
	// 这一步失败则停测，避免在坏结果上继续。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 取出组织节点，后面按岗给操作员授权。
	units, err := svc.Store().ListOrgUnits(ctx)
	// 这一步失败则停测，避免在坏结果上继续。
	if err != nil || len(units) == 0 {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("units %v", err)
	}
	// 用第一个组织给操作员授岗。
	shop := units[0]
	// 授权失败则停测，后面的身份就不对。
	if _, err := svc.GrantRole(ctx, saTok, pa.ID, service.RoleOperator, service.ScopeOrgUnit, &shop.ID); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 授权失败则停测，后面的身份就不对。
	if _, err := svc.GrantRole(ctx, saTok, pb.ID, service.RoleOperator, service.ScopeOrgUnit, &shop.ID); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 这一步被拒绝则停测，避免后面误判。
	if err := svc.Assign(ctx, saTok, pa.ID, shop.ID); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 这一步被拒绝则停测，避免后面误判。
	if err := svc.Assign(ctx, saTok, pb.ID, shop.ID); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 登录并取得会话，口令错误应当被拒绝。
	code, body = do(t, srv, "POST", base+"/pad/login", "", `{"loginName":"op-b","password":"`+defaultPass("op-b")+`"}`)
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pad b %d %s", code, body)
	}
	// 取出会话令牌，后面的请求要带上。
	bTok := gjson(t, body, "token")
	// 分配一个新身份，避免和别的测试撞上。
	idA := id.New().String()
	// 分配一个新身份，避免和别的测试撞上。
	idB := id.New().String()
	// 当前必须是对象，否则无法继续往下取。
	payload, _ := json.Marshal(map[string]any{
		"facts": []map[string]any{
			{
				"id": idA, "creatorId": pa.ID.String(), "orgUnitId": shop.ID.String(),
				"orgPath":  []map[string]string{{"id": shop.ID.String(), "name": shop.Name}},
				"lengthMm": 1500, "durationSec": 90,
				"occurredAt": time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC).Format(time.RFC3339),
			},
			{
				"id": idB, "creatorId": pb.ID.String(), "orgUnitId": shop.ID.String(),
				"orgPath":  []map[string]string{{"id": shop.ID.String(), "name": shop.Name}},
				"lengthMm": 400, "durationSec": 20,
				"occurredAt": time.Date(2026, 9, 17, 1, 0, 0, 0, time.UTC).Format(time.RFC3339),
			},
		},
	})
	// 上报焊事实，一条非法则整批应当被拒绝。
	code, body = do(t, srv, "POST", base+"/pad/weld-facts", "", string(payload))
	// 不是未授权则本测失败。
	if code != http.StatusUnauthorized {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("anon flush %d %s", code, body)
	}
	// 先数汇聚拒绝审计，匿名大正文被挡后应多一行。
	denyBefore := auditDenies(t, svc, "flush_weld_facts")
	// 匿名带两兆垃圾正文，应先验令牌回 401，不是 400 也不是 413。
	code, body = do(t, srv, "POST", base+"/pad/weld-facts", "", strings.Repeat("x", 2<<20))
	// 不是未授权则本测失败。
	if code != http.StatusUnauthorized {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("anon big flush %d %s", code, body)
	}
	// 先验令牌不能丢掉汇聚拒绝审计，少了就停测。
	if got := auditDenies(t, svc, "flush_weld_facts"); got != denyBefore+1 {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("flush_weld_facts deny audit %d -> %d", denyBefore, got)
	}
	// 上报焊事实，一条非法则整批应当被拒绝。
	code, body = do(t, srv, "POST", base+"/pad/weld-facts", bTok, string(payload))
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, idA) || !strings.Contains(body, idB) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("flush %d %s", code, body)
	}
	// 查看当前登录人的焊长合计。
	code, body = do(t, srv, "GET", base+"/pad/weld-stats", bTok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "lengthMm") != "400" || !strings.Contains(body, "runCount") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("b stats %d %s", code, body)
	}
	// 查看厂端报表，越权应当被拒绝。
	code, body = do(t, srv, "GET", base+"/weld-reports?group=project", saTok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, "runCount") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("project report %d %s", code, body)
	}
	// 按洛杉矶时区看日报，两条凌晨一点的 UTC 焊次都落到前一天。
	code, body = do(t, srv, "GET", base+"/weld-reports?group=day&tz=America/Los_Angeles", saTok, "")
	// 要有当地 09-15、不能有 UTC 才有的 09-17，否则时区没传到底。
	if code != http.StatusOK || !strings.Contains(body, `"day":"2026-09-15"`) || strings.Contains(body, `"day":"2026-09-17"`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("day report tz %d %s", code, body)
	}
	// 时区名不认识，应当回非法请求而不是悄悄按 UTC。
	code, body = do(t, srv, "GET", base+"/weld-reports?group=day&tz=Bad/Zone", saTok, "")
	// 不是非法请求则本测失败。
	if code != http.StatusBadRequest {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("bad tz %d %s", code, body)
	}
	// 再送一条厂直属焊，下钻按 direct 要能单独取出。
	idC := id.New().String()
	// 厂直属不带节点，路径为空。
	directPayload, _ := json.Marshal(map[string]any{
		"facts": []map[string]any{{
			"id": idC, "creatorId": pa.ID.String(), "orgPath": []map[string]string{},
			"lengthMm": 100, "durationSec": 5,
			"occurredAt": time.Date(2026, 9, 16, 2, 0, 0, 0, time.UTC).Format(time.RFC3339),
		}},
	})
	// 上报这条厂直属焊，应当收下。
	code, body = do(t, srv, "POST", base+"/pad/weld-facts", bTok, string(directPayload))
	// 不是成功，或回包里没有这条则失败。
	if code != http.StatusOK || !strings.Contains(body, idC) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("direct flush %d %s", code, body)
	}
	// 按条件查看每次起停。
	code, body = do(t, srv, "GET", base+"/weld-runs", saTok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, "op-a") || !strings.Contains(body, "op-b") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("runs %d %s", code, body)
	}
	// 合计按全部三条算，不受分页影响。
	if gjson(t, body, "total") != "3" || gjson(t, body, "lengthMm") != "2000" || gjson(t, body, "durationSec") != "115" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("runs totals %s", body)
	}
	// 每页一条，总数仍是三条。
	code, body = do(t, srv, "GET", base+"/weld-runs?limit=1&offset=1", saTok, "")
	// 解开下钻页，看本页条数和总数。
	page := decodeRunPage(t, body)
	// 不是成功，或本页不是一条、总数不是三条则失败。
	if code != http.StatusOK || len(page.Items) != 1 || page.Total != 3 {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("runs page %d %s", code, body)
	}
	// 只要厂直属，应只有刚送的那条。
	code, body = do(t, srv, "GET", base+"/weld-runs?orgUnitId=direct", saTok, "")
	// 解开下钻页，看是不是只有厂直属那条。
	page = decodeRunPage(t, body)
	// 不是成功，或混进了有节点的焊则失败。
	if code != http.StatusOK || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != idC || page.LengthMM != 100 {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("runs direct %d %s", code, body)
	}
	// 分页参数和组织编号不合法，都应当回非法请求。
	for _, q := range []string{"limit=-1", "limit=abc", "offset=-5", "orgUnitId=bad"} {
		// 带着坏参数查下钻。
		code, body = do(t, srv, "GET", base+"/weld-runs?"+q, saTok, "")
		// 不是非法请求则本测失败。
		if code != http.StatusBadRequest {
			// 与预期不符就停测，避免后面连环误判。
			t.Fatalf("runs %s %d %s", q, code, body)
		}
	}
	// 写入演示焊次，已经有了应当跳过。
	code, body = do(t, srv, "POST", base+"/weld-reports/demo", bTok, "")
	// 不是禁止则本测失败。
	if code != http.StatusForbidden {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("op demo %d %s", code, body)
	}
	// 写入演示焊次，已经有了应当跳过。
	code, body = do(t, srv, "POST", base+"/weld-reports/demo", saTok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, `"created":`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("demo %d %s", code, body)
	}
	// 积压一大批：工程名拉到 200 字上限，凑出两兆多正文，超过普通一兆上限。
	name := strings.Repeat("工程", 100)
	// 两千六百条，约两兆多，核对既不限字节到一兆也不限条数。
	facts := make([]map[string]any, 0, 2600)
	// 逐条拼出合法焊事实，创建人都是焊工A。
	for i := 0; i < 2600; i++ {
		// 每条新身份，避免和前面的事实撞上。
		facts = append(facts, map[string]any{
			"id": id.New().String(), "creatorId": pa.ID.String(), "orgUnitId": shop.ID.String(),
			"orgPath":     []map[string]string{{"id": shop.ID.String(), "name": shop.Name}},
			"projectName": name, "lengthMm": 10, "durationSec": 1,
			"occurredAt": time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC).Format(time.RFC3339),
		})
	}
	// 收成正文，拼不出来就停测。
	bigPayload, err := json.Marshal(map[string]any{"facts": facts})
	// 正文拼不出来或不够两兆，测不到批量上限。
	if err != nil || len(bigPayload) < 2<<20 {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("big payload %d %v", len(bigPayload), err)
	}
	// 登录后送这一大批，应当整批收下。
	code, body = do(t, srv, "POST", base+"/pad/weld-facts", bTok, string(bigPayload))
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("big flush %d %.200s", code, body)
	}
	// 接住回包里收下和拒绝的清单。
	var flushed struct {
		Accepted []string `json:"accepted"` // 已收下的事实身份
		Rejected []any    `json:"rejected"` // 被拒的事实
	}
	// 回包解不开则停测。
	if err := json.Unmarshal([]byte(body), &flushed); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("big flush body: %v", err)
	}
	// 收下条数应等于送出条数，说明没有条数上限。
	if len(flushed.Accepted) != len(facts) || len(flushed.Rejected) != 0 {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("big flush accepted %d rejected %d", len(flushed.Accepted), len(flushed.Rejected))
	}
}

// 下钻回包里的一次起停，只看身份。
type runPageItem struct {
	ID string `json:"id"` // 事实身份
}

// 下钻回包里测试要看的几项。
type runPageBody struct {
	Items    []runPageItem `json:"items"`    // 本页起停
	Total    int64         `json:"total"`    // 同条件总条数
	LengthMM int64         `json:"lengthMm"` // 同条件焊长合计
}

// 解开下钻回包，解不开就停测。
func decodeRunPage(t *testing.T, body string) runPageBody {
	// 标成辅助函数，失败时栈指向真正的用例。
	t.Helper()
	// 准备承接下钻页。
	var page runPageBody
	// 回包解不开则停测。
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("run page %s: %v", body, err)
	}
	return page
}

// 登录网页并带回会话，失败则停测。
func loginWeb(t *testing.T, srv *httptest.Server, base, login, pass string) string {
	// 标成辅助函数，失败时栈指向真正的用例。
	t.Helper()
	// 登录并取得会话，口令错误应当被拒绝。
	code, body := do(t, srv, "POST", base+"/login", "", `{"loginName":"`+login+`","password":"`+pass+`"}`)
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("login %s %d %s", login, code, body)
	}
	// 取出会话令牌，后面的请求要带上。
	return gjson(t, body, "token")
}

// 按登录名得到交付时的默认口令。
func defaultPass(login string) string {
	return login + "123456"
}

// 数本厂某个动作的拒绝审计有几行。
func auditDenies(t *testing.T, svc *service.Service, action string) int {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 读出本厂全部审计行，再按动作和结果筛。
	rows, err := svc.ListAudit(context.Background())
	// 这一步失败则停测，避免在坏结果上继续。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 从零起数，只算这一动作的拒绝。
	n := 0
	// 逐行看动作和结果。
	for _, row := range rows {
		// 动作对得上且是拒绝才算一行。
		if row.Action == action && row.Result == audit.Deny {
			// 多数一行拒绝。
			n++
		}
	}
	return n
}
