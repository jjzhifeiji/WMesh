// 工程焊道引用工艺身份；F2 空库四份、已有三份不自动插 T 排。
package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

const seedSingleID = "11111111-1111-4111-8111-111111111111"

func TestProjectProcessRefs(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", wanPass)
	if err != nil {
		t.Fatal(err)
	}
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "工艺", []byte(`{"name":"p","current":180}`))
	if err != nil {
		t.Fatal(err)
	}
	proc, err = h.WAN.PublishPlatformAsset(ctx, tok, proc.ID, proc.Revision)
	if err != nil {
		t.Fatal(err)
	}
	dep := global.AssetDep{ID: proc.ID, Revision: proc.Revision, Digest: proc.Digest}

	empty, err := h.WAN.CreatePlatformProject(ctx, tok, "空引用", []byte(`[]`), []global.AssetDep{dep})
	if err != nil {
		t.Fatal(err)
	}
	emptyBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, empty.ID)
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
	bad := []byte(`[{"templateId":"` + seedSingleID + `","processId":"` + id.New().String() + `"}]`)
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "错引用", bad, []global.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("create mismatch %v", err)
	}

	okBody := []byte(`[{"templateId":"` + seedSingleID + `","name":"w","processId":"` + proc.ID.String() + `"}]`)
	proj, err := h.WAN.CreatePlatformProject(ctx, tok, "对齐", okBody, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(proj.Deps) != 1 || proj.Deps[0].ID != proc.ID {
		t.Fatalf("pinned %+v", proj.Deps)
	}
	if _, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, proj.ID, proj.Revision, []byte(`[{"templateId":"`+seedSingleID+`","processId":"`+id.New().String()+`"}]`)); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("update mismatch %v", err)
	}
	proj, err = h.WAN.GetPlatformAsset(ctx, tok, proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WAN.SetPlatformProjectDeps(ctx, tok, proj.ID, proj.Revision, nil); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("drop dep %v", err)
	}

	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	if err != nil {
		t.Fatal(err)
	}
	srcProc := id.New()
	pbody := []byte(`{"name":"厂工艺","current":180}`)
	psnap := global.AssetSnapshot{
		SourceID: srcProc, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
		Kind: global.KindProcess, Name: "厂工艺", Content: pbody, Digest: digest.Sum(pbody),
		Copyable: true, Status: global.AssetAvailable,
	}
	plat, err := h.WAN.PromoteFromSnapshot(ctx, tok, psnap)
	if err != nil {
		t.Fatal(err)
	}
	plat, err = h.WAN.PublishPlatformAsset(ctx, tok, plat.ID, plat.Revision)
	if err != nil {
		t.Fatal(err)
	}
	projJSON, err := json.Marshal([]map[string]any{{
		"name":      "升档工程",
		"processId": srcProc.String(),
		"keep":      true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	projSrc := id.New()
	jsnap := global.AssetSnapshot{
		SourceID: projSrc, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
		Kind: global.KindProject, Name: "厂工程", Content: projJSON, Digest: digest.Sum(projJSON),
		Copyable: true, Status: global.AssetAvailable,
		Deps: []global.AssetDep{{ID: srcProc, Revision: 1, Digest: digest.Sum(pbody)}},
	}
	got, err := h.WAN.PromoteFromSnapshot(ctx, tok, jsnap)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Deps) != 1 || got.Deps[0].ID != plat.ID {
		t.Fatalf("deps %+v", got.Deps)
	}
	rewritten, err := h.WAN.ReadPlatformAssetContent(ctx, tok, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := contenttpl.CollectProcessIDsFromItems(contenttpl.SeedProjectItems(), rewritten)
	if err != nil || len(refs) != 1 || refs[0] != plat.ID.String() {
		t.Fatalf("rewritten refs %v %s", refs, rewritten)
	}
	var welds []map[string]any
	if err := json.Unmarshal(rewritten, &welds); err != nil || welds[0]["keep"] != true {
		t.Fatalf("keep %s", rewritten)
	}
	again, err := h.WAN.PromoteFromSnapshot(ctx, tok, jsnap)
	if err != nil || again.ID != got.ID || again.Revision != got.Revision {
		t.Fatalf("skip %+v %v", again, err)
	}
	missing := global.AssetSnapshot{
		SourceID: id.New(), SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
		Kind: global.KindProject, Name: "缺工艺", Content: projJSON, Digest: digest.Sum(projJSON),
		Copyable: true, Status: global.AssetAvailable,
		Deps: []global.AssetDep{{ID: id.New(), Revision: 1, Digest: digest.Sum(pbody)}},
	}
	if _, err := h.WAN.PromoteFromSnapshot(ctx, tok, missing); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("missing process %v", err)
	}

	oldBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	list, err := h.WAN.ListProjectTemplates(ctx, tok)
	if err != nil || len(list) != 4 {
		t.Fatalf("list %d %v", len(list), err)
	}
	foundTBar := false
	for _, row := range list {
		if row.ID.String() == contenttpl.SeedTplTBar && bytes.Contains(row.Schema, []byte("gapBands")) && !bytes.Contains(row.Schema, []byte("processPath")) {
			foundTBar = true
		}
	}
	if !foundTBar {
		t.Fatalf("empty db missing tbar: %+v", list)
	}
	single := list[0]
	for _, row := range list {
		if row.ID.String() == seedSingleID {
			single = row
			break
		}
	}
	renamed, err := h.WAN.UpdateProjectTemplate(ctx, tok, single.ID, single.Revision, "单层焊道改名", single.Schema)
	if err != nil || renamed.Revision != single.Revision+1 {
		t.Fatalf("rename %+v %v", renamed, err)
	}
	still, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proj.ID)
	if err != nil || !bytes.Equal(still, oldBody) {
		t.Fatalf("old body changed %s", still)
	}
	if _, err := h.WAN.UpdateProjectTemplate(ctx, tok, renamed.ID, renamed.Revision, "", renamed.Schema); !errors.Is(err, domain.ErrTemplateInvalid) {
		t.Fatalf("empty name %v", err)
	}
	created, err := h.WAN.CreateProjectTemplate(ctx, tok, "坡口", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.WAN.DeleteProjectTemplate(ctx, tok, created.ID); err != nil {
		t.Fatal(err)
	}
	extraBad := []byte(`[{"templateId":"` + seedSingleID + `","extraProcesses":[{"processId":"` + id.New().String() + `"}]}]`)
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "模版错引用", extraBad, []global.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("template mismatch %v", err)
	}
	extraOK := []byte(`[{"templateId":"` + seedSingleID + `","extraProcesses":[{"processId":"` + proc.ID.String() + `"}]}]`)
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "模版对齐", extraOK, nil); err != nil {
		t.Fatal(err)
	}
	listed, err := h.WAN.ListProjectTemplates(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range listed {
		if bytes.Contains(row.Schema, []byte(`"processId"`)) && !bytes.Contains(row.Schema, []byte("processPath")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("seed schema missing processId")
	}
	if _, err := h.WAN.ListProjectTemplates(ctx, "bad"); !errors.Is(err, domain.ErrUnauthorized) && !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("anon list %v", err)
	}

	pack, err := h.WAN.CreatePlatformProject(ctx, tok, "组包", []byte(`[{"templateId":"`+seedSingleID+`","processId":"`+proc.ID.String()+`"}]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	pack, err = h.WAN.PublishPlatformAsset(ctx, tok, pack.ID, pack.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.WAN.GrantFactoryAsset(ctx, tok, pack.ID, fac.Factory.ID); err != nil {
		t.Fatal(err)
	}
	snap, err := h.WAN.DistributeToFactory(ctx, tok, pack.ID, fac.Factory.ID)
	if err != nil || len(snap.Members) != 2 {
		t.Fatalf("members %+v %v", snap, err)
	}
}

func TestExistingProjectTemplatesKeepThree(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	if err != nil {
		t.Fatal(err)
	}
	list, err := h.WAN.ListProjectTemplates(ctx, tok)
	if err != nil || len(list) != 4 {
		t.Fatalf("seed %d %v", len(list), err)
	}
	var tbarID = list[0].ID
	found := false
	for _, row := range list {
		if row.ID.String() == contenttpl.SeedTplTBar {
			tbarID = row.ID
			found = true
		}
	}
	if !found {
		t.Fatal("empty db missing tbar")
	}
	if err := h.WAN.DeleteProjectTemplate(ctx, tok, tbarID); err != nil {
		t.Fatal(err)
	}
	again, err := h.WAN.ListProjectTemplates(ctx, tok)
	if err != nil || len(again) != 3 {
		t.Fatalf("after delete %d %v", len(again), err)
	}
	for _, row := range again {
		if row.ID.String() == contenttpl.SeedTplTBar {
			t.Fatal("auto reinserted tbar")
		}
	}
}
