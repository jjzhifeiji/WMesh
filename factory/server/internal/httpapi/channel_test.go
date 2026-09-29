// HTTP 适配：inbox 与 HTTPS 拉闭包密文；他机拒绝。
package httpapi_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/clientmqtt"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/service"
)

// 验收收件箱、闭包下载和策略下发。
func TestClientChannelHTTP(t *testing.T) {
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
	// 这一步被拒绝则停测，避免后面误判。
	if err := h.StartClientBroker("127.0.0.1:0"); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 装上厂内适配，引导口令只在测试里使用。
	api := httpapi.New(h, "boot-secret", "")
	// 测试里固定通道地址，不跟请求主机变化。
	api.ClientMQTTURL = "tcp://" + h.ClientMQTTAddr()
	// 拉起临时服务，测完由清理关掉。
	srv := httptest.NewServer(api.Router())
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
		t.Fatal(err)
	}
	// 租约写不进去则停测，后面读正文会失败。
	if err := svc.Store().GrantLocalLease(context.Background()); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 厂短码写不进去则停测。
	if err := svc.Store().PutFactoryShortCode(context.Background(), "F01"); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
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
		t.Fatalf("web login %d %s", code, body)
	}
	// 取出会话令牌，后面的请求要带上。
	saTok := gjson(t, body, "token")

	// 再生成一台的公钥，用来验证他机被拒绝。
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	// 密钥没有生成则停测，后面无法绑定。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 分配一个新身份，避免和别的测试撞上。
	cid := id.New().String()
	// 分配一个新身份，避免和别的测试撞上。
	cidB := id.New().String()
	// 把公钥收成文本，才能放进绑定请求。
	pk := base64.StdEncoding.EncodeToString(pub)
	// 登记或查看绑定设备。
	code, body = do(t, srv, "POST", base+"/clients", saTok, `{"id":"`+cid+`","name":"焊机","publicKey":"`+pk+`","bindingRevision":1}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("bind %d %s", code, body)
	}
	// 再生成一台的公钥，用来验证他机被拒绝。
	pubB, _, _ := ed25519.GenerateKey(rand.Reader)
	// 把公钥收成文本，才能放进绑定请求。
	pkB := base64.StdEncoding.EncodeToString(pubB)
	// 登记或查看绑定设备。
	code, body = do(t, srv, "POST", base+"/clients", saTok, `{"id":"`+cidB+`","name":"焊机B","publicKey":"`+pkB+`","bindingRevision":1}`)
	// 不是已创建则本测失败。
	if code != http.StatusCreated {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("bind b %d %s", code, body)
	}
	// 把机械臂号钉到这台设备上。
	code, body = do(t, srv, "POST", base+"/clients/"+cid+"/device", "", `{"deviceSerial":"ARM-1"}`)
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pin %d %s", code, body)
	}
	// 把机械臂号钉到这台设备上。
	code, body = do(t, srv, "POST", base+"/clients/"+cidB+"/device", "", `{"deviceSerial":"ARM-B"}`)
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pin b %d %s", code, body)
	}
	// 登录并取得会话，口令错误应当被拒绝。
	code, body = do(t, srv, "POST", base+"/clients/"+cid+"/login", "", `{"deviceSerial":"ARM-1","loginName":"sa","password":"secret"}`)
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || gjson(t, body, "mqttUrl") == "" || gjson(t, body, "signingPublicKey") == "" {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("client login %d %s", code, body)
	}
	// 取出会话令牌，后面的请求要带上。
	tok := gjson(t, body, "token")

	// 拉取对账清单，正文不应当出现。
	code, body = do(t, srv, "GET", base+"/clients/"+cid+"/inbox", tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, `"closures"`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("inbox %d %s", code, body)
	}
	// 拉取对账清单，正文不应当出现。
	code, body = do(t, srv, "GET", base+"/clients/"+cidB+"/inbox", tok, "")
	// 不是禁止则本测失败。
	if code != http.StatusForbidden {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("other inbox %d %s", code, body)
	}

	// 沿用这次请求的取消信号，调用方断开就停。
	ctx := context.Background()
	// 用工厂直属的位置，不挂组织节点。
	direct := service.WorkContext{Direct: true}
	// 准备一段包体，后面用来入库或下载。
	proc, err := svc.CreateFactoryProcess(ctx, saTok, direct, "工艺", []byte(`{"current":1}`))
	// 这一步失败则停测，避免在坏结果上继续。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 把草稿改为可用，失败则保持原状。
	proc, err = svc.PublishAsset(ctx, saTok, proc.ID, proc.Revision)
	// 这一步失败则停测，避免在坏结果上继续。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 工程正文点名刚造的工艺，用来组包。
	projBody := []byte(`[{"name":"w","processId":"` + proc.ID.String() + `"}]`)
	// 创建厂级工程，并记下直属或节点。
	proj, err := svc.CreateFactoryProject(ctx, saTok, direct, "工程", projBody, nil)
	// 这一步失败则停测，避免在坏结果上继续。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 把草稿改为可用，失败则保持原状。
	proj, err = svc.PublishAsset(ctx, saTok, proj.ID, proj.Revision)
	// 这一步失败则停测，避免在坏结果上继续。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}
	// 授权到设备失败则停测，后面无法拉包。
	if err := svc.GrantClientProject(ctx, saTok, proj.ID, uuid.MustParse(cid)); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(err)
	}

	// 拉取闭包，别的设备应当被拒绝。
	code, body = do(t, srv, "GET", base+"/clients/"+cid+"/closures/"+proj.ID.String(), tok, "")
	// 不是成功，或正文与预期不符则失败。
	if code != http.StatusOK || !strings.Contains(body, `"wrap"`) || strings.Contains(body, `"current":1`) {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("pull %d %s", code, body)
	}
	// 拉取闭包，别的设备应当被拒绝。
	code, body = do(t, srv, "GET", base+"/clients/"+cidB+"/closures/"+proj.ID.String(), tok, "")
	// 不是禁止则本测失败。
	if code != http.StatusForbidden {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("other pull %d %s", code, body)
	}

	// 准备一段包体，后面用来入库或下载。
	got := make(chan []byte, 2)
	// 用令牌连上本厂通道，连不上则停测。
	cli := mqttConnect(t, h.ClientMQTTAddr(), fid.String(), cid, tok)
	// 等到在线标记符合预期，超时则失败。
	waitAppOnline(t, svc, saTok, true)
	// 收到下行就把副本交给等待的一方。
	subTok := cli.Subscribe(clientmqtt.DownTopic(fid, uuid.MustParse(cid)), 1, func(_ mqtt.Client, m mqtt.Message) {
		// 在收到消息和超时之间等，避免一直挂住。
		select {
		// 把载荷副本送出去，避免原缓冲被改。
		case got <- append([]byte(nil), m.Payload()...):
		// 来不及接收就丢掉，避免堵住收包回调。
		default:
		}
	})
	// 没有在时限内连上或订上则停测。
	if !subTok.WaitTimeout(3*time.Second) || subTok.Error() != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal(subTok.Error())
	}
	// 读取或修改示教器策略。
	code, body = do(t, srv, "PUT", base+"/client-policy", saTok, `{"maxCachedProjects":3,"cacheScope":"all","persistUnwrapKey":false,"keyTtlSeconds":0}`)
	// 不是成功则本测失败。
	if code != http.StatusOK {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("policy %d %s", code, body)
	}
	// 在收到消息和超时之间等，避免一直挂住。
	select {
	// 取到一条下行，再核对类型和有没有正文。
	case raw := <-got:
		// 下行里不该带正文，带了就说明内容泄出。
		if clientmqtt.HasBody(raw) {
			// 与预期不符就停测，避免后面连环误判。
			t.Fatalf("mqtt body %s", raw)
		}
		// 应是策略通知，类型不对则本测失败。
		if !strings.Contains(string(raw), `"typ":"policy"`) {
			// 与预期不符就停测，避免后面连环误判。
			t.Fatalf("mqtt %s", raw)
		}
	// 超时仍无通知则失败，避免测试挂死。
	case <-time.After(4 * time.Second):
		// 与预期不符就停测，避免后面连环误判。
		t.Fatal("missed policy intent")
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
	// 取出人员身份，后面用来对上连接。
	personID := gjson(t, body, "account.id")
	// 用令牌连上本厂通道，连不上则停测。
	padMQTT := mqttConnect(t, h.ClientMQTTAddr(), fid.String(), personID, padTok)
	// 等到在线标记符合预期，超时则失败。
	waitAppOnline(t, svc, saTok, true)
	// 主动断开，在线标记随后应当掉下去。
	cli.Disconnect(250)
	// 主动断开，在线标记随后应当掉下去。
	padMQTT.Disconnect(250)
	// 等到在线标记符合预期，超时则失败。
	waitAppOnline(t, svc, saTok, false)
}

// 轮询在线标记，超时仍不符则失败。
func waitAppOnline(t *testing.T, svc *service.Service, tok string, want bool) {
	// 标成辅助函数，失败时栈指向真正的用例。
	t.Helper()
	// 给在线标记一段等待，超时就判失败。
	deadline := time.Now().Add(3 * time.Second)
	// 反复查看，直到符合预期或等待超时。
	for {
		// 读取名册，超管见全厂，其他人只见自己。
		cat, err := svc.Catalog(context.Background(), tok)
		// 这一步失败则停测，避免在坏结果上继续。
		if err != nil {
			// 与预期不符就停测，避免后面连环误判。
			t.Fatal(err)
		}
		// 在线标记已经符合就结束等待。
		if cat.Me.AppOnline == want {
			return
		}
		// 超过等待时间仍不符则判定失败。
		if time.Now().After(deadline) {
			// 与预期不符就停测，避免后面连环误判。
			t.Fatalf("appOnline=%v want %v", cat.Me.AppOnline, want)
		}
		// 间隔再查，避免空转把测试占满。
		time.Sleep(50 * time.Millisecond)
	}
}

// 用登录令牌连接通道，连不上就停测。
func mqttConnect(t *testing.T, addr, factoryID, clientID, token string) mqtt.Client {
	// 标成辅助函数，失败时栈指向真正的用例。
	t.Helper()
	// 组装连接参数，并关掉自动重连。
	opts := mqtt.NewClientOptions()
	// 指向本厂刚拉起的测试通道。
	opts.AddBroker("tcp://" + addr)
	// 用设备或人员身份区分这条连接。
	opts.SetClientID(clientID)
	// 用户名放工厂身份，通道据此认厂。
	opts.SetUsername(factoryID)
	// 口令放登录令牌，不是日常口令。
	opts.SetPassword(token)
	// 关掉自动重连，断线结果才稳定。
	opts.SetAutoReconnect(false)
	// 限时连接，避免通道不通时测试挂死。
	opts.SetConnectTimeout(3 * time.Second)
	// 按这些参数建立通道客户端。
	cli := mqtt.NewClient(opts)
	// 发起连接，等结果再做后面的步骤。
	tok := cli.Connect()
	// 没有在时限内连上或订上则停测。
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("mqtt: %v", tok.Error())
	}
	// 测完断开这条连接，避免占用通道。
	t.Cleanup(func() { cli.Disconnect(250) })
	return cli
}
