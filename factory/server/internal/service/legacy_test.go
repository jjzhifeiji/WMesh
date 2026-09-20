// F2：厂端导入旧工艺/工程；缺路径整份工程拒绝，已入工艺保留。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

func TestImportLegacy(t *testing.T) {
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
	pe := mustCreateRole(t, ctx, fac, saTok, "pe", "pe-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil)
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	procBody := []byte(`{"name":"角焊","current":170}`)
	okProj := []byte(`[{"id":"w1","name":"焊道","processPath":"1焊角.json","points":[]}]`)
	emptyProj := []byte(`[{"id":"w2","name":"空","processPath":"","points":[]}]`)
	badProj := []byte(`[{"id":"w3","name":"缺","processPath":"no-such.json","points":[]}]`)
	multiProj := []byte(`[{"id":"m1","name":"多层","basePath":{"processPath":"1焊角.json","points":[]},"passes":[{"id":"p1","process":{"current":1},"processPath":""}]}]`)
	tbarProj := []byte(`[{"id":"t1","name":"T","points":[{"id":"a","type":"GROOVE_A_LOWER"}]}]`)
	rootBody := []byte(`{"name":"打底","current":160}`)
	capBody := []byte(`{"name":"盖面","current":180}`)

	in := factory.LegacyImport{
		Processes: []factory.LegacyFile{
			{Path: "1焊角.json", Content: procBody},
			{Path: "Standard/6T1.2-T排立对接/1H-3-4/打底.json", Content: rootBody},
			{Path: "Standard/6T1.2-T排立对接/1H-3-4/盖面.json", Content: capBody},
		},
		Projects: []factory.LegacyFile{
			{Path: "single_layer/我的工程/平焊/project_data.json", Name: "平焊", Content: okProj},
			{Path: "single_layer/空/project_data.json", Content: emptyProj},
			{Path: "single_layer/坏/project_data.json", Content: badProj},
			{Path: "multi_layer/多层/multilayer_data.json", Content: multiProj},
			{Path: "tbar/对接/project_data.json", Content: tbarProj},
		},
	}
	if _, err := fac.ImportLegacy(ctx, saTok, in); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("sa import: %v", err)
	}
	if _, err := fac.ImportLegacy(ctx, op.tok, in); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("op import: %v", err)
	}

	got, err := fac.ImportLegacy(ctx, pe.tok, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Processes) != 3 {
		t.Fatalf("processes %d rejected %+v", len(got.Processes), got.Rejected)
	}
	if len(got.Projects) != 4 {
		t.Fatalf("projects %d rejected %+v", len(got.Projects), got.Rejected)
	}
	if got.Processes == nil || got.Projects == nil || got.Rejected == nil || got.Skipped == nil {
		t.Fatalf("nil slices %+v", got)
	}
	if len(got.Rejected) != 1 || !strings.Contains(got.Rejected[0].Path, "坏") {
		t.Fatalf("rejected %+v", got.Rejected)
	}

	var weldID string
	for _, p := range got.Processes {
		if p.Name == "角焊" {
			weldID = p.ID.String()
			if p.Status != factory.AssetAvailable || p.Level != factory.AssetLevelFactory {
				t.Fatalf("process %+v", p)
			}
		}
	}
	if weldID == "" {
		t.Fatal("missing 角焊")
	}

	for _, proj := range got.Projects {
		body, err := fac.ReadAssetContent(ctx, pe.tok, proj.ID)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte("processPath")) {
			t.Fatalf("processPath left in %s %s", proj.Name, body)
		}
		if bytes.Contains(body, []byte(`"process":{`)) {
			t.Fatalf("nested process left in %s %s", proj.Name, body)
		}
		full, err := fac.GetAsset(ctx, pe.tok, proj.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.Contains(proj.Name, "平焊") || proj.Name == "平焊":
			if !bytes.Contains(body, []byte(weldID)) || !bytes.Contains(body, []byte(contenttpl.SeedTplSingle)) {
				t.Fatalf("single %s", body)
			}
			if len(full.Deps) != 1 || full.Deps[0].ID.String() != weldID {
				t.Fatalf("single deps %+v", full.Deps)
			}
		case proj.Name == "空":
			if !bytes.Contains(body, []byte(`"processId":""`)) {
				t.Fatalf("empty %s", body)
			}
			if len(full.Deps) != 0 {
				t.Fatalf("empty deps %+v", full.Deps)
			}
		case strings.Contains(string(body), contenttpl.SeedTplMulti) || proj.Name == "多层":
			if !bytes.Contains(body, []byte(contenttpl.SeedTplMulti)) || !bytes.Contains(body, []byte(weldID)) {
				t.Fatalf("multi %s", body)
			}
		case bytes.Contains(body, []byte(contenttpl.SeedTplTBar)):
			if !bytes.Contains(body, []byte("gapBands")) || !bytes.Contains(body, []byte("rootProcessId")) {
				t.Fatalf("tbar %s", body)
			}
			if len(full.Deps) < 2 {
				t.Fatalf("tbar deps %+v", full.Deps)
			}
		}
	}

	rows, err := fac.ListAudit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bad := audit.Incomplete(rows); len(bad) > 0 {
		t.Fatalf("incomplete %#v", bad[0])
	}
	dump := audit.Dump(rows)
	if !audit.ContainsAny(dump, "import_legacy") {
		t.Fatalf("missing import_legacy: %s", dump)
	}
	if audit.ContainsAny(dump, "pe-pass", "sa-pass", string(procBody)) {
		t.Fatalf("secret or body leaked")
	}
}

