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

func TestChannelPresence(t *testing.T) {
	svc, tok, fid, priv := enrolledFactory(t)
	srv, mqttAddr := startChannel(t, svc)
	cli := connectMQTT(t, mqttAddr, fid, priv)
	assertOnline(t, srv, tok, true)
	cli.Disconnect(250)
	assertOnline(t, srv, tok, false)
}

func TestChannelPlatformClosure(t *testing.T) {
	svc, tok, fid, priv := enrolledFactory(t)
	srv, mqttAddr := startChannel(t, svc)
	cmds := subscribeDown(t, mqttAddr, fid, priv)

	code, body := do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"下发焊","content":"plat-body"}`)
	if code != http.StatusCreated {
		t.Fatalf("create %d %s", code, body)
	}
	pid := gjson(t, body, "id")
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/publish", tok, `{"expected":1}`)
	if code != http.StatusOK {
		t.Fatalf("publish %d %s", code, body)
	}
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdClosure && c.AssetID == pid
	})
	snap := pullClosure(t, srv, fid, priv, pid)
	if len(snap.Members) == 0 || !contentcrypt.IsEnvelope(snap.Members[0].Content) {
		t.Fatal("pull did not return sealed body")
	}

	code, body = do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"另一焊","content":"other-body"}`)
	if code != http.StatusCreated {
		t.Fatalf("create other %d %s", code, body)
	}
	pid2 := gjson(t, body, "id")
	code, body = do(t, srv, "POST", "/v1/assets/"+pid2+"/publish", tok, `{"expected":1}`)
	if code != http.StatusOK {
		t.Fatalf("publish other %d %s", code, body)
	}
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdClosure && c.AssetID == pid2
	})

	rev := gjson(t, doBody(t, srv, "GET", "/v1/assets/"+pid, tok), "revision")
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/rename", tok, `{"expected":`+rev+`,"name":"改名焊"}`)
	if code != http.StatusOK {
		t.Fatalf("rename %d %s", code, body)
	}
	resentOther := false
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		if c.Typ == service.CmdClosure && c.AssetID == pid2 {
			resentOther = true
			return true
		}
		return c.Typ == service.CmdClosure && c.AssetID == pid
	})
	if resentOther {
		t.Fatal("rename resent other asset")
	}

	idx := pullIndex(t, srv, fid, priv, "process")
	if !indexHas(idx, service.CmdClosure, pid) || !indexHas(idx, service.CmdClosure, pid2) {
		t.Fatalf("sync index missing assets: %+v", idx)
	}

	rev = gjson(t, body, "revision")
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/disable", tok, `{"expected":`+rev+`}`)
	if code != http.StatusOK {
		t.Fatalf("disable %d %s", code, body)
	}
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdClosure && c.AssetID == pid
	})

	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/delete", tok, "")
	if code != http.StatusOK {
		t.Fatalf("delete %d %s", code, body)
	}
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdRetract && c.AssetID == pid
	})
}

func TestChannelLeaseBeforeReplay(t *testing.T) {
	svc, tok, fid, priv := enrolledFactory(t)
	_, body := doSetup(t, svc, tok, `{"kind":"process","name":"握手焊","content":"plat-body"}`)
	pid := gjson(t, body, "id")
	srv, _ := startChannel(t, svc)
	lease := pullLease(t, srv, fid, priv)
	if len(lease) == 0 {
		t.Fatal("empty lease")
	}
	idx := pullIndex(t, srv, fid, priv, "")
	if !indexHas(idx, service.CmdClosure, pid) {
		t.Fatalf("index missing closure: %+v", idx)
	}
	snap := pullClosure(t, srv, fid, priv, pid)
	if len(snap.Members) == 0 || !contentcrypt.IsEnvelope(snap.Members[0].Content) {
		t.Fatal("control plane must not replace HTTPS body")
	}
}

