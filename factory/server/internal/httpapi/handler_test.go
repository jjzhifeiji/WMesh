// HTTP 适配：厂内引导、激活、登录、名册与组织人员。
package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
)

func TestFactoryHTTP(t *testing.T) {
	h, err := hub.New(testpg.AdminDSN())
	if err != nil {
		t.Fatalf("hub: %v", err)
	}
	fid := id.New()
	t.Cleanup(func() {
		_ = h.Drop(fid)
		h.Close()
	})
	srv := httptest.NewServer(httpapi.New(h, "boot-secret").Router())
	t.Cleanup(srv.Close)
	base := "/v1/factories/" + fid.String()

	code, body := do(t, srv, "GET", "/healthz", "", "")
	if code != http.StatusOK || gjson(t, body, "db") != "ok" || gjson(t, body, "oss") != "off" {
		t.Fatalf("healthz %d %s", code, body)
	}
	code, body = do(t, srv, "GET", "/v1/nope", "", "")
	if code != http.StatusNotFound || gjson(t, body, "error") != "not found" {
		t.Fatalf("unknown api %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/internal/bootstrap", "", `{"factoryId":"`+fid.String()+`","saLogin":"sa","saDisplay":"超管"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("anon bootstrap %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/internal/bootstrap", "boot-secre", `{"factoryId":"`+fid.String()+`","saLogin":"sa","saDisplay":"超管"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("wrong token bootstrap %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/internal/bootstrap", "boot-secret", `{"factoryId":"`+fid.String()+`","saLogin":"sa","saDisplay":"超管"}`)
	if code != http.StatusCreated {
		t.Fatalf("bootstrap %d %s", code, body)
	}
	act := gjson(t, body, "activationToken")
	code, body = do(t, srv, "POST", base+"/login", "", `{"loginName":"sa","password":"secret"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("pending login %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/activate", "", `{"loginName":"sa","activationToken":"`+act+`","password":"secret"}`)
	if code != http.StatusNoContent {
		t.Fatalf("activate %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/login", "", `{"loginName":"sa","password":"secret"}`)
	if code != http.StatusOK {
		t.Fatalf("login %d %s", code, body)
	}
	tok := gjson(t, body, "token")
	code, body = do(t, srv, "GET", base+"/catalog", tok, "")
	if code != http.StatusOK {
		t.Fatalf("catalog %d %s", code, body)
	}
	if strings.Contains(body, "PasswordHash") || strings.Contains(body, "passwordHash") || strings.Contains(body, "activationTokenHash") {
		t.Fatalf("secret leaked: %s", body)
	}
	if !strings.Contains(body, `"myGrants":[{`) || !strings.Contains(body, `"role":"factory_super_admin"`) {
		t.Fatalf("catalog must expose caller's own grants: %s", body)
	}
	code, body = do(t, srv, "POST", base+"/org-types", tok, `{"name":"车间"}`)
	if code != http.StatusCreated {
		t.Fatalf("org type %d %s", code, body)
	}
	typeID := gjson(t, body, "id")
	code, body = do(t, srv, "POST", base+"/org-units", tok, `{"typeId":"`+typeID+`","name":"一车间"}`)
	if code != http.StatusCreated {
		t.Fatalf("org unit %d %s", code, body)
	}
	unitID := gjson(t, body, "id")
	code, body = do(t, srv, "POST", base+"/people", tok, `{"loginName":"op1","displayName":"操作员"}`)
	if code != http.StatusCreated {
		t.Fatalf("person %d %s", code, body)
	}
	if gjson(t, body, "activationToken") == "" {
		t.Fatalf("person activation missing: %s", body)
	}
	personID := gjson(t, body, "account.id")
	code, body = do(t, srv, "POST", base+"/grants", tok, `{"personId":"`+personID+`","role":"operator","scopeKind":"org_unit","orgUnitId":"`+unitID+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("grant %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/assignments", tok, `{"personId":"`+personID+`","orgUnitId":"`+unitID+`"}`)
	if code != http.StatusNoContent {
		t.Fatalf("assign %d %s", code, body)
	}
	missing := id.New()
	code, body = do(t, srv, "POST", "/v1/factories/"+missing.String()+"/login", "", `{"loginName":"sa","password":"secret"}`)
	if code != http.StatusNotFound {
		t.Fatalf("missing factory %d %s", code, body)
	}
}

func do(t *testing.T, srv *httptest.Server, method, path, token, body string) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(b)
}

func gjson(t *testing.T, body, path string) string {
	t.Helper()
	var m any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("json %s: %v", body, err)
	}
	cur := m
	for _, p := range strings.Split(path, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("%s not object in %s", path, body)
		}
		cur = mm[p]
	}
	s, ok := cur.(string)
	if !ok {
		t.Fatalf("%s not string in %s", path, body)
	}
	return s
}
