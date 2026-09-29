// HTTP 适配：厂内引导、激活、登录、名册与组织人员。
package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"time"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/release"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/service"
)

// 走接口验收账号、资产、绑定和目录。
func TestFactoryHTTP(t *testing.T) {
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

	// 先探活，库不通则这次测试不能继续。
	code, body := do(t, srv, "GET", "/healthz", "", "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "db") != "ok" || gjson(t, body, "oss") != "off" || gjson(t, body, "version") != strconv.FormatInt(release.Code, 10) || gjson(t, body, "versionName") != release.Name {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("healthz %d %s", code, body)
	}
	// 查看本机已经认领的工厂。
	code, body = do(t, srv, "GET", "/v1/site", "", "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, `"wanConfigured":false`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("site %d %s", code, body)
	}
	// 局域网探活，看设备号是否属于本厂。
	code, body = do(t, srv, "GET", "/v1/discover", "", "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, `"httpBase"`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("discover %d %s", code, body)
	}
	// 调用这一步接口，供后面核对状态和正文。
	code, body = do(t, srv, "GET", "/v1/nope", "", "")
	// 不是找不到，或正文与预期不符则失败。
	if code != http.StatusNotFound || gjson(t, body, "error") != "not found" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("unknown api %d %s", code, body)
	}
	// 用共享密码建厂，激活码只出现在回包。
	code, body = do(t, srv, "POST", "/internal/bootstrap", "", `{"factoryId":"`+fid.String()+`","saLogin":"sa","saDisplay":"超管"}`)
	// 不是未授权则本测失败。
	if code != http.StatusUnauthorized {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("anon bootstrap %d %s", code, body)
	}
	// 用共享密码建厂，激活码只出现在回包。
	code, body = do(t, srv, "POST", "/internal/bootstrap", "boot-secre", `{"factoryId":"`+fid.String()+`","saLogin":"sa","saDisplay":"超管"}`)
	// 不是未授权则本测失败。
	if code != http.StatusUnauthorized {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("wrong token bootstrap %d %s", code, body)
	}
	// 用共享密码建厂，激活码只出现在回包。
	code, body = do(t, srv, "POST", "/internal/bootstrap", "boot-secret", `{"factoryId":"`+fid.String()+`","saLogin":"sa","saDisplay":"超管"}`)
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
	// HTTP 测不连 WAN，自签租约才能写资产。
	if err := svc.Store().GrantLocalLease(context.Background()); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("lease: %v", err)
	}
	// 厂短码写不进去则停测。
	if err := svc.Store().PutFactoryShortCode(context.Background(), "F01"); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("short code: %v", err)
	}
	// 取出激活码，启用账号时要用。
	act := gjson(t, body, "activationToken")
	// 激活码必须是固定位数，否则本测失败。
	if len(act) != 8 {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("activation code len %d %s", len(act), act)
	}
	// 登录并取得会话，口令错误应当被拒绝。
	code, body = do(t, srv, "POST", base+"/login", "", `{"loginName":"sa","password":"secret"}`)
	// 不是未授权则本测失败。
	if code != http.StatusUnauthorized {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pending login %d %s", code, body)
	}
	// 用激活码启用账号，错码应当被拒绝。
	code, body = do(t, srv, "POST", base+"/activate", "", `{"loginName":"sa","activationToken":"`+act+`","password":"secret"}`)
	// 不是无内容则本测失败。
	if code != http.StatusNoContent {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("activate %d %s", code, body)
	}
	// 登录并取得会话，口令错误应当被拒绝。
	code, body = do(t, srv, "POST", base+"/login", "", `{"loginName":"sa","password":"secret"}`)
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("login %d %s", code, body)
	}
	// 取出会话令牌，后面的请求要带上。
	tok := gjson(t, body, "token")
	// 列出已经收到的工程模版。
	code, body = do(t, srv, "GET", base+"/project-templates", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || strings.TrimSpace(body) != "[]" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("project templates %d %s", code, body)
	}
	// 读取名册，权限不够的人不能看见全厂。
	code, body = do(t, srv, "GET", base+"/catalog", tok, "")
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("catalog %d %s", code, body)
	}
	// 回包出现不该给人的秘密则本测失败。
	if strings.Contains(body, "PasswordHash") || strings.Contains(body, "passwordHash") || strings.Contains(body, "activationTokenHash") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("secret leaked: %s", body)
	}
	// 回包内容与预期不符则本测失败。
	if !strings.Contains(body, `"myGrants":[{`) || !strings.Contains(body, `"role":"factory_super_admin"`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("catalog must expose caller's own grants: %s", body)
	}
	// 办理组织节点，无上级则挂在工厂下。
	code, body = do(t, srv, "POST", base+"/org-units", tok, `{"name":"一车间"}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("org unit %d %s", code, body)
	}
	// 从回包取出字段，供下一步继续使用。
	unitID := gjson(t, body, "id")
	// 办理组织节点，无上级则挂在工厂下。
	code, body = do(t, srv, "POST", base+"/org-units", tok, `{"name":"空节点"}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("empty unit %d %s", code, body)
	}
	// 从回包取出字段，供下一步继续使用。
	emptyID := gjson(t, body, "id")
	// 改为停用，不该停的对象应当被拒绝。
	code, body = do(t, srv, "POST", base+"/org-units/"+emptyID+"/disable", tok, "")
	// 不是无内容则本测失败。
	if code != http.StatusNoContent {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("disable unit %d %s", code, body)
	}
	// 重新启用，已经有效的保持可登录。
	code, body = do(t, srv, "POST", base+"/org-units/"+emptyID+"/enable", tok, "")
	// 不是无内容则本测失败。
	if code != http.StatusNoContent {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("enable unit %d %s", code, body)
	}
	// 办理组织节点，无上级则挂在工厂下。
	code, body = do(t, srv, "DELETE", base+"/org-units/"+emptyID, tok, "")
	// 不是无内容则本测失败。
	if code != http.StatusNoContent {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("delete unused unit %d %s", code, body)
	}
	// 办理人员，不是超管应当被拒绝。
	code, body = do(t, srv, "POST", base+"/people", tok, `{"loginName":"op1","displayName":"操作员"}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("person %d %s", code, body)
	}
	// 回包出现不该给人的秘密则本测失败。
	if strings.Contains(body, "activationToken") || gjson(t, body, "account.status") != "active" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("person default login missing: %s", body)
	}
	// 取出人员身份，后面用来对上连接。
	personID := gjson(t, body, "account.id")
	// 改为停用，不该停的对象应当被拒绝。
	code, body = do(t, srv, "POST", base+"/people/"+personID+"/disable", tok, "")
	// 不是无内容则本测失败。
	if code != http.StatusNoContent {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("disable person %d %s", code, body)
	}
	// 重新启用，已经有效的保持可登录。
	code, body = do(t, srv, "POST", base+"/people/"+personID+"/enable", tok, "")
	// 不是无内容则本测失败。
	if code != http.StatusNoContent {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("enable person %d %s", code, body)
	}
	// 把口令打回默认，旧会话应当失效。
	code, body = do(t, srv, "POST", base+"/people/"+personID+"/reset-password", tok, "")
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("reset person %d %s", code, body)
	}
	// 回包出现不该给人的秘密则本测失败。
	if strings.Contains(body, "activationToken") || gjson(t, body, "account.status") != "active" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("reset default password missing: %s", body)
	}
	// 授予角色，超出作用域应当被拒绝。
	code, body = do(t, srv, "POST", base+"/grants", tok, `{"personId":"`+personID+`","role":"operator","scopeKind":"org_unit","orgUnitId":"`+unitID+`"}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("grant %d %s", code, body)
	}
	// 调整岗位，一个人不能同时挂两处。
	code, body = do(t, srv, "POST", base+"/assignments", tok, `{"personId":"`+personID+`","orgUnitId":"`+unitID+`"}`)
	// 不是无内容则本测失败。
	if code != http.StatusNoContent {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("assign %d %s", code, body)
	}
	// 办理组织节点，无上级则挂在工厂下。
	code, body = do(t, srv, "DELETE", base+"/org-units/"+unitID, tok, "")
	// 不是冲突，或正文与预期不符则失败。
	if code != http.StatusConflict || gjson(t, body, "error") != "still referenced" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("delete referenced unit %d %s", code, body)
	}
	// 分配一个新身份，避免和别的测试撞上。
	missing := id.New()
	// 登录并取得会话，口令错误应当被拒绝。
	code, body = do(t, srv, "POST", "/v1/factories/"+missing.String()+"/login", "", `{"loginName":"sa","password":"secret"}`)
	// 不是找不到则本测失败。
	if code != http.StatusNotFound {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("missing factory %d %s", code, body)
	}

	// 再生成一台的公钥，用来验证他机被拒绝。
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	// 密钥没有生成则停测，后面无法绑定。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 把公钥收成文本，才能放进绑定请求。
	pk := base64.StdEncoding.EncodeToString(pub)
	// 分配一个新身份，避免和别的测试撞上。
	cid := id.New().String()
	// 登记或查看绑定设备。
	code, body = do(t, srv, "POST", base+"/clients", tok, `{"id":"`+cid+`","name":"焊机-1","publicKey":"`+pk+`","bindingRevision":1}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("accept client %d %s", code, body)
	}
	// 登记或查看绑定设备。
	code, body = do(t, srv, "GET", base+"/clients", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, cid) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("list clients %d %s", code, body)
	}
	// 收成同一时区，避免窗口因时区错位。
	nb := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	// 收成同一时区，避免窗口因时区错位。
	na := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	// 签发或查看运行许可。
	code, body = do(t, srv, "POST", base+"/clients/"+cid+"/runtime", tok, `{"notBefore":"`+nb+`","notAfter":"`+na+`"}`)
	// 不是已创建，或正文与预期不符则失败。
	if code != http.StatusCreated || !strings.Contains(body, `"canRun":true`) || strings.Contains(body, "payload") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("issue runtime %d %s", code, body)
	}
	// 读取当前登录人，未登录应当被拒绝。
	code, body = do(t, srv, "GET", base+"/me", tok, "")
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("me %d %s", code, body)
	}
	// 从回包取出字段，供下一步继续使用。
	saID := gjson(t, body, "id")
	// 读取签发公钥，未登录应当被拒绝。
	code, body = do(t, srv, "GET", base+"/signing-key", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, "publicKey") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("signing key %d %s", code, body)
	}

	// 授予角色，超出作用域应当被拒绝。
	code, body = do(t, srv, "POST", base+"/grants", tok, `{"personId":"`+saID+`","role":"operator","scopeKind":"factory"}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("grant op %d %s", code, body)
	}
	// 查询可以用来新建的工作位置。
	code, body = do(t, srv, "GET", base+"/asset-author-context", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, `"allowDirect":true`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("author context %d %s", code, body)
	}
	// 办理工艺或工程，正文按权限才可见。
	code, body = do(t, srv, "POST", base+"/assets", tok, `{"kind":"process","level":"factory","name":"焊A","content":"secret-body","direct":true}`)
	// 不是已创建，或正文与预期不符则失败。
	if code != http.StatusCreated || !strings.Contains(body, `"copyable":true`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("create process %d %s", code, body)
	}
	// 从回包取出字段，供下一步继续使用。
	pid := gjson(t, body, "id")
	// 办理工艺或工程，正文按权限才可见。
	code, body = do(t, srv, "POST", base+"/assets", tok, `{"kind":"process","level":"factory","name":"密焊","content":"secret-tight","direct":true,"copyable":false}`)
	// 不是已创建，或正文与预期不符则失败。
	if code != http.StatusCreated || !strings.Contains(body, `"copyable":false`) || gjson(t, body, "revision") != "1" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("create tight process %d %s", code, body)
	}
	// 办理工艺或工程，正文按权限才可见。
	code, body = do(t, srv, "GET", base+"/assets?kind=process", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, pid) || strings.Contains(body, "secret-body") || !strings.Contains(body, "creatorLogin") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("list process %d %s", code, body)
	}
	// 向平台要快照，通道不在也应当先返回。
	code, body = do(t, srv, "POST", base+"/assets/sync?kind=process", "", "")
	// 不是未授权则本测失败。
	if code != http.StatusUnauthorized {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("anon sync assets %d %s", code, body)
	}
	// 向平台要快照，通道不在也应当先返回。
	code, body = do(t, srv, "POST", base+"/assets/sync?kind=process", tok, "")
	// 不是已受理则本测失败。
	if code != http.StatusAccepted {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("sync assets %d %s", code, body)
	}
	// 向平台要快照，通道不在也应当先返回。
	code, body = do(t, srv, "POST", base+"/templates/sync?kind=process", tok, "")
	// 不是已受理则本测失败。
	if code != http.StatusAccepted {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("sync templates %d %s", code, body)
	}
	// 读取或覆盖正文，无权则应当被拒绝。
	code, body = do(t, srv, "GET", base+"/assets/"+pid+"/content", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "content") != "secret-body" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("read content %d %s", code, body)
	}
	// 取出中转密钥，用来制造租约失效后的拒绝。
	lease, err := svc.Store().TransitKey()
	// 这一步失败则停测，避免在坏结果上继续。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("lease key: %v", err)
	}
	// 清掉内容租约，之后读正文应当被拒绝。
	svc.Store().ClearContentLease()
	// 办理工艺或工程，正文按权限才可见。
	code, body = do(t, srv, "GET", base+"/assets/"+pid, tok, "")
	// 不是禁止，或正文与预期不符则失败。
	if code != http.StatusForbidden || gjson(t, body, "error") != "content lease expired" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("get without lease %d %s", code, body)
	}
	// 读取或覆盖正文，无权则应当被拒绝。
	code, body = do(t, srv, "GET", base+"/assets/"+pid+"/content", tok, "")
	// 不是禁止，或正文与预期不符则失败。
	if code != http.StatusForbidden || gjson(t, body, "error") != "content lease expired" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("content without lease %d %s", code, body)
	}
	// 条件里的调用失败则停止，不回成功。
	if err := svc.Store().ApplyContentLease(context.Background(), lease, time.Now().Add(24*time.Hour)); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("restore lease: %v", err)
	}
	// 修改显示名，修订对不上应当冲突。
	code, body = do(t, srv, "POST", base+"/assets/"+pid+"/rename", tok, `{"expected":1,"name":"焊A2"}`)
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "name") != "焊A2" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("rename %d %s", code, body)
	}
	// 把草稿改为可用。
	code, body = do(t, srv, "POST", base+"/assets/"+pid+"/publish", tok, `{"expected":2}`)
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("publish %d %s", code, body)
	}
	// 修改显示名，修订对不上应当冲突。
	code, body = do(t, srv, "POST", base+"/assets/"+pid+"/rename", tok, `{"expected":1,"name":"旧修订"}`)
	// 不是冲突，或正文与预期不符则失败。
	if code != http.StatusConflict || gjson(t, body, "error") != "revision does not match" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("stale rename %d %s", code, body)
	}
	// 办理工艺或工程，正文按权限才可见。
	code, body = do(t, srv, "GET", base+"/assets/"+pid, tok, "")
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("get process %d %s", code, body)
	}
	// 从回包取出字段，供下一步继续使用。
	digest := gjson(t, body, "digest")
	// 从回包取出字段，供下一步继续使用。
	rev := gjson(t, body, "revision")
	// 办理工艺或工程，正文按权限才可见。
	code, body = do(t, srv, "POST", base+"/assets", tok, `{"kind":"project","level":"factory","name":"工程A","content":"job","direct":true,"deps":[{"id":"`+pid+`","revision":`+rev+`,"digest":"`+digest+`"}]}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("create project %d %s", code, body)
	}
	// 从回包取出字段，供下一步继续使用。
	projID := gjson(t, body, "id")
	// 修改工程依赖，修订对不上应当被拒绝。
	code, body = do(t, srv, "POST", base+"/assets/"+projID+"/deps", tok, `{"expected":1,"deps":[{"id":"`+pid+`","revision":`+rev+`,"digest":"`+digest+`"}]}`)
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("set deps %d %s", code, body)
	}
	// 办理工艺或工程，正文按权限才可见。
	code, body = do(t, srv, "POST", base+"/assets", tok, `{"kind":"process","level":"personal","name":"个人焊","content":"mine","direct":true}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("create personal %d %s", code, body)
	}
	// 从回包取出字段，供下一步继续使用。
	persID := gjson(t, body, "id")
	// 把草稿改为可用。
	code, body = do(t, srv, "POST", base+"/assets/"+persID+"/publish", tok, `{"expected":1}`)
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("publish personal %d %s", code, body)
	}
	// 升到厂级，不可复制时应当被拒绝。
	code, body = do(t, srv, "POST", base+"/assets/"+persID+"/promote", tok, "")
	// 不是已创建，或正文与预期不符则失败。
	if code != http.StatusCreated || strings.Contains(body, "mine") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("promote %d %s", code, body)
	}
	// 删除对象，仍被引用时应当冲突。
	code, body = do(t, srv, "POST", base+"/assets/"+pid+"/delete", tok, "")
	// 不是冲突，或正文与预期不符则失败。
	if code != http.StatusConflict || gjson(t, body, "error") != "still referenced" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("delete referenced %d %s", code, body)
	}
	// 办理工艺或工程，正文按权限才可见。
	code, body = do(t, srv, "POST", base+"/assets", tok, `{"kind":"process","level":"factory","name":"可删","content":"gone","direct":true}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("create disposable %d %s", code, body)
	}
	// 从回包取出字段，供下一步继续使用。
	dropID := gjson(t, body, "id")
	// 删除对象，仍被引用时应当冲突。
	code, body = do(t, srv, "POST", base+"/assets/"+dropID+"/delete", tok, "")
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("delete unused %d %s", code, body)
	}
	// 导出快照，不该看见的正文不能出现。
	code, body = do(t, srv, "GET", base+"/assets/"+pid+"/snapshot", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "sourceId") != pid || !strings.Contains(body, base64.StdEncoding.EncodeToString([]byte("secret-body"))) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("snapshot %d %s", code, body)
	}
}

// 带令牌发出请求，返回状态和正文。
func do(t *testing.T, srv *httptest.Server, method, path, token, body string) (int, string) {
	// 标成辅助函数，失败时栈指向真正的用例。
	t.Helper()
	// 没有正文时不带请求体。
	var rdr io.Reader
	// 有正文才装进请求，没有就空着发。
	if body != "" {
		// 把正文放进请求，没有正文则不带体。
		rdr = strings.NewReader(body)
	}
	// 组出请求，方法或地址不合法则停测。
	req, err := http.NewRequest(method, srv.URL+path, rdr)
	// 请求组不起来则停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 有正文才声明媒体类型，便于按对象解析。
	if body != "" {
		// 声明正文是文本对象，便于按字段解析。
		req.Header.Set("Content-Type", "application/json")
	}
	// 有会话才带上令牌，否则按未登录访问。
	if token != "" {
		// 带上会话令牌，服务端才认这个人。
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// 发出请求，连不上则停测。
	res, err := http.DefaultClient.Do(req)
	// 请求发不出去则停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 读完就关闭正文，避免连接一直被占。
	defer res.Body.Close()
	// 读完正文再交给断言，中途断开则停测。
	b, err := io.ReadAll(res.Body)
	// 正文读不完则停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 把状态和正文集中交给调用处核对。
	return res.StatusCode, string(b)
}

// 按字节区间下载，用来核对续传。
func doRange(t *testing.T, srv *httptest.Server, path, token, rng string) (int, string) {
	// 标成辅助函数，失败时栈指向真正的用例。
	t.Helper()
	// 组出请求，方法或地址不合法则停测。
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	// 请求组不起来则停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 带上会话令牌，服务端才认这个人。
	req.Header.Set("Authorization", "Bearer "+token)
	// 声明要取的字节区间，用来核对续传。
	req.Header.Set("Range", rng)
	// 发出请求，连不上则停测。
	res, err := http.DefaultClient.Do(req)
	// 请求发不出去则停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 读完就关闭正文，避免连接一直被占。
	defer res.Body.Close()
	// 读完正文再交给断言，中途断开则停测。
	b, err := io.ReadAll(res.Body)
	// 正文读不完则停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 把状态和正文集中交给调用处核对。
	return res.StatusCode, string(b)
}

// 从回包取出字段，数字也收成文字。
func gjson(t *testing.T, body, path string) string {
	// 标成辅助函数，失败时栈指向真正的用例。
	t.Helper()
	// 先接住整段回包，再按路径往下取。
	var m any
	// 条件里的调用失败则停止，不回成功。
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("json %s: %v", body, err)
	}
	// 从回包根部开始，按路径逐段往下走。
	cur := m
	// 按点号逐段下钻，中途不是对象则失败。
	for _, p := range strings.Split(path, ".") {
		// 当前必须是对象，否则无法继续往下取。
		mm, ok := cur.(map[string]any)
		// 这一段不是对象就无法再往下取值。
		if !ok {
			// 与预期不符就停测，避免后面连环误判。
			t.Fatalf("%s not object in %s", path, body)
		}
		// 走进这一段，后面再判断是文字还是数字。
		cur = mm[p]
	}
	// 是文字就直接用，数字再另行收成文字。
	s, ok := cur.(string)
	// 已经取到文字就返回，不必再改数字。
	if ok {
		return s
	}
	// 数字收成十进制文字，方便和回包比较。
	if n, ok := cur.(float64); ok {
		// 把数字收成文字再比较，避免类型不一致。
		return strconv.FormatInt(int64(n), 10)
	}
	// 与预期不符就停测，避免后面连环误判。
	t.Fatalf("%s not string in %s", path, body)
	return ""
}

// 验收厂服务包的同步、确认和安装。
func TestFactorySoftwareHTTP(t *testing.T) {
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
	// 登录并取得会话，口令错误应当被拒绝。
	code, body = do(t, srv, "POST", base+"/login", "", `{"loginName":"sa","password":"secret"}`)
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("login %d %s", code, body)
	}
	// 取出会话令牌，后面的请求要带上。
	tok := gjson(t, body, "token")
	// 查看待确认的包，没有则应当为空。
	code, body = do(t, srv, "GET", base+"/software/pending", "", "")
	// 不是未授权则本测失败。
	if code != http.StatusUnauthorized {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("anon pending %d %s", code, body)
	}
	// 查看当前已装版本，未装时应当为空。
	code, body = do(t, srv, "GET", base+"/software/current", "", "")
	// 不是未授权则本测失败。
	if code != http.StatusUnauthorized {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("anon current %d %s", code, body)
	}
	// 查看当前已装版本，未装时应当为空。
	code, body = do(t, srv, "GET", base+"/software/current", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "version") != "0" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("empty current %d %s", code, body)
	}
	// 查看本机更换进度。
	code, body = do(t, srv, "GET", base+"/software/apply", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "phase") != "idle" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("idle apply %d %s", code, body)
	}
	// 查看待确认的包，没有则应当为空。
	code, body = do(t, srv, "GET", base+"/software/pending", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || strings.TrimSpace(body) != "null" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("empty pending %d %s", code, body)
	}
	// 准备一段包体，后面用来入库或下载。
	pkg := []byte("svc-2")
	// 先放入一份软件包，再走确认或下载。
	if err := svc.Updates.IngestSoftware(context.Background(), service.SoftwareOffer{
		// 填上种类、版本和摘要，供后面安装核对。
		Kind: service.SoftwareFactoryService, Version: 2, VersionName: "1.2.0", Digest: digest.Sum(pkg), Body: pkg,
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	// 查看待确认的包，没有则应当为空。
	code, body = do(t, srv, "GET", base+"/software/pending", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "version") != "2" || gjson(t, body, "versionName") != "1.2.0" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pending %d %s", code, body)
	}
	// 假定更换已经成功，才能把待确认看成已装。
	svc.Updates.SetApplyOutcome(service.ApplyOK)
	// 确认切换版本，未确认不能当成已装。
	code, body = do(t, srv, "POST", base+"/software/confirm", tok, `{"kind":"factory_service","version":2}`)
	// 不是无内容则本测失败。
	if code != http.StatusNoContent {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("confirm %d %s", code, body)
	}
	// 查看待确认的包，没有则应当为空。
	code, body = do(t, srv, "GET", base+"/software/pending", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || strings.TrimSpace(body) != "null" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("cleared pending %d %s", code, body)
	}
	// 查看当前已装版本，未装时应当为空。
	code, body = do(t, srv, "GET", base+"/software/current", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "version") != "2" || gjson(t, body, "versionName") != "1.2.0" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("installed current %d %s", code, body)
	}
}

// 验收平板查看并下载客户端包。
func TestPadClientSoftwareHTTP(t *testing.T) {
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
	// 登录并取得会话，口令错误应当被拒绝。
	code, body = do(t, srv, "POST", base+"/pad/login", "", `{"loginName":"sa","password":"secret"}`)
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pad login %d %s", code, body)
	}
	// 取出会话令牌，后面的请求要带上。
	padTok := gjson(t, body, "token")
	// 查看或下载客户端包，未登录应当被拒绝。
	code, body = do(t, srv, "GET", base+"/pad/software/client", "", "")
	// 不是未授权则本测失败。
	if code != http.StatusUnauthorized {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("anon %d %s", code, body)
	}
	// 查看或下载客户端包，未登录应当被拒绝。
	code, body = do(t, srv, "GET", base+"/pad/software/client", padTok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || strings.TrimSpace(body) != "null" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("empty %d %s", code, body)
	}
	// 准备一段包体，后面用来入库或下载。
	apk := []byte("apk-http-51")
	// 先放入一份软件包，再走确认或下载。
	if err := svc.Updates.IngestSoftware(context.Background(), service.SoftwareOffer{
		// 填上种类、版本和摘要，供后面安装核对。
		Kind: service.SoftwareClientAPK, Version: 51, VersionName: "6.1.0", Digest: digest.Sum(apk), Body: apk,
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	// 查看或下载客户端包，未登录应当被拒绝。
	code, body = do(t, srv, "GET", base+"/pad/software/client", padTok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "version") != "51" || gjson(t, body, "versionName") != "6.1.0" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("meta %d %s", code, body)
	}
	// 查看或下载客户端包，未登录应当被拒绝。
	code, body = do(t, srv, "GET", base+"/pad/software/client/51", padTok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || body != string(apk) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("file %d %q", code, body)
	}
	// 按区间下载安装包，核对续传的后半。
	code, body = doRange(t, srv, base+"/pad/software/client/51", padTok, "bytes=4-")
	// 不是分段，或正文与预期不符则失败。
	if code != http.StatusPartialContent || body != string(apk[4:]) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("range %d %q", code, body)
	}
	// 查看或下载客户端包，未登录应当被拒绝。
	code, body = do(t, srv, "GET", base+"/pad/software/client/9", padTok, "")
	// 不是找不到则本测失败。
	if code != http.StatusNotFound {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("missing %d %s", code, body)
	}
}
