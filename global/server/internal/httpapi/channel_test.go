package httpapi_test

import (
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/contentcrypt"
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

	ctx := context.Background()
	c, _, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):]+"/v1/channel", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "enroll", "enrollmentCode": created.EnrollmentToken}); err != nil {
		t.Fatal(err)
	}
	var enrolled struct {
		Typ        string `json:"typ"`
		FactoryID  string `json:"factoryId"`
		SALogin    string `json:"saLogin"`
		SAPersonID string `json:"saPersonId"`
		Error      string `json:"error"`
	}
	if err := wsjson.Read(ctx, c, &enrolled); err != nil {
		t.Fatal(err)
	}
	if enrolled.Typ != "enrolled" || enrolled.FactoryID != created.Factory.ID.String() || enrolled.SALogin != "sa-a" {
		t.Fatalf("enrolled: %+v", enrolled)
	}
	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "claimed", "factoryPublicKey": pub}); err != nil {
		t.Fatal(err)
	}
	var welcome struct {
		Typ   string `json:"typ"`
		Error string `json:"error"`
	}
	if err := wsjson.Read(ctx, c, &welcome); err != nil {
		t.Fatal(err)
	}
	if welcome.Typ != "welcome" {
		t.Fatalf("welcome: %+v", welcome)
	}
}

func TestChannelPresence(t *testing.T) {
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
	wsURL := "ws" + srv.URL[len("http"):] + "/v1/channel"
	ctx := context.Background()

	pub, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	c1, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, c1, map[string]any{"typ": "enroll", "enrollmentCode": created.EnrollmentToken}); err != nil {
		t.Fatal(err)
	}
	var enrolled struct {
		Typ string `json:"typ"`
	}
	if err := wsjson.Read(ctx, c1, &enrolled); err != nil || enrolled.Typ != "enrolled" {
		t.Fatalf("enrolled: %+v %v", enrolled, err)
	}
	if err := wsjson.Write(ctx, c1, map[string]any{"typ": "claimed", "factoryPublicKey": pub}); err != nil {
		t.Fatal(err)
	}
	var welcome struct {
		Typ string `json:"typ"`
	}
	if err := wsjson.Read(ctx, c1, &welcome); err != nil || welcome.Typ != "welcome" {
		t.Fatalf("welcome: %+v %v", welcome, err)
	}
	writePingWaitPong(t, ctx, c1)
	assertOnline(t, srv, tok, true)

	c2, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, c2, map[string]any{"typ": "hello", "factoryId": created.Factory.ID.String()}); err != nil {
		t.Fatal(err)
	}
	var challenge struct {
		Typ   string `json:"typ"`
		Nonce []byte `json:"nonce"`
		Error string `json:"error"`
	}
	if err := wsjson.Read(ctx, c2, &challenge); err != nil || challenge.Typ != "challenge" {
		t.Fatalf("challenge: %+v %v", challenge, err)
	}
	sig := nodekey.Sign(priv, helloBytes(created.Factory.ID, challenge.Nonce))
	if err := wsjson.Write(ctx, c2, map[string]any{"typ": "hello_ack", "signature": sig}); err != nil {
		t.Fatal(err)
	}
	var ready struct {
		Typ string `json:"typ"`
	}
	if err := wsjson.Read(ctx, c2, &ready); err != nil || ready.Typ != "ready" {
		t.Fatalf("ready: %+v %v", ready, err)
	}
	c1.CloseNow()
	assertOnline(t, srv, tok, true)
	c2.CloseNow()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !channelOnline(t, srv, tok) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("still online after close")
}

