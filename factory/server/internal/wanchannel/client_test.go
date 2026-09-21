package wanchannel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	mochimqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
)

func TestEnrollConfirm(t *testing.T) {
	fid := uuid.New()
	pid := uuid.New()
	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	var claimed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/channel/enroll":
			writeJSON(w, map[string]any{
				"factoryId": fid.String(), "name": "厂A", "factoryShortCode": "F01",
				"saPersonId": pid.String(), "saLogin": "sa-a", "saDisplay": "超管A",
			})
		case "/v1/channel/claim":
			claimed = true
			writeJSON(w, map[string]any{"factoryId": fid.String(), "status": "active", "revision": 0, "factoryShortCode": "F01"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	ctx := context.Background()
	offer, err := Enroll(ctx, srv.URL, "code")
	if err != nil {
		t.Fatal(err)
	}
	if offer.FactoryID != fid || offer.SAPersonID != pid || offer.SALogin != "sa-a" {
		t.Fatalf("offer %+v", offer)
	}
	if err := Confirm(ctx, srv.URL, "code", pub); err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Fatal("claim not posted")
	}
}

func TestEnrollDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "factory is disabled"})
	}))
	t.Cleanup(srv.Close)
	_, err := Enroll(context.Background(), srv.URL, "code")
	if !errors.Is(err, domain.ErrFactoryDisabled) {
		t.Fatalf("enroll: %v", err)
	}
}

func TestEnrollRejectsWSURL(t *testing.T) {
	_, err := Enroll(context.Background(), "ws://127.0.0.1/v1/channel", "code")
	if !errors.Is(err, domain.ErrWANUnreachable) {
		t.Fatalf("ws url: %v", err)
	}
}

