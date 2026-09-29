// HTTP 适配：WAN 登录、建厂、拒绝代管厂内账号。
package httpapi_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/id"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/platform/release"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"

	"github.com/google/uuid"
)

// 登录、建厂，并确认代管厂内账号被拒绝。
func TestWANHTTP(t *testing.T) {
	// 打开测试库管理员，用来建一块独立库。
	admin := testpg.Open(t)
	// 建一块独立库，不碰别的测试数据。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_wan")
	// 把库迁到当前结构，再交给服务。
	svc := service.NewService(store.Open(testpg.OpenMigrated(t, dsn)))
	// 初始管理员没建起来，后面的测试不能做。
	if err := svc.BootstrapAdmin(context.Background(), "w", "wan-secret"); err != nil {
		// 初始管理员没建起来，就停测。
		t.Fatalf("bootstrap: %v", err)
	}
	// 起临时云端，只在本测里访问。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)

	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "GET", "/healthz", "", "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || gjson(t, body, "db") != "ok" || gjson(t, body, "build") != "test" || gjson(t, body, "version") != strconv.FormatInt(release.Code, 10) || gjson(t, body, "versionName") != release.Name || gjson(t, body, "oss") != "off" {
		// 探活结果不对，带上现场停测。
		t.Fatalf("healthz %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/nope", "", "")
	// 未知路径却没回找不到，就停测。
	if code != http.StatusNotFound || gjson(t, body, "error") != "not found" {
		// 未知接口没按找不到处理，就停测。
		t.Fatalf("unknown api %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/login", "", `{"loginName":"w","password":"bad"}`)
	// 该拒绝的匿名或错凭证却通过了，就停测。
	if code != http.StatusUnauthorized {
		// 错误密码没被拒绝，就停测。
		t.Fatalf("bad login %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/login", "", `{"loginName":"w","password":"wan-secret"}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 登录没成功，后面没有会话可用。
		t.Fatalf("login %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	tok := gjson(t, body, "token")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/me/password", tok, `{"password":"wan-secret-2"}`)
	// 成功却不是无正文，调用方会误判。
	if code != http.StatusNoContent {
		// 改密没成功，带上现场停测。
		t.Fatalf("change password %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/directory", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 名录没读成，带上现场停测。
		t.Fatalf("directory %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/factories", tok, `{"name":"厂A","saLogin":"sa-a","saDisplay":"超管"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 建厂没成功，带上现场停测。
		t.Fatalf("create factory %d %s", code, body)
	}
	// 建厂没带回一次性码，厂端无法认领。
	if gjson(t, body, "enrollmentToken") == "" {
		// 建厂没带回一次性码，就停测。
		t.Fatalf("enrollment token missing: %s", body)
	}
	// 取出字段供后面使用，缺了就停测。
	fid := gjson(t, body, "factory.id")
	// 新厂不是在营，后面的认领不能做。
	if gjson(t, body, "factory.status") != "active" {
		// 新厂不是在营，带上现场停测。
		t.Fatalf("new factory status %s", body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/factories", tok, `{"name":"厂B","saLogin":"sa-b","saDisplay":"超管B"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 建厂没成功，带上现场停测。
		t.Fatalf("create factory B %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	fidB := gjson(t, body, "factory.id")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/factories/"+fidB+"/disable", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || gjson(t, body, "status") != "disabled" {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("disable %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/factories/"+fidB+"/enable", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || gjson(t, body, "status") != "active" {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("enable %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "DELETE", "/v1/factories/"+fidB, tok, "")
	// 成功却不是无正文，调用方会误判。
	if code != http.StatusNoContent {
		// 交钥认领结果不对，带上现场停测。
		t.Fatalf("delete unclaimed %d %s", code, body)
	}
	// 留下厂钥，私钥只在本测里签名。
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 把包收成文本放进 JSON，超限会被拒绝。
	pk := base64.StdEncoding.EncodeToString(pub)
	// 没指定身份就发新号，保证一机一号。
	cid := id.New().String()
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/clients", tok, `{"name":"焊机-1","id":"`+cid+`","deviceSerial":"ARM-1","factoryId":"`+fid+`","publicKey":"`+pk+`"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("bind client %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/clients", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || !strings.Contains(body, cid) {
		// 列表结果不对，带上现场停测。
		t.Fatalf("list clients %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/factories/"+fid+"/offline-grants", tok, "")
	// 不该放行的操作没被拦住，就停测。
	if code != http.StatusForbidden {
		// 列表结果不对，带上现场停测。
		t.Fatalf("list offline grants %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/invite-wan-admin", tok, `{"loginName":"other"}`)
	// 不该放行的操作没被拦住，就停测。
	if code != http.StatusForbidden {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("invite %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/factories/"+fid+"/people", tok, `{"loginName":"p1"}`)
	// 不该放行的操作没被拦住，就停测。
	if code != http.StatusForbidden {
		// 带了人员字段却没被拒绝，就停测。
		t.Fatalf("proxy person %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/factories/"+fid+"/people", tok, "")
	// 不该放行的操作没被拦住，就停测。
	if code != http.StatusForbidden {
		// 列表结果不对，带上现场停测。
		t.Fatalf("list people %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"平台焊","content":"wan-body"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated || !strings.Contains(body, `"copyable":false`) {
		// 创建没成功，带上现场停测。
		t.Fatalf("create platform process %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	pid := gjson(t, body, "id")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"开焊","content":"open-body","copyable":true}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated || !strings.Contains(body, `"copyable":true`) || gjson(t, body, "revision") != "1" {
		// 创建没成功，带上现场停测。
		t.Fatalf("create copyable platform process %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/assets?kind=process", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || !strings.Contains(body, pid) || strings.Contains(body, "wan-body") {
		// 列表结果不对，带上现场停测。
		t.Fatalf("list platform %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/assets/"+pid+"/content", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || gjson(t, body, "content") != appliedProcess("wan-body") {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("read platform content %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/content", tok, `{"expected":1,"content":"wan-body-2"}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("update platform content %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/publish", tok, `{"expected":2}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 发布没成功，带上现场停测。
		t.Fatalf("publish platform %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/assets/"+pid, tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("get platform %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	digest := gjson(t, body, "digest")
	// 取出字段供后面使用，缺了就停测。
	rev := gjson(t, body, "revision")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/project-templates", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || !strings.Contains(body, `"processId"`) || strings.Contains(body, "processPath") {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("project templates %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets", tok, `{"kind":"project","name":"平台工程","content":"job","deps":[{"id":"`+pid+`","revision":`+rev+`,"digest":"`+digest+`"}]}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 创建没成功，带上现场停测。
		t.Fatalf("create platform project %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	projID := gjson(t, body, "id")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+projID+"/deps", tok, `{"expected":1,"deps":[{"id":"`+pid+`","revision":`+rev+`,"digest":"`+digest+`"}]}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("set project deps %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/disable", tok, `{"expected":`+rev+`}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || gjson(t, body, "status") != "disabled" {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("disable platform %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	disRev := gjson(t, body, "revision")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/enable", tok, `{"expected":`+disRev+`}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || gjson(t, body, "status") != "available" {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("enable platform %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/delete", tok, "")
	// 仍被引用或冲突却没拒绝，就停测。
	if code != http.StatusConflict || gjson(t, body, "error") != "still referenced" {
		// 删除结果不对，带上现场停测。
		t.Fatalf("delete referenced %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"可删","content":"gone"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 创建没成功，带上现场停测。
		t.Fatalf("create disposable %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	dropID := gjson(t, body, "id")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+dropID+"/copyable", tok, `{"expected":1,"copyable":true}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || !strings.Contains(body, `"copyable":true`) {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("set copyable %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+dropID+"/delete", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 删除结果不对，带上现场停测。
		t.Fatalf("delete unused %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/factories/"+fid+"/promotable-assets?kind=process", tok, "")
	// 厂不在线却没回暂不可用，就停测。
	if code != http.StatusServiceUnavailable || gjson(t, body, "error") != "factory channel is offline" {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("promotable offline %d %s", code, body)
	}
	// 算出摘要，厂端回包要对得上。
	sum := sha256.Sum256([]byte("from-fac"))
	// 把包收成文本放进 JSON，超限会被拒绝。
	snap := `{"sourceId":"` + id.New().String() + `","sourceRevision":1,"sourceFactoryId":"` + fid + `","kind":"process","name":"收厂级","content":"` + base64.StdEncoding.EncodeToString([]byte("from-fac")) + `","digest":"` + base64.StdEncoding.EncodeToString(sum[:]) + `","copyable":true,"status":"available","deps":[]}`
	code, body = do(t, srv, "POST", "/v1/assets/promote", tok, snap)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated || gjson(t, body, "status") != "draft" {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("promote snapshot %d %s", code, body)
	}
	// 没指定身份就发新号，保证一机一号。
	aid := id.New().String()
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/factories/"+fid+"/assets", tok, `{"name":"厂级","content":"x"}`)
	// 不该放行的操作没被拦住，就停测。
	if code != http.StatusForbidden {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("proxy factory asset %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/factories/"+fid+"/assets/"+aid, tok, "")
	// 不该放行的操作没被拦住，就停测。
	if code != http.StatusForbidden {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("get factory asset %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, _ = do(t, srv, "GET", "/v1/directory", "", "")
	// 该拒绝的匿名或错凭证却通过了，就停测。
	if code != http.StatusUnauthorized {
		// 匿名调用没被拒绝，带上现场停测。
		t.Fatalf("anon directory %d", code)
	}
}

// 发一条请求，带回状态和正文供断言。
func do(t *testing.T, srv *httptest.Server, method, path, token, body string) (int, string) {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 没正文就空着，有正文再装成读取器。
	var rdr io.Reader
	// 有正文才带上类型，空请求不要装成 JSON。
	if body != "" {
		// 有正文才装成读取器，空请求保持空。
		rdr = strings.NewReader(body)
	}
	// 组好请求，组失败就停测。
	req, err := http.NewRequest(method, srv.URL+path, rdr)
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 有正文才带上类型，空请求不要装成 JSON。
	if body != "" {
		// 标明 JSON，前端才按错误体解析。
		req.Header.Set("Content-Type", "application/json")
	}
	// 有会话才带上，匿名请求必须被拒绝。
	if token != "" {
		// 带上管理员会话，匿名请求会被拒绝。
		req.Header.Set("Authorization", "Bearer "+token)
	}
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
	b, err := io.ReadAll(res.Body)
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 把状态和正文交回调用它的测试。
	return res.StatusCode, string(b)
}

// 从响应里取出一个字段，坏 JSON 就停测。
func gjson(t *testing.T, body, path string) string {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 先解开成通用结构，坏 JSON 就停测。
	var m any
	// 解不开就停住，避免把坏包当成成功。
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		// 响应不是合法 JSON，带上现场停测。
		t.Fatalf("json %s: %v", body, err)
	}
	// 从根往下走，走到目标字段。
	cur := m
	// 沿字段路径往下取，中间断了就停测。
	for _, p := range strings.Split(path, ".") {
		// 这一层必须是对象，否则路径无效。
		mm, ok := cur.(map[string]any)
		// 底层不支持劫持，通道升级会失败。
		if !ok {
			// 这一层不是对象，路径读不下去。
			t.Fatalf("%s not object in %s", path, body)
		}
		// 进到下一层，缺了就停测。
		cur = mm[p]
	}
	// 文本字段直接用，不是文本再看数字。
	s, ok := cur.(string)
	// 是文本就直接用，数字再另转。
	if ok {
		return s
	}
	// 数字字段收成文本，方便和预期对比。
	if n, ok := cur.(float64); ok {
		// 数字收成文本，便于和预期对比。
		return strconv.FormatInt(int64(n), 10)
	}
	// 字段不是文本，没法按预期对比。
	t.Fatalf("%s not string in %s", path, body)
	return ""
}

// 按默认工艺字段表套用正文，供对比。
func appliedProcess(raw string) string {
	// 取出默认工艺字段表，套用失败就停。
	schema, err := contenttpl.Marshal(contenttpl.Default(contenttpl.KindProcess))
	// 失败就停住，避免把半成品当成完成。
	if err != nil {
		// 字段表套用不了就立刻失败，不能拿空结果对比。
		panic(err)
	}
	// 按字段表套用正文，结果用来对比响应。
	out, err := contenttpl.Apply(schema, []byte(raw))
	// 失败就停住，避免把半成品当成完成。
	if err != nil {
		// 字段表套用不了就立刻失败，不能拿空结果对比。
		panic(err)
	}
	// 把套用后的正文交回，供响应对比。
	return string(out)
}

// 发布厂包，匿名拒绝，认领厂可以拉到。
func TestWANSoftwareHTTP(t *testing.T) {
	// 打开测试库管理员，用来建一块独立库。
	admin := testpg.Open(t)
	// 建一块独立库，不碰别的测试数据。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_wan_sw")
	// 把库迁到当前结构，再交给服务。
	svc := service.NewService(store.Open(testpg.OpenMigrated(t, dsn)))
	// 初始管理员没建起来，后面的测试不能做。
	if err := svc.BootstrapAdmin(context.Background(), "w", "wan-secret"); err != nil {
		// 初始管理员没建起来，就停测。
		t.Fatalf("bootstrap: %v", err)
	}
	// 起临时云端，只在本测里访问。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)

	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/login", "", `{"loginName":"w","password":"wan-secret"}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 登录没成功，后面没有会话可用。
		t.Fatalf("login %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	tok := gjson(t, body, "token")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/software", "", "")
	// 该拒绝的匿名或错凭证却通过了，就停测。
	if code != http.StatusUnauthorized {
		// 匿名调用没被拒绝，带上现场停测。
		t.Fatalf("anon software %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/factories", tok, `{"name":"厂A","saLogin":"sa-a","saDisplay":"超管"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 建厂没成功，带上现场停测。
		t.Fatalf("create factory %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	fid := gjson(t, body, "factory.id")
	// 把包收成文本放进 JSON，超限会被拒绝。
	pkg := base64.StdEncoding.EncodeToString([]byte("svc-1"))
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/software", tok, `{"kind":"factory_service","version":1,"versionName":"1.0.0","content":"`+pkg+`"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated || gjson(t, body, "version") != "1" {
		// 发布没成功，带上现场停测。
		t.Fatalf("publish %d %s", code, body)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/software", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || !strings.Contains(body, `"versionName":"1.0.0"`) {
		// 列表结果不对，带上现场停测。
		t.Fatalf("list %d %s", code, body)
	}
	// 生成厂钥，私钥只留在本测。
	pub, priv, err := nodekey.Generate()
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/software/latest?kind=factory_service", "", "")
	// 该拒绝的匿名或错凭证却通过了，就停测。
	if code != http.StatusUnauthorized {
		// 匿名调用没被拒绝，带上现场停测。
		t.Fatalf("anon latest %d %s", code, body)
	}
	// 交钥没登记成功，认领就不能算完成。
	if err := svc.ConfirmEnroll(context.Background(), uuid.MustParse(fid), pub); err != nil {
		// 认领准备失败，就停测。
		t.Fatalf("enroll: %v", err)
	}
	// 用厂钥去拉，不带管理员会话。
	res := factoryReq(t, srv, http.MethodGet, "/v1/software/latest?kind=factory_service", uuid.MustParse(fid), priv)
	// 用完就关上，避免连接或文件一直占着。
	defer res.Body.Close()
	// 读完全部正文，读失败就停测。
	got, _ := io.ReadAll(res.Body)
	// 没成功就停测，避免把失败响应当成数据。
	if res.StatusCode != http.StatusOK || !strings.Contains(string(got), `"version":1`) {
		// 最高版本不对，带上现场停测。
		t.Fatalf("latest %d %s", res.StatusCode, got)
	}
}
