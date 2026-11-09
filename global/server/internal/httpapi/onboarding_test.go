// HTTP 适配：现场申请、云端审批和厂钥签名的找回入口。
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

// 带厂钥签名发一个 POST，交回状态和正文。
func signedPost(t *testing.T, srv *httptest.Server, path, body string, fid uuid.UUID, priv []byte) (int, string) {
	// 失败行号落到调用它的用例。
	t.Helper()
	// 组请求。
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
	// 组不出来就停测。
	if err != nil {
		// 这一步失败不能继续。
		t.Fatal(err)
	}
	// 用当前时刻签名。
	unix := time.Now().Unix()
	// 写上工厂身份。
	req.Header.Set("X-WMesh-Factory", fid.String())
	// 写上签名时刻。
	req.Header.Set("X-WMesh-Time", strconv.FormatInt(unix, 10))
	// 写上厂钥签名。
	req.Header.Set("X-WMesh-Sign", base64.StdEncoding.EncodeToString(nodekey.Sign(priv, nodekey.MQTTConnectPayload(fid, unix))))
	// 发出去。
	res, err := http.DefaultClient.Do(req)
	// 连不上就停测。
	if err != nil {
		// 这一步失败不能继续。
		t.Fatal(err)
	}
	// 读完关掉。
	defer res.Body.Close()
	// 读出正文。
	raw, err := io.ReadAll(res.Body)
	// 读不出就停测。
	if err != nil {
		// 这一步失败不能继续。
		t.Fatal(err)
	}
	// 交回状态和正文。
	return res.StatusCode, string(raw)
}

// 从申请、批准、认领到签名找回走一遍。
func TestOnboardingHTTP(t *testing.T) {
	// 打开测试库管理员。
	admin := testpg.Open(t)
	// 建一块独立库。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_wan")
	// 接上服务。
	svc := service.NewService(store.Open(testpg.OpenMigrated(t, dsn)))
	// 立管理员。
	if err := svc.BootstrapAdmin(context.Background(), "w", "wan-secret"); err != nil {
		// 这一步失败不能继续。
		t.Fatal(err)
	}
	// 登录拿票。
	tok, err := svc.Login(context.Background(), "w", "wan-secret")
	// 这一步失败不能继续。
	if err != nil {
		// 这一步失败不能继续。
		t.Fatal(err)
	}
	// 起临时云端。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉。
	t.Cleanup(srv.Close)

	// 匿名提交申请。
	code, body := do(t, srv, "POST", "/v1/channel/onboarding", "", `{"factoryName":"现场厂","saLogin":"boss","saDisplay":"老板","serviceVersionName":"1.1.37"}`)
	// 必须创建成功。
	if code != http.StatusCreated {
		// 申请结果不对就停测。
		t.Fatalf("apply %d %s", code, body)
	}
	// 记下密钥和申请身份。
	secret, reqID := gjson(t, body, "secret"), gjson(t, body, "requestId")
	// 查进度。
	code, body = do(t, srv, "POST", "/v1/channel/onboarding/status", "", `{"secret":"`+secret+`"}`)
	// 应该还在等批准。
	if code != http.StatusOK || gjson(t, body, "status") != "pending" {
		// 进度不对就停测。
		t.Fatalf("status %d %s", code, body)
	}
	// 匿名不能读待接入列表。
	if code, body = do(t, srv, "GET", "/v1/onboarding", "", ""); code != http.StatusUnauthorized {
		// 没拦住就停测。
		t.Fatalf("anon list %d %s", code, body)
	}
	// 匿名不能批准。
	if code, body = do(t, srv, "POST", "/v1/onboarding/"+reqID+"/approve", "", `{}`); code != http.StatusUnauthorized {
		// 没拦住就停测。
		t.Fatalf("anon approve %d %s", code, body)
	}
	// 管理员批准。
	if code, body = do(t, srv, "POST", "/v1/onboarding/"+reqID+"/approve", tok, `{}`); code != http.StatusOK {
		// 批准失败就停测。
		t.Fatalf("approve %d %s", code, body)
	}
	// 再批一次回冲突。
	if code, body = do(t, srv, "POST", "/v1/onboarding/"+reqID+"/approve", tok, `{}`); code != http.StatusConflict {
		// 状态码不对就停测。
		t.Fatalf("approve twice %d %s", code, body)
	}
	// 申请密钥走现有 enroll。
	code, body = do(t, srv, "POST", "/v1/channel/enroll", "", `{"enrollmentCode":"`+secret+`"}`)
	// 必须换到身份。
	if code != http.StatusOK || gjson(t, body, "saLogin") != "boss" {
		// 换身份失败就停测。
		t.Fatalf("enroll %d %s", code, body)
	}
	// 解析工厂身份。
	fid := uuid.MustParse(gjson(t, body, "factoryId"))
	// 生成厂钥。
	pub, priv, err := nodekey.Generate()
	// 这一步失败不能继续。
	if err != nil {
		// 这一步失败不能继续。
		t.Fatal(err)
	}
	// 走现有 ConfirmEnroll 登记公钥。
	if err := svc.ConfirmEnroll(context.Background(), fid, pub); err != nil {
		// 这一步失败不能继续。
		t.Fatal(err)
	}
	// 没签名不能申请找回。
	if code, body = do(t, srv, "POST", "/v1/channel/sa-reset", "", `{}`); code != http.StatusUnauthorized {
		// 没拦住就停测。
		t.Fatalf("unsigned reset %d %s", code, body)
	}
	// 另一把钥不能冒充这家厂。
	_, wrong, err := nodekey.Generate()
	// 这一步失败不能继续。
	if err != nil {
		// 这一步失败不能继续。
		t.Fatal(err)
	}
	// 用错误的钥签名。
	if code, body = signedPost(t, srv, "/v1/channel/sa-reset", `{}`, fid, wrong); code != http.StatusUnauthorized {
		// 没拦住就停测。
		t.Fatalf("wrong key reset %d %s", code, body)
	}
	// 用本厂钥申请找回。
	code, body = signedPost(t, srv, "/v1/channel/sa-reset", `{"serviceVersionName":"1.1.37"}`, fid, priv)
	// 必须创建成功。
	if code != http.StatusCreated {
		// 申请失败就停测。
		t.Fatalf("reset %d %s", code, body)
	}
	// 记下找回单。
	resetSecret, resetID := gjson(t, body, "secret"), gjson(t, body, "requestId")
	// 管理员批准找回。
	if code, body = do(t, srv, "POST", "/v1/onboarding/"+resetID+"/approve", tok, `{}`); code != http.StatusOK {
		// 批准失败就停测。
		t.Fatalf("approve reset %d %s", code, body)
	}
	// 用掉找回单。
	if code, body = signedPost(t, srv, "/v1/channel/sa-reset/consume", `{"secret":"`+resetSecret+`"}`, fid, priv); code != http.StatusNoContent {
		// 用单失败就停测。
		t.Fatalf("consume %d %s", code, body)
	}
	// 第二次回无效。
	if code, body = signedPost(t, srv, "/v1/channel/sa-reset/consume", `{"secret":"`+resetSecret+`"}`, fid, priv); code != http.StatusUnauthorized {
		// 重复用单没拦住就停测。
		t.Fatalf("consume twice %d %s", code, body)
	}
}