func TestChannelRenewReplaysClosures(t *testing.T) {
	svc, tok, fid, priv := enrolledFactory(t)
	_, body := doSetup(t, svc, tok, `{"kind":"process","name":"续期焊","content":"plat-body"}`)
	pid := gjson(t, body, "id")
	srv, _ := startChannel(t, svc)
	_ = pullLease(t, srv, fid, priv)
	_ = pullClosure(t, srv, fid, priv, pid)
	_ = pullLease(t, srv, fid, priv)
	idx := pullIndex(t, srv, fid, priv, "")
	if !indexHas(idx, service.CmdClosure, pid) {
		t.Fatal("renew index missing closure")
	}
}

func TestChannelOfflineThenReplay(t *testing.T) {
	svc, tok, fid, priv := enrolledFactory(t)
	srv, mqttAddr := startChannel(t, svc)
	cli := connectMQTT(t, mqttAddr, fid, priv)
	assertOnline(t, srv, tok, true)
	cli.Disconnect(250)
	assertOnline(t, srv, tok, false)

	code, body := do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"离线焊","content":"plat-body"}`)
	if code != http.StatusCreated {
		t.Fatalf("create %d %s", code, body)
	}
	pid := gjson(t, body, "id")
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/publish", tok, `{"expected":1}`)
	if code != http.StatusOK {
		t.Fatalf("publish %d %s", code, body)
	}

	_ = connectMQTT(t, mqttAddr, fid, priv)
	idx := pullIndex(t, srv, fid, priv, "")
	if !indexHas(idx, service.CmdClosure, pid) {
		t.Fatal("offline factory did not get closure on reconnect index")
	}
	snap := pullClosure(t, srv, fid, priv, pid)
	if len(snap.Members) == 0 || !contentcrypt.IsEnvelope(snap.Members[0].Content) {
		t.Fatal("reconnect pull missing envelope")
	}
}

func TestChannelLifecycle(t *testing.T) {
	svc, tok, fid, priv := enrolledFactory(t)
	srv, mqttAddr := startChannel(t, svc)
	cmds := subscribeDown(t, mqttAddr, fid, priv)
	code, body := do(t, srv, "POST", "/v1/factories/"+fid.String()+"/disable", tok, "")
	if code != http.StatusOK {
		t.Fatalf("disable %d %s", code, body)
	}
	st := waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdFactoryState && c.Status == "disabled" && c.Revision == 1
	})
	if st.Typ == "" {
		t.Fatal("no factory_state")
	}
}

func TestChannelClientBind(t *testing.T) {
	svc, tok, fid, priv := enrolledFactory(t)
	srv, mqttAddr := startChannel(t, svc)
	cmds := subscribeDown(t, mqttAddr, fid, priv)
	code, body := do(t, srv, "POST", "/v1/clients", tok, `{"name":"焊机-1","factoryId":"`+fid.String()+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("register %d %s", code, body)
	}
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdClientBind && c.ClientName == "焊机-1" && c.BindingRevision == 1
	})
}

