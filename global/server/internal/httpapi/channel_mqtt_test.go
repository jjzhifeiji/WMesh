package httpapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/contentcrypt"
	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/mqttbroker"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 厂上线后名录能看到版本，掉线则不再在线。
func TestChannelPresence(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 用厂钥连上通道，失败就停测。
	cli := connectMQTT(t, mqttAddr, fid, priv)
	// 等到名录反映在线或离线，超时失败。
	assertOnline(t, srv, tok, true)
	// 收成上报或应答，发不出去就停测。
	raw, err := json.Marshal(service.Cmd{
		Typ: service.CmdPresence, WebVersion: 2, WebVersionName: "1.1.0",
		ServiceVersion: 3, ServiceVersionName: "1.2.0",
	})
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 把应答或上报发回云端。
	tokPub := cli.Publish("wan/"+fid.String()+"/up", 1, false, raw)
	// 时限内没完成连接或订阅，就停测。
	if !tokPub.WaitTimeout(5*time.Second) || tokPub.Error() != nil {
		// 在线上报没发出去，就停测。
		t.Fatalf("presence: %v", tokPub.Error())
	}
	// 只等一小段，超时就当没有变化。
	deadline := time.Now().Add(2 * time.Second)
	// 在时限内反复看，超时再判失败。
	for time.Now().Before(deadline) {
		// 发出请求并记下状态，对不上就停测。
		_, body := do(t, srv, "GET", "/v1/directory", tok, "")
		// 接住响应里要核对的那一段。
		var dir service.Directory
		// 解不开就停住，避免把坏包当成成功。
		if json.Unmarshal([]byte(body), &dir) == nil && len(dir.Factories) == 1 && dir.Factories[0].WebVersion == 2 && dir.Factories[0].ServiceVersion == 3 && dir.Factories[0].WebVersionName == "1.1.0" && dir.Factories[0].ServiceVersionName == "1.2.0" {
			// 主动断开，用来断言掉线后的名录。
			cli.Disconnect(250)
			// 等到名录反映在线或离线，超时失败。
			assertOnline(t, srv, tok, false)
			return
		}
		// 稍等再看，避免通知还没写进名录。
		time.Sleep(50 * time.Millisecond)
	}
	// 这一步失败就不能继续，以免误判通过。
	t.Fatal("directory missing factory release")
}

// 发布后厂能拉到密封闭包，正文不走明文。
func TestChannelPlatformClosure(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 订阅该厂下行，收不到通知就无法断言。
	cmds := subscribeDown(t, mqttAddr, fid, priv)

	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"下发焊","content":"plat-body"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 创建没成功，带上现场停测。
		t.Fatalf("create %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	pid := gjson(t, body, "id")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/publish", tok, `{"expected":1}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 发布没成功，带上现场停测。
		t.Fatalf("publish %d %s", code, body)
	}
	// 只认这一条下行，其余继续等。
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdClosure && c.AssetID == pid
	})
	// 拉密封闭包，明文出现就是失败。
	snap := pullClosure(t, srv, fid, priv, pid)
	// 拉到的内容是空的，厂端无法继续。
	if len(snap.Members) == 0 || !contentcrypt.IsEnvelope(snap.Members[0].Content) {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal("pull did not return sealed body")
	}

	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"另一焊","content":"other-body"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 创建没成功，带上现场停测。
		t.Fatalf("create other %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	pid2 := gjson(t, body, "id")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+pid2+"/publish", tok, `{"expected":1}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 发布没成功，带上现场停测。
		t.Fatalf("publish other %d %s", code, body)
	}
	// 只认这一条下行，其余继续等。
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdClosure && c.AssetID == pid2
	})

	// 取出字段供后面使用，缺了就停测。
	rev := gjson(t, doBody(t, srv, "GET", "/v1/assets/"+pid, tok), "revision")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/rename", tok, `{"expected":`+rev+`,"name":"改名焊"}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("rename %d %s", code, body)
	}
	// 先当没有误发另一份，看见再记下来。
	resentOther := false
	// 只认这一条下行，其余继续等。
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		// 另一份也重发了就记下来，改名不应牵动它。
		if c.Typ == service.CmdClosure && c.AssetID == pid2 {
			// 改名却把另一份也重发了，记下来以便停测。
			resentOther = true
			return true
		}
		return c.Typ == service.CmdClosure && c.AssetID == pid
	})
	// 不该重发的那份又出现了，就停测。
	if resentOther {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal("rename resent other asset")
	}

	// 拉当前清单，看指令在不在。
	idx := pullIndex(t, srv, fid, priv, "process")
	// 清单里缺这条或仍留着不该留的指令。
	if !indexHas(idx, service.CmdClosure, pid) || !indexHas(idx, service.CmdClosure, pid2) {
		// 指令清单不对，带上现场停测。
		t.Fatalf("sync index missing assets: %+v", idx)
	}

	// 取出字段供后面使用，缺了就停测。
	rev = gjson(t, body, "revision")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/disable", tok, `{"expected":`+rev+`}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("disable %d %s", code, body)
	}
	// 只认这一条下行，其余继续等。
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdClosure && c.AssetID == pid
	})

	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/delete", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 删除结果不对，带上现场停测。
		t.Fatalf("delete %d %s", code, body)
	}
	// 只认这一条下行，其余继续等。
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdRetract && c.AssetID == pid
	})
}