func TestHold(t *testing.T) {
	fid := uuid.New()
	_, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	_, mqttAddr := startAllowBroker(t)
	var sawIndex bool
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/lease"):
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		case strings.HasSuffix(r.URL.Path, "/index"):
			sawIndex = true
			writeJSON(w, map[string]any{"cmds": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(httpSrv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sawIndex {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sawIndex {
		t.Fatal("no index pull")
	}
	cancel()
	<-errCh
}

func TestHoldDropsWhenBrokerStops(t *testing.T) {
	oldAlive := aliveEvery
	aliveEvery = 200 * time.Millisecond
	t.Cleanup(func() { aliveEvery = oldAlive })

	fid := uuid.New()
	_, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	srv, mqttAddr := startAllowBroker(t)
	sawIndex := make(chan struct{}, 1)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/lease"):
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		case strings.HasSuffix(r.URL.Path, "/index"):
			select {
			case sawIndex <- struct{}{}:
			default:
			}
			writeJSON(w, map[string]any{"cmds": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(httpSrv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	}()
	select {
	case <-sawIndex:
	case err := <-errCh:
		t.Fatalf("hold: %v", err)
	case <-ctx.Done():
		t.Fatal("no index")
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, domain.ErrWANUnreachable) {
			t.Fatalf("hold after broker stop: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("hold hung after broker stop")
	}
}

func TestHoldReportsPresence(t *testing.T) {
	fid := uuid.New()
	_, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	_, mqttAddr := startAllowBroker(t)
	got := make(chan Cmd, 1)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/lease"):
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		case strings.HasSuffix(r.URL.Path, "/index"):
			writeJSON(w, map[string]any{"cmds": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(httpSrv.Close)

	cli := mqtt.NewClient(mqtt.NewClientOptions().AddBroker("tcp://" + mqttAddr).SetClientID("watch"))
	if tok := cli.Connect(); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("watch connect: %v", tok.Error())
	}
	t.Cleanup(func() { cli.Disconnect(250) })
	if tok := cli.Subscribe("wan/"+fid.String()+"/up", 1, func(_ mqtt.Client, m mqtt.Message) {
		var cmd Cmd
		if json.Unmarshal(m.Payload(), &cmd) != nil {
			return
		}
		select {
		case got <- cmd:
		default:
		}
	}); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatal(tok.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	}()
	select {
	case cmd := <-got:
		if cmd.Typ != CmdPresence || cmd.WebVersion < 1 || cmd.ServiceVersion < 1 || cmd.WebVersionName == "" || cmd.ServiceVersionName == "" {
			t.Fatalf("presence %+v", cmd)
		}
	case err := <-errCh:
		t.Fatalf("hold: %v", err)
	case <-ctx.Done():
		t.Fatal("no presence")
	}
	cancel()
	<-errCh
}

func TestHoldLeaseFailKeepsConnection(t *testing.T) {
	fid := uuid.New()
	_, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	_, mqttAddr := startAllowBroker(t)
	gotIndex := make(chan struct{}, 1)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/lease") {
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("bad-lease"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/index") {
			select {
			case gotIndex <- struct{}{}:
			default:
			}
			writeJSON(w, map[string]any{"cmds": []any{}})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(httpSrv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, nil, nil, nil, nil, nil, nil, func(Lease) error {
			return domain.ErrContentLeaseExpired
		}, nil, nil)
	}()
	select {
	case <-gotIndex:
	case err := <-errCh:
		t.Fatalf("hold dropped: %v", err)
	case <-ctx.Done():
		t.Fatal("no index")
	}
	cancel()
	<-errCh
}

func TestHoldSoftwareNotifyPassesMetaOnly(t *testing.T) {
	fid := uuid.New()
	_, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	_, mqttAddr := startAllowBroker(t)
	got := make(chan json.RawMessage, 1)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/lease"):
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		case strings.HasSuffix(r.URL.Path, "/index"):
			writeJSON(w, map[string]any{"cmds": []any{}})
		case strings.Contains(r.URL.Path, "/software/latest"):
			if r.URL.Query().Get("kind") != "factory_service" {
				http.NotFound(w, r)
				return
			}
			writeJSON(w, map[string]any{"kind": "factory_service", "version": 1})
		case strings.Contains(r.URL.Path, "/pull/software"):
			t.Error("hold must not pull software body")
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(httpSrv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, nil, nil, nil, func(in json.RawMessage) error {
			got <- in
			return nil
		}, nil, nil, nil, nil, nil)
	}()
	select {
	case raw := <-got:
		var meta struct {
			Kind    string `json:"kind"`
			Version int64  `json:"version"`
			Body    []byte `json:"body"`
		}
		if json.Unmarshal(raw, &meta) != nil || meta.Kind != "factory_service" || meta.Version != 1 || len(meta.Body) > 0 {
			t.Fatalf("meta %s", raw)
		}
	case err := <-errCh:
		t.Fatalf("hold: %v", err)
	case <-ctx.Done():
		t.Fatal("no software")
	}
	cancel()
	<-errCh
}

func TestHoldPullsClientAPKFromMQTT(t *testing.T) {
	fid := uuid.New()
	_, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	broker, mqttAddr := startAllowBroker(t)
	var sawIndex bool
	got := make(chan json.RawMessage, 1)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/lease"):
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		case strings.HasSuffix(r.URL.Path, "/index"):
			sawIndex = true
			writeJSON(w, map[string]any{"cmds": []any{}})
		case strings.Contains(r.URL.Path, "/software/latest"):
			http.NotFound(w, r)
		case strings.Contains(r.URL.Path, "/pull/software"):
			t.Error("hold must not pull software body")
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(httpSrv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, nil, nil, nil, func(in json.RawMessage) error {
			got <- in
			return nil
		}, nil, nil, nil, nil, nil)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !sawIndex {
		time.Sleep(20 * time.Millisecond)
	}
	if !sawIndex {
		t.Fatal("no index")
	}
	publishDown(t, broker, fid, Cmd{Typ: CmdSoftware, Kind: "client_apk", Version: 9, VersionName: "6.9.0"})
	waitCh(t, ctx, errCh, "apk", got)
}

func TestHoldRetired(t *testing.T) {
	fid := uuid.New()
	_, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	_, mqttAddr := startAllowBroker(t)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/lease") {
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
			return
		}
		writeJSON(w, map[string]any{"cmds": []any{map[string]any{"typ": "factory_state", "status": "retired", "revision": 1}}})
	}))
	t.Cleanup(httpSrv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, func(State) error {
		return domain.ErrFactoryRetired
	}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if !errors.Is(err, domain.ErrFactoryRetired) {
		t.Fatalf("hold: %v", err)
	}
}

func TestHoldSendsSync(t *testing.T) {
	fid := uuid.New()
	_, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	_, mqttAddr := startAllowBroker(t)
	gotKind := make(chan string, 1)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/lease") {
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/index") {
			if k := r.URL.Query().Get("kind"); k != "" {
				select {
				case gotKind <- k:
				default:
				}
			}
			writeJSON(w, map[string]any{"cmds": []any{}})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(httpSrv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out := make(chan SyncRequest, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, out)
	}()
	time.Sleep(200 * time.Millisecond)
	out <- SyncRequest{Typ: "sync_closures", Kind: "process"}
	select {
	case k := <-gotKind:
		if k != "process" {
			t.Fatalf("kind %s", k)
		}
	case err := <-errCh:
		t.Fatalf("hold: %v", err)
	case <-ctx.Done():
		t.Fatal("no sync")
	}
	cancel()
	<-errCh
}

func TestHoldAppliesDownCmds(t *testing.T) {
	fid := uuid.New()
	_, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	assetID := uuid.New()
	tplID := uuid.New()
	cid := uuid.New()
	broker, mqttAddr := startAllowBroker(t)
	var sawIndex bool
	pulled := make(chan string, 8)
	got := struct {
		closure, template, software chan json.RawMessage
		retract                     chan uuid.UUID
		client                      chan ClientIntent
	}{
		closure:  make(chan json.RawMessage, 1),
		template: make(chan json.RawMessage, 1),
		software: make(chan json.RawMessage, 1),
		retract:  make(chan uuid.UUID, 1),
		client:   make(chan ClientIntent, 1),
	}
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/lease"):
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
		case strings.HasSuffix(r.URL.Path, "/index"):
			sawIndex = true
			writeJSON(w, map[string]any{"cmds": []any{}})
		case strings.Contains(r.URL.Path, "/pull/closure/"):
			pulled <- "closure"
			writeJSON(w, map[string]any{"assetId": assetID.String(), "body": "sealed"})
		case strings.Contains(r.URL.Path, "/pull/template/"):
			pulled <- "template"
			writeJSON(w, map[string]any{"id": tplID.String(), "schema": map[string]any{"root": "object"}})
		case strings.Contains(r.URL.Path, "/software/latest"):
			if r.URL.Query().Get("kind") != "factory_service" {
				http.NotFound(w, r)
				return
			}
			writeJSON(w, map[string]any{"kind": "factory_service", "version": 1})
		case strings.Contains(r.URL.Path, "/pull/software"):
			pulled <- "software"
			writeJSON(w, map[string]any{"kind": "factory_service", "version": 1, "body": []byte("apk-bytes")})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(httpSrv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, func(in ClientIntent) error {
			got.client <- in
			return nil
		}, nil, func(in json.RawMessage) error {
			got.closure <- in
			return nil
		}, func(in json.RawMessage) error {
			got.template <- in
			return nil
		}, func(in json.RawMessage) error {
			got.software <- in
			return nil
		}, func(id uuid.UUID) error {
			got.retract <- id
			return nil
		}, nil, nil, nil, nil)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !sawIndex {
		time.Sleep(20 * time.Millisecond)
	}
	if !sawIndex {
		t.Fatal("no index")
	}
	publishDown(t, broker, fid, Cmd{Typ: CmdClosure, AssetID: assetID.String()})
	publishDown(t, broker, fid, Cmd{Typ: CmdTemplate, TemplateID: tplID.String()})
	publishDown(t, broker, fid, Cmd{Typ: CmdRetract, AssetID: assetID.String()})
	publishDown(t, broker, fid, Cmd{Typ: CmdClientBind, ClientID: cid.String(), ClientName: "焊机-1", DeviceSerial: "ARM-1", BindingRevision: 1})
	waitCh(t, ctx, errCh, "closure", got.closure)
	waitCh(t, ctx, errCh, "template", got.template)
	waitCh(t, ctx, errCh, "software", got.software)
	select {
	case id := <-got.retract:
		if id != assetID {
			t.Fatalf("retract %s", id)
		}
	case err := <-errCh:
		t.Fatalf("hold: %v", err)
	case <-ctx.Done():
		t.Fatal("no retract")
	}
	select {
	case in := <-got.client:
		if in.Typ != CmdClientBind || in.ClientID != cid || in.Name != "焊机-1" || in.DeviceSerial != "ARM-1" {
			t.Fatalf("client %+v", in)
		}
	case err := <-errCh:
		t.Fatalf("hold: %v", err)
	case <-ctx.Done():
		t.Fatal("no client")
	}
	cancel()
	<-errCh
}

func TestHoldSyncClients(t *testing.T) {
	fid := uuid.New()
	cid := uuid.New()
	_, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	_, mqttAddr := startAllowBroker(t)
	got := make(chan []uuid.UUID, 1)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/lease") {
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
			return
		}
		writeJSON(w, map[string]any{"cmds": []any{
			map[string]any{"typ": "factory_state", "status": "active", "revision": 1},
			map[string]any{"typ": "client_bind", "clientId": cid.String(), "clientName": "焊机-1", "bindingRevision": 1},
		}})
	}))
	t.Cleanup(httpSrv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, func(keep []uuid.UUID) error {
			got <- keep
			return nil
		}, nil, nil, nil, nil, nil, nil, nil, nil)
	}()
	select {
	case keep := <-got:
		if len(keep) != 1 || keep[0] != cid {
			t.Fatalf("keep %+v", keep)
		}
	case err := <-errCh:
		t.Fatalf("hold: %v", err)
	case <-ctx.Done():
		t.Fatal("no sync")
	}
	cancel()
	<-errCh
}

