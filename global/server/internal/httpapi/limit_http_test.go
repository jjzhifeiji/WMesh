// HTTP 适配：正文上限、按来源限速、先验身份再读大正文。
package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 起一块带唯一管理员的云端库，交回服务和会话。
func limitService(t *testing.T) (*service.Service, string) {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 打开测试库管理员，用来建一块独立库。
	admin := testpg.Open(t)
	// 建一块独立库，不碰别的测试数据。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_wan_limit")
	// 把库迁到当前结构，再交给服务。
	svc := service.NewService(store.Open(testpg.OpenMigrated(t, dsn)))
	// 初始管理员没建起来，后面的测试不能做。
	if err := svc.BootstrapAdmin(context.Background(), "w", "wan-secret"); err != nil {
		// 初始管理员没建起来，就停测。
		t.Fatalf("bootstrap: %v", err)
	}
	// 直接走服务登录，不占 HTTP 来源名额。
	tok, err := svc.Login(context.Background(), "w", "wan-secret")
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	return svc, tok
}

// 每次新起一个云端，来源名额互不影响。
func limitServer(t *testing.T, svc *service.Service) *httptest.Server {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 起临时云端，只在本测里访问。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)
	return srv
}

// 造一份带超长名字的 JSON，长度正好是 size 字节。
func nameOfSize(size int) string {
	// 先算出外壳占的字节，余下全部填进名字。
	head, tail := `{"name":"`, `"}`
	// 名字填满剩下的字节，整份正好 size。
	return head + strings.Repeat("a", size-len(head)-len(tail)) + tail
}