// 先拿租约，再补放尚未收到的闭包。
func TestChannelLeaseBeforeReplay(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 建好并发布，失败就不要继续拉。
	_, body := doSetup(t, svc, tok, `{"kind":"process","name":"握手焊","content":"plat-body"}`)
	// 取出字段供后面使用，缺了就停测。
	pid := gjson(t, body, "id")
	// 起通道和临时云端，测完都会关掉。
	srv, _ := startChannel(t, svc)
	// 先拉租约，没有租约不应放正文。
	lease := pullLease(t, srv, fid, priv)
	// 拉到的内容是空的，厂端无法继续。
	if len(lease) == 0 {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal("empty lease")
	}
	// 拉当前清单，看指令在不在。
	idx := pullIndex(t, srv, fid, priv, "")
	// 清单里缺这条或仍留着不该留的指令。
	if !indexHas(idx, service.CmdClosure, pid) {
		// 指令清单不对，带上现场停测。
		t.Fatalf("index missing closure: %+v", idx)
	}
	// 拉密封闭包，明文出现就是失败。
	snap := pullClosure(t, srv, fid, priv, pid)
	// 拉到的内容是空的，厂端无法继续。
	if len(snap.Members) == 0 || !contentcrypt.IsEnvelope(snap.Members[0].Content) {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal("control plane must not replace HTTPS body")
	}
}

// 续租时把未收的闭包再放一次。
func TestChannelRenewReplaysClosures(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 建好并发布，失败就不要继续拉。
	_, body := doSetup(t, svc, tok, `{"kind":"process","name":"续期焊","content":"plat-body"}`)
	// 取出字段供后面使用，缺了就停测。
	pid := gjson(t, body, "id")
	// 起通道和临时云端，测完都会关掉。
	srv, _ := startChannel(t, svc)
	// 先拉租约，没有租约不应放正文。
	_ = pullLease(t, srv, fid, priv)
	// 拉密封闭包，明文出现就是失败。
	_ = pullClosure(t, srv, fid, priv, pid)
	// 先拉租约，没有租约不应放正文。
	_ = pullLease(t, srv, fid, priv)
	// 拉当前清单，看指令在不在。
	idx := pullIndex(t, srv, fid, priv, "")
	// 清单里缺这条或仍留着不该留的指令。
	if !indexHas(idx, service.CmdClosure, pid) {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal("renew index missing closure")
	}
}

// 离线再上线后，缺的闭包要补上。
func TestChannelOfflineThenReplay(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 用厂钥连上通道，失败就停测。
	cli := connectMQTT(t, mqttAddr, fid, priv)
	// 等到名录反映在线或离线，超时失败。
	assertOnline(t, srv, tok, true)
	// 主动断开，用来断言掉线后的名录。
	cli.Disconnect(250)
	// 等到名录反映在线或离线，超时失败。
	assertOnline(t, srv, tok, false)

	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"离线焊","content":"plat-body"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 创建没成功，带上现场停测。
		t.Fatalf("create %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	pid := gjson(t, body, "id")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/publish", tok, `{"expected":1}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 发布没成功，带上现场停测。
		t.Fatalf("publish %d %s", code, body)
	}

	// 用厂钥连上通道，失败就停测。
	_ = connectMQTT(t, mqttAddr, fid, priv)
	// 拉当前清单，看指令在不在。
	idx := pullIndex(t, srv, fid, priv, "")
	// 清单里缺这条或仍留着不该留的指令。
	if !indexHas(idx, service.CmdClosure, pid) {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal("offline factory did not get closure on reconnect index")
	}
	// 拉密封闭包，明文出现就是失败。
	snap := pullClosure(t, srv, fid, priv, pid)
	// 拉到的内容是空的，厂端无法继续。
	if len(snap.Members) == 0 || !contentcrypt.IsEnvelope(snap.Members[0].Content) {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal("reconnect pull missing envelope")
	}
}

// 停用和注销会推给当时在线的厂。
func TestChannelLifecycle(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 订阅该厂下行，收不到通知就无法断言。
	cmds := subscribeDown(t, mqttAddr, fid, priv)
	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/factories/"+fid.String()+"/disable", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("disable %d %s", code, body)
	}
	// 只认这一条下行，其余继续等。
	st := waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdFactoryState && c.Status == "disabled" && c.Revision == 1
	})
	// 没等到治理指令，厂端收不到停用。
	if st.Typ == "" {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal("no factory_state")
	}
}

// 分配设备后，厂端收到绑定指令。
func TestChannelClientBind(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 订阅该厂下行，收不到通知就无法断言。
	cmds := subscribeDown(t, mqttAddr, fid, priv)
	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/clients", tok, `{"name":"焊机-1","deviceSerial":"ARM-1","factoryId":"`+fid.String()+`"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("register %d %s", code, body)
	}
	// 只认这一条下行，其余继续等。
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdClientBind && c.ClientName == "焊机-1" && c.DeviceSerial == "ARM-1" && c.BindingRevision == 1
	})
}

