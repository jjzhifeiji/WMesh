// 令牌与设备绑定（D-59）：厂网登录或登录后补报带的臂号把会话钉到一台设备，MQTT 只认这一台。
package httpapi_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"
	"github.com/google/uuid"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/service"
)

// 登录带 X 的臂号：报 Y 回服务不可用、报 X 连上；同一次登录补报 Y 回冲突；同人不带臂号重登后补报 Y，Y 才连得上。
func TestPadMQTTBoundToLoginClient(t *testing.T) {
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
	// 拉起本厂设备通道，起不来就停测。
	if err := h.StartClientBroker("127.0.0.1:0"); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 装上厂内适配，测试不连 WAN。
	api := httpapi.New(h, "")
	// 拉起临时服务，测完由清理关掉。
	srv := httptest.NewServer(api.Router())
	// 测完关闭临时服务，把端口让出来。
	t.Cleanup(srv.Close)
	// 拼出本厂前缀，后面的路径都接在后面。
	base := "/v1/factories/" + fid.String()
	// 按约定身份建厂，初始超管当场有效。
	svc, _ := enrollSA(t, h, fid)
	// 登记两台设备 X、Y 并各钉臂号。
	cx := bindArm(t, svc, "焊机X", "ARM-HX")
	// 第二台设备。
	cy := bindArm(t, svc, "焊机Y", "ARM-HY")
	// 通道地址，后面每次连都用它。
	addr := h.ClientMQTTAddr()

	// 示教器在 X 上登录，登录请求带着 X 的臂号。
	code, body := do(t, srv, "POST", base+"/pad/login", "", `{"loginName":"sa","password":"secret","deviceSerial":"ARM-HX"}`)
	// 登不上则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pad login %d %s", code, body)
	}
	// 取出令牌 A。
	tokA := gjson(t, body, "token")
	// 令牌 A 报 Y（Y 无人在线）连不上，回服务不可用，不当令牌失效。
	if err := mqttTry(addr, fid, cy, tokA); !errors.Is(err, packets.ErrorRefusedServerUnavailable) {
		// 放行或拒绝码不对就停测。
		t.Fatalf("A as Y: %v", err)
	}
	// 令牌 A 报 X 连上。
	if err := mqttTry(addr, fid, cx, tokA); err != nil {
		// 被误拒就停测。
		t.Fatalf("A as X: %v", err)
	}
	// 同一次登录里补报 Y 不改绑定，回冲突。
	code, body = do(t, srv, "POST", base+"/pad/login-log", tokA, `{"deviceSerial":"ARM-HY","clientId":"`+cy.String()+`"}`)
	// 不是冲突或错误码不对则本测失败。
	if code != http.StatusConflict || !strings.Contains(body, "session is bound to another client") {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("rebind %d %s", code, body)
	}

	// 同一人换到 Y 上重新登录，这次登录时没读到臂号。
	code, body = do(t, srv, "POST", base+"/pad/login", "", `{"loginName":"sa","password":"secret"}`)
	// 登不上则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("relogin %d %s", code, body)
	}
	// 取出令牌 B。
	tokB := gjson(t, body, "token")
	// 还没对上臂，令牌 B 报 Y 也不让连。
	if err := mqttTry(addr, fid, cy, tokB); !errors.Is(err, packets.ErrorRefusedServerUnavailable) {
		// 放行或拒绝码不对就停测。
		t.Fatalf("unbound B as Y: %v", err)
	}
	// 对上 Y 后补报现场，会话钉到 Y。
	code, body = do(t, srv, "POST", base+"/pad/login-log", tokB, `{"deviceSerial":"ARM-HY","clientId":"`+cy.String()+`"}`)
	// 不是成功则本测失败。
	if code != http.StatusNoContent {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("bind Y %d %s", code, body)
	}
	// 令牌 B 报 Y 连上。
	if err := mqttTry(addr, fid, cy, tokB); err != nil {
		// 被误拒就停测。
		t.Fatalf("B as Y: %v", err)
	}
	// X 上的旧令牌 A 已随新登录作废，回认证失败（3.1.1 上是未授权），示教器据此登出。
	if err := mqttTry(addr, fid, cx, tokA); !errors.Is(err, packets.ErrorRefusedNotAuthorised) {
		// 放行或拒绝码不对就停测。
		t.Fatalf("old A as X: %v", err)
	}
}

// bindArm 登记一台未作废设备并钉臂号，失败就停测。
func bindArm(t *testing.T, svc *service.Service, name, serial string) uuid.UUID {
	// 标成辅助函数，失败时栈指向真正的用例。
	t.Helper()
	// 生成设备公钥。
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	// 生成失败就停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 新开一个身份，避免和别的测试撞上。
	cid := id.New()
	// 绑定只认 WAN 通道这一个入口，测试直接走它预置。
	if _, err := svc.Node.AcceptBinding(context.Background(), cid, name, pub, 1); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("bind: %v", err)
	}
	// 钉臂号，失败就停测。
	if _, err := svc.Store().PinDeviceSerial(context.Background(), cid, serial, false); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pin: %v", err)
	}
	return cid
}

// mqttTry 连一次并交回 CONNACK 拒绝原因；连上就断开交回空。
func mqttTry(addr string, fid, cid uuid.UUID, token string) error {
	// 组装连接参数，并关掉自动重连。
	opts := mqtt.NewClientOptions()
	// 指向本厂测试通道。
	opts.AddBroker("tcp://" + addr)
	// 填上机器号。
	opts.SetClientID(cid.String())
	// 用户名放工厂身份。
	opts.SetUsername(fid.String())
	// 口令放登录令牌。
	opts.SetPassword(token)
	// 关掉自动重连，只试这一次。
	opts.SetAutoReconnect(false)
	// 限时连接，避免挂死。
	opts.SetConnectTimeout(3 * time.Second)
	// 按参数建客户端。
	cli := mqtt.NewClient(opts)
	// 发起连接。
	tok := cli.Connect()
	// 等不到结果就当超时。
	if !tok.WaitTimeout(5 * time.Second) {
		// 交回超时，调用方按失败判。
		return errors.New("connect timeout")
	}
	// 连上了就断开。
	if tok.Error() == nil {
		// 断开这条试连。
		cli.Disconnect(100)
	}
	// 交回拒绝原因，连上则为空。
	return tok.Error()
}
