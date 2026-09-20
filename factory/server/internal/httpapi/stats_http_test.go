package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/service"
)

func TestWeldFactsHTTP(t *testing.T) {
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
	saTok := loginWeb(t, srv, base, "sa", "secret")
	code, body = do(t, srv, "POST", base+"/org-units", saTok, `{"name":"车间"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("unit %d %s", code, body)
	}
	// 建人走服务，避免 HTTP 建人响应差一档。
	ctx := context.Background()
	pa, err := svc.CreatePerson(ctx, saTok, "op-a", "焊工A")
	if err != nil {
		t.Fatal(err)
	}
	pb, err := svc.CreatePerson(ctx, saTok, "op-b", "焊工B")
	if err != nil {
		t.Fatal(err)
	}
	units, err := svc.Store().ListOrgUnits(ctx)
	if err != nil || len(units) == 0 {
		t.Fatalf("units %v", err)
	}
	shop := units[0]
	if _, err := svc.GrantRole(ctx, saTok, pa.ID, service.RoleOperator, service.ScopeOrgUnit, &shop.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GrantRole(ctx, saTok, pb.ID, service.RoleOperator, service.ScopeOrgUnit, &shop.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Assign(ctx, saTok, pa.ID, shop.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Assign(ctx, saTok, pb.ID, shop.ID); err != nil {
		t.Fatal(err)
	}
	code, body = do(t, srv, "POST", base+"/pad/login", "", `{"loginName":"op-b","password":"`+defaultPass("op-b")+`"}`)
	if code != http.StatusOK {
		t.Fatalf("pad b %d %s", code, body)
	}
	bTok := gjson(t, body, "token")
	idA := id.New().String()
	idB := id.New().String()
	payload, _ := json.Marshal(map[string]any{
		"facts": []map[string]any{
			{
				"id": idA, "creatorId": pa.ID.String(), "orgUnitId": shop.ID.String(),
				"orgPath": []map[string]string{{"id": shop.ID.String(), "name": shop.Name}},
				"lengthMm": 1500, "durationSec": 90,
				"occurredAt": time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC).Format(time.RFC3339),
			},
			{
				"id": idB, "creatorId": pb.ID.String(), "orgUnitId": shop.ID.String(),
				"orgPath": []map[string]string{{"id": shop.ID.String(), "name": shop.Name}},
				"lengthMm": 400, "durationSec": 20,
				"occurredAt": time.Date(2026, 9, 17, 1, 0, 0, 0, time.UTC).Format(time.RFC3339),
			},
		},
	})
	code, body = do(t, srv, "POST", base+"/pad/weld-facts", "", string(payload))
	if code != http.StatusUnauthorized {
		t.Fatalf("anon flush %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/pad/weld-facts", bTok, string(payload))
	if code != http.StatusOK || !strings.Contains(body, idA) || !strings.Contains(body, idB) {
		t.Fatalf("flush %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/pad/weld-stats", bTok, "")
	if code != http.StatusOK || gjson(t, body, "lengthMm") != "400" || !strings.Contains(body, "runCount") {
		t.Fatalf("b stats %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/weld-reports?group=project", saTok, "")
	if code != http.StatusOK || !strings.Contains(body, "runCount") {
		t.Fatalf("project report %d %s", code, body)
	}
	code, body = do(t, srv, "GET", base+"/weld-runs", saTok, "")
	if code != http.StatusOK || !strings.Contains(body, "op-a") || !strings.Contains(body, "op-b") {
		t.Fatalf("runs %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/weld-reports/demo", bTok, "")
	if code != http.StatusForbidden {
		t.Fatalf("op demo %d %s", code, body)
	}
	code, body = do(t, srv, "POST", base+"/weld-reports/demo", saTok, "")
	if code != http.StatusOK || !strings.Contains(body, `"created":`) {
		t.Fatalf("demo %d %s", code, body)
	}
}

func loginWeb(t *testing.T, srv *httptest.Server, base, login, pass string) string {
	t.Helper()
	code, body := do(t, srv, "POST", base+"/login", "", `{"loginName":"`+login+`","password":"`+pass+`"}`)
	if code != http.StatusOK {
		t.Fatalf("login %s %d %s", login, code, body)
	}
	return gjson(t, body, "token")
}

func defaultPass(login string) string {
	return login + "123456"
}