// 经通道把厂级升成平台级草稿。
func TestPromoteViaChannel(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 留下厂或资产身份，后面用来认领或升档。
	aid := fid
	// 算出摘要，厂端回包要对得上。
	sum := sha256.Sum256([]byte("from-fac"))
	// 用厂钥连上通道，失败就停测。
	cli := connectMQTT(t, mqttAddr, fid, priv)
	// 收下下行，坏包丢掉，缓冲满了就弃。
	tokSub := cli.Subscribe("wan/"+fid.String()+"/down", 1, func(_ mqtt.Client, m mqtt.Message) {
		// 接住解开的指令或快照，坏包就丢掉。
		var cmd service.Cmd
		// 解不开就停住，避免把坏包当成成功。
		if json.Unmarshal(m.Payload(), &cmd) != nil {
			return
		}
		// 不是升档问询就忽略，避免误回。
		if cmd.Typ != service.CmdAssetList && cmd.Typ != service.CmdAssetSnapshot {
			return
		}
		// 按问询准备回包，其他指令不回答。
		reply := service.Cmd{ReqID: cmd.ReqID}
		// 问列表就回可升档元数据，不含正文。
		if cmd.Typ == service.CmdAssetList {
			// 按问询类型填回包，列表和快照不能混。
			reply.Typ = service.CmdAssetListOK
			// 收成 JSON，编不出就不要发出去。
			reply.Assets, _ = json.Marshal([]map[string]any{{
				"id": aid.String(), "kind": "process", "name": "厂级焊", "revision": 1,
				"digest": sum[:], "status": "available", "copyable": true,
			}})
			// 问的是快照就回厂级正文，列表走另一支。
		} else {
			// 按问询类型填回包，列表和快照不能混。
			reply.Typ = service.CmdAssetSnapOK
			// 收成 JSON，编不出就不要发出去。
			reply.Snapshot, _ = json.Marshal(map[string]any{
				"sourceId": aid.String(), "sourceRevision": 1, "sourceFactoryId": fid.String(),
				"kind": "process", "name": "厂级焊", "content": []byte("from-fac"), "digest": sum[:],
				"copyable": true, "status": "available", "deps": []any{},
			})
		}
		// 收成 JSON，编不出就不要发出去。
		raw, _ := json.Marshal(reply)
		// 把应答或上报发回云端。
		cli.Publish("wan/"+fid.String()+"/up", 1, false, raw)
	})
	// 时限内没完成连接或订阅，就停测。
	if !tokSub.WaitTimeout(5*time.Second) || tokSub.Error() != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(tokSub.Error())
	}
	// 等到名录反映在线或离线，超时失败。
	assertOnline(t, srv, tok, true)

	// 只等一小段，超时就当没有变化。
	deadline := time.Now().Add(5 * time.Second)
	// 先留着状态，时限内成功再停止等待。
	var code int
	// 先留着正文，时限内出现目标再停止等待。
	var resp string
	// 在时限内反复看，超时再判失败。
	for time.Now().Before(deadline) {
		// 发出请求并记下状态，对不上就停测。
		code, resp = do(t, srv, "GET", "/v1/factories/"+fid.String()+"/promotable-assets?kind=process", tok, "")
		// 这一步没成功就停测，避免误判通过。
		if code == http.StatusOK && strings.Contains(resp, "厂级焊") {
			break
		}
		// 稍等再看，避免通知还没写进名录。
		time.Sleep(50 * time.Millisecond)
	}
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK || !strings.Contains(resp, "厂级焊") {
		// 列表结果不对，带上现场停测。
		t.Fatalf("list %d %s", code, resp)
	}
	// 发出请求并记下状态，对不上就停测。
	code, resp = do(t, srv, "POST", "/v1/assets/promote-from", tok, `{"factoryId":"`+fid.String()+`","assetId":"`+aid.String()+`"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated || !strings.Contains(resp, `"level":"platform"`) || !strings.Contains(resp, `"status":"draft"`) {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("promote-from %d %s", code, resp)
	}
}

// 改模版后厂收到字段表，并可拉回。
func TestChannelTemplate(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 订阅该厂下行，收不到通知就无法断言。
	cmds := subscribeDown(t, mqttAddr, fid, priv)
	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "GET", "/v1/templates?kind=process", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("get tpl %d %s", code, body)
	}
	// 接住响应里要核对的那一段。
	var tpl struct {
		ID       string          `json:"id"`       // 模版的稳定身份，拉正文时要对上。
		Revision int64           `json:"revision"` // 当前修订，对不上就拒绝保存。
		Schema   json.RawMessage `json:"schema"`   // 字段表原文，坏表由服务拒绝。
	}
	// 解不开就停住，避免把坏包当成成功。
	if err := json.Unmarshal([]byte(body), &tpl); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 解开当前字段表，准备再加一个字段。
	var sch contenttpl.Schema
	// 解不开就停住，避免把坏包当成成功。
	if err := json.Unmarshal(tpl.Schema, &sch); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 加一个字段，用来断言模版修订会下发。
	sch.Fields = append(sch.Fields, contenttpl.Field{Key: "gas", Label: "气体", Type: contenttpl.TypeNumber, Unit: "L/min", Default: 15.0})
	// 收成 JSON，编不出就不要发出去。
	raw, err := json.Marshal(sch)
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/templates", tok, `{"kind":"process","expected":`+strconv.FormatInt(tpl.Revision, 10)+`,"schema":`+string(raw)+`}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("update tpl %d %s", code, body)
	}
	// 只认这一条下行，其余继续等。
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdTemplate && c.TemplateID == tpl.ID
	})
	// 用厂钥去拉，不带管理员会话。
	res := factoryReq(t, srv, http.MethodGet, "/v1/channel/pull/template/"+tpl.ID, fid, priv)
	// 用完就关上，避免连接或文件一直占着。
	defer res.Body.Close()
	// 读完全部正文，读失败就停测。
	got, _ := io.ReadAll(res.Body)
	// 没成功就停测，避免把失败响应当成数据。
	if res.StatusCode != http.StatusOK || !strings.Contains(string(got), `"kind":"process"`) {
		// 拉取结果不对，带上现场停测。
		t.Fatalf("pull tpl %d %s", res.StatusCode, got)
	}
}

