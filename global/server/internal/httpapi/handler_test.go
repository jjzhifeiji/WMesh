// HTTP 适配：WAN 登录、建厂、拒绝代管厂内账号。
package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/id"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

type stubBoot struct {
	token string
}

func (s stubBoot) Bootstrap(_ context.Context, _ uuid.UUID, _, _ string) (uuid.UUID, string, error) {
	return id.New(), s.token, nil
}

func TestWANHTTP(t *testing.T) {
	admin := testpg.Open(t)
	_, dsn := testpg.CreateDB(t, admin, "wmesh_wan")
	svc := service.NewService(store.Open(testpg.OpenMigrated(t, dsn)), stubBoot{token: "act-once"})
	if err := svc.BootstrapAdmin(context.Background(), "w", "wan-secret"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	srv := httptest.NewServer(httpapi.New(svc).Router())
	t.Cleanup(srv.Close)

	code, body := do(t, srv, "POST", "/v1/login", "", `{"loginName":"w","password":"bad"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("bad login %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/login", "", `{"loginName":"w","password":"wan-secret"}`)
	if code != http.StatusOK {
		t.Fatalf("login %d %s", code, body)
	}
	tok := gjson(t, body, "token")
	code, body = do(t, srv, "GET", "/v1/directory", tok, "")
	if code != http.StatusOK {
		t.Fatalf("directory %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/factories", tok, `{"name":"厂A","saLogin":"sa-a","saDisplay":"超管"}`)
	if code != http.StatusCreated {
		t.Fatalf("create factory %d %s", code, body)
	}
	if gjson(t, body, "activationToken") != "act-once" {
		t.Fatalf("activation token missing: %s", body)
	}
	fid := gjson(t, body, "factory.id")
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
	if !ok {
		t.Fatalf("%s not string in %s", path, body)
	}
	return s
}
