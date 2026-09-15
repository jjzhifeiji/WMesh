// HTTP 适配：厂出站认领 HTTPS，日常 MQTT 指令加 HTTPS 拉正文。
package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/nodekey"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

func TestChannelEnroll(t *testing.T) {
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
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	t.Cleanup(srv.Close)

	enrollBody, err := json.Marshal(map[string]string{"enrollmentCode": created.EnrollmentToken})
	if err != nil {
		t.Fatal(err)
	}
	code, body := do(t, srv, "POST", "/v1/channel/enroll", "", string(enrollBody))
	if code != http.StatusOK {
		t.Fatalf("enroll %d %s", code, body)
	}
	if gjson(t, body, "factoryId") != created.Factory.ID.String() || gjson(t, body, "saLogin") != "sa-a" {
		t.Fatalf("enroll: %s", body)
	}
	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	claimBody, err := json.Marshal(map[string]any{"enrollmentCode": created.EnrollmentToken, "factoryPublicKey": pub})
	if err != nil {
		t.Fatal(err)
	}
	code, body = do(t, srv, "POST", "/v1/channel/claim", "", string(claimBody))
	if code != http.StatusOK {
		t.Fatalf("claim %d %s", code, body)
	}
	if gjson(t, body, "factoryId") != created.Factory.ID.String() || gjson(t, body, "status") != service.FactoryActive {
		t.Fatalf("claim: %s", body)
	}
}

func TestChannelEnrollAfterDelete(t *testing.T) {
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
	if _, err := svc.DisableFactory(context.Background(), tok, created.Factory.ID); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	t.Cleanup(srv.Close)

	enrollBody, err := json.Marshal(map[string]string{"enrollmentCode": created.EnrollmentToken})
	if err != nil {
		t.Fatal(err)
	}
	code, body := do(t, srv, "POST", "/v1/channel/enroll", "", string(enrollBody))
	if code != http.StatusForbidden || !strings.Contains(body, "factory is disabled") {
		t.Fatalf("enroll disabled %d %s", code, body)
	}
	if _, err := svc.EnableFactory(context.Background(), tok, created.Factory.ID); err != nil {
		t.Fatal(err)
	}
	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmEnroll(context.Background(), created.Factory.ID, pub); err != nil {
		t.Fatal(err)
	}
	code, body = do(t, srv, "DELETE", "/v1/factories/"+created.Factory.ID.String(), tok, "")
	if code != http.StatusOK {
		t.Fatalf("delete %d %s", code, body)
	}
	code, body = do(t, srv, "POST", "/v1/channel/enroll", "", string(enrollBody))
	if code != http.StatusUnauthorized || !strings.Contains(body, "invalid enrollment") {
		t.Fatalf("enroll after delete %d %s", code, body)
	}
	claimBody, err := json.Marshal(map[string]any{"enrollmentCode": created.EnrollmentToken, "factoryPublicKey": pub})
	if err != nil {
		t.Fatal(err)
	}
	code, body = do(t, srv, "POST", "/v1/channel/claim", "", string(claimBody))
	if code != http.StatusUnauthorized || !strings.Contains(body, "invalid enrollment") {
		t.Fatalf("claim after delete %d %s", code, body)
	}
}

func assertOnline(t *testing.T, srv *httptest.Server, tok string, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if channelOnline(t, srv, tok) == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("want online=%v", want)
}

func channelOnline(t *testing.T, srv *httptest.Server, tok string) bool {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/directory", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("directory %d %s", res.StatusCode, body)
	}
	return strings.Contains(string(body), `"channelOnline":true`)
}
