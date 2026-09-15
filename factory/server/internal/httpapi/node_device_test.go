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
	svc, err := h.Service(context.Background(), fid)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	if err := svc.Store().GrantLocalLease(context.Background()); err != nil {
		t.Fatalf("lease: %v", err)
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

	code, body = do(t, srv, "POST", base+"/clients/"+cid+"/login", "", `{"deviceSerial":"ARM-9","loginName":"sa","password":"secret"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("mismatch %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/clients/"+cid+"/login", "", `{"deviceSerial":"ARM-1","loginName":"sa","password":"secret"}`)
	if code != http.StatusOK || gjson(t, body, "token") == "" || gjson(t, body, "unwrapKey") == "" || !strings.Contains(body, `"persistUnwrapKey":false`) {
		t.Fatalf("client login %d %s", code, body)
	}
}