// 超大正文回 413 且不写审计；超长名字与登录名截断后入审计；匿名大正文先回 401。
func TestBodyLimitHTTP(t *testing.T) {
	// 起库和服务，拿到管理员会话。
	svc, tok := limitService(t)
	// 起临时云端。
	srv := limitServer(t, svc)
	// 拒绝入口随手编一个厂身份，不看厂在不在。
	denyPath := "/v1/factories/" + uuid.NewString() + "/assets"
	// 先数一下审计行，超限请求不该多出来。
	before, err := svc.ListAudit(context.Background())
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 两兆名字塞给拒绝入口，应直接回 413。
	code, body := do(t, srv, "POST", denyPath, "", nameOfSize(2<<20))
	// 状态或错误句不对就停测。
	if code != http.StatusRequestEntityTooLarge || gjson(t, body, "error") != "request body too large" {
		// 超大正文没被拦成 413，就停测。
		t.Fatalf("2MiB deny %d %.200s", code, body)
	}
	// 再数一次审计行。
	after, err := svc.ListAudit(context.Background())
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 超限请求写了审计就停测，能灌爆审计表。
	if len(after) != len(before) {
		// 审计多出行，带上条数停测。
		t.Fatalf("audit grew %d -> %d", len(before), len(after))
	}
	// 一千字，不超正文上限，应照常拒绝。
	long := strings.Repeat("x", 1000)
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", denyPath, "", `{"name":"`+long+`"}`)
	// 不该放行的操作没被拦住，就停测。
	if code != http.StatusForbidden {
		// 拒绝入口回包不对，带上现场停测。
		t.Fatalf("long deny %d %s", code, body)
	}
	// 一千字登录名去登录，口令不对应照常拒绝。
	code, body = do(t, srv, "POST", "/v1/login", "", `{"loginName":"`+long+`","password":"bad"}`)
	// 不该放行的登录没被拦住，就停测。
	if code != http.StatusUnauthorized {
		// 登录回包不对，带上现场停测。
		t.Fatalf("long login %d %s", code, body)
	}
	// 读审计，核对超长字符串已截断。
	rows, err := svc.ListAudit(context.Background())
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 记下两条拒绝审计是否都找到。
	foundTarget, foundLogin := false, false
	// 逐行找这两次拒绝，看截断结果。
	for _, row := range rows {
		// 代建原件拒绝：对象超过 512 字加省略号就停测。
		if row.Action == "create_factory_asset" {
			// 记下找到了代建拒绝。
			foundTarget = true
			// 对象没截断就停测。
			if utf8.RuneCountInString(row.Target) > 513 {
				// 审计没截断，带上长度停测。
				t.Fatalf("target not clipped: %d", utf8.RuneCountInString(row.Target))
			}
		}
		// 登录拒绝：登录名超过 128 字加省略号就停测。
		if row.Action == "login" && row.ClaimedLogin != nil {
			// 记下找到了登录拒绝。
			foundLogin = true
			// 登录名没截断就停测。
			if utf8.RuneCountInString(*row.ClaimedLogin) > 129 {
				// 审计没截断，带上长度停测。
				t.Fatalf("login not clipped: %d", utf8.RuneCountInString(*row.ClaimedLogin))
			}
		}
	}
	// 没找到拒绝审计就停测，被拒要留痕。
	if !foundTarget || !foundLogin {
		// 拒绝没落审计，就停测。
		t.Fatalf("deny audit missing target=%v login=%v", foundTarget, foundLogin)
	}
	// 匿名发两兆软件包，应先回 401，不读包。
	code, body = do(t, srv, "POST", "/v1/software", "", `{"content":"`+strings.Repeat("A", 2<<20)+`"}`)
	// 该拒绝的匿名却没回未授权，就停测。
	if code != http.StatusUnauthorized {
		// 匿名上传没先验身份，带上现场停测。
		t.Fatalf("anon software %d %.200s", code, body)
	}
	// 先数导入拒绝审计，匿名被挡后应多一行。
	denyBefore := auditDenies(t, svc, "import_legacy")
	// 匿名发两兆旧文件导入，应先回 401。
	code, body = do(t, srv, "POST", "/v1/legacy-import", "", `{"processes":[{"path":"a","content":"`+strings.Repeat("A", 2<<20)+`"}]}`)
	// 该拒绝的匿名却没回未授权，就停测。
	if code != http.StatusUnauthorized {
		// 匿名导入没先验身份，带上现场停测。
		t.Fatalf("anon legacy %d %.200s", code, body)
	}
	// 先验身份不能丢掉导入拒绝审计，少了就停测。
	if got := auditDenies(t, svc, "import_legacy"); got != denyBefore+1 {
		// 拒绝没落审计，带上前后条数停测。
		t.Fatalf("import_legacy deny audit %d -> %d", denyBefore, got)
	}
	// 管理员导入两兆正文（前面全是空白），批量上限放得下。
	code, body = do(t, srv, "POST", "/v1/legacy-import", tok, strings.Repeat(" ", 2<<20)+`{"processes":[],"projects":[]}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 批量入口被普通上限卡住，就停测。
		t.Fatalf("admin legacy 2MiB %d %.200s", code, body)
	}
}

// 同一来源连错十次后第十一次回 429；拒绝入口同样按来源限速。
func TestRateLimitHTTP(t *testing.T) {
	// 起库和服务。
	svc, _ := limitService(t)
	// 登录单独起一个云端，名额不和别的入口混。
	loginSrv := limitServer(t, svc)
	// 前十次错口令都应照常回 401。
	for i := range 10 {
		// 发出请求并记下状态，对不上就停测。
		code, body := do(t, loginSrv, "POST", "/v1/login", "", `{"loginName":"w","password":"bad"}`)
		// 突发以内应照常比对口令。
		if code != http.StatusUnauthorized {
			// 突发以内就被挡，带上次数停测。
			t.Fatalf("login %d: %d %s", i+1, code, body)
		}
	}
	// 第十一次应被来源闸挡下。
	code, body := do(t, loginSrv, "POST", "/v1/login", "", `{"loginName":"w","password":"bad"}`)
	// 状态或错误句不对就停测。
	if code != http.StatusTooManyRequests || gjson(t, body, "error") != "too many login attempts" {
		// 来源限速没生效，带上现场停测。
		t.Fatalf("11th login %d %s", code, body)
	}
	// 代管拒绝入口另起一个云端单测。
	anonSrv := limitServer(t, svc)
	// 随手编一个厂身份，拒绝入口不看厂在不在。
	denyPath := "/v1/factories/" + uuid.NewString() + "/assets"
	// 前十次都应照常拒绝。
	for i := range 10 {
		// 发出请求并记下状态，对不上就停测。
		code, body := do(t, anonSrv, "POST", denyPath, "", fmt.Sprintf(`{"name":"n%d"}`, i))
		// 突发以内应照常回禁止。
		if code != http.StatusForbidden {
			// 突发以内就被挡，带上次数停测。
			t.Fatalf("deny %d: %d %s", i+1, code, body)
		}
	}
	// 第十一次应被来源闸挡下。
	code, body = do(t, anonSrv, "POST", denyPath, "", `{"name":"n10"}`)
	// 状态或错误句不对就停测。
	if code != http.StatusTooManyRequests || gjson(t, body, "error") != "too many requests" {
		// 匿名入口限速没生效，带上现场停测。
		t.Fatalf("11th deny %d %s", code, body)
	}
}

// 数某个动作的拒绝审计有几行。
func auditDenies(t *testing.T, svc *service.Service, action string) int {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 读出全部审计行，再按动作和结果筛。
	rows, err := svc.ListAudit(context.Background())
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
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

// 焊汇总两兆照收，超过六十四兆回 413。
func TestWeldSummaryBodyLimitHTTP(t *testing.T) {
	// 起库和服务，拿到管理员会话。
	svc, tok := limitService(t)
	// 建一家厂，后面用厂钥上送。
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
	// 起临时云端。
	srv := limitServer(t, svc)
	// 拼四千行汇总，工程名较长，凑到两兆左右。
	var b strings.Builder
	// 先写外壳开头。
	b.WriteString(`{"rows":[`)
	// 逐行写汇总，工程名各不相同。
	for i := range 4000 {
		// 第二行起先补逗号。
		if i > 0 {
			// 行与行之间用逗号隔开。
			b.WriteString(",")
		}
		// 写一行汇总，工程名带序号免得撞键。
		fmt.Fprintf(&b, `{"day":"2026-09-17","projectName":"工程-%04d-%s","weldKind":"single","runCount":5,"lengthMm":4000,"durationSec":200}`, i, strings.Repeat("长", 140))
	}
	// 写外壳结尾。
	b.WriteString(`]}`)
	// 正文不到两兆就停测，测不出放宽。
	if b.Len() < 2<<20 {
		// 正文偏小，带上字节数停测。
		t.Fatalf("body only %d bytes", b.Len())
	}
	// 用厂钥上送，应整表收下。
	code, raw := doFactory(t, srv, created.Factory.ID, priv, "POST", "/v1/channel/weld-summaries", b.String())
	// 成功应回 200 带回执，状态不对就是被拦了。
	if code != http.StatusOK {
		// 两兆汇总被拦，带上现场停测。
		t.Fatalf("2MiB summaries %d %.200s", code, raw)
	}
	// 超过六十四兆的正文，应直接回 413。
	huge := strings.Repeat(" ", 64<<20+1) + `{"rows":[]}`
	// 用厂钥上送超大正文。
	code, raw = doFactory(t, srv, created.Factory.ID, priv, "POST", "/v1/channel/weld-summaries", huge)
	// 状态或错误句不对就停测。
	if code != http.StatusRequestEntityTooLarge || gjson(t, raw, "error") != "request body too large" {
		// 超大汇总没被拦成 413，就停测。
		t.Fatalf("huge summaries %d %.200s", code, raw)
	}
}
