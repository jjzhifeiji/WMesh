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

func TestClientChannelHTTP(t *testing.T) {
	h, err := hub.New(testpg.AdminDSN())
	if err != nil {
		t.Fatalf("hub: %v", err)
	}
	fid := id.New()
	t.Cleanup(func() {
		_ = h.Drop(fid)
		h.Close()
	})
	if err := h.StartClientBroker("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	api := httpapi.New(h, "boot-secret", "")
	api.ClientMQTTURL = "tcp://" + h.ClientMQTTAddr()
	srv := httptest.NewServer(api.Router())
	t.Cleanup(srv.Close)
	base := "/v1/factories/" + fid.String()

	code, body := do(t, srv, "POST", "/internal/bootstrap", "boot-secret", `{"factoryId":"`+fid.String()+`","saLogin":"sa","saDisplay":"超管"}`)
	if code != http.StatusCreated {
		t.Fatalf("bootstrap %d %s", code, body)
	}
	svc, err := h.Service(context.Background(), fid)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Store().GrantLocalLease(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Store().PutFactoryShortCode(context.Background(), "F01"); err != nil {
		t.Fatal(err)
	}
	act := gjson(t, body, "activationToken")
	code, body = do(t, srv, "POST", base+"/activate", "", `{"loginName":"sa","activationToken":"`+act+`","password":"secret"}`)
	if code != http.StatusNoContent {
		t.Fatalf("activate %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/login", "", `{"loginName":"sa","password":"secret"}`)
	if code != http.StatusOK {
		t.Fatalf("web login %d %s", code, body)
	}
	saTok := gjson(t, body, "token")

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cid := id.New().String()
	cidB := id.New().String()
	pk := base64.StdEncoding.EncodeToString(pub)
	code, body = do(t, srv, "POST", base+"/clients", saTok, `{"id":"`+cid+`","name":"焊机","publicKey":"`+pk+`","bindingRevision":1}`)
	if code != http.StatusCreated {
		t.Fatalf("bind %d %s", code, body)
	}
	pubB, _, _ := ed25519.GenerateKey(rand.Reader)
	pkB := base64.StdEncoding.EncodeToString(pubB)
	code, body = do(t, srv, "POST", base+"/clients", saTok, `{"id":"`+cidB+`","name":"焊机B","publicKey":"`+pkB+`","bindingRevision":1}`)
	if code != http.StatusCreated {
		t.Fatalf("bind b %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/clients/"+cid+"/device", "", `{"deviceSerial":"ARM-1"}`)
	if code != http.StatusOK {
		t.Fatalf("pin %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/clients/"+cidB+"/device", "", `{"deviceSerial":"ARM-B"}`)
	if code != http.StatusOK {
		t.Fatalf("pin b %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/clients/"+cid+"/login", "", `{"deviceSerial":"ARM-1","loginName":"sa","password":"secret"}`)
	if code != http.StatusOK || gjson(t, body, "mqttUrl") == "" || gjson(t, body, "signingPublicKey") == "" {
		t.Fatalf("client login %d %s", code, body)
	}
	tok := gjson(t, body, "token")

	code, body = do(t, srv, "GET", base+"/clients/"+cid+"/inbox", tok, "")
	if code != http.StatusOK || !strings.Contains(body, `"closures"`) {
		t.Fatalf("inbox %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/clients/"+cidB+"/inbox", tok, "")
	if code != http.StatusForbidden {
		t.Fatalf("other inbox %d %s", code, body)
	}

	ctx := context.Background()
	direct := service.WorkContext{Direct: true}
	proc, err := svc.CreateFactoryProcess(ctx, saTok, direct, "工艺", []byte(`{"current":1}`))
	if err != nil {
		t.Fatal(err)
	}
	proc, err = svc.PublishAsset(ctx, saTok, proc.ID, proc.Revision)
	if err != nil {
		t.Fatal(err)
	}
	projBody := []byte(`[{"name":"w","processId":"` + proc.ID.String() + `"}]`)
	proj, err := svc.CreateFactoryProject(ctx, saTok, direct, "工程", projBody, nil)
	if err != nil {
		t.Fatal(err)
	}
	proj, err = svc.PublishAsset(ctx, saTok, proj.ID, proj.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.GrantClientProject(ctx, saTok, proj.ID, uuid.MustParse(cid)); err != nil {
		t.Fatal(err)
	}

	code, body = do(t, srv, "GET", base+"/clients/"+cid+"/closures/"+proj.ID.String(), tok, "")
	if code != http.StatusOK || !strings.Contains(body, `"wrap"`) || strings.Contains(body, `"current":1`) {
		t.Fatalf("pull %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/clients/"+cidB+"/closures/"+proj.ID.String(), tok, "")
	if code != http.StatusForbidden {
		t.Fatalf("other pull %d %s", code, body)
	}

	got := make(chan []byte, 2)
	cli := mqttConnect(t, h.ClientMQTTAddr(), fid.String(), cid, tok)
	waitAppOnline(t, svc, saTok, true)
	subTok := cli.Subscribe(clientmqtt.DownTopic(fid, uuid.MustParse(cid)), 1, func(_ mqtt.Client, m mqtt.Message) {
		select {
		case got <- append([]byte(nil), m.Payload()...):
		default:
		}
	})
	if !subTok.WaitTimeout(3*time.Second) || subTok.Error() != nil {
		t.Fatal(subTok.Error())
	}
	code, body = do(t, srv, "PUT", base+"/client-policy", saTok, `{"maxCachedProjects":3,"cacheScope":"all","persistUnwrapKey":false,"keyTtlSeconds":0}`)
	if code != http.StatusOK {
		t.Fatalf("policy %d %s", code, body)
	}
	select {
	case raw := <-got:
		if clientmqtt.HasBody(raw) {
			t.Fatalf("mqtt body %s", raw)
		}
		if !strings.Contains(string(raw), `"typ":"policy"`) {
			t.Fatalf("mqtt %s", raw)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("missed policy intent")
	}

	code, body = do(t, srv, "POST", base+"/pad/login", "", `{"loginName":"sa","password":"secret"}`)
	if code != http.StatusOK {
		t.Fatalf("pad login %d %s", code, body)
	}
	padTok := gjson(t, body, "token")
	personID := gjson(t, body, "account.id")
	padMQTT := mqttConnect(t, h.ClientMQTTAddr(), fid.String(), personID, padTok)
	waitAppOnline(t, svc, saTok, true)
	cli.Disconnect(250)
	padMQTT.Disconnect(250)
	waitAppOnline(t, svc, saTok, false)
}

func waitAppOnline(t *testing.T, svc *service.Service, tok string, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		cat, err := svc.Catalog(context.Background(), tok)
		if err != nil {
			t.Fatal(err)
		}
		if cat.Me.AppOnline == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("appOnline=%v want %v", cat.Me.AppOnline, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func mqttConnect(t *testing.T, addr, factoryID, clientID, token string) mqtt.Client {
	t.Helper()
	opts := mqtt.NewClientOptions()
	opts.AddBroker("tcp://" + addr)
	opts.SetClientID(clientID)
	opts.SetUsername(factoryID)
	opts.SetPassword(token)
	opts.SetAutoReconnect(false)
	opts.SetConnectTimeout(3 * time.Second)
	cli := mqtt.NewClient(opts)
	tok := cli.Connect()
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("mqtt: %v", tok.Error())
	}
	t.Cleanup(func() { cli.Disconnect(250) })
	return cli
}
