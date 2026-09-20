// HTTP 适配：厂钥上送焊汇总，管理员读跨厂报表。
package httpapi_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

func TestWeldSummaryHTTP(t *testing.T) {
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
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	t.Cleanup(srv.Close)

	body := `{"rows":[{"day":"2026-09-17","projectName":"单层-舷侧分段","weldKind":"single","runCount":5,"lengthMm":4000,"durationSec":200}]}`
	code, raw := doFactory(t, srv, created.Factory.ID, priv, "POST", "/v1/channel/weld-summaries", body)
	if code != http.StatusNoContent {
		t.Fatalf("put %d %s", code, raw)
	}
	code, raw = do(t, srv, "GET", "/v1/weld-reports", tok, "")
	if code != http.StatusOK || !strings.Contains(raw, `"projectName":"单层-舷侧分段"`) || strings.Contains(raw, "loginName") {
		t.Fatalf("list %d %s", code, raw)
	}
	code, raw = doFactory(t, srv, created.Factory.ID, priv, "POST", "/v1/channel/weld-summaries", `{"rows":[{"day":"2026-09-17","projectName":"x","weldKind":"single","runCount":1,"lengthMm":1,"durationSec":1,"personId":"no"}]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("person field %d %s", code, raw)
	}
	code, _ = do(t, srv, "GET", "/v1/weld-reports", "", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("anon %d", code)
	}
}

func doFactory(t *testing.T, srv *httptest.Server, factoryID uuid.UUID, priv []byte, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	unix := time.Now().Unix()
	sig := nodekey.Sign(priv, nodekey.MQTTConnectPayload(factoryID, unix))
	req.Header.Set("X-WMesh-Factory", factoryID.String())
	req.Header.Set("X-WMesh-Time", strconv.FormatInt(unix, 10))
	req.Header.Set("X-WMesh-Sign", base64.StdEncoding.EncodeToString(sig))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(raw)
}
