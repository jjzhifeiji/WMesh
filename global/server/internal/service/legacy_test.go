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

// 旧文件收入平台级，缺路径的工程整份拒绝。
func TestImportLegacy(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}

	// 准备正文样本，后面入库和比对都用它。
	procBody := []byte(`{"name":"角焊","current":170}`)
	// 准备正文样本，后面入库和比对都用它。
	okProj := []byte(`[{"id":"w1","name":"焊道","processPath":"1焊角.json","points":[]}]`)
	// 准备正文样本，后面入库和比对都用它。
	emptyProj := []byte(`[{"id":"w2","name":"空","processPath":"","points":[]}]`)
	// 准备正文样本，后面入库和比对都用它。
	badProj := []byte(`[{"id":"w3","name":"缺","processPath":"no-such.json","points":[]}]`)
	// 准备正文样本，后面入库和比对都用它。
	multiProj := []byte(`[{"id":"m1","name":"多层","basePath":{"processPath":"1焊角.json","points":[]},"passes":[{"id":"p1","process":{"current":1},"processPath":""}]}]`)
	// 准备正文样本，后面入库和比对都用它。
	tbarProj := []byte(`[{"id":"t1","name":"T","points":[{"id":"a","type":"GROOVE_A_LOWER"}]}]`)
	// 准备正文样本，后面入库和比对都用它。
	rootBody := []byte(`{"name":"打底","current":160}`)
	// 准备正文样本，后面入库和比对都用它。
	capBody := []byte(`{"name":"盖面","current":180}`)

	// 准备一次导入，文件缺了就验不了拒绝和跳过。
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
	// 导入旧文件失败就停，导入失败旧文件就没进来。
	if err != nil {
		// 不符即停：导入旧文件失败。
		t.Fatal(err)
	}
	// 导入结果的列表都不能是空，空了说明没装好。
	if got.Processes == nil || got.Projects == nil || got.Rejected == nil || got.Skipped == nil {
		// 不符即停：导入结果的列表都不能是空。
		t.Fatalf("nil slices %+v", got)
	}
	// 返回值条数不是3就停，这一步不能算通过。
	if len(got.Processes) != 3 {
		// 不符即停：返回值条数不是3。
		t.Fatalf("processes %d rejected %+v", len(got.Processes), got.Rejected)
	}
	// 返回值条数不是4就停，这一步不能算通过。
	if len(got.Projects) != 4 {
		// 不符即停：返回值条数不是4。
		t.Fatalf("projects %d rejected %+v", len(got.Projects), got.Rejected)
	}
	// 返回值条数不是1或正文缺了应有内容就停，不能当通过。
	if len(got.Rejected) != 1 || !strings.Contains(got.Rejected[0].Path, "坏") {
		// 不符即停：返回值条数不是1或正文缺了应有内容。
		t.Fatalf("rejected %+v", got.Rejected)
	}

	// 先留出位置，循环里找到再填，找不到就失败。
	var weldID string
	// 逐项检查，漏一项这一步就不能算通过。
	for _, p := range got.Processes {
		// 遇到「角焊」才进这支，漏了后面钉不住。
		if p.Name == "角焊" {
			// 把身份收成文本，后面比对焊道引用靠它。
			weldID = p.ID.String()
			// 这份工艺状态不是可用或级别不是平台级就停，不能当通过。
			if p.Status != service.AssetAvailable || p.Level != service.AssetLevelPlatform {
				// 不符即停：这份工艺状态不是可用或级别不是平台级。
				t.Fatalf("process %+v", p)
			}
		}
	}
	// 这里结果是空的就停，这一步不能算通过。
	if weldID == "" {
		// 不符即停：这里结果是空的。
		t.Fatal("missing 角焊")
	}

	// 逐项检查，漏一项这一步就不能算通过。
	for _, proj := range got.Projects {
		// 读平台正文，读不到就无法比对。
		body, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proj.ID)
		// 读平台正文失败就停，读不到就无法比对。
		if err != nil {
			// 不符即停：读平台正文失败。
			t.Fatal(err)
		}
		// 正文还留着旧路径就停，导入没有换成身份。
		if bytes.Contains(body, []byte("processPath")) {
			// 不符即停：正文还留着旧路径。
			t.Fatalf("processPath left in %s %s", proj.Name, body)
		}
		// 读取资产，读不到就无法核对原件。
		full, err := h.WAN.GetPlatformAsset(ctx, tok, proj.ID)
		// 读取资产失败就停，读不到就无法核对原件。
		if err != nil {
			// 不符即停：读取资产失败。
			t.Fatal(err)
		}
		// 按工程种类分头核对，串了说明导入分错类。
		switch {
		// 平焊工程要含工艺身份和单层模版，缺了就是没替换。
		case proj.Name == "平焊":
			// 这里正文缺了应有内容就停，这一步不能算通过。
			if !bytes.Contains(body, []byte(weldID)) || !bytes.Contains(body, []byte(contenttpl.SeedTplSingle)) {
				// 不符即停：这里正文缺了应有内容。
				t.Fatalf("single %s", body)
			}
			// 读回的工程条数不是1或依赖没有钉住就停，不能当通过。
			if len(full.Deps) != 1 || full.Deps[0].ID.String() != weldID {
				// 不符即停：读回的工程条数不是1或依赖没有钉住。
				t.Fatalf("single deps %+v", full.Deps)
			}
		// 空工程的工艺身份必须为空，填上了就是误绑。
		case proj.Name == "空":
			// 这里正文缺了应有内容就停，这一步不能算通过。
			if !bytes.Contains(body, []byte(`"processId":""`)) {
				// 不符即停：这里正文缺了应有内容。
				t.Fatalf("empty %s", body)
			}
		// T排工程要带坡口字段，没有说明模版没套上。
		case bytes.Contains(body, []byte(contenttpl.SeedTplTBar)):
			// 这里正文缺了应有内容就停，这一步不能算通过。
			if !bytes.Contains(body, []byte("gapBands")) {
				// 不符即停：这里正文缺了应有内容。
				t.Fatalf("tbar %s", body)
			}
		}
	}

	// 准备一次导入，文件缺了就验不了拒绝和跳过。
	empty, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{})
	// 导入结果的列表都不能是空，空了说明没装好。
	if err != nil || empty.Processes == nil || empty.Projects == nil || empty.Rejected == nil || empty.Skipped == nil {
		// 不符即停：导入结果的列表都不能是空。
		t.Fatalf("empty %+v %v", empty, err)
	}

	// 读审计，读不到就无法核对有没有记。
	rows, err := h.WAN.ListAudit(ctx)
	// 读审计失败就停，读不到就无法核对有没有记。
	if err != nil {
		// 不符即停：读审计失败。
		t.Fatal(err)
	}
	// 审计记录缺字段就停，以后对不了账。
	if bad := audit.Incomplete(rows); len(bad) > 0 {
		// 不符即停：审计记录缺字段。
		t.Fatalf("incomplete %#v", bad[0])
	}
	// 审计里必须有这一笔，缺了就没法对账。
	if !audit.ContainsAny(audit.Dump(rows), "import_legacy") {
		// 不符即停：审计里必须有这一笔。
		t.Fatalf("missing import_legacy")
	}
}