func TestHoldSyncClientsSkipsDisabled(t *testing.T) {
	fid := uuid.New()
	_, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	_, mqttAddr := startAllowBroker(t)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/lease") {
			writeJSON(w, map[string]any{"typ": "lease", "lease": []byte("lease-key-32-bytes-long-enough!!"), "notAfter": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
			return
		}
		writeJSON(w, map[string]any{"cmds": []any{map[string]any{"typ": "factory_state", "status": "disabled", "revision": 1}}})
	}))
	t.Cleanup(httpSrv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Hold(ctx, "tcp://"+mqttAddr, httpSrv.URL, fid, priv, nil, nil, func([]uuid.UUID) error {
			t.Error("reconcile while disabled")
			return nil
		}, nil, nil, nil, nil, nil, nil, nil, nil)
	}()
	time.Sleep(400 * time.Millisecond)
	select {
	case err := <-errCh:
		t.Fatalf("hold: %v", err)
	default:
	}
	cancel()
	<-errCh
}

func waitCh[T any](t *testing.T, ctx context.Context, errCh <-chan error, name string, ch <-chan T) {
	t.Helper()
	select {
	case <-ch:
	case err := <-errCh:
		t.Fatalf("%s hold: %v", name, err)
	case <-ctx.Done():
		t.Fatalf("no %s", name)
	}
}

func startAllowBroker(t *testing.T) (*mochimqtt.Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := mochimqtt.New(&mochimqtt.Options{
		InlineClient: true,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err := server.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatal(err)
	}
	if err := server.AddListener(listeners.NewNet("t1", ln)); err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve() }()
	t.Cleanup(func() {
		defer func() { _ = recover() }()
		_ = server.Close()
	})
	return server, ln.Addr().String()
}

// 等 Hold 订上后再往 down 投一条。
func publishDown(t *testing.T, srv *mochimqtt.Server, fid uuid.UUID, cmd Cmd) {
	t.Helper()
	raw, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Publish("wan/"+fid.String()+"/down", raw, false, 1); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
