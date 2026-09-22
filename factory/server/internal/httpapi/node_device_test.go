// HTTP 适配：钉机械臂号、本机登录领钥。
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

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
)

func TestClientDeviceLoginHTTP(t *testing.T) {
	h, err := hub.New(testpg.AdminDSN())
	if err != nil {
		t.Fatalf("hub: %v", err)
	}
	fid := id.New()
	t.Cleanup(func() {
		_ = h.Drop(fid)
		h.Close()
	})
	srv := httptest.NewServer(httpapi.New(h, "boot-secret", "").Router())
	t.Cleanup(srv.Close)
	base := "/v1/factories/" + fid.String()

	code, body := do(t, srv, "POST", "/internal/bootstrap", "boot-secret", `{"factoryId":"`+fid.String()+`","saLogin":"sa","saDisplay":"超管"}`)
	if code != http.StatusCreated {
		t.Fatalf("bootstrap %d %s", code, body)
	}
	saID := gjson(t, body, "personId")
	svc, err := h.Service(context.Background(), fid)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	if err := svc.Store().GrantLocalLease(context.Background()); err != nil {
		t.Fatalf("lease: %v", err)
	}
	if err := svc.Store().PutFactoryName(context.Background(), "一号厂"); err != nil {
		t.Fatalf("factory name: %v", err)
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
	tok := gjson(t, body, "token")

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pk := base64.StdEncoding.EncodeToString(pub)
	cid := id.New().String()
	code, body = do(t, srv, "POST", base+"/clients", tok, `{"id":"`+cid+`","name":"焊机","publicKey":"`+pk+`","bindingRevision":1}`)
	if code != http.StatusCreated {
		t.Fatalf("bind %d %s", code, body)
	}

	code, body = do(t, srv, "POST", base+"/clients/"+cid+"/device", "", `{"deviceSerial":""}`)
	if code != http.StatusBadRequest {
		t.Fatalf("empty serial %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/clients/"+cid+"/device", "", `{"deviceSerial":"ARM-1"}`)
	if code != http.StatusOK || gjson(t, body, "deviceSerial") != "ARM-1" || strings.Contains(body, "unwrapKey") {
		t.Fatalf("pin %d %s", code, body)
	}

	code, body = do(t, srv, "POST", base+"/pad/login", "", `{"loginName":"sa","password":"secret","appVersion":52,"appVersionName":"6.1.1","deviceModel":"TB-X606F","deviceManufacturer":"Lenovo","androidRelease":"10","networkName":"Factory-WiFi"}`)
	if code != http.StatusOK || gjson(t, body, "token") == "" || gjson(t, body, "unwrapKey") == "" || !strings.Contains(body, `"deviceSerial":"ARM-1"`) {
		t.Fatalf("pad login %d %s", code, body)
	}
	if mqtt := gjson(t, body, "mqttUrl"); !strings.HasPrefix(mqtt, "tcp://") || !strings.Contains(mqtt, ":52184") {
		t.Fatalf("pad mqttUrl %s", mqtt)
	}
	if strings.Contains(body, `"deviceSerial":"ARM-1","unwrapKey"`) {
		t.Fatalf("device key leaked %s", body)
	}
	padTok := gjson(t, body, "token")
	code, body = do(t, srv, "GET", base+"/people/"+saID+"/logins", tok, "")
	if code != http.StatusOK || !strings.Contains(body, `"kind":"pad"`) || !strings.Contains(body, `"deviceModel":"TB-X606F"`) || !strings.Contains(body, `"networkName":"Factory-WiFi"`) || strings.Contains(body, "secret") {
		t.Fatalf("login logs %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/pad/login-log", padTok, `{"appVersion":52,"appVersionName":"6.1.1","deviceSerial":"ARM-1","deviceModel":"TB-X606F","deviceManufacturer":"Lenovo","androidRelease":"10","networkName":"Factory-WiFi","clientId":"`+cid+`"}`)
	if code != http.StatusNoContent {
		t.Fatalf("login-log %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/people/"+saID+"/logins", tok, "")
	if code != http.StatusOK || !strings.Contains(body, `"deviceSerial":"ARM-1"`) || !strings.Contains(body, `"clientName":"焊机"`) || !strings.Contains(body, cid) {
		t.Fatalf("login-log rows %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/pad/assets", padTok, `{"kind":"process","name":"平板工艺","content":"{\"current\":170}","id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","code":"GY-C0008-000001"}`)
	if code != http.StatusCreated || gjson(t, body, "status") != "available" || gjson(t, body, "level") != "personal" || gjson(t, body, "id") != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("pad create %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/pad/assets/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/content", padTok, `{"content":"{\"current\":180}"}`)
	if code != http.StatusOK || gjson(t, body, "revision") != "2" {
		t.Fatalf("pad apply %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/assets/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/content", padTok, "")
	if code != http.StatusOK || !strings.Contains(body, "180") {
		t.Fatalf("pad read %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/assets/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/content", padTok, `{"expected":1,"content":"stale"}`)
	if code != http.StatusConflict {
		t.Fatalf("web stale %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/pad/inbox", padTok, "")
	if code != http.StatusOK || !strings.Contains(body, `"closures"`) {
		t.Fatalf("pad inbox %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/clients/"+cid+"/inbox", padTok, "")
	if code != http.StatusForbidden {
		t.Fatalf("pad token on client inbox %d %s", code, body)
	}

	code, body = do(t, srv, "POST", base+"/clients/"+cid+"/login", "", `{"deviceSerial":"ARM-9","loginName":"sa","password":"secret"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("mismatch %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/clients/"+cid+"/login", "", `{"deviceSerial":"ARM-1","loginName":"sa","password":"secret"}`)
	if code != http.StatusOK || gjson(t, body, "token") == "" || gjson(t, body, "unwrapKey") == "" || !strings.Contains(body, `"persistUnwrapKey":false`) {
		t.Fatalf("client login %d %s", code, body)
	}

	code, body = do(t, srv, "GET", "/v1/discover?deviceSerial=ARM-1", "", "")
	if code != http.StatusOK || !strings.Contains(body, `"belongs":true`) || !strings.Contains(body, cid) || !strings.Contains(body, fid.String()) || !strings.Contains(body, `"factoryName":"一号厂"`) {
		t.Fatalf("discover mine %d %s", code, body)
	}
	if strings.Contains(body, "unwrapKey") || strings.Contains(body, "password") {
		t.Fatalf("discover leaked %s", body)
	}
	code, body = do(t, srv, "GET", "/v1/discover?deviceSerial=ARM-OTHER", "", "")
	if code != http.StatusOK || strings.Contains(body, `"belongs":true`) {
		t.Fatalf("discover other %d %s", code, body)
	}
}
