// HTTP 适配：本厂 Client 策略读写。
package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/secret"
	"wmesh/factory/internal/platform/testpg"
)

// 验收策略的读取权限和修订升高。
func TestFactoryClientPolicyHTTP(t *testing.T) {
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

	// 读取或修改示教器策略。
	code, body = do(t, srv, "GET", base+"/client-policy", "", "")
	// 不是未授权则本测失败。
	if code != http.StatusUnauthorized {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("anon get %d %s", code, body)
	}
	// 读取或修改示教器策略。
	code, body = do(t, srv, "GET", base+"/client-policy", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "revision") != "0" || gjson(t, body, "maxCachedProjects") != "2" ||
		gjson(t, body, "cacheScope") != "all" || !strings.Contains(body, `"encryptPouch":true`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("default %d %s", code, body)
	}

	// 读取或修改示教器策略。
	code, body = do(t, srv, "PUT", base+"/client-policy", tok, `{"maxCachedProjects":3,"cacheScope":"current","persistUnwrapKey":true,"keyTtlSeconds":60,"extra":{"k":true}}`)
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "revision") != "1" || gjson(t, body, "maxCachedProjects") != "3" ||
		gjson(t, body, "cacheScope") != "current" || !strings.Contains(body, `"encryptPouch":true`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("put %d %s", code, body)
	}

	// 读取或修改示教器策略。
	code, body = do(t, srv, "PUT", base+"/client-policy", tok, `{"maxCachedProjects":3,"cacheScope":"current","persistUnwrapKey":true,"keyTtlSeconds":60,"encryptPouch":false}`)
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, `"encryptPouch":false`) || gjson(t, body, "revision") != "2" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("encrypt off %d %s", code, body)
	}
	// 读取或修改示教器策略。
	code, body = do(t, srv, "PUT", base+"/client-policy", tok, `{"maxCachedProjects":3,"cacheScope":"current","persistUnwrapKey":true,"keyTtlSeconds":60}`)
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, `"encryptPouch":false`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("omit keeps encrypt %d %s", code, body)
	}

	// 读取或修改示教器策略。
	code, body = do(t, srv, "PUT", base+"/client-policy", tok, `{"maxCachedProjects":3,"cacheScope":"nope","persistUnwrapKey":false,"keyTtlSeconds":0}`)
	// 不是禁止则本测失败。
	if code != http.StatusForbidden {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("bad scope %d %s", code, body)
	}
	// 读取或修改示教器策略。
	code, body = do(t, srv, "GET", base+"/client-policy", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "revision") != "3" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("still %d %s", code, body)
	}

	// 办理人员，不是超管应当被拒绝。
	code, body = do(t, srv, "POST", base+"/people", tok, `{"loginName":"op1","displayName":"操作员"}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("person %d %s", code, body)
	}
	// 取出人员身份，后面用来对上连接。
	personID := gjson(t, body, "account.id")
	// 授予角色，超出作用域应当被拒绝。
	code, body = do(t, srv, "POST", base+"/grants", tok, `{"personId":"`+personID+`","role":"operator","scopeKind":"factory"}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("grant %d %s", code, body)
	}
	// 登录并取得会话，口令错误应当被拒绝。
	code, body = do(t, srv, "POST", base+"/login", "", `{"loginName":"op1","password":"`+secret.DefaultPersonPassword("op1")+`"}`)
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("op login %d %s", code, body)
	}
	// 取出会话令牌，后面的请求要带上。
	opTok := gjson(t, body, "token")
	// 读取或修改示教器策略。
	code, body = do(t, srv, "PUT", base+"/client-policy", opTok, `{"maxCachedProjects":9,"cacheScope":"all","persistUnwrapKey":false,"keyTtlSeconds":0}`)
	// 不是禁止则本测失败。
	if code != http.StatusForbidden {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("op put %d %s", code, body)
	}
	// 读取或修改示教器策略。
	code, body = do(t, srv, "GET", base+"/client-policy", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "revision") != "3" || gjson(t, body, "maxCachedProjects") != "3" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("op must not write %d %s", code, body)
	}
	// 回包内容与预期不符则本测失败。
	if strings.Contains(body, "secret") || strings.Contains(body, opTok) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("secret leaked: %s", body)
	}
}
