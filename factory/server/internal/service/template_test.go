// 厂端接收模版副本不改已有正文；之后新建才套用。
package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

func TestAcceptTemplateDelivery(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	seed, fac, err := h.Provision(ctx, "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	if err := fac.Activate(ctx, "sa-a", seed.ActivationToken, "sa-pass"); err != nil {
		t.Fatal(err)
	}
	sa := mustLogin(t, ctx, fac, "sa-a", "sa-pass")
	pe := mustCreateRole(t, ctx, fac, sa, "pe-a", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	direct := factory.WorkContext{Direct: true}
	body := []byte(`{"name":"厂内","current":190,"legacy":true}`)
	proc, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "厂内工艺", body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fac.ReadAssetContent(ctx, pe.tok, proc.ID)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("before template %q %v", got, err)
	}

	schema, err := contenttpl.Marshal(contenttpl.Default(contenttpl.KindProcess))
	if err != nil {
		t.Fatal(err)
	}
	fid := seed.ID
	snap := factory.TemplateSnapshot{
		ID: id.New(), Kind: factory.KindProcess, Revision: 1,
		Schema: json.RawMessage(schema), Digest: digest.Sum(schema), TargetFactoryID: &fid,
	}
	bad := snap
	bad.Digest = bytes.Repeat([]byte{1}, 32)
	if err := fac.AcceptTemplateDelivery(ctx, bad); !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("bad digest: %v", err)
	}
	if err := fac.AcceptTemplateDelivery(ctx, snap); err != nil {
		t.Fatal(err)
	}
	got, err = fac.ReadAssetContent(ctx, pe.tok, proc.ID)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("after %s want %s err %v", got, body, err)
	}
	still, err := fac.GetAsset(ctx, pe.tok, proc.ID)
	if err != nil || still.Revision != proc.Revision {
		t.Fatalf("rev %+v", still)
	}
	tpl, err := fac.GetTemplate(ctx, pe.tok, factory.KindProcess)
	if err != nil || tpl.Revision != 1 {
		t.Fatalf("tpl %+v %v", tpl, err)
	}

	created, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "新工艺", body)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := fac.ReadAssetContent(ctx, pe.tok, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	want, err := contenttpl.Apply(schema, body)
	if err != nil || !bytes.Equal(applied, want) {
		t.Fatalf("new %s want %s err %v", applied, want, err)
	}
}

func TestListProjectTemplates(t *testing.T) {
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
	empty, err := fac.ListProjectTemplates(ctx, sa)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty %+v %v", empty, err)
	}
	var single contenttpl.ProjectItemSchema
	for _, it := range contenttpl.SeedProjectItems() {
		if it.Name == "单层焊道" {
			single = it
			break
		}
	}
	raw, err := contenttpl.Marshal(contenttpl.ObjectSchema(single.Fields))
	if err != nil {
		t.Fatal(err)
	}
	fid := seed.ID
	if err := fac.AcceptTemplateDelivery(ctx, factory.TemplateSnapshot{
		ID: id.New(), Kind: factory.KindProject, Name: single.Name, Revision: 3, Schema: raw, Digest: digest.Sum(raw),
		TargetFactoryID: &fid,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := fac.ListProjectTemplates(ctx, sa)
	if err != nil || len(rows) != 1 || rows[0].Name != single.Name || rows[0].Revision != 3 {
		t.Fatalf("list %+v %v", rows, err)
	}
	anonID := id.New()
	anon := factory.TemplateSnapshot{
		ID: anonID, Kind: factory.KindProject, Revision: 1, Schema: raw, Digest: digest.Sum(raw),
		TargetFactoryID: &fid,
	}
	if err := fac.AcceptTemplateDelivery(ctx, anon); err != nil {
		t.Fatal(err)
	}
	anon.Name = "坡口"
	if err := fac.AcceptTemplateDelivery(ctx, anon); err != nil {
		t.Fatal(err)
	}
	rows, err = fac.ListProjectTemplates(ctx, sa)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.ID == anonID && row.Name == "坡口" && row.Revision == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("name not filled %+v", rows)
	}
	stale := []byte(`{"root":"array","templates":[{"id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","kind":"corner","name":"包角","extra":true}]}`)
	if _, err := fac.Store().InsertTemplateReplica(ctx, factory.ContentTemplate{
		ID: id.New(), Kind: factory.KindProject, Revision: 9, Schema: stale, Digest: digest.Sum(stale),
	}); err != nil {
		t.Fatal(err)
	}
	rows, err = fac.ListProjectTemplates(ctx, sa)
	if err != nil || len(rows) != 2 {
		t.Fatalf("stale catalog %+v %v", rows, err)
	}
}