// 发布厂包只通知元数据，正文不进通道。
func TestChannelSoftware(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 给下行留缓冲，测例再按类型来取。
	rawCh := make(chan []byte, 8)
	// 用厂钥连上通道，失败就停测。
	cli := connectMQTT(t, mqttAddr, fid, priv)
	// 收下下行，坏包丢掉，缓冲满了就弃。
	sub := cli.Subscribe("wan/"+fid.String()+"/down", 1, func(_ mqtt.Client, m mqtt.Message) {
		// 要么收下一条，要么到点判失败。
		select {
		// 缓冲还空就送进去，满了就丢掉。
		case rawCh <- append([]byte(nil), m.Payload()...):
		// 对不上的错误当内部故障，不把原文抛出。
		default:
		}
	})
	// 时限内没完成连接或订阅，就停测。
	if !sub.WaitTimeout(5*time.Second) || sub.Error() != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(sub.Error())
	}
	// 把包收成文本放进 JSON，超限会被拒绝。
	pkg := base64.StdEncoding.EncodeToString([]byte("svc-mqtt-body"))
	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/software", tok, `{"kind":"factory_service","version":1,"versionName":"1.0.0","content":"`+pkg+`"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 发布没成功，带上现场停测。
		t.Fatalf("publish %d %s", code, body)
	}
	// 到点还没出现就判失败，避免挂死。
	deadline := time.After(2 * time.Second)
	// 先当还没看见通知，看见再停循环。
	var saw bool
	// 直到看见厂包通知或超时，避免漏收。
	for !saw {
		// 要么收下一条，要么到点判失败。
		select {
		// 收到一条就交给后面的条件筛选。
		case raw := <-rawCh:
			// 响应里缺该有的内容，或混进了不该有的。
			if strings.Contains(string(raw), "svc-mqtt-body") {
				// 这一步失败就不能继续，以免误判通过。
				t.Fatal("software body leaked into mqtt")
			}
			// 接住解开的指令或快照，坏包就丢掉。
			var c service.Cmd
			// 解不开就停住，避免把坏包当成成功。
			if json.Unmarshal(raw, &c) == nil && c.Typ == service.CmdSoftware && c.Kind == "factory_service" && c.Version == 1 {
				// 已经看见厂包通知，可以停止等待。
				saw = true
			}
		// 到点还没有就失败，避免测试挂死。
		case <-deadline:
			// 这一步失败就不能继续，以免误判通过。
			t.Fatal("no factory software mqtt")
		}
	}
	// 用厂钥去拉，不带管理员会话。
	res := factoryReq(t, srv, http.MethodGet, "/v1/software/latest?kind=factory_service", fid, priv)
	// 用完就关上，避免连接或文件一直占着。
	defer res.Body.Close()
	// 读完全部正文，读失败就停测。
	got, _ := io.ReadAll(res.Body)
	// 没成功就停测，避免把失败响应当成数据。
	if res.StatusCode != http.StatusOK || !strings.Contains(string(got), `"kind":"factory_service"`) {
		// 最高版本不对，带上现场停测。
		t.Fatalf("latest %d %s", res.StatusCode, got)
	}
	// 用厂钥去拉，不带管理员会话。
	res = factoryReq(t, srv, http.MethodGet, "/v1/channel/pull/software?kind=factory_service&version=1", fid, priv)
	// 用完就关上，避免连接或文件一直占着。
	defer res.Body.Close()
	// 读完全部正文，读失败就停测。
	got, _ = io.ReadAll(res.Body)
	// 没成功就停测，避免把失败响应当成数据。
	if res.StatusCode != http.StatusOK || !strings.Contains(string(got), `"kind":"factory_service"`) {
		// 拉取结果不对，带上现场停测。
		t.Fatalf("pull software %d %s", res.StatusCode, got)
	}
	// 拉当前清单，看指令在不在。
	idx := pullIndex(t, srv, fid, priv, "")
	// 清单里缺这条或仍留着不该留的指令。
	if indexHasKind(idx, "software", "factory_service") {
		// 指令清单不对，带上现场停测。
		t.Fatalf("index still has software: %+v", idx)
	}
}

