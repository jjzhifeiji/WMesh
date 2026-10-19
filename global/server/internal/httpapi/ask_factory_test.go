// HTTP 适配：问厂升档和认领回读都经服务，不在 HTTP 层直接读库。
package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 停用、注销或不存在的厂，问升档列表、目录和升档都由服务按状态拦下。
func TestAskFactoryStatusGate(t *testing.T) {
	// 准备已认领厂和管理员会话，不挂通道总线。
	svc, tok, fid, _ := enrolledFactory(t)
	// 起临时云端，只在本测里访问。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)

	// 在营但没连通道，三处都该回厂不在线。
	askFactoryAll(t, srv, tok, fid, http.StatusServiceUnavailable, "factory channel is offline")
	// 停用这家厂，用来断言停用厂不再被问。
	if _, err := svc.DisableFactory(context.Background(), tok, fid); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 停用的厂三处都该回已停用，不能当成不在线。
	askFactoryAll(t, srv, tok, fid, http.StatusForbidden, "factory is disabled")
	// 注销这家厂，已认领的只改成注销。
	if _, err := svc.DeleteFactory(context.Background(), tok, fid); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 注销的厂三处都该回已注销。
	askFactoryAll(t, srv, tok, fid, http.StatusForbidden, "factory is retired")
	// 名录里没有的厂三处都该回找不到。
	askFactoryAll(t, srv, tok, uuid.New(), http.StatusNotFound, "not found")
}

// 认领应答里的状态、治理修订和短码，与库里厂档一致。
func TestChannelClaimEchoesFactory(t *testing.T) {
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
	// 建一家厂，后面用建厂码认领。
	created, err := svc.CreateFactory(context.Background(), tok, "厂A", "sa-a", "超管A")
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 先停用一次，让治理修订高于初始值。
	if _, err := svc.DisableFactory(context.Background(), tok, created.Factory.ID); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 再启用，才能继续认领。
	if _, err := svc.EnableFactory(context.Background(), tok, created.Factory.ID); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 起临时云端，只在本测里访问。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)
	// 生成厂钥，私钥只留在本测。
	pub, _, err := nodekey.Generate()
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 把建厂码和厂钥收成认领请求。
	claimBody, err := json.Marshal(map[string]any{"enrollmentCode": created.EnrollmentToken, "factoryPublicKey": pub})
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 发出认领请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/channel/claim", "", string(claimBody))
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 交钥认领结果不对，带上现场停测。
		t.Fatalf("claim %d %s", code, body)
	}
	// 读库里的厂档当对照，测试夹具可以直读。
	fac, err := svc.Store().FactoryByID(context.Background(), created.Factory.ID)
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 修订没升或短码为空，对照就没意义。
	if fac.LifecycleRevision < 2 || fac.ShortCode == "" {
		// 前置没造好，带上现场停测。
		t.Fatalf("precondition revision=%d shortCode=%q", fac.LifecycleRevision, fac.ShortCode)
	}
	// 应答三项和库里任一项不一致，就停测。
	if gjson(t, body, "status") != fac.Status || gjson(t, body, "revision") != strconv.FormatInt(fac.LifecycleRevision, 10) || gjson(t, body, "factoryShortCode") != fac.ShortCode {
		// 认领应答和库里对不上，带上现场停测。
		t.Fatalf("claim %s, want status=%s revision=%d shortCode=%s", body, fac.Status, fac.LifecycleRevision, fac.ShortCode)
	}
}

// 依次问升档列表、厂目录和从厂升档，三处都该回同一个码和原因。
func askFactoryAll(t *testing.T, srv *httptest.Server, tok string, fid uuid.UUID, wantCode int, wantErr string) {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 问升档列表并记下状态，对不上就停测。
	code, body := do(t, srv, "GET", "/v1/factories/"+fid.String()+"/promotable-assets?kind=process", tok, "")
	// 码或原因不对，说明没按厂状态拦下。
	if code != wantCode || gjson(t, body, "error") != wantErr {
		// 升档列表结果不对，带上现场停测。
		t.Fatalf("promotable %d %s, want %d %s", code, body, wantCode, wantErr)
	}
	// 问厂目录并记下状态，对不上就停测。
	code, body = do(t, srv, "GET", "/v1/factories/"+fid.String()+"/fs?kind=process", tok, "")
	// 码或原因不对，说明没按厂状态拦下。
	if code != wantCode || gjson(t, body, "error") != wantErr {
		// 厂目录结果不对，带上现场停测。
		t.Fatalf("factory fs %d %s, want %d %s", code, body, wantCode, wantErr)
	}
	// 从该厂升档并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/promote-from", tok, `{"factoryId":"`+fid.String()+`","assetId":"`+uuid.NewString()+`"}`)
	// 码或原因不对，说明没按厂状态拦下。
	if code != wantCode || gjson(t, body, "error") != wantErr {
		// 从厂升档结果不对，带上现场停测。
		t.Fatalf("promote-from %d %s, want %d %s", code, body, wantCode, wantErr)
	}
}