func TestChannelPlatformClosure(t *testing.T) {
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
	wsURL := "ws" + srv.URL[len("http"):] + "/v1/channel"
	ctx := context.Background()

	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "enroll", "enrollmentCode": created.EnrollmentToken}); err != nil {
		t.Fatal(err)
	}
	var enrolled struct {
		Typ string `json:"typ"`
	}
	if err := wsjson.Read(ctx, c, &enrolled); err != nil || enrolled.Typ != "enrolled" {
		t.Fatalf("enrolled: %+v %v", enrolled, err)
	}
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "claimed", "factoryPublicKey": pub}); err != nil {
		t.Fatal(err)
	}
	var welcome struct {
		Typ string `json:"typ"`
	}
	if err := wsjson.Read(ctx, c, &welcome); err != nil || welcome.Typ != "welcome" {
		t.Fatalf("welcome: %+v %v", welcome, err)
	}
	writePingWaitPong(t, ctx, c)

	code, body := do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"下发焊","content":"plat-body"}`)
	if code != http.StatusCreated {
		t.Fatalf("create %d %s", code, body)
	}
	pid := gjson(t, body, "id")
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/publish", tok, `{"expected":1}`)
	if code != http.StatusOK {
		t.Fatalf("publish %d %s", code, body)
	}
	if !waitChannel(t, ctx, c, func(msg channelWire) bool {
		return msg.Typ == "platform_closure" && msg.Closure != nil && msg.Closure.AssetID == pid && len(msg.Closure.Members) > 0 && len(msg.Closure.Members[0].Content) == 0
	}) {
		t.Fatal("no platform_closure")
	}
	if !waitChannel(t, ctx, c, func(msg channelWire) bool {
		return msg.Typ == "platform_closure_body" && msg.Closure != nil && msg.Closure.AssetID == pid && len(msg.Closure.Members) > 0 && contentcrypt.IsEnvelope(msg.Closure.Members[0].Content)
	}) {
		t.Fatal("no platform_closure_body")
	}

	rev := gjson(t, body, "revision")
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/disable", tok, `{"expected":`+rev+`}`)
	if code != http.StatusOK {
		t.Fatalf("disable %d %s", code, body)
	}
	if !waitChannel(t, ctx, c, func(msg channelWire) bool {
		return msg.Typ == "platform_closure" && msg.Closure != nil && msg.Closure.AssetID == pid && msg.Closure.Status == "disabled"
	}) {
		t.Fatal("no disabled platform_closure")
	}

	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/delete", tok, "")
	if code != http.StatusOK {
		t.Fatalf("delete %d %s", code, body)
	}
	if !waitChannel(t, ctx, c, func(msg channelWire) bool {
		return msg.Typ == "platform_retract" && msg.AssetID == pid
	}) {
		t.Fatal("no platform_retract")
	}
}

func TestChannelLeaseBeforeReplay(t *testing.T) {
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
	code, body := do(t, srv, "POST", "/v1/assets", tok, `{"kind":"process","name":"握手焊","content":"plat-body"}`)
	if code != http.StatusCreated {
		t.Fatalf("create %d %s", code, body)
	}
	pid := gjson(t, body, "id")
	code, body = do(t, srv, "POST", "/v1/assets/"+pid+"/publish", tok, `{"expected":1}`)
	if code != http.StatusOK {
		t.Fatalf("publish %d %s", code, body)
	}

	wsURL := "ws" + srv.URL[len("http"):] + "/v1/channel"
	ctx := context.Background()
	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "enroll", "enrollmentCode": created.EnrollmentToken}); err != nil {
		t.Fatal(err)
	}
	var enrolled struct {
		Typ string `json:"typ"`
	}
	if err := wsjson.Read(ctx, c, &enrolled); err != nil || enrolled.Typ != "enrolled" {
		t.Fatalf("enrolled: %+v %v", enrolled, err)
	}
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "claimed", "factoryPublicKey": pub}); err != nil {
		t.Fatal(err)
	}
	var welcome struct {
		Typ string `json:"typ"`
	}
	if err := wsjson.Read(ctx, c, &welcome); err != nil || welcome.Typ != "welcome" {
		t.Fatalf("welcome: %+v %v", welcome, err)
	}

	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	var first channelWire
	err = wsjson.Read(readCtx, c, &first)
	cancel()
	if err != nil || first.Typ != "content_lease" {
		t.Fatalf("first after welcome: %+v %v", first, err)
	}
	bodies := 0
	sawMeta := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && bodies < 2 {
		readCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		var msg channelWire
		err := wsjson.Read(readCtx, c, &msg)
		cancel()
		if err != nil {
			continue
		}
		if msg.Typ == "platform_closure" && msg.Closure != nil && msg.Closure.AssetID == pid && len(msg.Closure.Members) > 0 {
			if len(msg.Closure.Members[0].Content) != 0 {
				t.Fatal("control plane carried body")
			}
			sawMeta = true
		}
		if msg.Typ == "platform_closure_body" && msg.Closure != nil && msg.Closure.AssetID == pid && len(msg.Closure.Members) > 0 && contentcrypt.IsEnvelope(msg.Closure.Members[0].Content) {
			bodies++
		}
	}
	if !sawMeta || bodies < 2 {
		t.Fatalf("lease-then-replay meta=%v bodies=%d", sawMeta, bodies)
	}
}

func TestChannelLifecycle(t *testing.T) {
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
	wsURL := "ws" + srv.URL[len("http"):] + "/v1/channel"
	ctx := context.Background()

	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "enroll", "enrollmentCode": created.EnrollmentToken}); err != nil {
		t.Fatal(err)
	}
	var enrolled struct {
		Typ string `json:"typ"`
	}
	if err := wsjson.Read(ctx, c, &enrolled); err != nil || enrolled.Typ != "enrolled" {
		t.Fatalf("enrolled: %+v %v", enrolled, err)
	}
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "claimed", "factoryPublicKey": pub}); err != nil {
		t.Fatal(err)
	}
	var welcome struct {
		Typ string `json:"typ"`
	}
	if err := wsjson.Read(ctx, c, &welcome); err != nil || welcome.Typ != "welcome" {
		t.Fatalf("welcome: %+v %v", welcome, err)
	}
	writePingWaitPong(t, ctx, c)

	fid := created.Factory.ID.String()
	code, body := do(t, srv, "POST", "/v1/factories/"+fid+"/disable", tok, "")
	if code != http.StatusOK {
		t.Fatalf("disable %d %s", code, body)
	}
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var st struct {
		Typ      string `json:"typ"`
		Status   string `json:"status"`
		Revision int64  `json:"revision"`
	}
	if err := wsjson.Read(readCtx, c, &st); err != nil || st.Typ != "factory_state" || st.Status != "disabled" || st.Revision != 1 {
		t.Fatalf("factory_state %+v %v", st, err)
	}
}

func TestChannelClientBind(t *testing.T) {
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
	wsURL := "ws" + srv.URL[len("http"):] + "/v1/channel"
	ctx := context.Background()

	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "enroll", "enrollmentCode": created.EnrollmentToken}); err != nil {
		t.Fatal(err)
	}
	var enrolled struct {
		Typ string `json:"typ"`
	}
	if err := wsjson.Read(ctx, c, &enrolled); err != nil || enrolled.Typ != "enrolled" {
		t.Fatalf("enrolled: %+v %v", enrolled, err)
	}
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "claimed", "factoryPublicKey": pub}); err != nil {
		t.Fatal(err)
	}
	var welcome struct {
		Typ string `json:"typ"`
	}
	if err := wsjson.Read(ctx, c, &welcome); err != nil || welcome.Typ != "welcome" {
		t.Fatalf("welcome: %+v %v", welcome, err)
	}
	writePingWaitPong(t, ctx, c)

	fid := created.Factory.ID.String()
	code, body := do(t, srv, "POST", "/v1/clients", tok, `{"name":"焊机-1","factoryId":"`+fid+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("register %d %s", code, body)
	}
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var bind struct {
		Typ             string `json:"typ"`
		ClientName      string `json:"clientName"`
		BindingRevision int64  `json:"bindingRevision"`
		Error           string `json:"error"`
	}
	if err := wsjson.Read(readCtx, c, &bind); err != nil || bind.Typ != "client_bind" || bind.ClientName != "焊机-1" || bind.BindingRevision != 1 {
		t.Fatalf("client_bind %+v %v", bind, err)
	}
}