// 客户端包只通知该厂，另一厂也能收到自己的。
func TestChannelClientAPKNotify(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 再认领一家厂，用来看通知有没有串厂。
	fidB, privB := enrollAnother(t, svc, tok, "厂B", "sa-b")
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 订阅该厂下行，收不到通知就无法断言。
	cmdsA := subscribeDown(t, mqttAddr, fid, priv)
	// 订阅该厂下行，收不到通知就无法断言。
	cmdsB := subscribeDown(t, mqttAddr, fidB, privB)
	// 把包收成文本放进 JSON，超限会被拒绝。
	pkg := base64.StdEncoding.EncodeToString([]byte("apk-mqtt-body"))
	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/software", tok, `{"kind":"client_apk","version":8,"versionName":"6.8.0","content":"`+pkg+`"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 发布没成功，带上现场停测。
		t.Fatalf("publish %d %s", code, body)
	}
	// 只认这一条下行，其余继续等。
	gotA := waitMQTT(t, cmdsA, func(c service.Cmd) bool {
		return c.Typ == service.CmdSoftware && c.Kind == "client_apk" && c.Version == 8
	})
	// 只认这一条下行，其余继续等。
	gotB := waitMQTT(t, cmdsB, func(c service.Cmd) bool {
		return c.Typ == service.CmdSoftware && c.Kind == "client_apk" && c.Version == 8
	})
	// 两侧通知的版本名不一致，就停测。
	if gotA.VersionName != "6.8.0" || gotB.VersionName != "6.8.0" {
		// 结果和点名或全部的预期不一致。
		t.Fatalf("meta %+v %+v", gotA, gotB)
	}
	// 用厂钥去拉，不带管理员会话。
	res := factoryReq(t, srv, http.MethodGet, "/v1/channel/pull/software?kind=client_apk&version=8", fid, priv)
	// 用完就关上，避免连接或文件一直占着。
	defer res.Body.Close()
	// 读完全部正文，读失败就停测。
	got, _ := io.ReadAll(res.Body)
	// 没成功就停测，避免把失败响应当成数据。
	if res.StatusCode != http.StatusOK || !strings.Contains(string(got), `"kind":"client_apk"`) {
		// 拉取结果不对，带上现场停测。
		t.Fatalf("pull apk %d %s", res.StatusCode, got)
	}
	// 拉当前清单，看指令在不在。
	idx := pullIndex(t, srv, fid, priv, "")
	// 清单里缺这条或仍留着不该留的指令。
	if indexHasKind(idx, "software", "client_apk") {
		// 指令清单不对，带上现场停测。
		t.Fatalf("index has software: %+v", idx)
	}
}

// 改分先让旧厂作废，再让新厂绑定。
func TestChannelClientRebind(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fidA, privA := enrolledFactory(t)
	// 再认领一家厂，用来看通知有没有串厂。
	fidB, privB := enrollAnother(t, svc, tok, "厂B", "sa-b")
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 订阅该厂下行，收不到通知就无法断言。
	cmdsA := subscribeDown(t, mqttAddr, fidA, privA)
	// 订阅该厂下行，收不到通知就无法断言。
	cmdsB := subscribeDown(t, mqttAddr, fidB, privB)
	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/clients", tok, `{"name":"焊机-1","deviceSerial":"ARM-1","factoryId":"`+fidA.String()+`"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("register %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	cid := gjson(t, body, "id")
	// 只认这一条下行，其余继续等。
	waitMQTT(t, cmdsA, func(c service.Cmd) bool {
		return c.Typ == service.CmdClientBind && c.ClientID == cid
	})
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/clients/"+cid+"/rebind", tok, `{"factoryId":"`+fidB.String()+`"}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("rebind %d %s", code, body)
	}
	// 只认这一条下行，其余继续等。
	waitMQTT(t, cmdsA, func(c service.Cmd) bool {
		return c.Typ == service.CmdClientVoid && c.ClientID == cid
	})
	// 只认这一条下行，其余继续等。
	waitMQTT(t, cmdsB, func(c service.Cmd) bool {
		return c.Typ == service.CmdClientBind && c.ClientID == cid
	})
}

// 重新启用会推给当时在线的厂。
func TestChannelLifecycleEnable(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 订阅该厂下行，收不到通知就无法断言。
	cmds := subscribeDown(t, mqttAddr, fid, priv)
	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/factories/"+fid.String()+"/disable", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("disable %d %s", code, body)
	}
	// 只认这一条下行，其余继续等。
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdFactoryState && c.Status == "disabled"
	})
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/factories/"+fid.String()+"/enable", tok, "")
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("enable %d %s", code, body)
	}
	// 只认这一条下行，其余继续等。
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdFactoryState && c.Status == "active" && c.Revision == 2
	})
}

