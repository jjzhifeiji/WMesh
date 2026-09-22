// C1：钉机械臂号、本机登录领钥；空号/他机号/未绑定拒绝。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	"wmesh/factory/internal/platform/secret"
	factory "wmesh/factory/internal/service"
)

func TestRegisterDeviceAndLogin(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	cid := id.New()
	if _, err := fac.AcceptBinding(ctx, cid, "焊机", pub, 1); err != nil {
		t.Fatal(err)
	}

	if _, err := fac.RegisterDevice(ctx, cid, ""); !errors.Is(err, domain.ErrDeviceSerialRequired) {
		t.Fatalf("empty: %v", err)
	}
	row, err := fac.RegisterDevice(ctx, cid, "ARM-1")
	if err != nil || row.DeviceSerial != "ARM-1" || len(row.UnwrapKey) != 0 {
		t.Fatalf("pin %+v %v", row, err)
	}
	again, err := fac.RegisterDevice(ctx, cid, "ARM-1")
	if err != nil || again.DeviceSerial != "ARM-1" {
		t.Fatalf("idempotent %+v %v", again, err)
	}

	cid2 := id.New()
	pub2, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.AcceptBinding(ctx, cid2, "焊机2", pub2, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.RegisterDevice(ctx, cid2, "ARM-1"); !errors.Is(err, domain.ErrDeviceSerialTaken) {
		t.Fatalf("taken: %v", err)
	}

	if _, err := fac.LoginOnClient(ctx, cid, "", "op", "op-pass"); !errors.Is(err, domain.ErrDeviceSerialRequired) {
		t.Fatalf("login empty: %v", err)
	}
	if _, err := fac.LoginOnClient(ctx, cid, "ARM-9", "op", "op-pass"); !errors.Is(err, domain.ErrDeviceSerialMismatch) {
		t.Fatalf("mismatch: %v", err)
	}
	if _, err := fac.LoginOnClient(ctx, id.New(), "ARM-1", "op", "op-pass"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unbound: %v", err)
	}

	sess, err := fac.LoginOnClient(ctx, cid, "ARM-1", "op", "op-pass")
	if err != nil || sess.Token == "" || len(sess.UnwrapKey) != 32 || sess.Account.ID != op.acc.ID {
		t.Fatalf("login %+v %v", sess, err)
	}
	if sess.Policy.PersistUnwrapKey || sess.Policy.CacheScope != factory.CacheScopeAll {
		t.Fatalf("policy %+v", sess.Policy)
	}
	if err := fac.Store().PutClientShortCode(ctx, cid, "C0008"); err != nil {
		t.Fatal(err)
	}
	sess, err = fac.LoginOnClient(ctx, cid, "ARM-1", "op", "op-pass")
	if err != nil || sess.ClientShortCode != "C0008" {
		t.Fatalf("short %+v %v", sess, err)
	}
	hasOp := false
	for _, r := range sess.Roles {
		if r == factory.RoleOperator {
			hasOp = true
		}
	}
	if !hasOp {
		t.Fatalf("roles %v", sess.Roles)
	}
	if _, err := fac.RequireActive(ctx, sess.Token); err != nil {
		t.Fatal(err)
	}

	if _, err := fac.RegisterDevice(ctx, cid, "ARM-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.LoginOnClient(ctx, cid, "ARM-1", "op", "op-pass"); !errors.Is(err, domain.ErrDeviceSerialMismatch) {
		t.Fatalf("old arm: %v", err)
	}
	if _, err := fac.LoginOnClient(ctx, cid, "ARM-2", "op", "op-pass"); err != nil {
		t.Fatal(err)
	}

	if err := fac.VoidBinding(ctx, cid); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.RegisterDevice(ctx, cid, "ARM-3"); !errors.Is(err, domain.ErrBindingVoid) {
		t.Fatalf("void pin: %v", err)
	}
	if _, err := fac.LoginOnClient(ctx, cid, "ARM-2", "op", "op-pass"); !errors.Is(err, domain.ErrBindingVoid) {
		t.Fatalf("void login: %v", err)
	}

	rows, err := fac.ListAudit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bad := audit.Incomplete(rows); len(bad) > 0 {
		t.Fatalf("incomplete %#v", bad[0])
	}
	dump := audit.Dump(rows)
	if !audit.ContainsAny(dump, "client_register") || !audit.ContainsAny(dump, "client_login") {
		t.Fatalf("audit %s", dump)
	}
	if audit.ContainsAny(dump, "op-pass", "sa-pass") {
		t.Fatal("password leaked")
	}
}

func TestPadLoginListsDevicesWithoutSerial(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	cidA := id.New()
	cidB := id.New()
	pubA, _, _ := nodekey.Generate()
	pubB, _, _ := nodekey.Generate()
	if _, err := fac.AcceptBinding(ctx, cidA, "焊机A", pubA, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.AcceptBinding(ctx, cidB, "焊机B", pubB, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.RegisterDevice(ctx, cidA, "ARM-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.RegisterDevice(ctx, cidB, "ARM-B"); err != nil {
		t.Fatal(err)
	}

	sess, err := fac.LoginPad(ctx, "op", "op-pass")
	if err != nil || sess.Token == "" || sess.Account.ID != op.acc.ID || len(sess.Devices) != 2 || len(sess.UnwrapKey) != 32 {
		t.Fatalf("pad %+v %v", sess, err)
	}
	seen := map[string]factory.PadDevice{}
	for _, d := range sess.Devices {
		if d.DeviceSerial == "" {
			t.Fatalf("device %+v", d)
		}
		seen[d.DeviceSerial] = d
	}
	if _, ok := seen["ARM-A"]; !ok {
		t.Fatal("missing ARM-A")
	}
	if _, ok := seen["ARM-B"]; !ok {
		t.Fatal("missing ARM-B")
	}

	listed, err := fac.ListClients(ctx, saTok)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range listed {
		if c.OperatorID != nil {
			t.Fatalf("pad login occupied operator %+v", c)
		}
	}

	if _, err := fac.ClientInbox(ctx, sess.Token, cidA); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("operator inbox: %v", err)
	}
	if _, err := fac.PadClientInbox(ctx, sess.Token); err != nil {
		t.Fatalf("pad inbox: %v", err)
	}
	if _, err := fac.AuthClientMQTT(ctx, op.acc.ID, sess.Token); err != nil {
		t.Fatalf("pad person mqtt: %v", err)
	}
	if _, err := fac.AuthClientMQTT(ctx, cidA, sess.Token); err != nil {
		t.Fatalf("pad bound mqtt: %v", err)
	}

	rows, err := fac.ListAudit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !audit.ContainsAny(audit.Dump(rows), "pad_login") {
		t.Fatalf("audit %s", audit.Dump(rows))
	}
}

func TestPadLoginMarksAppOnlineAndVersion(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	cat, err := fac.Catalog(ctx, saTok)
	if err != nil {
		t.Fatal(err)
	}
	if cat.Me.AppOnline {
		t.Fatal("web login must not mark app online")
	}
	for _, p := range cat.People {
		if p.ID == op.acc.ID && (p.AppOnline || p.AppVersion != 0 || p.AppLastSeenAt != nil) {
			t.Fatalf("idle app %+v", p)
		}
	}

	if _, err := fac.LoginPad(ctx, "op", "op-pass"); err != nil {
		t.Fatal(err)
	}
	if err := fac.NoteAppPresence(ctx, op.acc.ID, 52, "6.1.1"); err != nil {
		t.Fatal(err)
	}
	cat, err = fac.Catalog(ctx, saTok)
	if err != nil {
		t.Fatal(err)
	}
	var row factory.Account
	for _, p := range cat.People {
		if p.ID == op.acc.ID {
			row = p
		}
	}
	if row.AppOnline || row.AppVersion != 52 || row.AppVersionName != "6.1.1" || row.AppLastSeenAt == nil {
		t.Fatalf("pad session must not mark mqtt online %+v", row)
	}

	fac.NoteAppMQTT(ctx, op.acc.ID, true)
	cat, err = fac.Catalog(ctx, saTok)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cat.People {
		if p.ID == op.acc.ID {
			row = p
		}
	}
	if !row.AppOnline || row.AppVersion != 52 {
		t.Fatalf("mqtt online %+v", row)
	}

	raw, _ := json.Marshal(map[string]any{"typ": "presence", "version": 53, "versionName": "6.1.2"})
	fac.HandleAppUp(ctx, op.acc.ID, raw)
	fac.NoteAppMQTT(ctx, op.acc.ID, true)
	fac.NoteAppMQTT(ctx, op.acc.ID, false)
	cat, err = fac.Catalog(ctx, saTok)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cat.People {
		if p.ID == op.acc.ID && (!p.AppOnline || p.AppVersion != 53 || p.AppVersionName != "6.1.2") {
			t.Fatalf("one mqtt still online %+v", p)
		}
	}
	fac.NoteAppMQTT(ctx, op.acc.ID, false)
	cat, err = fac.Catalog(ctx, saTok)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cat.People {
		if p.ID == op.acc.ID && (p.AppOnline || p.AppVersion != 53) {
			t.Fatalf("mqtt offline %+v", p)
		}
	}
}

func TestPersonLoginLogsKeepDeviceAndWifi(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	cid := id.New()
	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.AcceptBinding(ctx, cid, "焊机A", pub, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.RegisterDevice(ctx, cid, "ARM-1"); err != nil {
		t.Fatal(err)
	}

	if _, err := fac.LoginPad(ctx, "op", "op-pass"); err != nil {
		t.Fatal(err)
	}
	snap := factory.LoginSnap{
		Kind: factory.LoginKindPad, AppVersion: 52, AppVersionName: "6.1.1",
		DeviceModel: "TB-X606F", DeviceManufacturer: "Lenovo", AndroidRelease: "10",
		NetworkName: "Factory-WiFi",
	}
	if err := fac.RecordAppLogin(ctx, op.acc.ID, snap); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.ListPersonLogins(ctx, op.tok, op.acc.ID); err == nil {
		t.Fatal("operator must not read login logs")
	}
	rows, err := fac.ListPersonLogins(ctx, saTok, op.acc.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("logs %+v %v", rows, err)
	}
	got := rows[0]
	if got.Kind != factory.LoginKindPad || got.AppVersion != 52 || got.AppVersionName != "6.1.1" ||
		got.DeviceModel != "TB-X606F" || got.NetworkName != "Factory-WiFi" || got.DeviceSerial != "" {
		t.Fatalf("pad log %+v", got)
	}

	raw, _ := json.Marshal(map[string]any{
		"typ": "presence", "version": 52, "versionName": "6.1.1",
		"deviceModel": "TB-X606F", "deviceManufacturer": "Lenovo", "androidRelease": "10",
		"networkName": "Factory-WiFi",
	})
	fac.HandleAppUp(ctx, op.acc.ID, raw)
	rows, err = fac.ListPersonLogins(ctx, saTok, op.acc.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("same mqtt %+v %v", rows, err)
	}

	raw, _ = json.Marshal(map[string]any{
		"typ": "presence", "version": 52, "versionName": "6.1.1",
		"deviceSerial": "ARM-1", "deviceModel": "TB-X606F", "deviceManufacturer": "Lenovo",
		"androidRelease": "10", "networkName": "Factory-WiFi",
	})
	fac.HandleAppUp(ctx, op.acc.ID, raw)
	rows, err = fac.ListPersonLogins(ctx, saTok, op.acc.ID)
	if err != nil || len(rows) != 2 || rows[0].Kind != factory.LoginKindMQTT || rows[0].DeviceSerial != "ARM-1" || rows[0].ClientName != "焊机A" {
		t.Fatalf("serial mqtt %+v %v", rows, err)
	}
	if rows[0].ClientID == nil || *rows[0].ClientID != cid {
		t.Fatalf("client id %+v", rows[0].ClientID)
	}
	cat, err := fac.Catalog(ctx, saTok)
	if err != nil {
		t.Fatal(err)
	}
	var seen factory.Account
	for _, p := range cat.People {
		if p.ID == op.acc.ID {
			seen = p
		}
	}
	if seen.AppClientName != "焊机A" || seen.AppDeviceSerial != "ARM-1" {
		t.Fatalf("last device %+v", seen)
	}
}

func TestPadLoginIssuesUnwrapKeyWithoutSerial(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	_ = mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	cid := id.New()
	pub, _, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.AcceptBinding(ctx, cid, "焊机", pub, 1); err != nil {
		t.Fatal(err)
	}

	sess, err := fac.LoginPad(ctx, "op", "op-pass")
	if err != nil || len(sess.Devices) != 1 {
		t.Fatalf("pad %+v %v", sess, err)
	}
	if sess.Devices[0].ID != cid || sess.Devices[0].DeviceSerial != "" || len(sess.UnwrapKey) != 32 {
		t.Fatalf("device %+v key %d", sess.Devices[0], len(sess.UnwrapKey))
	}
}

func TestLoginOnClientClearsOtherDevice(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	cidA := id.New()
	cidB := id.New()
	pubA, _, _ := nodekey.Generate()
	pubB, _, _ := nodekey.Generate()
	if _, err := fac.AcceptBinding(ctx, cidA, "A", pubA, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.AcceptBinding(ctx, cidB, "B", pubB, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.RegisterDevice(ctx, cidA, "ARM-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.RegisterDevice(ctx, cidB, "ARM-B"); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.LoginOnClient(ctx, cidA, "ARM-A", "op", "op-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.LoginOnClient(ctx, cidB, "ARM-B", "op", "op-pass"); err != nil {
		t.Fatal(err)
	}
	listed, err := fac.ListClients(ctx, saTok)
	if err != nil {
		t.Fatal(err)
	}
	var aOp, bOp bool
	for _, c := range listed {
		if c.ID == cidA && c.OperatorID != nil && *c.OperatorID == op.acc.ID {
			aOp = true
		}
		if c.ID == cidB && c.OperatorID != nil && *c.OperatorID == op.acc.ID {
			bOp = true
		}
	}
	if aOp || !bOp {
		t.Fatalf("operator still on old client: a=%v b=%v", aOp, bOp)
	}
}

func TestAppTokenUsesKeyTTL(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	open, err := fac.LoginPad(ctx, "op", "op-pass")
	if err != nil {
		t.Fatal(err)
	}
	row, err := fac.Store().SessionByTokenHash(ctx, secret.TokenHash(open.Token))
	if err != nil {
		t.Fatal(err)
	}
	if row.ExpiresAt.Year() < 9999 {
		t.Fatalf("ttl 0 should last until logout, expires %s", row.ExpiresAt)
	}
	cur, err := fac.GetClientPolicy(ctx, saTok)
	if err != nil {
		t.Fatal(err)
	}
	cur.KeyTTLSeconds = 90
	if _, err := fac.SetClientPolicy(ctx, saTok, cur); err != nil {
		t.Fatal(err)
	}
	limited, err := fac.LoginPad(ctx, "op", "op-pass")
	if err != nil {
		t.Fatal(err)
	}
	row, err = fac.Store().SessionByTokenHash(ctx, secret.TokenHash(limited.Token))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if row.ExpiresAt.Before(now.Add(60*time.Second)) || row.ExpiresAt.After(now.Add(2*time.Minute)) {
		t.Fatalf("ttl 90s expires %s", row.ExpiresAt)
	}
}