func TestChannelHelloAfterDelete(t *testing.T) {
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
	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmEnroll(context.Background(), created.Factory.ID, pub); err != nil {
		t.Fatal(err)
	}
	fid := created.Factory.ID.String()
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	t.Cleanup(srv.Close)

	code, body := do(t, srv, "DELETE", "/v1/factories/"+fid, tok, "")
	if code != http.StatusOK {
		t.Fatalf("delete %d %s", code, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):]+"/v1/channel", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "hello", "factoryId": fid}); err != nil {
		t.Fatal(err)
	}
	var msg struct {
		Typ   string `json:"typ"`
		Error string `json:"error"`
	}
	if err := wsjson.Read(ctx, c, &msg); err != nil || msg.Typ != "error" || msg.Error != "factory is retired" {
		t.Fatalf("hello after delete: %+v %v", msg, err)
	}
}

func TestPromoteViaChannel(t *testing.T) {
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
	wsURL := "ws" + srv.URL[len("http"):] + "/v1/channel"
	ctx := context.Background()
	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "enroll", "enrollmentCode": created.EnrollmentToken}); err != nil {
		t.Fatal(err)
	}
	var enrolled struct {
		Typ string `json:"typ"`
	}
	if err := wsjson.Read(ctx, c, &enrolled); err != nil || enrolled.Typ != "enrolled" {
		t.Fatalf("enrolled: %+v %v", enrolled, err)
	}
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "claimed", "factoryPublicKey": pub}); err != nil {
		t.Fatal(err)
	}
	var welcome struct {
		Typ string `json:"typ"`
	}
	if err := wsjson.Read(ctx, c, &welcome); err != nil || welcome.Typ != "welcome" {
		t.Fatalf("welcome: %+v %v", welcome, err)
	}

	aid := created.Factory.ID
	body := []byte("from-fac")
	sum := sha256.Sum256(body)
	go func() {
		for {
			var msg struct {
				Typ     string `json:"typ"`
				ReqID   string `json:"reqId"`
				Kind    string `json:"kind"`
				AssetID string `json:"assetId"`
			}
			if err := wsjson.Read(ctx, c, &msg); err != nil {
				return
			}
			switch msg.Typ {
			case "asset_list":
				_ = wsjson.Write(ctx, c, map[string]any{
					"typ": "asset_list_ok", "reqId": msg.ReqID,
					"assets": []map[string]any{{
						"id": aid.String(), "kind": "process", "name": "厂级焊", "revision": 1,
						"digest": sum[:], "status": "available", "copyable": true,
					}},
				})
			case "asset_snapshot":
				_ = wsjson.Write(ctx, c, map[string]any{
					"typ": "asset_snapshot_ok", "reqId": msg.ReqID,
					"snapshot": map[string]any{
						"sourceId": aid.String(), "sourceRevision": 1, "sourceFactoryId": created.Factory.ID.String(),
						"kind": "process", "name": "厂级焊", "content": body, "digest": sum[:],
						"copyable": true, "status": "available", "deps": []any{},
					},
				})
			}
		}
	}()
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "ping"}); err != nil {
		t.Fatal(err)
	}
	assertOnline(t, srv, tok, true)

	deadline := time.Now().Add(3 * time.Second)
	var code int
	var resp string
	for time.Now().Before(deadline) {
		code, resp = do(t, srv, "GET", "/v1/factories/"+created.Factory.ID.String()+"/promotable-assets?kind=process", tok, "")
		if code == http.StatusOK && strings.Contains(resp, "厂级焊") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if code != http.StatusOK || !strings.Contains(resp, "厂级焊") {
		t.Fatalf("list %d %s", code, resp)
	}
	code, resp = do(t, srv, "POST", "/v1/assets/promote-from", tok, `{"factoryId":"`+created.Factory.ID.String()+`","assetId":"`+aid.String()+`"}`)
	if code != http.StatusCreated || !strings.Contains(resp, `"level":"platform"`) || !strings.Contains(resp, `"status":"draft"`) {
		t.Fatalf("promote-from %d %s", code, resp)
	}
}

