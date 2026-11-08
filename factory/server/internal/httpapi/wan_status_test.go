package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
)

// 通道现状要登录才能看；没连过 WAN 时报断开，不带最近连上和租约到期。
func TestWANStatusHTTP(t *testing.T) {
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
	srv := httptest.NewServer(httpapi.New(h, "").Router())
	// 测完关闭临时服务，把端口让出来。
	t.Cleanup(srv.Close)
	// 拼出本厂前缀，后面的路径都接在后面。
	base := "/v1/factories/" + fid.String()
	// 按约定身份建厂，初始超管当场有效。
	svc, _ := enrollSA(t, h, fid)
	// 未登录看通道现状应被拒。
	code, body := do(t, srv, "GET", base+"/wan-status", "", "")
	// 匿名放行就停测。
	if code != http.StatusUnauthorized {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("anon wan-status %d %s", code, body)
	}
	// 登录拿会话，后面带上它看现状。
	tok := loginWeb(t, srv, base, "sa", "secret")
	// 登录后看通道现状。
	code, body = do(t, srv, "GET", base+"/wan-status", tok, "")
	// 没连过 WAN：应报未配置、断开，不带最近连上和租约到期。
	if code != http.StatusOK || !strings.Contains(body, `"configured":false`) || !strings.Contains(body, `"connected":false`) || strings.Contains(body, "lastConnectedAt") || strings.Contains(body, "leaseExpiresAt") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("wan-status %d %s", code, body)
	}
	// 租约没了（WAN 断开时厂端重启、或到期）：业务页被挡。
	svc.Store().ClearContentLease()
	// 同一会话再看通道现状，横幅正要报租约没了，不能被租约挡成 403。
	code, body = do(t, srv, "GET", base+"/wan-status", tok, "")
	// 被租约挡住就停测（D-91）。
	if code != http.StatusOK || !strings.Contains(body, `"connected":false`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("wan-status without lease %d %s", code, body)
	}
	// 没登录的照样拒，租约豁免不等于匿名放行。
	code, body = do(t, srv, "GET", base+"/wan-status", "", "")
	// 匿名放行就停测。
	if code != http.StatusUnauthorized {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("anon wan-status without lease %d %s", code, body)
	}
}

// 租约没了（D-91 附带两处）：本人信息和名册照给，超管仍认得出；写操作照旧挡；登录页清单报租约无效。
func TestNoLeaseReadsHTTP(t *testing.T) {
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
	srv := httptest.NewServer(httpapi.New(h, "").Router())
	// 测完关闭临时服务，把端口让出来。
	t.Cleanup(srv.Close)
	// 拼出本厂前缀，后面的路径都接在后面。
	base := "/v1/factories/" + fid.String()
	// 按约定身份建厂，初始超管当场有效，自签了租约。
	svc, _ := enrollSA(t, h, fid)
	// 租约在时登录页清单报租约有效；没配平台地址不算断开。
	if f := siteFactory(t, srv, fid.String()); !f.LeaseReady || !f.WANConnected {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("site with lease %+v", f)
	}
	// 租约在时登录拿会话。
	tok := loginWeb(t, srv, base, "sa", "secret")
	// 租约没了（WAN 断开时厂端重启、或到期）。
	svc.Store().ClearContentLease()
	// 登录页一打开就该知道租约无效，不等点登录。
	if f := siteFactory(t, srv, fid.String()); f.LeaseReady {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("site without lease %+v", f)
	}
	// 同一会话读本人信息，不含正文，不该被租约挡。
	code, body := do(t, srv, "GET", base+"/me", tok, "")
	// 被挡或不是本人就停测。
	if code != http.StatusOK || !strings.Contains(body, `"loginName":"sa"`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("me without lease %d %s", code, body)
	}
	// 同一会话读名册，网页要靠它认登录人和超管。
	code, body = do(t, srv, "GET", base+"/catalog", tok, "")
	// 被挡、或超管角色没带出来就停测（否则页面会把超管说成不是超管）。
	if code != http.StatusOK || !strings.Contains(body, `"factory_super_admin"`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("catalog without lease %d %s", code, body)
	}
	// 写操作照旧挡在租约门外。
	code, body = do(t, srv, "POST", base+"/org-units", tok, `{"name":"无租约车间"}`)
	// 放行或不是租约错就停测。
	if code != http.StatusForbidden || !strings.Contains(body, "content lease expired") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("write without lease %d %s", code, body)
	}
	// 没登录的照样拒，租约豁免不等于匿名放行。
	code, body = do(t, srv, "GET", base+"/catalog", "", "")
	// 匿名放行就停测。
	if code != http.StatusUnauthorized {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("anon catalog without lease %d %s", code, body)
	}
}

// siteFactory 读登录页清单，取出指定厂那一行；共享测试库里别的测试也在建厂，按编号找。
func siteFactory(t *testing.T, srv *httptest.Server, fid string) hub.SiteFactory {
	// 标成辅助函数，失败时栈指向真正的用例。
	t.Helper()
	// 未登录也能读登录页清单。
	code, body := do(t, srv, "GET", "/v1/site", "", "")
	// 读不到就停测。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("site %d %s", code, body)
	}
	// 接住清单。
	var out struct {
		Factories []hub.SiteFactory `json:"factories"` // 本机已认领的工厂
	}
	// 解析失败就停测。
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("site json %s: %v", body, err)
	}
	// 逐行找本测的厂。
	for _, f := range out.Factories {
		// 编号对上就交回。
		if f.ID.String() == fid {
			return f
		}
	}
	// 清单里没有本测的厂就停测。
	t.Fatalf("site missing %s: %s", fid, body)
	return hub.SiteFactory{}
}