func TestPromoteViaChannel(t *testing.T) {
	svc, tok, fid, priv := enrolledFactory(t)
	srv, mqttAddr := startChannel(t, svc)
	aid := fid
	sum := sha256.Sum256([]byte("from-fac"))
	cli := connectMQTT(t, mqttAddr, fid, priv)
	tokSub := cli.Subscribe("wan/"+fid.String()+"/down", 1, func(_ mqtt.Client, m mqtt.Message) {
		var cmd service.Cmd
		if json.Unmarshal(m.Payload(), &cmd) != nil {
			return
		}
		if cmd.Typ != service.CmdAssetList && cmd.Typ != service.CmdAssetSnapshot {
			return
		}
		reply := service.Cmd{ReqID: cmd.ReqID}
		if cmd.Typ == service.CmdAssetList {
			reply.Typ = service.CmdAssetListOK
			reply.Assets, _ = json.Marshal([]map[string]any{{
				"id": aid.String(), "kind": "process", "name": "厂级焊", "revision": 1,
				"digest": sum[:], "status": "available", "copyable": true,
			}})
		} else {
			reply.Typ = service.CmdAssetSnapOK
			reply.Snapshot, _ = json.Marshal(map[string]any{
				"sourceId": aid.String(), "sourceRevision": 1, "sourceFactoryId": fid.String(),
				"kind": "process", "name": "厂级焊", "content": []byte("from-fac"), "digest": sum[:],
				"copyable": true, "status": "available", "deps": []any{},
			})
		}
		raw, _ := json.Marshal(reply)
		cli.Publish("wan/"+fid.String()+"/up", 1, false, raw)
	})
	if !tokSub.WaitTimeout(5*time.Second) || tokSub.Error() != nil {
		t.Fatal(tokSub.Error())
	}
	assertOnline(t, srv, tok, true)

	deadline := time.Now().Add(5 * time.Second)
	var code int
	var resp string
	for time.Now().Before(deadline) {
		code, resp = do(t, srv, "GET", "/v1/factories/"+fid.String()+"/promotable-assets?kind=process", tok, "")
		if code == http.StatusOK && strings.Contains(resp, "厂级焊") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if code != http.StatusOK || !strings.Contains(resp, "厂级焊") {
		t.Fatalf("list %d %s", code, resp)
	}
	code, resp = do(t, srv, "POST", "/v1/assets/promote-from", tok, `{"factoryId":"`+fid.String()+`","assetId":"`+aid.String()+`"}`)
	if code != http.StatusCreated || !strings.Contains(resp, `"level":"platform"`) || !strings.Contains(resp, `"status":"draft"`) {
		t.Fatalf("promote-from %d %s", code, resp)
	}
}

func TestChannelTemplate(t *testing.T) {
	svc, tok, fid, priv := enrolledFactory(t)
	srv, mqttAddr := startChannel(t, svc)
	cmds := subscribeDown(t, mqttAddr, fid, priv)
	code, body := do(t, srv, "GET", "/v1/templates?kind=process", tok, "")
	if code != http.StatusOK {
		t.Fatalf("get tpl %d %s", code, body)
	}
	var tpl struct {
		ID       string          `json:"id"`
		Revision int64           `json:"revision"`
		Schema   json.RawMessage `json:"schema"`
	}
	if err := json.Unmarshal([]byte(body), &tpl); err != nil {
		t.Fatal(err)
	}
	var sch contenttpl.Schema
	if err := json.Unmarshal(tpl.Schema, &sch); err != nil {
		t.Fatal(err)
	}
	sch.Fields = append(sch.Fields, contenttpl.Field{Key: "gas", Label: "气体", Type: contenttpl.TypeNumber, Unit: "L/min", Default: 15.0})
	raw, err := json.Marshal(sch)
	if err != nil {
		t.Fatal(err)
	}
	code, body = do(t, srv, "POST", "/v1/templates", tok, `{"kind":"process","expected":`+strconv.FormatInt(tpl.Revision, 10)+`,"schema":`+string(raw)+`}`)
	if code != http.StatusOK {
		t.Fatalf("update tpl %d %s", code, body)
	}
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdTemplate && c.TemplateID == tpl.ID
	})
	res := factoryReq(t, srv, http.MethodGet, "/v1/channel/pull/template/"+tpl.ID, fid, priv)
	defer res.Body.Close()
	got, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(got), `"kind":"process"`) {
		t.Fatalf("pull tpl %d %s", res.StatusCode, got)
	}
}

