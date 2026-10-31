// HTTP 适配：示教器厂网登录、现场登录记录、平板资产与探活；本机旧入口已撤。
package httpapi_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
)

// 验收厂网登录、现场记录和设备号是否属于本厂。
func TestClientDeviceLoginHTTP(t *testing.T) {
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
	svc, saUUID := enrollSA(t, h, fid)
	// 转成文本，后面拼路径要用。
	saID := saUUID.String()
	// 条件里的调用失败则停止，不回成功。
	if err := svc.Store().PutFactoryName(context.Background(), "一号厂"); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("factory name: %v", err)
	}
	// 登录并取得会话，口令错误应当被拒绝。
	code, body := do(t, srv, "POST", base+"/login", "", `{"loginName":"sa","password":"secret"}`)
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("web login %d %s", code, body)
	}
	// 取出会话令牌，后面的请求要带上。
	tok := gjson(t, body, "token")

	// 再生成一台的公钥，用来验证他机被拒绝。
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	// 密钥没有生成则停测，后面无法绑定。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 分配一个新身份，避免和别的测试撞上。
	cid := id.New().String()
	// 绑定只认 WAN 通道这一个入口，测试直接走它预置。
	if _, err := svc.Node.AcceptBinding(context.Background(), uuid.MustParse(cid), "焊机", pub, 1); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("bind: %v", err)
	}
	// 补登这台焊机的短码，示教器自带的 C 段号才会被认。
	if err := svc.Store().PutClientShortCode(context.Background(), uuid.MustParse(cid), "C0008"); err != nil {
		// 登记短码失败就停测，后面的新建会被拒。
		t.Fatal(err)
	}

	// 厂端钉臂号的旧入口已撤，应落到兜底的找不到。
	code, body = do(t, srv, "POST", base+"/clients/"+cid+"/device", tok, `{"deviceSerial":"ARM-1"}`)
	// 不是找不到则本测失败。
	if code != http.StatusNotFound {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("old pin %d %s", code, body)
	}
	// 臂号随 WAN 绑定帧来，测试直接在库里钉上。
	if _, err := svc.Store().PinDeviceSerial(context.Background(), uuid.MustParse(cid), "ARM-1", false); err != nil {
		// 钉号失败就停测。
		t.Fatalf("pin: %v", err)
	}
	// 读回设备名录，号应是 ARM-1，回包不带解封钥。
	code, body = do(t, srv, "GET", base+"/clients", tok, "")
	// 读不到、号不对或泄出解封钥则本测失败。
	if code != http.StatusOK || !strings.Contains(body, `"deviceSerial":"ARM-1"`) || strings.Contains(body, "unwrapKey") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("serial listed %d %s", code, body)
	}

	// 登录并取得会话，口令错误应当被拒绝。
	code, body = do(t, srv, "POST", base+"/pad/login", "", `{"loginName":"sa","password":"secret","appVersion":52,"appVersionName":"6.1.1","deviceModel":"TB-X606F","deviceManufacturer":"Lenovo","androidRelease":"10","networkName":"Factory-WiFi"}`)
	// 状态不对，或回包泄出了不该给的秘密。
	if code != http.StatusOK || gjson(t, body, "token") == "" || gjson(t, body, "unwrapKey") == "" || !strings.Contains(body, `"deviceSerial":"ARM-1"`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pad login %d %s", code, body)
	}
	// 通道地址格式不对则本测失败。
	if mqtt := gjson(t, body, "mqttUrl"); !strings.HasPrefix(mqtt, "tcp://") || !strings.Contains(mqtt, ":52184") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pad mqttUrl %s", mqtt)
	}
	// 回包出现不该给人的秘密则本测失败。
	if strings.Contains(body, `"deviceSerial":"ARM-1","unwrapKey"`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("device key leaked %s", body)
	}
	// 取出会话令牌，后面的请求要带上。
	padTok := gjson(t, body, "token")
	// 查看现场登录，回包里不能有口令。
	code, body = do(t, srv, "GET", base+"/people/"+saID+"/logins", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, `"kind":"pad"`) || !strings.Contains(body, `"deviceModel":"TB-X606F"`) || !strings.Contains(body, `"networkName":"Factory-WiFi"`) || strings.Contains(body, "secret") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("login logs %d %s", code, body)
	}
	// 补记一次现场登录。
	code, body = do(t, srv, "POST", base+"/pad/login-log", padTok, `{"appVersion":52,"appVersionName":"6.1.1","deviceSerial":"ARM-1","deviceModel":"TB-X606F","deviceManufacturer":"Lenovo","androidRelease":"10","networkName":"Factory-WiFi","clientId":"`+cid+`"}`)
	// 不是无内容则本测失败。
	if code != http.StatusNoContent {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("login-log %d %s", code, body)
	}
	// 查看现场登录，回包里不能有口令。
	code, body = do(t, srv, "GET", base+"/people/"+saID+"/logins", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, `"deviceSerial":"ARM-1"`) || !strings.Contains(body, `"clientName":"焊机"`) || !strings.Contains(body, cid) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("login-log rows %d %s", code, body)
	}
	// 办理工艺或工程，正文按权限才可见。
	code, body = do(t, srv, "POST", base+"/pad/assets", padTok, `{"kind":"process","name":"平板工艺","content":"{\"current\":170}","id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","code":"GY-C0008-000001"}`)
	// 不是已创建，或正文与预期不符则失败。
	if code != http.StatusCreated || gjson(t, body, "status") != "available" || gjson(t, body, "level") != "personal" || gjson(t, body, "id") != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pad create %d %s", code, body)
	}
	// 读取或覆盖正文，无权则应当被拒绝。
	code, body = do(t, srv, "POST", base+"/pad/assets/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/content", padTok, `{"content":"{\"current\":180}"}`)
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "revision") != "2" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pad apply %d %s", code, body)
	}
	// 读取或覆盖正文，无权则应当被拒绝。
	code, body = do(t, srv, "GET", base+"/assets/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/content", padTok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, "180") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pad read %d %s", code, body)
	}
	// 读取或覆盖正文，无权则应当被拒绝。
	code, body = do(t, srv, "POST", base+"/assets/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/content", padTok, `{"expected":1,"content":"stale"}`)
	// 不是冲突则本测失败。
	if code != http.StatusConflict {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("web stale %d %s", code, body)
	}
	// 拉取对账清单，正文不应当出现。
	code, body = do(t, srv, "GET", base+"/pad/inbox", padTok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, `"closures"`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pad inbox %d %s", code, body)
	}
	// 本机收件箱旧入口已撤，应落到兜底的找不到。
	code, body = do(t, srv, "GET", base+"/clients/"+cid+"/inbox", padTok, "")
	// 不是找不到则本测失败。
	if code != http.StatusNotFound {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("old client inbox %d %s", code, body)
	}

	// 本机登录领钥的旧入口已撤，应落到兜底的找不到。
	code, body = do(t, srv, "POST", base+"/clients/"+cid+"/login", "", `{"deviceSerial":"ARM-1","loginName":"sa","password":"secret"}`)
	// 不是找不到则本测失败。
	if code != http.StatusNotFound {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("old client login %d %s", code, body)
	}

	// 局域网探活，看设备号是否属于本厂。
	code, body = do(t, srv, "GET", "/v1/discover?deviceSerial=ARM-1", "", "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, `"belongs":true`) || !strings.Contains(body, cid) || !strings.Contains(body, fid.String()) || !strings.Contains(body, `"factoryName":"一号厂"`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("discover mine %d %s", code, body)
	}
	// 回包出现不该给人的秘密则本测失败。
	if strings.Contains(body, "unwrapKey") || strings.Contains(body, "password") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("discover leaked %s", body)
	}
	// 局域网探活，看设备号是否属于本厂。
	code, body = do(t, srv, "GET", "/v1/discover?deviceSerial=ARM-OTHER", "", "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || strings.Contains(body, `"belongs":true`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("discover other %d %s", code, body)
	}
}