// 工程发布后厂能拉到带依赖的闭包。
func TestChannelProject(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 订阅该厂下行，收不到通知就无法断言。
	cmds := subscribeDown(t, mqttAddr, fid, priv)
	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"下发焊","content":"plat-body"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 创建没成功，带上现场停测。
		t.Fatalf("create proc %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	pid := gjson(t, body, "id")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/publish", tok, `{"expected":1}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 发布没成功，带上现场停测。
		t.Fatalf("publish proc %d %s", code, body)
	}
	// 只认这一条下行，其余继续等。
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdClosure && c.AssetID == pid
	})
	// 只取元数据正文，读失败由里面停测。
	meta := doBody(t, srv, "GET", "/v1/assets/"+pid, tok)
	// 取出字段供后面使用，缺了就停测。
	digest := gjson(t, meta, "digest")
	// 取出字段供后面使用，缺了就停测。
	rev := gjson(t, meta, "revision")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets", tok, `{"kind":"project","name":"平台工程","content":"job","deps":[{"id":"`+pid+`","revision":`+rev+`,"digest":"`+digest+`"}]}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 创建没成功，带上现场停测。
		t.Fatalf("create project %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	projID := gjson(t, body, "id")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+projID+"/publish", tok, `{"expected":1}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 发布没成功，带上现场停测。
		t.Fatalf("publish project %d %s", code, body)
	}
	// 只认这一条下行，其余继续等。
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdClosure && c.AssetID == projID && c.Kind == "project"
	})
	// 拉密封闭包，明文出现就是失败。
	snap := pullClosure(t, srv, fid, priv, projID)
	// 拉到的内容是空的，厂端无法继续。
	if len(snap.Members) == 0 {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal("project pull empty")
	}
}

// 回连清单里要带上设备绑定。
func TestChannelClientBindOnIndex(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, _ := startChannel(t, svc)
	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/clients", tok, `{"name":"焊机-1","deviceSerial":"ARM-1","factoryId":"`+fid.String()+`"}`)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 结果和预期不一致，带上现场停测。
		t.Fatalf("register %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	cid := gjson(t, body, "id")
	// 拉当前清单，看指令在不在。
	idx := pullIndex(t, srv, fid, priv, "")
	// 先当清单里没有绑定，找到再改。
	found := false
	// 在清单里找目标指令，没有就判缺失。
	for _, c := range idx {
		// 类型和设备都对上，才算绑定已经在清单里。
		if c.Typ == service.CmdClientBind && c.ClientID == cid {
			// 已经看见绑定，不必再往下找。
			found = true
		}
	}
	// 回连清单里没有绑定，设备对不上本机。
	if !found {
		// 指令清单不对，带上现场停测。
		t.Fatalf("index missing bind: %+v", idx)
	}
}

// 准备已认领的厂和管理员会话。
func enrolledFactory(t *testing.T) (*service.Service, string, uuid.UUID, []byte) {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
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
	// 建一家厂，后面用建厂码或身份继续。
	created, err := svc.CreateFactory(context.Background(), tok, "厂A", "sa-a", "超管A")
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 生成厂钥，私钥只留在本测。
	pub, priv, err := nodekey.Generate()
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 交钥没登记成功，认领就不能算完成。
	if err := svc.ConfirmEnroll(context.Background(), created.Factory.ID, pub); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	return svc, tok, created.Factory.ID, priv
}

// 再认领一家厂，用来对照另一侧。
func enrollAnother(t *testing.T, svc *service.Service, tok, name, login string) (uuid.UUID, []byte) {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 建一家厂，后面用建厂码或身份继续。
	created, err := svc.CreateFactory(context.Background(), tok, name, login, "超管")
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 生成厂钥，私钥只留在本测。
	pub, priv, err := nodekey.Generate()
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 交钥没登记成功，认领就不能算完成。
	if err := svc.ConfirmEnroll(context.Background(), created.Factory.ID, pub); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	return created.Factory.ID, priv
}

// 在本机起通道和临时云端，测完关掉。
func startChannel(t *testing.T, svc *service.Service) (*httptest.Server, string) {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 在本机随机口听通道，供厂端连入。
	bus, err := mqttbroker.Listen("127.0.0.1:0", mqttbroker.Hooks{
		// 厂钥对不上就拒绝连入通道。
		Auth: func(factoryID uuid.UUID, unix int64, sig []byte) error {
			// 核对接线证明，对不上就拒绝连接。
			return svc.Channel.VerifyFactoryProof(context.Background(), factoryID, unix, sig)
		},
		// 连上之后把该厂标成在线。
		Online: func(factoryID uuid.UUID) {
			// 标成在线，失败不打断这次接线。
			_ = svc.MarkChannelOnline(context.Background(), factoryID)
		},
		// 断开之后把该厂标成离线。
		Offline: func(factoryID uuid.UUID) {
			// 标成离线，失败不打断这次断开。
			_ = svc.MarkChannelOffline(context.Background(), factoryID)
		},
		// 厂端上行交给通道，不在这里改业务。
		Up: func(factoryID uuid.UUID, payload []byte) {
			// 上行交给通道处理，不在适配层改业务。
			svc.Channel.HandleFactoryUp(context.Background(), factoryID, payload)
		},
	})
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 测完关掉通道，避免端口继续占着。
	t.Cleanup(func() { _ = bus.Close() })
	// 让服务走这块通道，通知才能到达厂端。
	svc.SetBus(bus)
	// 起临时云端，只在本测里访问。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)
	// 把临时服务和通道地址交给后面的连接。
	return srv, bus.Addr()
}

// 用厂钥连上通道，签错就停测。
func connectMQTT(t *testing.T, addr string, fid uuid.UUID, priv []byte) mqtt.Client {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 准备连接参数，签错就不要订阅。
	opts := mqtt.NewClientOptions()
	// 指到本机通道，不连外网。
	opts.AddBroker("tcp://" + addr)
	// 用厂身份标明连接，便于通道认出是谁。
	opts.SetClientID(fid.String())
	// 用厂身份标明连接，便于通道认出是谁。
	opts.SetUsername(fid.String())
	// 用厂钥签口令，签错就连不上。
	opts.SetPassword(nodekey.SignMQTTPassword(priv, fid, time.Now().Unix()))
	// 保留会话，短暂断开后还能补指令。
	opts.SetCleanSession(false)
	// 关掉自动重连，掉线由测例自己断言。
	opts.SetAutoReconnect(false)
	// 建通道客户端，连上后再收下行。
	cli := mqtt.NewClient(opts)
	// 发起连接，超时或被拒绝就停测。
	tok := cli.Connect()
	// 时限内没完成连接或订阅，就停测。
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		// 通道没连上，就停测。
		t.Fatalf("mqtt connect: %v", tok.Error())
	}
	// 测完断开客户端，避免会话留到下一例。
	t.Cleanup(func() { cli.Disconnect(250) })
	return cli
}

