// HTTP 适配：厂内引导、激活、登录、名册与组织人员。
package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"time"

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
	srv := httptest.NewServer(httpapi.New(h, "boot-secret", "").Router())
	t.Cleanup(srv.Close)
	base := "/v1/factories/" + fid.String()

	code, body := do(t, srv, "GET", "/healthz", "", "")
	if code != http.StatusOK || gjson(t, body, "db") != "ok" || gjson(t, body, "oss") != "off" {
		t.Fatalf("healthz %d %s", code, body)
	}
	code, body = do(t, srv, "GET", "/v1/site", "", "")
	if code != http.StatusOK || !strings.Contains(body, `"wanConfigured":false`) {
		t.Fatalf("site %d %s", code, body)
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
	svc, err := h.Service(context.Background(), fid)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	// HTTP 测不连 WAN，自签租约才能写资产。
	if err := svc.Store().GrantLocalLease(context.Background()); err != nil {
		t.Fatalf("lease: %v", err)
	}
	if err := svc.Store().PutFactoryShortCode(context.Background(), "F01"); err != nil {
		t.Fatalf("short code: %v", err)
	}
	act := gjson(t, body, "activationToken")
	if len(act) != 8 {
		t.Fatalf("activation code len %d %s", len(act), act)
	}
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
	code, body = do(t, srv, "GET", base+"/project-templates", tok, "")
	if code != http.StatusOK || strings.TrimSpace(body) != "[]" {
		t.Fatalf("project templates %d %s", code, body)
	}
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
	code, body = do(t, srv, "POST", base+"/org-units", tok, `{"name":"一车间"}`)
	if code != http.StatusCreated {
		t.Fatalf("org unit %d %s", code, body)
	}
	unitID := gjson(t, body, "id")
	code, body = do(t, srv, "POST", base+"/org-units", tok, `{"name":"空节点"}`)
	if code != http.StatusCreated {
		t.Fatalf("empty unit %d %s", code, body)
	}
	emptyID := gjson(t, body, "id")
	code, body = do(t, srv, "POST", base+"/org-units/"+emptyID+"/disable", tok, "")
	if code != http.StatusNoContent {
		t.Fatalf("disable unit %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/org-units/"+emptyID+"/enable", tok, "")
	if code != http.StatusNoContent {
		t.Fatalf("enable unit %d %s", code, body)
	}
	code, body = do(t, srv, "DELETE", base+"/org-units/"+emptyID, tok, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete unused unit %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/people", tok, `{"loginName":"op1","displayName":"操作员"}`)
	if code != http.StatusCreated {
		t.Fatalf("person %d %s", code, body)
	}
	if strings.Contains(body, "activationToken") || gjson(t, body, "account.status") != "active" {
		t.Fatalf("person default login missing: %s", body)
	}
	personID := gjson(t, body, "account.id")
	code, body = do(t, srv, "POST", base+"/people/"+personID+"/disable", tok, "")
	if code != http.StatusNoContent {
		t.Fatalf("disable person %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/people/"+personID+"/enable", tok, "")
	if code != http.StatusNoContent {
		t.Fatalf("enable person %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/people/"+personID+"/reset-password", tok, "")
	if code != http.StatusOK {
		t.Fatalf("reset person %d %s", code, body)
	}
	if strings.Contains(body, "activationToken") || gjson(t, body, "account.status") != "active" {
		t.Fatalf("reset default password missing: %s", body)
	}
	code, body = do(t, srv, "POST", base+"/grants", tok, `{"personId":"`+personID+`","role":"operator","scopeKind":"org_unit","orgUnitId":"`+unitID+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("grant %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/assignments", tok, `{"personId":"`+personID+`","orgUnitId":"`+unitID+`"}`)
	if code != http.StatusNoContent {
		t.Fatalf("assign %d %s", code, body)
	}
	code, body = do(t, srv, "DELETE", base+"/org-units/"+unitID, tok, "")
	if code != http.StatusConflict || gjson(t, body, "error") != "still referenced" {
		t.Fatalf("delete referenced unit %d %s", code, body)
	}
	missing := id.New()
	code, body = do(t, srv, "POST", "/v1/factories/"+missing.String()+"/login", "", `{"loginName":"sa","password":"secret"}`)
	if code != http.StatusNotFound {
		t.Fatalf("missing factory %d %s", code, body)
	}

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pk := base64.StdEncoding.EncodeToString(pub)
	cid := id.New().String()
	code, body = do(t, srv, "POST", base+"/clients", tok, `{"id":"`+cid+`","name":"焊机-1","publicKey":"`+pk+`","bindingRevision":1}`)
	if code != http.StatusCreated {
		t.Fatalf("accept client %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/clients", tok, "")
	if code != http.StatusOK || !strings.Contains(body, cid) {
		t.Fatalf("list clients %d %s", code, body)
	}
	nb := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	na := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	code, body = do(t, srv, "POST", base+"/clients/"+cid+"/runtime", tok, `{"notBefore":"`+nb+`","notAfter":"`+na+`"}`)
	if code != http.StatusCreated || !strings.Contains(body, `"canRun":true`) || strings.Contains(body, "payload") {
		t.Fatalf("issue runtime %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/me", tok, "")
	if code != http.StatusOK {
		t.Fatalf("me %d %s", code, body)
	}
	saID := gjson(t, body, "id")
	code, body = do(t, srv, "GET", base+"/signing-key", tok, "")
	if code != http.StatusOK || !strings.Contains(body, "publicKey") {
		t.Fatalf("signing key %d %s", code, body)
	}

	code, body = do(t, srv, "POST", base+"/grants", tok, `{"personId":"`+saID+`","role":"process_engineer","scopeKind":"factory"}`)
	if code != http.StatusCreated {
		t.Fatalf("grant pe %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/asset-author-context", tok, "")
	if code != http.StatusOK || !strings.Contains(body, `"allowDirect":true`) {
		t.Fatalf("author context %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/assets", tok, `{"kind":"process","level":"factory","name":"焊A","content":"secret-body","direct":true}`)
	if code != http.StatusCreated {
		t.Fatalf("create process %d %s", code, body)
	}
	pid := gjson(t, body, "id")
	code, body = do(t, srv, "GET", base+"/assets?kind=process", tok, "")
	if code != http.StatusOK || !strings.Contains(body, pid) || strings.Contains(body, "secret-body") || !strings.Contains(body, "creatorLogin") {
		t.Fatalf("list process %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/assets/"+pid+"/content", tok, "")
	if code != http.StatusOK || gjson(t, body, "content") != "secret-body" {
		t.Fatalf("read content %d %s", code, body)
	}
	lease, err := svc.Store().TransitKey()
	if err != nil {
		t.Fatalf("lease key: %v", err)
	}
	svc.Store().ClearContentLease()
	code, body = do(t, srv, "GET", base+"/assets/"+pid, tok, "")
	if code != http.StatusForbidden || gjson(t, body, "error") != "content lease expired" {
		t.Fatalf("get without lease %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/assets/"+pid+"/content", tok, "")
	if code != http.StatusForbidden || gjson(t, body, "error") != "content lease expired" {
		t.Fatalf("content without lease %d %s", code, body)
	}
	if err := svc.Store().ApplyContentLease(context.Background(), lease, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("restore lease: %v", err)
	}
	code, body = do(t, srv, "POST", base+"/assets/"+pid+"/rename", tok, `{"expected":1,"name":"焊A2"}`)
	if code != http.StatusOK || gjson(t, body, "name") != "焊A2" {
		t.Fatalf("rename %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/assets/"+pid+"/publish", tok, `{"expected":2}`)
	if code != http.StatusOK {
		t.Fatalf("publish %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/assets/"+pid+"/rename", tok, `{"expected":1,"name":"旧修订"}`)
	if code != http.StatusConflict || gjson(t, body, "error") != "revision does not match" {
		t.Fatalf("stale rename %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/assets/"+pid, tok, "")
	if code != http.StatusOK {
		t.Fatalf("get process %d %s", code, body)
	}
	digest := gjson(t, body, "digest")
	rev := gjson(t, body, "revision")
	code, body = do(t, srv, "POST", base+"/assets", tok, `{"kind":"project","level":"factory","name":"工程A","content":"job","direct":true,"deps":[{"id":"`+pid+`","revision":`+rev+`,"digest":"`+digest+`"}]}`)
	if code != http.StatusCreated {
		t.Fatalf("create project %d %s", code, body)
	}
	projID := gjson(t, body, "id")
	code, body = do(t, srv, "POST", base+"/assets/"+projID+"/deps", tok, `{"expected":1,"deps":[{"id":"`+pid+`","revision":`+rev+`,"digest":"`+digest+`"}]}`)
	if code != http.StatusOK {
		t.Fatalf("set deps %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/assets", tok, `{"kind":"process","level":"personal","name":"个人焊","content":"mine","direct":true}`)
	if code != http.StatusCreated {
		t.Fatalf("create personal %d %s", code, body)
	}
	persID := gjson(t, body, "id")
	code, body = do(t, srv, "POST", base+"/assets/"+persID+"/publish", tok, `{"expected":1}`)
	if code != http.StatusOK {
		t.Fatalf("publish personal %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/assets/"+persID+"/promote", tok, "")
	if code != http.StatusCreated || strings.Contains(body, "mine") {
		t.Fatalf("promote %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/assets/"+pid+"/delete", tok, "")
	if code != http.StatusConflict || gjson(t, body, "error") != "still referenced" {
		t.Fatalf("delete referenced %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/assets", tok, `{"kind":"process","level":"factory","name":"可删","content":"gone","direct":true}`)
	if code != http.StatusCreated {
		t.Fatalf("create disposable %d %s", code, body)
	}
	dropID := gjson(t, body, "id")
	code, body = do(t, srv, "POST", base+"/assets/"+dropID+"/delete", tok, "")
	if code != http.StatusOK {
		t.Fatalf("delete unused %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/assets/"+pid+"/snapshot", tok, "")
	if code != http.StatusOK || gjson(t, body, "sourceId") != pid || !strings.Contains(body, base64.StdEncoding.EncodeToString([]byte("secret-body"))) {
		t.Fatalf("snapshot %d %s", code, body)
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
	if ok {
		return s
	}
	if n, ok := cur.(float64); ok {
		return strconv.FormatInt(int64(n), 10)
	}
	t.Fatalf("%s not string in %s", path, body)
	return ""
}