func assertOnline(t *testing.T, srv *httptest.Server, tok string, want bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
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

func helloBytes(factoryID [16]byte, nonce []byte) []byte {
	b := make([]byte, 0, 18+16+len(nonce))
	b = append(b, "wmesh-wan-hello-v1"...)
	b = append(b, factoryID[:]...)
	b = append(b, nonce...)
	return b
}

type channelWire struct {
	Typ     string `json:"typ"`
	AssetID string `json:"assetId"`
	Closure *struct {
		AssetID string `json:"assetId"`
		Status  string `json:"status"`
		Members []struct {
			Content []byte `json:"content"`
		} `json:"members"`
	} `json:"closure"`
}

func waitChannel(t *testing.T, ctx context.Context, c *websocket.Conn, match func(channelWire) bool) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		var msg channelWire
		err := wsjson.Read(readCtx, c, &msg)
		cancel()
		if err != nil {
			continue
		}
		if match(msg) {
			return true
		}
	}
	return false
}

func writePingWaitPong(t *testing.T, ctx context.Context, c *websocket.Conn) {
	t.Helper()
	if err := wsjson.Write(ctx, c, map[string]any{"typ": "ping"}); err != nil {
		t.Fatal(err)
	}
	if !waitChannel(t, ctx, c, func(msg channelWire) bool { return msg.Typ == "pong" }) {
		t.Fatal("pong")
	}
}