// 订阅下行并收成指令，坏包丢掉。
func subscribeDown(t *testing.T, addr string, fid uuid.UUID, priv []byte) <-chan service.Cmd {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 用厂钥连上通道，失败就停测。
	cli := connectMQTT(t, addr, fid, priv)
	// 给下行留缓冲，测例再按类型来取。
	ch := make(chan service.Cmd, 32)
	// 收下下行，坏包丢掉，缓冲满了就弃。
	tok := cli.Subscribe("wan/"+fid.String()+"/down", 1, func(_ mqtt.Client, m mqtt.Message) {
		// 接住解开的指令或快照，坏包就丢掉。
		var cmd service.Cmd
		// 解不开就停住，避免把坏包当成成功。
		if json.Unmarshal(m.Payload(), &cmd) != nil {
			return
		}
		// 要么收下一条，要么到点判失败。
		select {
		// 缓冲还空就送进去，满了就丢掉。
		case ch <- cmd:
		// 对不上的错误当内部故障，不把原文抛出。
		default:
		}
	})
	// 时限内没完成连接或订阅，就停测。
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(tok.Error())
	}
	return ch
}

// 等到匹配的下行，超时就停测。
func waitMQTT(t *testing.T, ch <-chan service.Cmd, match func(service.Cmd) bool) service.Cmd {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 到点还没出现就判失败，避免挂死。
	deadline := time.After(5 * time.Second)
	// 一直等下一条，超时由另一支打断。
	for {
		// 要么收下一条，要么到点判失败。
		select {
		// 收到一条就交给后面的条件筛选。
		case c := <-ch:
			// 对上了就返回，没对上继续等下一条。
			if match(c) {
				return c
			}
		// 到点还没有就失败，避免测试挂死。
		case <-deadline:
			// 这一步失败就不能继续，以免误判通过。
			t.Fatal("no mqtt cmd")
			return service.Cmd{}
		}
	}
}

// 带厂钥签名去拉云端，不带会话。
func factoryReq(t *testing.T, srv *httptest.Server, method, path string, fid uuid.UUID, priv []byte) *http.Response {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 组好请求，组失败就停测。
	req, err := http.NewRequest(method, srv.URL+path, nil)
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	// 用当前时刻签名，过期会被拒绝。
	unix := time.Now().Unix()
	// 用厂钥签时刻，云端用来认厂。
	sig := nodekey.Sign(priv, nodekey.MQTTConnectPayload(fid, unix))
	// 补上厂钥或会话，缺了应被拒绝。
	req.Header.Set("X-WMesh-Factory", fid.String())
	// 带上签名时刻，过期会被拒绝。
	req.Header.Set("X-WMesh-Time", strconv.FormatInt(unix, 10))
	// 带上厂钥签名，对不上就未授权。
	req.Header.Set("X-WMesh-Sign", base64.StdEncoding.EncodeToString(sig))
	// 发出去，连不上就停测。
	res, err := http.DefaultClient.Do(req)
	// 这一步失败就不能继续，以免误判通过。
	if err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	return res
}

