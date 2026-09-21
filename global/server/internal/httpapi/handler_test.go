// HTTP 适配：WAN 登录、建厂、拒绝代管厂内账号。
package httpapi_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/id"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/platform/release"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"

	"github.com/google/uuid"
)

func TestWANHTTP(t *testing.T) {
	admin := testpg.Open(t)
	_, dsn := testpg.CreateDB(t, admin, "wmesh_wan")
	svc := service.NewService(store.Open(testpg.OpenMigrated(t, dsn)))
	if err := svc.BootstrapAdmin(context.Background(), "w", "wan-secret"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	t.Cleanup(srv.Close)

	code, body := do(t, srv, "GET", "/healthz", "", "")
	if code != http.StatusOK || gjson(t, body, "db") != "ok" || gjson(t, body, "build") != "test" || gjson(t, body, "version") != strconv.FormatInt(release.Code, 10) || gjson(t, body, "versionName") != release.Name || gjson(t, body, "oss") != "off" {
		t.Fatalf("healthz %d %s", code, body)
	}
	code, body = do(t, srv, "GET", "/v1/nope", "", "")
	if code != http.StatusNotFound || gjson(t, body, "error") != "not found" {
		t.Fatalf("unknown api %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/login", "", `{"loginName":"w","password":"bad"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("bad login %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/login", "", `{"loginName":"w","password":"wan-secret"}`)
	if code != http.StatusOK {
		t.Fatalf("login %d %s", code, body)
	}
	tok := gjson(t, body, "token")
	code, body = do(t, srv, "POST", "/v1/me/password", tok, `{"password":"wan-secret-2"}`)
	if code != http.StatusNoContent {
		t.Fatalf("change password %d %s", code, body)
	}
	code, body = do(t, srv, "GET", "/v1/directory", tok, "")
	if code != http.StatusOK {
		t.Fatalf("directory %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/factories", tok, `{"name":"厂A","saLogin":"sa-a","saDisplay":"超管"}`)
	if code != http.StatusCreated {
		t.Fatalf("create factory %d %s", code, body)
	}
	if gjson(t, body, "enrollmentToken") == "" {
		t.Fatalf("enrollment token missing: %s", body)
	}
	fid := gjson(t, body, "factory.id")
	if gjson(t, body, "factory.status") != "active" {
		t.Fatalf("new factory status %s", body)
	}
	code, body = do(t, srv, "POST", "/v1/factories", tok, `{"name":"厂B","saLogin":"sa-b","saDisplay":"超管B"}`)
	if code != http.StatusCreated {
		t.Fatalf("create factory B %d %s", code, body)
	}
	fidB := gjson(t, body, "factory.id")
	code, body = do(t, srv, "POST", "/v1/factories/"+fidB+"/disable", tok, "")
	if code != http.StatusOK || gjson(t, body, "status") != "disabled" {
		t.Fatalf("disable %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/factories/"+fidB+"/enable", tok, "")
	if code != http.StatusOK || gjson(t, body, "status") != "active" {
		t.Fatalf("enable %d %s", code, body)
	}
	code, body = do(t, srv, "DELETE", "/v1/factories/"+fidB, tok, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete unclaimed %d %s", code, body)
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pk := base64.StdEncoding.EncodeToString(pub)
	cid := id.New().String()
	code, body = do(t, srv, "POST", "/v1/clients", tok, `{"name":"焊机-1","id":"`+cid+`","deviceSerial":"ARM-1","factoryId":"`+fid+`","publicKey":"`+pk+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("bind client %d %s", code, body)
	}
	code, body = do(t, srv, "GET", "/v1/clients", tok, "")
	if code != http.StatusOK || !strings.Contains(body, cid) {
		t.Fatalf("list clients %d %s", code, body)
	}
	code, body = do(t, srv, "GET", "/v1/factories/"+fid+"/offline-grants", tok, "")
	if code != http.StatusForbidden {
		t.Fatalf("list offline grants %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/invite-wan-admin", tok, `{"loginName":"other"}`)
	if code != http.StatusForbidden {
		t.Fatalf("invite %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/factories/"+fid+"/people", tok, `{"loginName":"p1"}`)
	if code != http.StatusForbidden {
		t.Fatalf("proxy person %d %s", code, body)
	}
	code, body = do(t, srv, "GET", "/v1/factories/"+fid+"/people", tok, "")
	if code != http.StatusForbidden {
		t.Fatalf("list people %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"平台焊","content":"wan-body"}`)
	if code != http.StatusCreated || !strings.Contains(body, `"copyable":false`) {
		t.Fatalf("create platform process %d %s", code, body)
	}
	pid := gjson(t, body, "id")
	code, body = do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"开焊","content":"open-body","copyable":true}`)
	if code != http.StatusCreated || !strings.Contains(body, `"copyable":true`) || gjson(t, body, "revision") != "1" {
		t.Fatalf("create copyable platform process %d %s", code, body)
	}
	code, body = do(t, srv, "GET", "/v1/assets?kind=process", tok, "")
	if code != http.StatusOK || !strings.Contains(body, pid) || strings.Contains(body, "wan-body") {
		t.Fatalf("list platform %d %s", code, body)
	}
	code, body = do(t, srv, "GET", "/v1/assets/"+pid+"/content", tok, "")
	if code != http.StatusOK || gjson(t, body, "content") != appliedProcess("wan-body") {
		t.Fatalf("read platform content %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/content", tok, `{"expected":1,"content":"wan-body-2"}`)
	if code != http.StatusOK {
		t.Fatalf("update platform content %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/publish", tok, `{"expected":2}`)
	if code != http.StatusOK {
		t.Fatalf("publish platform %d %s", code, body)
	}
	code, body = do(t, srv, "GET", "/v1/assets/"+pid, tok, "")
	if code != http.StatusOK {
		t.Fatalf("get platform %d %s", code, body)
	}
	digest := gjson(t, body, "digest")
	rev := gjson(t, body, "revision")
	code, body = do(t, srv, "GET", "/v1/project-templates", tok, "")
	if code != http.StatusOK || !strings.Contains(body, `"processId"`) || strings.Contains(body, "processPath") {
		t.Fatalf("project templates %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/assets", tok, `{"kind":"project","name":"平台工程","content":"job","deps":[{"id":"`+pid+`","revision":`+rev+`,"digest":"`+digest+`"}]}`)
	if code != http.StatusCreated {
		t.Fatalf("create platform project %d %s", code, body)
	}
	projID := gjson(t, body, "id")
	code, body = do(t, srv, "POST", "/v1/assets/"+projID+"/deps", tok, `{"expected":1,"deps":[{"id":"`+pid+`","revision":`+rev+`,"digest":"`+digest+`"}]}`)
	if code != http.StatusOK {
		t.Fatalf("set project deps %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/disable", tok, `{"expected":`+rev+`}`)
	if code != http.StatusOK || gjson(t, body, "status") != "disabled" {
		t.Fatalf("disable platform %d %s", code, body)
	}
	disRev := gjson(t, body, "revision")
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/enable", tok, `{"expected":`+disRev+`}`)
	if code != http.StatusOK || gjson(t, body, "status") != "available" {
		t.Fatalf("enable platform %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/delete", tok, "")
	if code != http.StatusConflict || gjson(t, body, "error") != "still referenced" {
		t.Fatalf("delete referenced %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"可删","content":"gone"}`)
	if code != http.StatusCreated {
		t.Fatalf("create disposable %d %s", code, body)
	}
	dropID := gjson(t, body, "id")
	code, body = do(t, srv, "POST", "/v1/assets/"+dropID+"/copyable", tok, `{"expected":1,"copyable":true}`)
	if code != http.StatusOK || !strings.Contains(body, `"copyable":true`) {
		t.Fatalf("set copyable %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/assets/"+dropID+"/delete", tok, "")
	if code != http.StatusOK {
		t.Fatalf("delete unused %d %s", code, body)
	}
	code, body = do(t, srv, "GET", "/v1/factories/"+fid+"/promotable-assets?kind=process", tok, "")
	if code != http.StatusServiceUnavailable || gjson(t, body, "error") != "factory channel is offline" {
		t.Fatalf("promotable offline %d %s", code, body)
	}
	sum := sha256.Sum256([]byte("from-fac"))
	snap := `{"sourceId":"` + id.New().String() + `","sourceRevision":1,"sourceFactoryId":"` + fid + `","kind":"process","name":"收厂级","content":"` + base64.StdEncoding.EncodeToString([]byte("from-fac")) + `","digest":"` + base64.StdEncoding.EncodeToString(sum[:]) + `","copyable":true,"status":"available","deps":[]}`
	code, body = do(t, srv, "POST", "/v1/assets/promote", tok, snap)
	if code != http.StatusCreated || gjson(t, body, "status") != "draft" {
		t.Fatalf("promote snapshot %d %s", code, body)
	}
	aid := id.New().String()
	code, body = do(t, srv, "POST", "/v1/factories/"+fid+"/assets", tok, `{"name":"厂级","content":"x"}`)
	if code != http.StatusForbidden {
		t.Fatalf("proxy factory asset %d %s", code, body)
	}
	code, body = do(t, srv, "GET", "/v1/factories/"+fid+"/assets/"+aid, tok, "")
	if code != http.StatusForbidden {
		t.Fatalf("get factory asset %d %s", code, body)
	}
	code, _ = do(t, srv, "GET", "/v1/directory", "", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("anon directory %d", code)
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

func appliedProcess(raw string) string {
	schema, err := contenttpl.Marshal(contenttpl.Default(contenttpl.KindProcess))
	if err != nil {
		panic(err)
	}
	out, err := contenttpl.Apply(schema, []byte(raw))
	if err != nil {
		panic(err)
	}
	return string(out)
}

func TestWANSoftwareHTTP(t *testing.T) {
	admin := testpg.Open(t)
	_, dsn := testpg.CreateDB(t, admin, "wmesh_wan_sw")
	svc := service.NewService(store.Open(testpg.OpenMigrated(t, dsn)))
	if err := svc.BootstrapAdmin(context.Background(), "w", "wan-secret"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	t.Cleanup(srv.Close)

	code, body := do(t, srv, "POST", "/v1/login", "", `{"loginName":"w","password":"wan-secret"}`)
	if code != http.StatusOK {
		t.Fatalf("login %d %s", code, body)
	}
	tok := gjson(t, body, "token")
	code, body = do(t, srv, "GET", "/v1/software", "", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("anon software %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/factories", tok, `{"name":"厂A","saLogin":"sa-a","saDisplay":"超管"}`)
	if code != http.StatusCreated {
		t.Fatalf("create factory %d %s", code, body)
	}
	fid := gjson(t, body, "factory.id")
	pkg := base64.StdEncoding.EncodeToString([]byte("svc-1"))
	code, body = do(t, srv, "POST", "/v1/software", tok, `{"kind":"factory_service","version":1,"versionName":"1.0.0","content":"`+pkg+`"}`)
	if code != http.StatusCreated || gjson(t, body, "version") != "1" {
		t.Fatalf("publish %d %s", code, body)
	}
	code, body = do(t, srv, "GET", "/v1/software", tok, "")
	if code != http.StatusOK || !strings.Contains(body, `"versionName":"1.0.0"`) {
		t.Fatalf("list %d %s", code, body)
	}
	pub, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	code, body = do(t, srv, "GET", "/v1/software/latest?kind=factory_service", "", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("anon latest %d %s", code, body)
	}
	if err := svc.ConfirmEnroll(context.Background(), uuid.MustParse(fid), pub); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	res := factoryReq(t, srv, http.MethodGet, "/v1/software/latest?kind=factory_service", uuid.MustParse(fid), priv)
	defer res.Body.Close()
	got, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(got), `"version":1`) {
		t.Fatalf("latest %d %s", res.StatusCode, got)
	}
}