// 同名再导入时跳过或覆盖，依赖仍钉住原工艺。
func TestImportLegacySameName(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 准备一份旧工艺，名字或电流错了导入会偏。
	proc := service.LegacyFile{Path: "1焊角.json", Content: []byte(`{"name":"角焊","current":170}`)}
	// 准备一份旧工程，路径缺了整份会被拒绝。
	proj := service.LegacyFile{Path: "single_layer/平焊/project_data.json", Content: []byte(`[{"id":"w1","processPath":"1焊角.json","points":[]}]`)}
	// 准备一份旧工艺，名字或电流错了导入会偏。
	first, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Processes: []service.LegacyFile{proc}, Projects: []service.LegacyFile{proj}})
	// 导入旧文件失败或条数不是1就停，不能当通过。
	if err != nil || len(first.Processes) != 1 || len(first.Projects) != 1 {
		// 不符即停：导入旧文件失败或条数不是1。
		t.Fatalf("first %+v %v", first, err)
	}
	// 准备一次导入，文件缺了就验不了拒绝和跳过。
	skip, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Processes: []service.LegacyFile{proc},
		Projects: []service.LegacyFile{
			proj,
			{Path: "single_layer/另焊/project_data.json", Content: []byte(`[{"id":"w2","processPath":"1焊角.json","points":[]}]`)},
		},
	})
	// 导入旧文件失败就停，导入失败旧文件就没进来。
	if err != nil {
		// 不符即停：导入旧文件失败。
		t.Fatal(err)
	}
	// 跳过结果多项不符预期就停，说明没有按规则落下。
	if len(skip.Processes) != 0 || len(skip.Projects) != 1 || len(skip.Skipped) != 2 {
		// 不符即停：跳过结果多项不符预期。
		t.Fatalf("skip %+v", skip)
	}
	// 读取资产，读不到就无法核对原件。
	full, err := h.WAN.GetPlatformAsset(ctx, tok, skip.Projects[0].ID)
	// 读取资产失败或条数不是1就停，不能当通过。
	if err != nil || len(full.Deps) != 1 || full.Deps[0].ID != first.Processes[0].ID {
		// 不符即停：读取资产失败或条数不是1。
		t.Fatalf("pin %+v %v", full.Deps, err)
	}
	// 准备一次导入，文件缺了就验不了拒绝和跳过。
	over, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Overwrite: true,
		Processes: []service.LegacyFile{{Path: "1焊角.json", Content: []byte(`{"name":"角焊","current":999}`)}},
		Projects:  []service.LegacyFile{proj},
	})
	// 导入旧文件失败或条数不是1就停，不能当通过。
	if err != nil || len(over.Processes) != 1 || len(over.Projects) != 1 {
		// 不符即停：导入旧文件失败或条数不是1。
		t.Fatalf("over %+v %v", over, err)
	}
	// 覆盖结果身份变了就停，覆盖不该换成新的一份。
	if over.Processes[0].ID != first.Processes[0].ID || over.Projects[0].ID != first.Projects[0].ID {
		// 不符即停：覆盖结果身份变了。
		t.Fatalf("id changed")
	}
	// 读平台正文，读不到就无法比对。
	body, err := h.WAN.ReadPlatformAssetContent(ctx, tok, over.Processes[0].ID)
	// 读平台正文失败或正文缺了应有内容就停，不能当通过。
	if err != nil || !bytes.Contains(body, []byte("999")) {
		// 不符即停：读平台正文失败或正文缺了应有内容。
		t.Fatalf("content %s %v", body, err)
	}
	// 准备一次导入，文件缺了就验不了拒绝和跳过。
	ren, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Rename: true,
		Processes: []service.LegacyFile{
			{Path: "yy/1焊角.json", Content: []byte(`{"name":"角焊","current":1}`)},
		},
		Projects: []service.LegacyFile{
			{Path: "single_layer/平焊/multilayer_data.json", Content: []byte(`[{"id":"m","processPath":"yy/1焊角.json","points":[]}]`)},
		},
	})
	// 导入旧文件失败或条数不是1就停，不能当通过。
	if err != nil || len(ren.Processes) != 1 || len(ren.Projects) != 1 || len(ren.Skipped) != 0 {
		// 不符即停：导入旧文件失败或条数不是1。
		t.Fatalf("rename %+v %v", ren, err)
	}
	// 这里身份没有分开或名称不是yy/1焊角就停，不能当通过。
	if ren.Processes[0].ID == first.Processes[0].ID || ren.Processes[0].Name != "yy/1焊角" {
		// 不符即停：这里身份没有分开或名称不是yy/1焊角。
		t.Fatalf("process name %+v", ren.Processes[0])
	}
	// 这里身份没有分开或名称不是single_layer/平焊-多层就停，不能当通过。
	if ren.Projects[0].ID == first.Projects[0].ID || ren.Projects[0].Name != "single_layer/平焊-多层" {
		// 不符即停：这里身份没有分开或名称不是single_layer/平焊-多层。
		t.Fatalf("project name %+v", ren.Projects[0])
	}
	// 准备一次导入，文件缺了就验不了拒绝和跳过。
	one, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Processes: []service.LegacyFile{
			{Path: "1焊角.json", Content: []byte(`{"name":"角焊","current":1}`)},
			{Path: "zz/1焊角.json", Content: []byte(`{"name":"角焊","current":2}`), Rename: true},
		},
		Projects: []service.LegacyFile{
			{Path: "single_layer/平焊/project_data.json", Content: proj.Content, Overwrite: true},
		},
	})
	// 导入旧文件失败或条数不是1就停，不能当通过。
	if err != nil || len(one.Processes) != 1 || len(one.Projects) != 1 {
		// 不符即停：导入旧文件失败或条数不是1。
		t.Fatalf("one %+v %v", one, err)
	}
	// 这里名称不是zz/1焊角或身份变了就停，不能当通过。
	if one.Processes[0].Name != "zz/1焊角" || one.Projects[0].ID != first.Projects[0].ID {
		// 不符即停：这里名称不是zz/1焊角或身份变了。
		t.Fatalf("one names %+v %+v", one.Processes[0], one.Projects[0])
	}
	// 读平台正文，读不到就无法比对。
	kept, err := h.WAN.ReadPlatformAssetContent(ctx, tok, first.Processes[0].ID)
	// 读平台正文失败或正文留着不该有的旧字段就停，不能当通过。
	if err != nil || bytes.Contains(kept, []byte("\"current\":2")) {
		// 不符即停：读平台正文失败或正文留着不该有的旧字段。
		t.Fatalf("process should stay %s %v", kept, err)
	}
}