func TestChannelSoftware(t *testing.T) {
	svc, tok, fid, priv := enrolledFactory(t)
	srv, mqttAddr := startChannel(t, svc)
	rawCh := make(chan []byte, 8)
	cli := connectMQTT(t, mqttAddr, fid, priv)
	sub := cli.Subscribe("wan/"+fid.String()+"/down", 1, func(_ mqtt.Client, m mqtt.Message) {
		select {
		case rawCh <- append([]byte(nil), m.Payload()...):
		default:
		}
	})
	if !sub.WaitTimeout(5*time.Second) || sub.Error() != nil {
		t.Fatal(sub.Error())
	}
	pkg := base64.StdEncoding.EncodeToString([]byte("svc-mqtt-body"))
	code, body := do(t, srv, "POST", "/v1/software", tok, `{"kind":"factory_service","version":1,"versionName":"1.0.0","content":"`+pkg+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("publish %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/software/distribute", tok, `{"kind":"factory_service","version":1,"factoryId":"`+fid.String()+`"}`)
	if code != http.StatusOK {
		t.Fatalf("distribute %d %s", code, body)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case raw := <-rawCh:
			if strings.Contains(string(raw), "svc-mqtt-body") {
				t.Fatal("software body leaked into mqtt")
			}
			var c service.Cmd
			if json.Unmarshal(raw, &c) == nil && c.Typ == service.CmdSoftware && c.Kind == "factory_service" && c.Version == 1 {
				goto pulled
			}
		case <-deadline:
			t.Fatal("no software cmd")
		}
	}
pulled:
	res := factoryReq(t, srv, http.MethodGet, "/v1/channel/pull/software?kind=factory_service&version=1", fid, priv)
	defer res.Body.Close()
	got, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(got), `"kind":"factory_service"`) {
		t.Fatalf("pull software %d %s", res.StatusCode, got)
	}
	idx := pullIndex(t, srv, fid, priv, "")
	if !indexHasKind(idx, service.CmdSoftware, "factory_service") {
		t.Fatalf("index missing software: %+v", idx)
	}
}

func TestChannelClientRebind(t *testing.T) {
	svc, tok, fidA, privA := enrolledFactory(t)
	fidB, privB := enrollAnother(t, svc, tok, "厂B", "sa-b")
	srv, mqttAddr := startChannel(t, svc)
	cmdsA := subscribeDown(t, mqttAddr, fidA, privA)
	cmdsB := subscribeDown(t, mqttAddr, fidB, privB)
	code, body := do(t, srv, "POST", "/v1/clients", tok, `{"name":"焊机-1","factoryId":"`+fidA.String()+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("register %d %s", code, body)
	}
	cid := gjson(t, body, "id")
	waitMQTT(t, cmdsA, func(c service.Cmd) bool {
		return c.Typ == service.CmdClientBind && c.ClientID == cid
	})
	code, body = do(t, srv, "POST", "/v1/clients/"+cid+"/rebind", tok, `{"factoryId":"`+fidB.String()+`"}`)
	if code != http.StatusOK {
		t.Fatalf("rebind %d %s", code, body)
	}
	waitMQTT(t, cmdsA, func(c service.Cmd) bool {
		return c.Typ == service.CmdClientVoid && c.ClientID == cid
	})
	waitMQTT(t, cmdsB, func(c service.Cmd) bool {
		return c.Typ == service.CmdClientBind && c.ClientID == cid
	})
}

func TestChannelLifecycleEnable(t *testing.T) {
	svc, tok, fid, priv := enrolledFactory(t)
	srv, mqttAddr := startChannel(t, svc)
	cmds := subscribeDown(t, mqttAddr, fid, priv)
	code, body := do(t, srv, "POST", "/v1/factories/"+fid.String()+"/disable", tok, "")
	if code != http.StatusOK {
		t.Fatalf("disable %d %s", code, body)
	}
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdFactoryState && c.Status == "disabled"
	})
	code, body = do(t, srv, "POST", "/v1/factories/"+fid.String()+"/enable", tok, "")
	if code != http.StatusOK {
		t.Fatalf("enable %d %s", code, body)
	}
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdFactoryState && c.Status == "active" && c.Revision == 2
	})
}

