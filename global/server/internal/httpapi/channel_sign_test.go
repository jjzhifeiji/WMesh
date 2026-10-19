package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"

	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/service"
)

// 报文外层或签名原文里出现这段文字都算漏出，签名原文编成 base64 后子串查不出来。
func payloadHas(raw []byte, needle string) bool {
	// 外层原文里直接出现就算。
	if strings.Contains(string(raw), needle) {
		return true
	}
	// 解开外层，再看签名原文。
	var c service.Cmd
	// 解不开就只看外层。
	if json.Unmarshal(raw, &c) != nil {
		return false
	}
	// 签名原文里出现也算漏出。
	return strings.Contains(string(c.Signed), needle)
}

// 核对一条下行：签名对本厂验得过、对别厂验不过，签名原文和外层字段一致；交回原文。
func assertSignedFor(t *testing.T, pub []byte, fid uuid.UUID, c service.Cmd) service.Cmd {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 没带原文或签名就停测。
	if len(c.Signed) == 0 || len(c.Sig) == 0 {
		// 下行没签名，带上现场停测。
		t.Fatalf("unsigned %+v", c)
	}
	// 对本厂要验得过。
	if !nodekey.Verify(pub, nodekey.ChannelCmdPayload(fid, c.Signed), c.Sig) {
		// 验不过就停测。
		t.Fatalf("bad sig %s", c.Signed)
	}
	// 换成别厂身份要验不过，转投不能成功。
	if nodekey.Verify(pub, nodekey.ChannelCmdPayload(uuid.New(), c.Signed), c.Sig) {
		// 别厂也验得过就停测。
		t.Fatal("sig not bound to factory")
	}
	// 去掉签名后的外层，应和签名原文同一份。
	outer := c
	// 清掉签名字段再编。
	outer.Signed, outer.Sig = nil, nil
	// 编出外层，和签名原文逐字节比。
	raw, err := json.Marshal(outer)
	// 编不出就停测。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 外层和签名原文不一致就停测，老厂和新厂会看到两样指令。
	if !bytes.Equal(raw, c.Signed) {
		// 带上两边停测。
		t.Fatalf("outer %s signed %s", raw, c.Signed)
	}
	// 解开签名原文交回。
	var in service.Cmd
	// 解不开就停测。
	if err := json.Unmarshal(c.Signed, &in); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	return in
}

// 拉索引并交回指令和回包里的 WAN 公钥。
func pullSignedIndex(t *testing.T, srv *httptest.Server, fid uuid.UUID, priv []byte) ([]service.Cmd, []byte) {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 用厂钥签名去拉全量索引。
	res := factoryReq(t, srv, http.MethodGet, "/v1/channel/index", fid, priv)
	// 用完就关上。
	defer res.Body.Close()
	// 读完全部正文。
	body, _ := io.ReadAll(res.Body)
	// 没成功就停测。
	if res.StatusCode != http.StatusOK {
		// 带上现场停测。
		t.Fatalf("index %d %s", res.StatusCode, body)
	}
	// 接住指令和公钥。
	var out struct {
		Cmds         []service.Cmd `json:"cmds"`         // 当前指令
		WANPublicKey []byte        `json:"wanPublicKey"` // 下行验签公钥
	}
	// 解不开就停测。
	if err := json.Unmarshal(body, &out); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	return out.Cmds, out.WANPublicKey
}

// 索引每条都带签名，用回包公钥加本厂身份验得过，换成别厂验不过。
func TestChannelIndexSigned(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, _ := startChannel(t, svc)
	// 分配一台设备，索引里才有绑定。
	code, body := do(t, srv, "POST", "/v1/clients", tok, `{"name":"焊机-签","deviceSerial":"ARM-S","factoryId":"`+fid.String()+`"}`)
	// 没建成就停测。
	if code != http.StatusCreated {
		// 带上现场停测。
		t.Fatalf("register %d %s", code, body)
	}
	// 拉索引和公钥。
	cmds, pub := pullSignedIndex(t, srv, fid, priv)
	// 回包公钥要和库里那把一致。
	row, err := svc.Store().WANSigningKey(context.Background())
	// 读不出就停测。
	if err != nil || !bytes.Equal(row.PublicKey, pub) {
		// 公钥对不上就停测。
		t.Fatalf("index pub %x row %x %v", pub, row.PublicKey, err)
	}
	// 记下看到的绑定。
	sawBind := false
	// 逐条核对签名。
	for _, c := range cmds {
		// 签名对本厂验得过、原文和外层一致。
		in := assertSignedFor(t, pub, fid, c)
		// 原文里的类型要和外层一致。
		if in.Typ != c.Typ {
			// 类型不一致就停测。
			t.Fatalf("typ %s vs %s", in.Typ, c.Typ)
		}
		// 记下绑定已签。
		if in.Typ == service.CmdClientBind && in.ClientName == "焊机-签" {
			// 看到了这台设备的绑定。
			sawBind = true
		}
	}
	// 没有绑定就停测，前提不成立。
	if !sawBind {
		// 带上清单停测。
		t.Fatalf("no signed bind %+v", cmds)
	}
}

