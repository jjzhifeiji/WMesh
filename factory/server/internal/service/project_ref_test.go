// 工程焊道引用工艺身份：对齐 deps，不套旧正文。
package service_test

import (
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

func TestProjectProcessRefs(t *testing.T) {
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
	direct := factory.WorkContext{Direct: true}

	proc, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "工艺", []byte(`{"name":"p","current":180}`))
	if err != nil {
		t.Fatal(err)
	}
	proc, err = fac.PublishAsset(ctx, pe.tok, proc.ID, proc.Revision)
	if err != nil {
		t.Fatal(err)
	}
	dep := factory.AssetDep{ID: proc.ID, Revision: proc.Revision, Digest: proc.Digest}

	empty, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "空引用", []byte(`[{"name":"焊道"}]`), []factory.AssetDep{dep})
	if err != nil {
		t.Fatal(err)
	}
	emptyBody, err := fac.ReadAssetContent(ctx, pe.tok, empty.ID)
	if err != nil {
		t.Fatal(err)
	}
	schJSON, err := contenttpl.Marshal(contenttpl.Default(contenttpl.KindProject))
	if err != nil {
		t.Fatal(err)
	}
	ids, err := contenttpl.CollectProcessIDs(schJSON, emptyBody)
	if err != nil || len(ids) != 0 {
		t.Fatalf("empty refs %v %v", ids, err)
	}
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "错引用", []byte(`[{"processId":"`+id.New().String()+`"}]`), []factory.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("create mismatch %v", err)
	}

	okBody := []byte(`[{"name":"w","processId":"` + proc.ID.String() + `"}]`)
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "对齐", okBody, []factory.AssetDep{dep})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.UpdateAssetContent(ctx, pe.tok, proj.ID, proj.Revision, []byte(`[{"processId":"`+id.New().String()+`"}]`)); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("update mismatch %v", err)
	}
	proj, err = fac.GetAsset(ctx, pe.tok, proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fac.SetProjectDeps(ctx, pe.tok, proj.ID, proj.Revision, nil); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("drop dep %v", err)
	}

	schema, err := contenttpl.Marshal(contenttpl.Default(contenttpl.KindProject))
	if err != nil {
		t.Fatal(err)
	}
	var sch contenttpl.Schema
	if err := json.Unmarshal(schema, &sch); err != nil {
		t.Fatal(err)
	}
	sch.Item.Fields = append(sch.Item.Fields, contenttpl.Field{
		Key: "alt", Label: "另", Type: contenttpl.TypeObject,
		Fields: []contenttpl.Field{{Key: "processId", Label: "工艺", Type: contenttpl.TypeString, Default: ""}},
	})
	raw, err := contenttpl.Marshal(sch)
	if err != nil {
		t.Fatal(err)
	}
	fid := seed.ID
	if err := fac.AcceptTemplateDelivery(ctx, factory.TemplateSnapshot{
		ID: id.New(), Kind: factory.KindProject, Revision: 9, Schema: raw, Digest: digest.Sum(raw),
		TargetFactoryID: &fid,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "模版错引用", []byte(`[{"alt":{"processId":"`+id.New().String()+`"}}]`), []factory.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("template mismatch %v", err)
	}
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "模版对齐", []byte(`[{"alt":{"processId":"`+proc.ID.String()+`"}}]`), []factory.AssetDep{dep}); err != nil {
		t.Fatal(err)
	}
}