func TestChannelProject(t *testing.T) {
	svc, tok, fid, priv := enrolledFactory(t)
	srv, mqttAddr := startChannel(t, svc)
	cmds := subscribeDown(t, mqttAddr, fid, priv)
	code, body := do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"下发焊","content":"plat-body"}`)
	if code != http.StatusCreated {
		t.Fatalf("create proc %d %s", code, body)
	}
	pid := gjson(t, body, "id")
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/publish", tok, `{"expected":1}`)
	if code != http.StatusOK {
		t.Fatalf("publish proc %d %s", code, body)
	}
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdClosure && c.AssetID == pid
	})
	meta := doBody(t, srv, "GET", "/v1/assets/"+pid, tok)
	digest := gjson(t, meta, "digest")
	rev := gjson(t, meta, "revision")
	code, body = do(t, srv, "POST", "/v1/assets", tok, `{"kind":"project","name":"平台工程","content":"job","deps":[{"id":"`+pid+`","revision":`+rev+`,"digest":"`+digest+`"}]}`)
	if code != http.StatusCreated {
		t.Fatalf("create project %d %s", code, body)
	}
	projID := gjson(t, body, "id")
	code, body = do(t, srv, "POST", "/v1/assets/"+projID+"/publish", tok, `{"expected":1}`)
	if code != http.StatusOK {
		t.Fatalf("publish project %d %s", code, body)
	}
	waitMQTT(t, cmds, func(c service.Cmd) bool {
		return c.Typ == service.CmdClosure && c.AssetID == projID && c.Kind == "project"
	})
	snap := pullClosure(t, srv, fid, priv, projID)
	if len(snap.Members) == 0 {
		t.Fatal("project pull empty")
	}
}

func TestChannelClientBindOnIndex(t *testing.T) {
	svc, tok, fid, priv := enrolledFactory(t)
	srv, _ := startChannel(t, svc)
	code, body := do(t, srv, "POST", "/v1/clients", tok, `{"name":"焊机-1","factoryId":"`+fid.String()+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("register %d %s", code, body)
	}
	cid := gjson(t, body, "id")
	idx := pullIndex(t, srv, fid, priv, "")
	found := false
	for _, c := range idx {
		if c.Typ == service.CmdClientBind && c.ClientID == cid {
			found = true
		}
	}
	if !found {
		t.Fatalf("index missing bind: %+v", idx)
	}
}

func enrolledFactory(t *testing.T) (*service.Service, string, uuid.UUID, []byte) {
	t.Helper()
	admin := testpg.Open(t)
	_, dsn := testpg.CreateDB(t, admin, "wmesh_wan")
	svc := service.NewService(store.Open(testpg.OpenMigrated(t, dsn)))
	if err := svc.BootstrapAdmin(context.Background(), "w", "wan-secret"); err != nil {
		t.Fatal(err)
	}
	tok, err := svc.Login(context.Background(), "w", "wan-secret")
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateFactory(context.Background(), tok, "厂A", "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmEnroll(context.Background(), created.Factory.ID, pub); err != nil {
		t.Fatal(err)
	}
	return svc, tok, created.Factory.ID, priv
}

func enrollAnother(t *testing.T, svc *service.Service, tok, name, login string) (uuid.UUID, []byte) {
	t.Helper()
	created, err := svc.CreateFactory(context.Background(), tok, name, login, "超管")
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmEnroll(context.Background(), created.Factory.ID, pub); err != nil {
		t.Fatal(err)
	}
	return created.Factory.ID, priv
}

func startChannel(t *testing.T, svc *service.Service) (*httptest.Server, string) {
	t.Helper()
	bus, err := mqttbroker.Listen("127.0.0.1:0", mqttbroker.Hooks{
		Auth: func(factoryID uuid.UUID, unix int64, sig []byte) error {
			return svc.Channel.VerifyFactoryProof(context.Background(), factoryID, unix, sig)
		},
		Online: func(factoryID uuid.UUID) {
			_ = svc.MarkChannelOnline(context.Background(), factoryID)
		},
		Offline: func(factoryID uuid.UUID) {
			_ = svc.MarkChannelOffline(context.Background(), factoryID)
		},
		Up: func(factoryID uuid.UUID, payload []byte) {
			svc.Channel.HandleFactoryUp(context.Background(), factoryID, payload)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bus.Close() })
	svc.SetBus(bus)
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	t.Cleanup(srv.Close)
	return srv, bus.Addr()
}

func connectMQTT(t *testing.T, addr string, fid uuid.UUID, priv []byte) mqtt.Client {
	t.Helper()
	opts := mqtt.NewClientOptions()
	opts.AddBroker("tcp://" + addr)
	opts.SetClientID(fid.String())
	opts.SetUsername(fid.String())
	opts.SetPassword(nodekey.SignMQTTPassword(priv, fid, time.Now().Unix()))
	opts.SetCleanSession(false)
	opts.SetAutoReconnect(false)
	cli := mqtt.NewClient(opts)
	tok := cli.Connect()
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("mqtt connect: %v", tok.Error())
	}
	t.Cleanup(func() { cli.Disconnect(250) })
	return cli
}

