// HTTP 适配：本厂 Client 策略读写。
package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/secret"
	"wmesh/factory/internal/platform/testpg"
)

func TestFactoryClientPolicyHTTP(t *testing.T) {
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
		t.Fatalf("login %d %s", code, body)
	}
	tok := gjson(t, body, "token")

	code, body = do(t, srv, "GET", base+"/client-policy", "", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("anon get %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/client-policy", tok, "")
	if code != http.StatusOK || gjson(t, body, "revision") != "0" || gjson(t, body, "maxCachedProjects") != "2" ||
		gjson(t, body, "cacheScope") != "all" || !strings.Contains(body, `"encryptPouch":true`) {
		t.Fatalf("default %d %s", code, body)
	}

	code, body = do(t, srv, "PUT", base+"/client-policy", tok, `{"maxCachedProjects":3,"cacheScope":"current","persistUnwrapKey":true,"keyTtlSeconds":60,"extra":{"k":true}}`)
	if code != http.StatusOK || gjson(t, body, "revision") != "1" || gjson(t, body, "maxCachedProjects") != "3" ||
		gjson(t, body, "cacheScope") != "current" || !strings.Contains(body, `"encryptPouch":true`) {
		t.Fatalf("put %d %s", code, body)
	}

	code, body = do(t, srv, "PUT", base+"/client-policy", tok, `{"maxCachedProjects":3,"cacheScope":"current","persistUnwrapKey":true,"keyTtlSeconds":60,"encryptPouch":false}`)
	if code != http.StatusOK || !strings.Contains(body, `"encryptPouch":false`) || gjson(t, body, "revision") != "2" {
		t.Fatalf("encrypt off %d %s", code, body)
	}
	code, body = do(t, srv, "PUT", base+"/client-policy", tok, `{"maxCachedProjects":3,"cacheScope":"current","persistUnwrapKey":true,"keyTtlSeconds":60}`)
	if code != http.StatusOK || !strings.Contains(body, `"encryptPouch":false`) {
		t.Fatalf("omit keeps encrypt %d %s", code, body)
	}

	code, body = do(t, srv, "PUT", base+"/client-policy", tok, `{"maxCachedProjects":3,"cacheScope":"nope","persistUnwrapKey":false,"keyTtlSeconds":0}`)
	if code != http.StatusForbidden {
		t.Fatalf("bad scope %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/client-policy", tok, "")
	if code != http.StatusOK || gjson(t, body, "revision") != "3" {
		t.Fatalf("still %d %s", code, body)
	}

	code, body = do(t, srv, "POST", base+"/people", tok, `{"loginName":"op1","displayName":"操作员"}`)
	if code != http.StatusCreated {
		t.Fatalf("person %d %s", code, body)
	}
	personID := gjson(t, body, "account.id")
	code, body = do(t, srv, "POST", base+"/grants", tok, `{"personId":"`+personID+`","role":"operator","scopeKind":"factory"}`)
	if code != http.StatusCreated {
		t.Fatalf("grant %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/login", "", `{"loginName":"op1","password":"`+secret.DefaultPersonPassword("op1")+`"}`)
	if code != http.StatusOK {
		t.Fatalf("op login %d %s", code, body)
	}
	opTok := gjson(t, body, "token")
	code, body = do(t, srv, "PUT", base+"/client-policy", opTok, `{"maxCachedProjects":9,"cacheScope":"all","persistUnwrapKey":false,"keyTtlSeconds":0}`)
	if code != http.StatusForbidden {
		t.Fatalf("op put %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/client-policy", tok, "")
	if code != http.StatusOK || gjson(t, body, "revision") != "3" || gjson(t, body, "maxCachedProjects") != "3" {
		t.Fatalf("op must not write %d %s", code, body)
	}
	if strings.Contains(body, "secret") || strings.Contains(body, opTok) {
		t.Fatalf("secret leaked: %s", body)
	}
}
