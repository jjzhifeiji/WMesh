// 工程焊道引用工艺身份：对齐 deps、升档改写、不套旧正文。
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

	empty, err := h.WAN.CreatePlatformProject(ctx, tok, "空引用", []byte(`[{"name":"焊道"}]`), []global.AssetDep{dep})
	if err != nil {
		t.Fatal(err)
	}
	emptyBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, empty.ID)
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
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "错引用", []byte(`[{"processId":"`+id.New().String()+`"}]`), []global.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("create mismatch %v", err)
	}

	okBody := []byte(`[{"name":"w","processId":"` + proc.ID.String() + `"}]`)
	proj, err := h.WAN.CreatePlatformProject(ctx, tok, "对齐", okBody, []global.AssetDep{dep})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, proj.ID, proj.Revision, []byte(`[{"processId":"`+id.New().String()+`"}]`)); !errors.Is(err, domain.ErrAssetDependency) {
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
	refs, err := contenttpl.CollectProcessIDs(schJSON, rewritten)
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
	tpl, err := h.WAN.GetTemplate(ctx, tok, global.KindProject)
	if err != nil {
		t.Fatal(err)
	}
	var sch contenttpl.Schema
	if err := json.Unmarshal(tpl.Schema, &sch); err != nil {
		t.Fatal(err)
	}
	sch.Item.Fields = append(sch.Item.Fields, contenttpl.Field{Key: "note", Label: "备注", Type: contenttpl.TypeString, Default: ""})
	raw, err := json.Marshal(sch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WAN.UpdateTemplate(ctx, tok, global.KindProject, tpl.Revision, raw); err != nil {
		t.Fatal(err)
	}
	still, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proj.ID)
	if err != nil || !bytes.Equal(still, oldBody) {
		t.Fatalf("old body changed %s", still)
	}
	tpl, err = h.WAN.GetTemplate(ctx, tok, global.KindProject)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(tpl.Schema, &sch); err != nil {
		t.Fatal(err)
	}
	sch.Item.Fields = append(sch.Item.Fields, contenttpl.Field{
		Key: "alt", Label: "另", Type: contenttpl.TypeObject,
		Fields: []contenttpl.Field{{Key: "processId", Label: "工艺", Type: contenttpl.TypeString, Default: ""}},
	})
	raw, err = json.Marshal(sch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WAN.UpdateTemplate(ctx, tok, global.KindProject, tpl.Revision, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "模版错引用", []byte(`[{"alt":{"processId":"`+id.New().String()+`"}}]`), []global.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("template mismatch %v", err)
	}
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "模版对齐", []byte(`[{"alt":{"processId":"`+proc.ID.String()+`"}}]`), []global.AssetDep{dep}); err != nil {
		t.Fatal(err)
	}
	builtin, err := h.WAN.BuiltinSchema(ctx, tok, global.KindProject)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(builtin, []byte(`"processId"`)) || bytes.Contains(builtin, []byte("processPath")) {
		t.Fatalf("builtin %s", builtin)
	}
	if _, err := h.WAN.BuiltinSchema(ctx, "bad", global.KindProject); !errors.Is(err, domain.ErrUnauthorized) && !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("anon builtin %v", err)
	}

	pack, err := h.WAN.CreatePlatformProject(ctx, tok, "组包", []byte(`[{"processId":"`+proc.ID.String()+`"}]`), []global.AssetDep{dep})
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