func subscribeDown(t *testing.T, addr string, fid uuid.UUID, priv []byte) <-chan service.Cmd {
	t.Helper()
	cli := connectMQTT(t, addr, fid, priv)
	ch := make(chan service.Cmd, 32)
	tok := cli.Subscribe("wan/"+fid.String()+"/down", 1, func(_ mqtt.Client, m mqtt.Message) {
		var cmd service.Cmd
		if json.Unmarshal(m.Payload(), &cmd) != nil {
			return
		}
		select {
		case ch <- cmd:
		default:
		}
	})
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatal(tok.Error())
	}
	return ch
}

func waitMQTT(t *testing.T, ch <-chan service.Cmd, match func(service.Cmd) bool) service.Cmd {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case c := <-ch:
			if match(c) {
				return c
			}
		case <-deadline:
			t.Fatal("no mqtt cmd")
			return service.Cmd{}
		}
	}
}

func factoryReq(t *testing.T, srv *httptest.Server, method, path string, fid uuid.UUID, priv []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	unix := time.Now().Unix()
	sig := nodekey.Sign(priv, nodekey.MQTTConnectPayload(fid, unix))
	req.Header.Set("X-WMesh-Factory", fid.String())
	req.Header.Set("X-WMesh-Time", strconv.FormatInt(unix, 10))
	req.Header.Set("X-WMesh-Sign", base64.StdEncoding.EncodeToString(sig))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func pullLease(t *testing.T, srv *httptest.Server, fid uuid.UUID, priv []byte) []byte {
	t.Helper()
	res := factoryReq(t, srv, http.MethodGet, "/v1/channel/lease", fid, priv)
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("lease %d %s", res.StatusCode, body)
	}
	var out struct {
		Lease []byte `json:"lease"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out.Lease
}

func pullIndex(t *testing.T, srv *httptest.Server, fid uuid.UUID, priv []byte, kind string) []service.Cmd {
	t.Helper()
	path := "/v1/channel/index"
	if kind != "" {
		path += "?kind=" + kind
	}
	res := factoryReq(t, srv, http.MethodGet, path, fid, priv)
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("index %d %s", res.StatusCode, body)
	}
	var out struct {
		Cmds []service.Cmd `json:"cmds"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out.Cmds
}

func pullClosure(t *testing.T, srv *httptest.Server, fid uuid.UUID, priv []byte, assetID string) service.ClosureSnapshot {
	t.Helper()
	res := factoryReq(t, srv, http.MethodGet, "/v1/channel/pull/closure/"+assetID, fid, priv)
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("pull closure %d %s", res.StatusCode, body)
	}
	var snap service.ClosureSnapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatal(err)
	}
	return snap
}

func indexHas(cmds []service.Cmd, typ, assetID string) bool {
	for _, c := range cmds {
		if c.Typ == typ && c.AssetID == assetID {
			return true
		}
	}
	return false
}

func indexHasKind(cmds []service.Cmd, typ, kind string) bool {
	for _, c := range cmds {
		if c.Typ == typ && c.Kind == kind {
			return true
		}
	}
	return false
}

func doSetup(t *testing.T, svc *service.Service, tok, createJSON string) (int, string) {
	t.Helper()
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	t.Cleanup(srv.Close)
	code, body := do(t, srv, "POST", "/v1/assets", tok, createJSON)
	if code != http.StatusCreated {
		t.Fatalf("create %d %s", code, body)
	}
	pid := gjson(t, body, "id")
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/publish", tok, `{"expected":1}`)
	if code != http.StatusOK {
		t.Fatalf("publish %d %s", code, body)
	}
	return code, body
}

func doBody(t *testing.T, srv *httptest.Server, method, path, token string) string {
	t.Helper()
	_, body := do(t, srv, method, path, token, "")
	return body
}