func TestImportLegacyMissingKeepsProcess(t *testing.T) {
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
	pe := mustCreateRole(t, ctx, fac, saTok, "pe", "pe-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil)
	got, err := fac.ImportLegacy(ctx, pe.tok, factory.LegacyImport{
		Processes: []factory.LegacyFile{{Path: "keep.json", Content: []byte(`{"name":"留","current":1}`)}},
		Projects:  []factory.LegacyFile{{Path: "gone/project_data.json", Content: []byte(`[{"processPath":"missing.json"}]`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Processes) != 1 || len(got.Projects) != 0 || len(got.Rejected) != 1 {
		t.Fatalf("%+v", got)
	}
	empty, err := fac.ImportLegacy(ctx, pe.tok, factory.LegacyImport{})
	if err != nil || empty.Processes == nil || empty.Projects == nil || empty.Rejected == nil || empty.Skipped == nil {
		t.Fatalf("empty %+v %v", empty, err)
	}
	if got.Processes[0].Name != "留" || got.Processes[0].Status != factory.AssetAvailable {
		t.Fatalf("kept %+v", got.Processes[0])
	}
}

func TestImportLegacySameName(t *testing.T) {
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
	pe := mustCreateRole(t, ctx, fac, saTok, "pe", "pe-pass", factory.RoleProcessEngineer, factory.ScopeFactory, nil)
	proc := factory.LegacyFile{Path: "1焊角.json", Content: []byte(`{"name":"角焊","current":170}`)}
	proj := factory.LegacyFile{Path: "single_layer/平焊/project_data.json", Content: []byte(`[{"id":"w1","processPath":"1焊角.json","points":[]}]`)}
	first, err := fac.ImportLegacy(ctx, pe.tok, factory.LegacyImport{Processes: []factory.LegacyFile{proc}, Projects: []factory.LegacyFile{proj}})
	if err != nil || len(first.Processes) != 1 || len(first.Projects) != 1 {
		t.Fatalf("first %+v %v", first, err)
	}
	skip, err := fac.ImportLegacy(ctx, pe.tok, factory.LegacyImport{
		Processes: []factory.LegacyFile{proc},
		Projects: []factory.LegacyFile{
			proj,
			{Path: "single_layer/另焊/project_data.json", Content: []byte(`[{"id":"w2","processPath":"1焊角.json","points":[]}]`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(skip.Processes) != 0 || len(skip.Projects) != 1 || len(skip.Skipped) != 2 {
		t.Fatalf("skip %+v", skip)
	}
	if skip.Projects[0].Name != "另焊" {
		t.Fatalf("new project %+v", skip.Projects[0])
	}
	full, err := fac.GetAsset(ctx, pe.tok, skip.Projects[0].ID)
	if err != nil || len(full.Deps) != 1 || full.Deps[0].ID != first.Processes[0].ID {
		t.Fatalf("pin %+v %v", full.Deps, err)
	}
	over, err := fac.ImportLegacy(ctx, pe.tok, factory.LegacyImport{
		Overwrite: true,
		Processes: []factory.LegacyFile{{Path: "1焊角.json", Content: []byte(`{"name":"角焊","current":999}`)}},
		Projects:  []factory.LegacyFile{proj},
	})
	if err != nil || len(over.Processes) != 1 || len(over.Projects) != 1 {
		t.Fatalf("over %+v %v", over, err)
	}
	if over.Processes[0].ID != first.Processes[0].ID || over.Projects[0].ID != first.Projects[0].ID {
		t.Fatalf("id changed %s %s", over.Processes[0].ID, over.Projects[0].ID)
	}
	if over.Processes[0].Revision <= first.Processes[0].Revision {
		t.Fatalf("rev %d", over.Processes[0].Revision)
	}
	body, err := fac.ReadAssetContent(ctx, pe.tok, over.Processes[0].ID)
	if err != nil || !bytes.Contains(body, []byte("999")) {
		t.Fatalf("content %s %v", body, err)
	}
	ren, err := fac.ImportLegacy(ctx, pe.tok, factory.LegacyImport{
		Rename: true,
		Processes: []factory.LegacyFile{
			{Path: "yy/1焊角.json", Content: []byte(`{"name":"角焊","current":1}`)},
		},
		Projects: []factory.LegacyFile{
			{Path: "single_layer/平焊/multilayer_data.json", Content: []byte(`[{"id":"m","processPath":"yy/1焊角.json","points":[]}]`)},
		},
	})
	if err != nil || len(ren.Processes) != 1 || len(ren.Projects) != 1 || len(ren.Skipped) != 0 {
		t.Fatalf("rename %+v %v", ren, err)
	}
	if ren.Processes[0].ID == first.Processes[0].ID || ren.Processes[0].Name != "yy/1焊角" {
		t.Fatalf("process name %+v", ren.Processes[0])
	}
	if ren.Projects[0].ID == first.Projects[0].ID || ren.Projects[0].Name != "single_layer/平焊-多层" {
		t.Fatalf("project name %+v", ren.Projects[0])
	}
	one, err := fac.ImportLegacy(ctx, pe.tok, factory.LegacyImport{
		Processes: []factory.LegacyFile{
			{Path: "1焊角.json", Content: []byte(`{"name":"角焊","current":1}`)},
			{Path: "zz/1焊角.json", Content: []byte(`{"name":"角焊","current":2}`), Rename: true},
		},
		Projects: []factory.LegacyFile{
			{Path: "single_layer/平焊/project_data.json", Content: proj.Content, Overwrite: true},
		},
	})
	if err != nil || len(one.Processes) != 1 || len(one.Projects) != 1 {
		t.Fatalf("one %+v %v", one, err)
	}
	if one.Processes[0].Name != "zz/1焊角" || one.Projects[0].ID != first.Projects[0].ID {
		t.Fatalf("one names %+v %+v", one.Processes[0], one.Projects[0])
	}
	kept, err := fac.ReadAssetContent(ctx, pe.tok, first.Processes[0].ID)
	if err != nil || bytes.Contains(kept, []byte("\"current\":2")) {
		t.Fatalf("process should stay %s %v", kept, err)
	}
}