// 分配设备和停用厂经 MQTT 推下去时都带签名，原文和外层一致。
func TestChannelDownSigned(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 订阅该厂下行。
	cmds := subscribeDown(t, mqttAddr, fid, priv)
	// 取当前验签公钥。
	pub := svc.Channel.ChannelPublicKey()
	// 分配一台设备。
	code, body := do(t, srv, "POST", "/v1/clients", tok, `{"name":"焊机-推","deviceSerial":"ARM-P","factoryId":"`+fid.String()+`"}`)
	// 没建成就停测。
	if code != http.StatusCreated {
		// 带上现场停测。
		t.Fatalf("register %d %s", code, body)
	}
	// 等到这台设备的绑定。
	bind := waitMQTT(t, cmds, func(c service.Cmd) bool { return c.Typ == service.CmdClientBind && c.ClientName == "焊机-推" })
	// 绑定要带本厂签名。
	if in := assertSignedFor(t, pub, fid, bind); in.DeviceSerial != "ARM-P" {
		// 原文里的臂号不对就停测。
		t.Fatalf("bind inner %+v", in)
	}
	// 停用这家厂。
	code, body = do(t, srv, "POST", "/v1/factories/"+fid.String()+"/disable", tok, "")
	// 没成功就停测。
	if code != http.StatusOK {
		// 带上现场停测。
		t.Fatalf("disable %d %s", code, body)
	}
	// 等到停用指令。
	st := waitMQTT(t, cmds, func(c service.Cmd) bool { return c.Typ == service.CmdFactoryState && c.Status == "disabled" })
	// 停用要带本厂签名。
	if in := assertSignedFor(t, pub, fid, st); in.Status != "disabled" {
		// 原文里的状态不对就停测。
		t.Fatalf("state inner %+v", in)
	}
}

// 升档问询经 MQTT 下发时带签名，签名覆盖问询号。
func TestCallFactorySigned(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 用厂钥连上通道，同一条连接收问询、发回执。
	cli := connectMQTT(t, mqttAddr, fid, priv)
	// 接住收到的问询。
	asked := make(chan service.Cmd, 4)
	// 收问询并答空清单。
	sub := cli.Subscribe("wan/"+fid.String()+"/down", 1, func(_ mqtt.Client, m mqtt.Message) {
		// 接住解开的指令。
		var c service.Cmd
		// 解不开或不是清单问询就忽略。
		if json.Unmarshal(m.Payload(), &c) != nil || c.Typ != service.CmdAssetList {
			return
		}
		// 记下问询，后面核对签名。
		asked <- c
		// 按问询号答一份空清单。
		raw, _ := json.Marshal(service.Cmd{Typ: service.CmdAssetListOK, ReqID: c.ReqID, Assets: json.RawMessage(`[]`)})
		// 发回执。
		cli.Publish("wan/"+fid.String()+"/up", 1, false, raw)
	})
	// 订不上就停测。
	if !sub.WaitTimeout(5*time.Second) || sub.Error() != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(sub.Error())
	}
	// 等名录显示在线，问询才发得出去。
	assertOnline(t, srv, tok, true)
	// 经服务问一次升档清单。
	if _, err := svc.CallFactory(context.Background(), fid, service.Cmd{Typ: service.CmdAssetList, Kind: "process"}); err != nil {
		// 问不到就停测。
		t.Fatalf("call %v", err)
	}
	// 取出收到的问询。
	var c service.Cmd
	// 收到或超时谁先到走谁。
	select {
	// 收到问询就核对。
	case c = <-asked:
	// 超时没收到就停测。
	case <-time.After(5 * time.Second):
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal("no asset_list")
	}
	// 问询要带本厂签名。
	in := assertSignedFor(t, svc.Channel.ChannelPublicKey(), fid, c)
	// 签名原文里的问询号要和外层一致且非空。
	if in.ReqID == "" || in.ReqID != c.ReqID || in.Kind != "process" {
		// 问询号没被签名覆盖就停测。
		t.Fatalf("inner %+v outer %+v", in, c)
	}
}

// 辅助函数要能在签名原文里查出子串，外层查不到的也要算漏出。
func TestPayloadHasSeesSigned(t *testing.T) {
	// 签名原文里夹着包体字样。
	raw, err := json.Marshal(service.Cmd{Typ: service.CmdSoftware, Signed: []byte(`{"body":"svc-mqtt-body"}`)})
	// 编不出就停测。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 外层原文是 base64，直接查不到。
	if strings.Contains(string(raw), "svc-mqtt-body") {
		// 前提不成立就停测。
		t.Fatal("plain contains")
	}
	// 辅助函数要查得到。
	if !payloadHas(raw, "svc-mqtt-body") {
		// 查不出签名原文里的子串就停测。
		t.Fatal("payloadHas missed signed")
	}
}
