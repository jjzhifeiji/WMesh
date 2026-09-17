// 平板登录拉的工艺/工程与厂端列表同一范围，不另走下发授权。
package service_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contentcrypt"
	factory "wmesh/factory/internal/service"
)

func TestPadInboxMatchesFactoryList(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil)
	_ = mustCreateRole(t, ctx, fac, sa, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	direct := factory.WorkContext{Direct: true}
	draftProc, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "草稿工艺", []byte(`{"current":1}`))
	if err != nil {
		t.Fatal(err)
	}
	proc, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "工艺", []byte(`{"current":180}`))
	if err != nil {
		t.Fatal(err)
	}
	proc, err = fac.PublishAsset(ctx, pe.tok, proc.ID, proc.Revision)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`[{"name":"w","processId":"` + proc.ID.String() + `"}]`)
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "工程", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	proj, err = fac.PublishAsset(ctx, pe.tok, proj.ID, proj.Revision)
	if err != nil {
		t.Fatal(err)
	}
	draftProj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "草稿工程", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := fac.CreatePersonalProject(ctx, pe.tok, direct, "个人工程", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	mine, err = fac.PublishAsset(ctx, pe.tok, mine.ID, mine.Revision)
	if err != nil {
		t.Fatal(err)
	}

	sess, err := fac.LoginPad(ctx, "op", "op-pass")
	if err != nil || len(sess.UnwrapKey) != 32 || len(sess.Devices) != 0 {
		t.Fatalf("pad keys %+v %v", sess, err)
	}
	listed, err := fac.ListAssets(ctx, sess.Token, "")
	if err != nil {
		t.Fatal(err)
	}
	box, err := fac.PadClientInbox(ctx, sess.Token)
	if err != nil {
		t.Fatal(err)
	}
	if len(box.Closures) != len(listed) {
		t.Fatalf("pad %d list %d", len(box.Closures), len(listed))
	}
	for _, a := range listed {
		if !padInboxHas(box, a.ID) {
			t.Fatalf("missing %s %s", a.Kind, a.Name)
		}
	}
	if !padInboxHas(box, draftProc.ID) || !padInboxHas(box, draftProj.ID) || !padInboxHas(box, mine.ID) {
		t.Fatalf("draft/personal %+v", box.Closures)
	}

	gotProc, err := fac.PadPullClientClosure(ctx, sess.Token, proc.ID)
	if err != nil || !contentcrypt.IsEnvelope(gotProc.Wrap) || gotProc.Snapshot.Kind != factory.KindProcess {
		t.Fatalf("process pull %+v %v", gotProc, err)
	}
	opened, err := factory.OpenTransit(fac.Store().FactoryID(), sess.Account.ID, sess.UnwrapKey, gotProc)
	if err != nil || len(opened.Members) != 1 {
		t.Fatalf("person key open %+v %v", opened, err)
	}
	gotDraft, err := fac.PadPullClientClosure(ctx, sess.Token, draftProj.ID)
	if err != nil || gotDraft.Snapshot.Kind != factory.KindProject || len(gotDraft.Wrap) != 0 || len(gotDraft.Snapshot.Members) != 1 {
		t.Fatalf("draft pull %+v %v", gotDraft, err)
	}
	if contentcrypt.IsEnvelope(gotDraft.Snapshot.Members[0].Content) {
		t.Fatal("project must be plaintext")
	}
	if !bytes.Contains(gotDraft.Snapshot.Members[0].Content, []byte(proc.ID.String())) {
		t.Fatalf("project missing processId %s", gotDraft.Snapshot.Members[0].Content)
	}
	gotMine, err := fac.PadPullClientClosure(ctx, sess.Token, mine.ID)
	if err != nil || gotMine.Snapshot.AssetID != mine.ID {
		t.Fatalf("personal pull %+v %v", gotMine, err)
	}

	if _, err := fac.SetClientPolicy(ctx, sa, factory.ClientPolicy{
		MaxCachedProjects: 2, CacheScope: factory.CacheScopeCurrent, PersistUnwrapKey: false, KeyTTLSeconds: 0, EncryptPouch: true,
	}); err != nil {
		t.Fatal(err)
	}
	again, err := fac.PadClientInbox(ctx, sess.Token)
	if err != nil || !padInboxHas(again, proj.ID) || !padInboxHas(again, proc.ID) {
		t.Fatalf("current scope pad %+v %v", again, err)
	}
}

func padInboxHas(box factory.ClientInbox, id uuid.UUID) bool {
	for _, r := range box.Closures {
		if r.AssetID == id {
			return true
		}
	}
	return false
}
