// 工程焊道引用工艺身份：对齐 deps，不套旧正文。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

const seedSingleID = "11111111-1111-4111-8111-111111111111"

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

	empty, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "空引用", []byte(`[]`), []factory.AssetDep{dep})
	if err != nil {
		t.Fatal(err)
	}
	emptyBody, err := fac.ReadAssetContent(ctx, pe.tok, empty.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := contenttpl.CollectProcessIDsFromItems(contenttpl.SeedProjectItems(), emptyBody)
	if err != nil || len(ids) != 0 {
		t.Fatalf("empty refs %v %v", ids, err)
	}
	if string(emptyBody) != "[]" {
		t.Fatalf("empty body %s", emptyBody)
	}
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "错引用", []byte(`[{"templateId":"`+seedSingleID+`","processId":"`+id.New().String()+`"}]`), []factory.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("create mismatch %v", err)
	}

	okBody := []byte(`[{"name":"w","processId":"` + proc.ID.String() + `"}]`)
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "对齐", okBody, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(proj.Deps) != 1 || proj.Deps[0].ID != proc.ID {
		t.Fatalf("pinned %+v", proj.Deps)
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

	var single contenttpl.ProjectItemSchema
	for _, it := range contenttpl.SeedProjectItems() {
		if it.ID == seedSingleID {
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
		ID: uuid.MustParse(seedSingleID), Kind: factory.KindProject, Name: single.Name, Revision: 9, Schema: raw, Digest: digest.Sum(raw),
		TargetFactoryID: &fid,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "模版错引用", []byte(`[{"templateId":"`+seedSingleID+`","extraProcesses":[{"processId":"`+id.New().String()+`"}]}]`), []factory.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("template mismatch %v", err)
	}
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "模版对齐", []byte(`[{"templateId":"`+seedSingleID+`","extraProcesses":[{"processId":"`+proc.ID.String()+`"}]}]`), nil); err != nil {
		t.Fatal(err)
	}

	own, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "个人工艺", []byte(`{"name":"mine","current":160}`))
	if err != nil {
		t.Fatal(err)
	}
	own, err = fac.PublishAsset(ctx, pe.tok, own.ID, own.Revision)
	if err != nil {
		t.Fatal(err)
	}
	pinnedPersonal, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "钉个人", []byte(`[{"templateId":"`+seedSingleID+`","processId":"`+own.ID.String()+`"}]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pinnedPersonal.Deps) != 1 || pinnedPersonal.Deps[0].ID != own.ID {
		t.Fatalf("personal pin %+v", pinnedPersonal.Deps)
	}
}