// 拉内容租约，失败就停测。
func pullLease(t *testing.T, srv *httptest.Server, fid uuid.UUID, priv []byte) []byte {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 用厂钥去拉，不带管理员会话。
	res := factoryReq(t, srv, http.MethodGet, "/v1/channel/lease", fid, priv)
	// 用完就关上，避免连接或文件一直占着。
	defer res.Body.Close()
	// 读完全部正文，读失败就停测。
	body, _ := io.ReadAll(res.Body)
	// 没成功就停测，避免把失败响应当成数据。
	if res.StatusCode != http.StatusOK {
		// 租约没拉到，带上现场停测。
		t.Fatalf("lease %d %s", res.StatusCode, body)
	}
	// 接住响应里要核对的那一段。
	var out struct {
		Lease []byte `json:"lease"` // 内容租约钥，过期后正文解不开。
	}
	// 解不开就停住，避免把坏包当成成功。
	if err := json.Unmarshal(body, &out); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	return out.Lease
}

// 拉当前指令清单，失败就停测。
func pullIndex(t *testing.T, srv *httptest.Server, fid uuid.UUID, priv []byte, kind string) []service.Cmd {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 留下路径，用来判断是厂还是现场设备。
	path := "/v1/channel/index"
	if kind != "" {
		// 带上种类过滤，空种类则要全部指令。
		path += "?kind=" + kind
	}
	// 用厂钥去拉，不带管理员会话。
	res := factoryReq(t, srv, http.MethodGet, path, fid, priv)
	// 用完就关上，避免连接或文件一直占着。
	defer res.Body.Close()
	// 读完全部正文，读失败就停测。
	body, _ := io.ReadAll(res.Body)
	// 没成功就停测，避免把失败响应当成数据。
	if res.StatusCode != http.StatusOK {
		// 指令清单不对，带上现场停测。
		t.Fatalf("index %d %s", res.StatusCode, body)
	}
	// 接住响应里要核对的那一段。
	var out struct {
		Cmds []service.Cmd `json:"cmds"` // 当前该厂该有的指令，不含正文。
	}
	// 解不开就停住，避免把坏包当成成功。
	if err := json.Unmarshal(body, &out); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	return out.Cmds
}

// 拉一条密封闭包，失败就停测。
func pullClosure(t *testing.T, srv *httptest.Server, fid uuid.UUID, priv []byte, assetID string) service.ClosureSnapshot {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 用厂钥去拉，不带管理员会话。
	res := factoryReq(t, srv, http.MethodGet, "/v1/channel/pull/closure/"+assetID, fid, priv)
	// 用完就关上，避免连接或文件一直占着。
	defer res.Body.Close()
	// 读完全部正文，读失败就停测。
	body, _ := io.ReadAll(res.Body)
	// 没成功就停测，避免把失败响应当成数据。
	if res.StatusCode != http.StatusOK {
		// 拉取结果不对，带上现场停测。
		t.Fatalf("pull closure %d %s", res.StatusCode, body)
	}
	// 接住升档快照，坏包按坏请求拒绝。
	var snap service.ClosureSnapshot
	// 解不开就停住，避免把坏包当成成功。
	if err := json.Unmarshal(body, &snap); err != nil {
		// 这一步失败就不能继续，以免误判通过。
		t.Fatal(err)
	}
	return snap
}

// 清单里是否已有这条资产指令。
func indexHas(cmds []service.Cmd, typ, assetID string) bool {
	// 在清单里找目标指令，没有就判缺失。
	for _, c := range cmds {
		// 类型和目标都对上才算厂端能看到。
		if c.Typ == typ && c.AssetID == assetID {
			return true
		}
	}
	return false
}

// 清单里是否已有这种指令。
func indexHasKind(cmds []service.Cmd, typ, kind string) bool {
	// 在清单里找目标指令，没有就判缺失。
	for _, c := range cmds {
		// 类型和目标都对上才算厂端能看到。
		if c.Typ == typ && c.Kind == kind {
			return true
		}
	}
	return false
}

// 建好并发布一条平台资产，失败就停测。
func doSetup(t *testing.T, svc *service.Service, tok, createJSON string) (int, string) {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 起临时云端，只在本测里访问。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)
	// 发出请求并记下状态，对不上就停测。
	code, body := do(t, srv, "POST", "/v1/assets", tok, createJSON)
	// 没建成就停测，避免把失败当成新记录。
	if code != http.StatusCreated {
		// 创建没成功，带上现场停测。
		t.Fatalf("create %d %s", code, body)
	}
	// 取出字段供后面使用，缺了就停测。
	pid := gjson(t, body, "id")
	// 发出请求并记下状态，对不上就停测。
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/publish", tok, `{"expected":1}`)
	// 这一步没成功就停测，避免误判通过。
	if code != http.StatusOK {
		// 发布没成功，带上现场停测。
		t.Fatalf("publish %d %s", code, body)
	}
	return code, body
}

// 只取响应正文，状态由调用方断言。
func doBody(t *testing.T, srv *httptest.Server, method, path, token string) string {
	// 标成辅助，失败行号落到调用它的测试。
	t.Helper()
	// 发出请求并记下状态，对不上就停测。
	_, body := do(t, srv, method, path, token, "")
	return body
}
