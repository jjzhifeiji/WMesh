// 验收 WAN 旧文件导入：收入平台级，缺路径整份工程拒绝。
package service_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/service"
)

func TestImportLegacy(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	if err != nil {
		t.Fatal(err)
	}

	procBody := []byte(`{"name":"角焊","current":170}`)
	okProj := []byte(`[{"id":"w1","name":"焊道","processPath":"1焊角.json","points":[]}]`)
	emptyProj := []byte(`[{"id":"w2","name":"空","processPath":"","points":[]}]`)
	badProj := []byte(`[{"id":"w3","name":"缺","processPath":"no-such.json","points":[]}]`)
	multiProj := []byte(`[{"id":"m1","name":"多层","basePath":{"processPath":"1焊角.json","points":[]},"passes":[{"id":"p1","process":{"current":1},"processPath":""}]}]`)
	tbarProj := []byte(`[{"id":"t1","name":"T","points":[{"id":"a","type":"GROOVE_A_LOWER"}]}]`)
	rootBody := []byte(`{"name":"打底","current":160}`)
	capBody := []byte(`{"name":"盖面","current":180}`)

	got, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Processes: []service.LegacyFile{
			{Path: "1焊角.json", Content: procBody},
			{Path: "Standard/6T1.2-T排立对接/1H-3-4/打底.json", Content: rootBody},
			{Path: "Standard/6T1.2-T排立对接/1H-3-4/盖面.json", Content: capBody},
		},
		Projects: []service.LegacyFile{
			{Path: "single_layer/我的工程/平焊/project_data.json", Name: "平焊", Content: okProj},
			{Path: "single_layer/空/project_data.json", Content: emptyProj},
			{Path: "single_layer/坏/project_data.json", Content: badProj},
			{Path: "multi_layer/多层/multilayer_data.json", Content: multiProj},
			{Path: "tbar/对接/project_data.json", Content: tbarProj},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Processes == nil || got.Projects == nil || got.Rejected == nil || got.Skipped == nil {
		t.Fatalf("nil slices %+v", got)
	}
	if len(got.Processes) != 3 {
		t.Fatalf("processes %d rejected %+v", len(got.Processes), got.Rejected)
	}
	if len(got.Projects) != 4 {
		t.Fatalf("projects %d rejected %+v", len(got.Projects), got.Rejected)
	}
	if len(got.Rejected) != 1 || !strings.Contains(got.Rejected[0].Path, "坏") {
		t.Fatalf("rejected %+v", got.Rejected)
	}

	var weldID string
	for _, p := range got.Processes {
		if p.Name == "角焊" {
			weldID = p.ID.String()
			if p.Status != service.AssetAvailable || p.Level != service.AssetLevelPlatform {
				t.Fatalf("process %+v", p)
			}
		}
	}
	if weldID == "" {
		t.Fatal("missing 角焊")
	}

	for _, proj := range got.Projects {
		body, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proj.ID)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte("processPath")) {
			t.Fatalf("processPath left in %s %s", proj.Name, body)
		}
		full, err := h.WAN.GetPlatformAsset(ctx, tok, proj.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case proj.Name == "平焊":
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
		case bytes.Contains(body, []byte(contenttpl.SeedTplTBar)):
			if !bytes.Contains(body, []byte("gapBands")) {
				t.Fatalf("tbar %s", body)
			}
		}
	}

	empty, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{})
	if err != nil || empty.Processes == nil || empty.Projects == nil || empty.Rejected == nil || empty.Skipped == nil {
		t.Fatalf("empty %+v %v", empty, err)
	}

	rows, err := h.WAN.ListAudit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bad := audit.Incomplete(rows); len(bad) > 0 {
		t.Fatalf("incomplete %#v", bad[0])
	}
	if !audit.ContainsAny(audit.Dump(rows), "import_legacy") {
		t.Fatalf("missing import_legacy")
	}
}

func TestImportLegacySameName(t *testing.T) {
	ctx := context.Background()
	h := New(t)
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		t.Fatal(err)
	}
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	if err != nil {
		t.Fatal(err)
	}
	proc := service.LegacyFile{Path: "1焊角.json", Content: []byte(`{"name":"角焊","current":170}`)}
	proj := service.LegacyFile{Path: "single_layer/平焊/project_data.json", Content: []byte(`[{"id":"w1","processPath":"1焊角.json","points":[]}]`)}
	first, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Processes: []service.LegacyFile{proc}, Projects: []service.LegacyFile{proj}})
	if err != nil || len(first.Processes) != 1 || len(first.Projects) != 1 {
		t.Fatalf("first %+v %v", first, err)
	}
	skip, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Processes: []service.LegacyFile{proc},
		Projects: []service.LegacyFile{
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
	full, err := h.WAN.GetPlatformAsset(ctx, tok, skip.Projects[0].ID)
	if err != nil || len(full.Deps) != 1 || full.Deps[0].ID != first.Processes[0].ID {
		t.Fatalf("pin %+v %v", full.Deps, err)
	}
	over, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Overwrite: true,
		Processes: []service.LegacyFile{{Path: "1焊角.json", Content: []byte(`{"name":"角焊","current":999}`)}},
		Projects:  []service.LegacyFile{proj},
	})
	if err != nil || len(over.Processes) != 1 || len(over.Projects) != 1 {
		t.Fatalf("over %+v %v", over, err)
	}
	if over.Processes[0].ID != first.Processes[0].ID || over.Projects[0].ID != first.Projects[0].ID {
		t.Fatalf("id changed")
	}
	body, err := h.WAN.ReadPlatformAssetContent(ctx, tok, over.Processes[0].ID)
	if err != nil || !bytes.Contains(body, []byte("999")) {
		t.Fatalf("content %s %v", body, err)
	}
	ren, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Rename: true,
		Processes: []service.LegacyFile{
			{Path: "yy/1焊角.json", Content: []byte(`{"name":"角焊","current":1}`)},
		},
		Projects: []service.LegacyFile{
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
	one, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Processes: []service.LegacyFile{
			{Path: "1焊角.json", Content: []byte(`{"name":"角焊","current":1}`)},
			{Path: "zz/1焊角.json", Content: []byte(`{"name":"角焊","current":2}`), Rename: true},
		},
		Projects: []service.LegacyFile{
			{Path: "single_layer/平焊/project_data.json", Content: proj.Content, Overwrite: true},
		},
	})
	if err != nil || len(one.Processes) != 1 || len(one.Projects) != 1 {
		t.Fatalf("one %+v %v", one, err)
	}
	if one.Processes[0].Name != "zz/1焊角" || one.Projects[0].ID != first.Projects[0].ID {
		t.Fatalf("one names %+v %+v", one.Processes[0], one.Projects[0])
	}
	kept, err := h.WAN.ReadPlatformAssetContent(ctx, tok, first.Processes[0].ID)
	if err != nil || bytes.Contains(kept, []byte("\"current\":2")) {
		t.Fatalf("process should stay %s %v", kept, err)
	}
}
