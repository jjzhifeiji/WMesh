// HTTP 适配：WAN 旧文件导入。
package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

func TestLegacyImportHTTP(t *testing.T) {
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
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	t.Cleanup(srv.Close)

	code, body := do(t, srv, "POST", "/v1/legacy-import", tok, `{"processes":[],"projects":[]}`)
	if code != http.StatusOK || !strings.Contains(body, `"processes":[]`) || !strings.Contains(body, `"rejected":[]`) || !strings.Contains(body, `"skipped":[]`) {
		t.Fatalf("empty %d %s", code, body)
	}
	code, _ = do(t, srv, "POST", "/v1/legacy-import", "", `{"processes":[],"projects":[]}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("anon %d", code)
	}
}
