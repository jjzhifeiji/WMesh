// 验收 WAN 旧文件导入：收入平台级，缺路径整份工程拒绝。
package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
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
	// 角焊被单层和多层共用，按类型拆成两份，共四份工艺。
	if len(got.Processes) != 4 {
		// 不符即停：返回值条数不是4。
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
	// 多层那份角焊的身份，扫到再填上。
	var multiID string
	// 逐项检查，漏一项这一步就不能算通过。
	for _, p := range got.Processes {
		// 遇到「角焊」才进这支，漏了后面钉不住。
		if p.Name == "角焊" {
			// 把身份收成文本，后面比对焊道引用靠它。
			weldID = p.ID.String()
			// 这份工艺状态不是可用、级别不是平台级或不是单层就停，不能当通过。
			if p.Status != service.AssetAvailable || p.Level != service.AssetLevelPlatform || p.WeldKind != store.WeldKindSingle {
				// 不符即停：这份工艺状态不是可用或级别不是平台级。
				t.Fatalf("process %+v", p)
			}
		}
		// 多层工程引用的那份应带多层后缀，作业类型是多层。
		if p.Name == "角焊-多层" {
			// 把身份收成文本，后面比对多层工程靠它。
			multiID = p.ID.String()
			// 作业类型不是多层说明拆份没按类型来。
			if p.WeldKind != store.WeldKindMultilayer {
				// 不符即停：多层那份作业类型不对。
				t.Fatalf("multi process %+v", p)
			}
		}
	}
	// 这里结果是空的就停，这一步不能算通过。
	if weldID == "" || multiID == "" {
		// 不符即停：共用工艺没拆成两份。
		t.Fatalf("missing 角焊 %+v", got.Processes)
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
		// 多层工程应钉多层那份角焊，作业类型是多层。
		case proj.Name == "多层":
			// 作业类型不对或钉的不是多层那份就停。
			if full.WeldKind != store.WeldKindMultilayer || len(full.Deps) != 1 || full.Deps[0].ID.String() != multiID || !bytes.Contains(body, []byte(multiID)) {
				// 不符即停：多层工程钉错了那份。
				t.Fatalf("multi %s %+v %s", full.WeldKind, full.Deps, body)
			}
		// T排工程要带坡口字段，没有说明模版没套上。
		case bytes.Contains(body, []byte(contenttpl.SeedTplTBar)):
			// 这里正文缺了应有内容就停，这一步不能算通过。
			if !bytes.Contains(body, []byte("gapBands")) {
				// 不符即停：这里正文缺了应有内容。
				t.Fatalf("tbar %s", body)
			}
			// 作业类型不是 T 排或依赖不全就停。
			if full.WeldKind != store.WeldKindTBar || len(full.Deps) != 2 {
				// 不符即停：T 排工程的作业类型或依赖不对。
				t.Fatalf("tbar deps %s %+v", full.WeldKind, full.Deps)
			}
			// T 排工程的依赖都应是 T 排那份。
			assertLegacyDepKinds(t, ctx, h.WAN, tok, full.Deps, store.WeldKindTBar)
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
	// 内容没变的工艺按摘要沿用，原因应写内容相同。
	if !legacySkipped(skip.Skipped, "1焊角.json", "same content") {
		// 不符即停：原因不对说明没按摘要沿用。
		t.Fatalf("skip reason %+v", skip.Skipped)
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

// 首批同名不同路径的工艺各有身份；原样重导沿用，覆盖时各改各的。
func TestImportLegacyPathIdentity(t *testing.T) {
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
	// 标准目录那份同名工艺，偏移 1.5。
	std := service.LegacyFile{Path: "Standard/1T1.2/1P-1F/1K6.5mmD.json", Content: []byte(`{"name":"1K6.5mmD","offsetX":1.5,"current":250}`)}
	// 用户目录那份同名工艺，偏移 10。
	usr := service.LegacyFile{Path: "User/1K6.5mmD.json", Content: []byte(`{"name":"1K6.5mmD","offsetX":10,"current":250}`)}
	// T 排两个缝隙目录各放一份打底和盖面，名字都一样；后一个目录放前面验排序。
	gaps := []service.LegacyFile{
		{Path: "Standard/6T1.2-T排立对接/1H-4-5/打底.json", Content: []byte(`{"name":"打底","current":165}`)},
		{Path: "Standard/6T1.2-T排立对接/1H-4-5/盖面.json", Content: []byte(`{"name":"盖面","current":185}`)},
		{Path: "Standard/6T1.2-T排立对接/1H-3-4/打底.json", Content: []byte(`{"name":"打底","current":160}`)},
		{Path: "Standard/6T1.2-T排立对接/1H-3-4/盖面.json", Content: []byte(`{"name":"盖面","current":180}`)},
	}
	// 用户目录那份放最前，排序后仍应由标准目录留原名。
	procs := append([]service.LegacyFile{usr, std}, gaps...)
	// 引用用户目录那份工艺的单层工程。
	userBody := []byte(`[{"id":"w1","processPath":"User/1K6.5mmD.json","points":[]}]`)
	// 不带缝隙带的 T 排工程，导入时按目录补带。
	tbarBody := []byte(`[{"id":"t1","name":"T","points":[{"id":"a","type":"GROOVE_A_LOWER"}]}]`)
	// 首批工程：两份同名不同路径的工程，加上单层和 T 排各一份。
	projs := []service.LegacyFile{
		{Path: "single_layer/b/平焊/project_data.json", Content: userBody},
		{Path: "single_layer/a/平焊/project_data.json", Content: userBody},
		{Path: "single_layer/用户/project_data.json", Content: userBody},
		{Path: "tbar/对接/project_data.json", Content: tbarBody},
	}
	// 首批开着覆盖导入，本批同名也不能互相改写。
	first, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Overwrite: true, Processes: procs, Projects: projs})
	// 导入失败或条数不对就停，说明本批同名被并掉了。
	if err != nil || len(first.Processes) != 6 || len(first.Projects) != 4 || len(first.Skipped) != 0 || len(first.Rejected) != 0 {
		// 不符即停：首批条数不对。
		t.Fatalf("first %+v %v", first, err)
	}
	// 按名字收起工艺身份，后面核对引用要用。
	ids := map[string]string{}
	// 逐条收名字和身份，同名会互相盖掉。
	for _, p := range first.Processes {
		// 名字对到身份文本。
		ids[p.Name] = p.ID.String()
	}
	// 本批该有的六个名字。
	wantNames := []string{"1K6.5mmD", "User/1K6.5mmD", "打底", "盖面", "Standard/6T1.2-T排立对接/1H-4-5/打底", "Standard/6T1.2-T排立对接/1H-4-5/盖面"}
	// 逐个核对名字都在，缺了说明没按路径另起名。
	for _, n := range wantNames {
		// 名字不在说明另起名的规则偏了。
		if ids[n] == "" {
			// 不符即停：缺了按路径另起的名字。
			t.Fatalf("missing %s in %v", n, ids)
		}
	}
	// 标准目录那份的正文不能被用户目录那份改写。
	stdBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, first.Processes[0].ID)
	// 读正文失败或被改写就停，说明开覆盖时盖了本批刚建的。
	if err != nil || first.Processes[0].ID.String() != ids["1K6.5mmD"] || !bytes.Contains(stdBody, []byte(`"offsetX":1.5`)) {
		// 不符即停：标准目录那份被改写。
		t.Fatalf("std body %s %v", stdBody, err)
	}
	// 按名字收起工程，后面逐份核对。
	projByName := map[string]service.Asset{}
	// 逐份收名字和工程。
	for _, p := range first.Projects {
		// 名字对到工程。
		projByName[p.Name] = p
	}
	// 两份同名工程应各自入库，后一份按路径另起名。
	if projByName["平焊"].ID == projByName["single_layer/b/平焊"].ID || projByName["single_layer/b/平焊"].Name == "" {
		// 不符即停：同名工程被并成一条。
		t.Fatalf("projects %+v", first.Projects)
	}
	// 单层工程应钉用户目录那份工艺。
	assertLegacyDeps(t, ctx, h.WAN, tok, projByName["用户"].ID, ids["User/1K6.5mmD"])
	// 读 T 排工程正文，核对缝隙带。
	tbar, err := h.WAN.ReadPlatformAssetContent(ctx, tok, projByName["对接"].ID)
	// 读平台正文失败就停，读不到就无法比对。
	if err != nil {
		// 不符即停：读平台正文失败。
		t.Fatal(err)
	}
	// 两条缝隙带的打底和盖面应各是各的目录那份。
	assertLegacyBands(t, tbar, []string{ids["打底"], ids["盖面"], ids["Standard/6T1.2-T排立对接/1H-4-5/打底"], ids["Standard/6T1.2-T排立对接/1H-4-5/盖面"]})

	// 原样再导一次，另带两份新工程核对路径对照。
	again, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Processes: procs,
		Projects: append(slices.Clone(projs),
			service.LegacyFile{Path: "single_layer/用户二/project_data.json", Content: userBody},
			service.LegacyFile{Path: "tbar/对接二/project_data.json", Content: tbarBody},
		),
	})
	// 导入失败或新建了工艺就停，说明重导出了副本。
	if err != nil || len(again.Processes) != 0 || len(again.Projects) != 2 || len(again.Skipped) != 10 {
		// 不符即停：重导条数不对。
		t.Fatalf("again %+v %v", again, err)
	}
	// 每份工艺都应按摘要沿用，原因写内容相同。
	for _, f := range procs {
		// 缺了说明这份没按摘要找回首导的身份。
		if !legacySkipped(again.Skipped, f.Path, "same content") {
			// 不符即停：这份没按摘要沿用。
			t.Fatalf("not same content %s %+v", f.Path, again.Skipped)
		}
	}
	// 按名字收起新工程。
	againByName := map[string]service.Asset{}
	// 逐份收名字和工程。
	for _, p := range again.Projects {
		// 名字对到工程。
		againByName[p.Name] = p
	}
	// 新单层工程仍应钉用户目录那份。
	assertLegacyDeps(t, ctx, h.WAN, tok, againByName["用户二"].ID, ids["User/1K6.5mmD"])
	// 读新 T 排工程正文，核对缝隙带。
	tbar2, err := h.WAN.ReadPlatformAssetContent(ctx, tok, againByName["对接二"].ID)
	// 读平台正文失败就停，读不到就无法比对。
	if err != nil {
		// 不符即停：读平台正文失败。
		t.Fatal(err)
	}
	// 路径对照不变，缝隙带仍钉首导那几份。
	assertLegacyBands(t, tbar2, []string{ids["打底"], ids["盖面"], ids["Standard/6T1.2-T排立对接/1H-4-5/打底"], ids["Standard/6T1.2-T排立对接/1H-4-5/盖面"]})

	// 两份都改了内容，开覆盖再导。
	over, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Overwrite: true,
		Processes: []service.LegacyFile{
			{Path: usr.Path, Content: []byte(`{"name":"1K6.5mmD","offsetX":12,"current":250}`)},
			{Path: std.Path, Content: []byte(`{"name":"1K6.5mmD","offsetX":2.5,"current":250}`)},
		},
	})
	// 导入失败或条数不对就停，说明覆盖没各改各的。
	if err != nil || len(over.Processes) != 2 || len(over.Skipped) != 0 {
		// 不符即停：覆盖条数不对。
		t.Fatalf("over %+v %v", over, err)
	}
	// 每份覆盖后的身份和名字都应不变，正文各自更新。
	for _, p := range over.Processes {
		// 身份应是首导那份，变了说明新建了工艺。
		if ids[p.Name] != p.ID.String() {
			// 不符即停：覆盖换了身份。
			t.Fatalf("identity changed %+v", p)
		}
		// 读回正文，核对改的是自己那份。
		body, err := h.WAN.ReadPlatformAssetContent(ctx, tok, p.ID)
		// 读平台正文失败就停，读不到就无法比对。
		if err != nil {
			// 不符即停：读平台正文失败。
			t.Fatal(err)
		}
		// 标准目录那份应改成 2.5，用户目录那份改成 12。
		want := map[string]string{"1K6.5mmD": `"offsetX":2.5`, "User/1K6.5mmD": `"offsetX":12`}[p.Name]
		// 正文对不上说明覆盖串到了别的身份。
		if want == "" || !bytes.Contains(body, []byte(want)) {
			// 不符即停：覆盖串到了别的身份。
			t.Fatalf("over body %s %s", p.Name, body)
		}
	}
}

// 共用工艺按作业类型拆份，工程和工艺的作业类型都对，导入后网页能改正文。
func TestImportLegacyWeldKind(t *testing.T) {
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
	// 单层和多层工程共用的一份工艺，另有 T 排目录和 6T1.2 样例目录。
	procs := []service.LegacyFile{
		{Path: "User/1K6.5mmD.json", Content: []byte(`{"name":"1K6.5mmD","current":250}`)},
		{Path: "Standard/T排立对接/1H-3-4/打底.json", Content: []byte(`{"name":"打底","current":160}`)},
		{Path: "Standard/T排立对接/1H-3-4/盖面.json", Content: []byte(`{"name":"盖面","current":180}`)},
		{Path: "Standard/6T1.2/2H-4-5/打底.json", Content: []byte(`{"name":"打底","current":165}`)},
		{Path: "其它.json", Content: []byte(`{"name":"其它","current":100}`)},
	}
	// 多层工程正文，基准层引用共用那份工艺。
	multiBody := []byte(`[{"id":"m1","basePath":{"processPath":"User/1K6.5mmD.json","points":[]},"passes":[]}]`)
	// 单层、多层、T 排各一份，另加一份混了单层和多层的工程。
	projs := []service.LegacyFile{
		{Path: "single_layer/单/project_data.json", Content: []byte(`[{"id":"s1","processPath":"User/1K6.5mmD.json","points":[]}]`)},
		{Path: "multi_layer/多/multilayer_data.json", Content: multiBody},
		{Path: "tbar/对接/project_data.json", Content: []byte(`[{"id":"t1","points":[{"id":"a","type":"GROOVE_A_LOWER"}]}]`)},
		{Path: "single_layer/混/project_data.json", Content: []byte(`[{"id":"a","processPath":"User/1K6.5mmD.json","points":[]},{"id":"b","basePath":{"processPath":"User/1K6.5mmD.json","points":[]},"passes":[]}]`)},
	}
	// 首次导入整批，混了焊缝的那份应被拒。
	got, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Processes: procs, Projects: projs})
	// 导入失败或条数不对就停，说明拆份或拒绝不对。
	if err != nil || len(got.Processes) != 6 || len(got.Projects) != 3 || len(got.Rejected) != 1 {
		// 不符即停：首批条数不对。
		t.Fatalf("first %+v %v", got, err)
	}
	// 混了焊缝的工程整份拒绝，原因写作业类型不符。
	if got.Rejected[0].Path != "single_layer/混/project_data.json" || got.Rejected[0].Reason != domain.ErrWeldKindMismatch.Error() {
		// 不符即停：混了焊缝的工程没按作业类型拒绝。
		t.Fatalf("rejected %+v", got.Rejected)
	}
	// 按名字收起工艺，后面核对身份和作业类型。
	procByName := map[string]service.Asset{}
	// 逐条收名字和工艺。
	for _, p := range got.Processes {
		// 名字对到工艺。
		procByName[p.Name] = p
	}
	// 每份工艺该有的名字和作业类型。
	wantKinds := map[string]string{
		"1K6.5mmD":                 store.WeldKindSingle,
		"1K6.5mmD-多层":              store.WeldKindMultilayer,
		"打底":                       store.WeldKindTBar,
		"Standard/T排立对接/1H-3-4/打底": store.WeldKindTBar,
		"盖面":                       store.WeldKindTBar,
		"其它":                       store.WeldKindSingle,
	}
	// 逐个核对名字在、作业类型对。
	for n, k := range wantKinds {
		// 名字不在或作业类型不对说明拆份或预扫错了。
		if p, ok := procByName[n]; !ok || p.WeldKind != k {
			// 不符即停：工艺名字或作业类型不对。
			t.Fatalf("process %s want %s in %+v", n, k, got.Processes)
		}
	}
	// 按名字收起工程，后面逐份核对。
	projByName := map[string]service.Asset{}
	// 逐份收名字和工程。
	for _, p := range got.Projects {
		// 名字对到工程。
		projByName[p.Name] = p
	}
	// 每份工程该有的作业类型。
	for n, k := range map[string]string{"单": store.WeldKindSingle, "多": store.WeldKindMultilayer, "对接": store.WeldKindTBar} {
		// 作业类型不对说明新建工程没按正文推断。
		if projByName[n].WeldKind != k {
			// 不符即停：工程作业类型不对。
			t.Fatalf("project %s want %s got %+v", n, k, projByName[n])
		}
	}
	// 单层工程钉单层那份共用工艺。
	assertLegacyDeps(t, ctx, h.WAN, tok, projByName["单"].ID, procByName["1K6.5mmD"].ID.String())
	// 多层工程钉带多层后缀那份。
	assertLegacyDeps(t, ctx, h.WAN, tok, projByName["多"].ID, procByName["1K6.5mmD-多层"].ID.String())
	// 读取 T 排工程，核对依赖都是 T 排。
	tbarFull, err := h.WAN.GetPlatformAsset(ctx, tok, projByName["对接"].ID)
	// 读取失败或依赖条数不对就停。
	if err != nil || len(tbarFull.Deps) != 3 {
		// 不符即停：T 排工程依赖条数不对。
		t.Fatalf("tbar deps %+v %v", tbarFull.Deps, err)
	}
	// T 排工程的依赖全是 T 排那份。
	assertLegacyDepKinds(t, ctx, h.WAN, tok, tbarFull.Deps, store.WeldKindTBar)
	// 读 T 排工程正文，核对缝隙带。
	tbarBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, projByName["对接"].ID)
	// 读平台正文失败就停，读不到就无法比对。
	if err != nil {
		// 不符即停：读平台正文失败。
		t.Fatal(err)
	}
	// T 排目录那条带在前，6T1.2 样例目录只有打底。
	assertLegacyBands(t, tbarBody, []string{procByName["Standard/T排立对接/1H-3-4/打底"].ID.String(), procByName["盖面"].ID.String(), procByName["打底"].ID.String(), ""})
	// 多层和 T 排工程导入后网页改正文都要能过。
	for _, n := range []string{"多", "对接"} {
		// 读回工程正文，原样改一次。
		body, err := h.WAN.ReadPlatformAssetContent(ctx, tok, projByName[n].ID)
		// 读平台正文失败就停，读不到就无法比对。
		if err != nil {
			// 不符即停：读平台正文失败。
			t.Fatal(err)
		}
		// 网页改正文，作业类型不符会被拒。
		if _, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, projByName[n].ID, projByName[n].Revision, body); err != nil {
			// 不符即停：导入的作业类型和正文不符。
			t.Fatalf("update %s: %v", n, err)
		}
	}

	// 只重试那份多层工程，工艺照旧全部上送。
	retry, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Processes: procs,
		Projects:  []service.LegacyFile{{Path: "multi_layer/多/multilayer_data.json", Content: multiBody, Overwrite: true}},
	})
	// 导入失败、新建了工艺或工程没盖上就停，说明重试多出了副本。
	if err != nil || len(retry.Processes) != 0 || len(retry.Projects) != 1 || len(retry.Rejected) != 0 {
		// 不符即停：重试条数不对。
		t.Fatalf("retry %+v %v", retry, err)
	}
	// 共用那份按多层找回首导的带后缀那份。
	if !legacySkipped(retry.Skipped, "User/1K6.5mmD.json", "same content") || retry.Projects[0].ID != projByName["多"].ID {
		// 不符即停：重试没找回首导的身份。
		t.Fatalf("retry skipped %+v %+v", retry.Skipped, retry.Projects)
	}
	// 重试后多层工程仍钉带多层后缀那份。
	assertLegacyDeps(t, ctx, h.WAN, tok, projByName["多"].ID, procByName["1K6.5mmD-多层"].ID.String())
}

// 库里已有旧导入的单层工艺和工程，开覆盖重导后多层工程改成多层并钉多层那份。
func TestImportLegacyWeldKindOverwrite(t *testing.T) {
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
	// 单层和多层工程共用的旧工艺。
	proc := service.LegacyFile{Path: "User/1K6.5mmD.json", Content: []byte(`{"name":"1K6.5mmD","current":250}`)}
	// 引用共用工艺的单层工程。
	single := service.LegacyFile{Path: "single_layer/单/project_data.json", Content: []byte(`[{"id":"s1","processPath":"User/1K6.5mmD.json","points":[]}]`)}
	// 先只导单层工程，库里留下单层那份工艺。
	old, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Processes: []service.LegacyFile{proc}, Projects: []service.LegacyFile{single}})
	// 导入失败或条数不对就停，否则后面没有旧数据。
	if err != nil || len(old.Processes) != 1 || len(old.Projects) != 1 {
		// 不符即停：旧数据没导进来。
		t.Fatalf("old %+v %v", old, err)
	}
	// 读回旧工艺，拿修订和摘要钉依赖。
	oldProc, err := h.WAN.GetPlatformAsset(ctx, tok, old.Processes[0].ID)
	// 读取资产失败就停，读不到就无法钉依赖。
	if err != nil {
		// 不符即停：读取资产失败。
		t.Fatal(err)
	}
	// 旧导入落下的多层工程：正文是多层，作业类型却是单层。
	oldBody := []byte(`[{"id":"m1","kind":"multi","templateId":"` + contenttpl.SeedTplMulti + `","basePath":{"processId":"` + oldProc.ID.String() + `","points":[]},"passes":[]}]`)
	// 绕过服务直接落一条旧导入的工程，模拟修复前的数据。
	oldProj, err := h.WAN.Store().InsertAsset(ctx, service.Asset{
		Kind: service.KindProject, Name: "多", Status: service.AssetAvailable, Copyable: true, WeldKind: store.WeldKindSingle,
		Content: oldBody, Digest: digest.Sum(oldBody), CreatorID: oldProc.CreatorID,
		Deps: []service.AssetDep{{ID: oldProc.ID, Revision: oldProc.Revision, Digest: oldProc.Digest}},
	})
	// 绕过服务直接入库失败就停，否则后面没有旧数据。
	if err != nil {
		// 不符即停：绕过服务直接入库失败。
		t.Fatal(err)
	}
	// 开覆盖重导单层和多层工程。
	over, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Overwrite: true,
		Processes: []service.LegacyFile{proc},
		Projects: []service.LegacyFile{
			single,
			{Path: "multi_layer/多/multilayer_data.json", Content: []byte(`[{"id":"m1","basePath":{"processPath":"User/1K6.5mmD.json","points":[]},"passes":[]}]`)},
		},
	})
	// 导入失败或条数不对就停，说明多层那份没拆出来或工程没盖上。
	if err != nil || len(over.Processes) != 1 || len(over.Projects) != 2 || len(over.Rejected) != 0 {
		// 不符即停：覆盖重导条数不对。
		t.Fatalf("over %+v %v", over, err)
	}
	// 新拆出的应是带多层后缀的多层工艺，单层那份沿用。
	if over.Processes[0].Name != "1K6.5mmD-多层" || over.Processes[0].WeldKind != store.WeldKindMultilayer || !legacySkipped(over.Skipped, proc.Path, "same content") {
		// 不符即停：多层那份没按类型拆出。
		t.Fatalf("over process %+v %+v", over.Processes, over.Skipped)
	}
	// 读回旧工程，核对作业类型和依赖。
	full, err := h.WAN.GetPlatformAsset(ctx, tok, oldProj.ID)
	// 读取失败、类型没改或依赖不对就停。
	if err != nil || full.WeldKind != store.WeldKindMultilayer || len(full.Deps) != 1 || full.Deps[0].ID != over.Processes[0].ID {
		// 不符即停：旧工程没改成多层或钉错了那份。
		t.Fatalf("over project %+v %v", full, err)
	}
	// 单层工程仍钉单层那份旧工艺。
	assertLegacyDeps(t, ctx, h.WAN, tok, old.Projects[0].ID, oldProc.ID.String())
}

// 共用工艺拆份后跳过行按类型分开；按带回的类型和后缀重试，只动对应那份。
func TestImportLegacyRetrySplitCopy(t *testing.T) {
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
	// 单层和多层工程共用的旧工艺。
	proc := service.LegacyFile{Path: "User/1K6.5mmD.json", Content: []byte(`{"name":"1K6.5mmD","current":250}`)}
	// 引用共用工艺的单层和多层工程各一份。
	projs := []service.LegacyFile{
		{Path: "single_layer/单/project_data.json", Content: []byte(`[{"id":"s1","processPath":"User/1K6.5mmD.json","points":[]}]`)},
		{Path: "multi_layer/多/multilayer_data.json", Content: []byte(`[{"id":"m1","basePath":{"processPath":"User/1K6.5mmD.json","points":[]},"passes":[]}]`)},
	}
	// 首导把共用工艺拆成单层和带多层后缀的两份。
	first, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Processes: []service.LegacyFile{proc}, Projects: projs})
	// 导入失败或条数不对就停，说明没拆成两份。
	if err != nil || len(first.Processes) != 2 || len(first.Projects) != 2 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("first %+v %v", first, err)
	}
	// 按名字收起两份工艺。
	byName := map[string]service.Asset{}
	// 逐条收名字和工艺。
	for _, p := range first.Processes {
		// 名字对到工艺。
		byName[p.Name] = p
	}
	// 取出单层那份和多层那份。
	singleCopy, multiCopy := byName["1K6.5mmD"], byName["1K6.5mmD-多层"]
	// 少了任一份就停，后面的重试无从核对。
	if singleCopy.ID == uuid.Nil || multiCopy.ID == uuid.Nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("split %+v", first.Processes)
	}
	// 原样重导，两份都内容相同。
	same, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Processes: []service.LegacyFile{proc}, Projects: projs})
	// 重导失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把重导的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 两份各占一行，靠作业类型和后缀分开，不再是两条一样的。
	wantSame := []service.LegacyReject{
		{Path: proc.Path, Reason: "same content", WeldKind: store.WeldKindSingle},
		{Path: proc.Path, Reason: "same content", WeldKind: store.WeldKindMultilayer, Suffix: "-多层"},
	}
	// 逐行核对两份都在。
	for _, want := range wantSame {
		// 少了哪一行就停，说明跳过行没带上类型。
		if !slices.Contains(same.Skipped, want) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("same skipped %+v want %+v", same.Skipped, want)
		}
	}
	// 改过正文的同一份工艺。
	changed := service.LegacyFile{Path: proc.Path, Content: []byte(`{"name":"1K6.5mmD","current":260}`)}
	// 不开覆盖再导，两份都按同名内容不同跳过。
	diff, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Processes: []service.LegacyFile{changed}, Projects: projs})
	// 导入失败或新建了工艺就停，同名不开覆盖应只跳过。
	if err != nil || len(diff.Processes) != 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("diff %+v %v", diff, err)
	}
	// 多层那份的跳过行，带多层类型和后缀。
	multiRow := service.LegacyReject{Path: proc.Path, Reason: "name exists, content differs", WeldKind: store.WeldKindMultilayer, Suffix: "-多层"}
	// 没有这一行就停，网页没法单独重试多层那份。
	if !slices.Contains(diff.Skipped, multiRow) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("diff skipped %+v", diff.Skipped)
	}
	// 重试前读单层那份台账，后面核对没被顺带改掉。
	before, err := h.WAN.GetPlatformAsset(ctx, tok, singleCopy.ID)
	// 读台账失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把读台账的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 照网页重试多层那行：只带这份工艺，带回类型和后缀并开覆盖。
	retry, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Processes: []service.LegacyFile{{
		Path: changed.Path, Content: changed.Content, Overwrite: true, WeldKind: multiRow.WeldKind, Suffix: multiRow.Suffix,
	}}})
	// 失败或盖的不是多层那份就停，说明后缀那份仍盖不到。
	if err != nil || len(retry.Processes) != 1 || retry.Processes[0].ID != multiCopy.ID || len(retry.Rejected) != 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("retry %+v %v", retry, err)
	}
	// 重试后再读单层那份台账。
	after, err := h.WAN.GetPlatformAsset(ctx, tok, singleCopy.ID)
	// 读失败或修订、摘要变了就停，说明单层那份被顺带覆盖。
	if err != nil || after.Revision != before.Revision || !bytes.Equal(after.Digest, before.Digest) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("single changed %+v %v", after, err)
	}
	// 单层那行按重命名重试，应按路径另起一份单层。
	ren, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Processes: []service.LegacyFile{{
		Path: changed.Path, Content: changed.Content, Rename: true, WeldKind: store.WeldKindSingle,
	}}})
	// 失败、名字或作业类型不对就停，说明重命名没按单层那份来。
	if err != nil || len(ren.Processes) != 1 || ren.Processes[0].Name != "User/1K6.5mmD" || ren.Processes[0].WeldKind != store.WeldKindSingle {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("rename %+v %v", ren, err)
	}
	// 带回的类型不认、后缀对不上或只带后缀，都整份拒绝。
	bad, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Processes: []service.LegacyFile{
		{Path: "a.json", Content: changed.Content, WeldKind: "bogus"},
		{Path: "b.json", Content: changed.Content, WeldKind: store.WeldKindMultilayer, Suffix: "-T排"},
		{Path: "c.json", Content: changed.Content, Suffix: "-多层"},
	}})
	// 导入报错、落了库或拒绝条数不对就停。
	if err != nil || len(bad.Processes) != 0 || len(bad.Rejected) != 3 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("bad hint %+v %v", bad, err)
	}
	// 逐条核对拒绝原因。
	for _, r := range bad.Rejected {
		// 原因不是作业类型不认就停。
		if r.Reason != "invalid weld kind" {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("bad reason %+v", bad.Rejected)
		}
	}
}

// assertLegacyDepKinds 依赖里的每份工艺都是给定作业类型。
func assertLegacyDepKinds(t *testing.T, ctx context.Context, wan *service.Service, tok string, deps []service.AssetDep, kind string) {
	// 标记为辅助函数，报错行指向调用处。
	t.Helper()
	// 逐条依赖读回工艺。
	for _, d := range deps {
		// 读取依赖的工艺。
		p, err := wan.GetPlatformAsset(ctx, tok, d.ID)
		// 读不到或作业类型不对就停，说明钉错了那份。
		if err != nil || p.WeldKind != kind {
			// 不符即停：依赖的作业类型不对。
			t.Fatalf("dep %s kind %s want %s %v", d.ID, p.WeldKind, kind, err)
		}
	}
}

// legacySkipped 跳过清单里有这条路径且原因相同。
func legacySkipped(list []service.LegacyReject, path, reason string) bool {
	// 逐条找路径和原因都对上的。
	for _, r := range list {
		// 路径和原因都对上就算有。
		if r.Path == path && r.Reason == reason {
			return true
		}
	}
	return false
}

// assertLegacyDeps 工程只钉了这一份工艺。
func assertLegacyDeps(t *testing.T, ctx context.Context, wan *service.Service, tok string, projectID uuid.UUID, processID string) {
	// 标记为辅助函数，报错行指向调用处。
	t.Helper()
	// 读取资产，读不到就无法核对依赖。
	full, err := wan.GetPlatformAsset(ctx, tok, projectID)
	// 读取失败或依赖不对就停，说明钉错了工艺。
	if err != nil || len(full.Deps) != 1 || full.Deps[0].ID.String() != processID {
		// 不符即停：依赖钉错了工艺。
		t.Fatalf("deps %+v want %s %v", full.Deps, processID, err)
	}
}

// assertLegacyBands T 排正文里两条缝隙带的打底、盖面依次是给定身份。
func assertLegacyBands(t *testing.T, body []byte, want []string) {
	// 标记为辅助函数，报错行指向调用处。
	t.Helper()
	// 只取第一个元素的缝隙带。
	var items []struct {
		// 按目录补上的缝隙带，层号小的在前。
		GapBands []struct {
			Root string `json:"rootProcessId"` // 打底工艺身份
			Cap  string `json:"capProcessId"`  // 盖面工艺身份
		} `json:"gapBands"` // 按目录补上的缝隙带
	}
	// 正文解不开或没有元素就停。
	if err := json.Unmarshal(body, &items); err != nil || len(items) == 0 {
		// 不符即停：T 排正文解不开。
		t.Fatalf("tbar body %s %v", body, err)
	}
	// 按带收出打底和盖面，顺序是层号再间隙。
	var got []string
	// 逐条带收打底和盖面。
	for _, b := range items[0].GapBands {
		// 先打底后盖面。
		got = append(got, b.Root, b.Cap)
	}
	// 和预期逐个比，不同说明缝隙带钉错了目录。
	if !slices.Equal(got, want) {
		// 不符即停：缝隙带钉错了目录。
		t.Fatalf("bands %v want %v", got, want)
	}
}

// 同名工艺按很深的路径另起名时截到 64 字，序号也算在内；原样重导按内容沿用。
func TestImportLegacyRenameLongPath(t *testing.T) {
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
	// 先导一份浅路径的「角焊」，占住这个名字。
	first, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Processes: []service.LegacyFile{{Path: "1焊角.json", Content: []byte(`{"name":"角焊","current":1}`)}},
	})
	// 导入失败或条数不是 1 就停，不能当通过。
	if err != nil || len(first.Processes) != 1 {
		// 不符即停：首导没把工艺收进来。
		t.Fatalf("first %+v %v", first, err)
	}
	// 七十个字的目录，去掉扩展名后的路径名远超 64 字。
	deep := strings.Repeat("深", 70)
	// 两份同名工艺放在很深的路径下，前 64 字相同。
	ren := service.LegacyImport{Rename: true, Processes: []service.LegacyFile{
		{Path: deep + "/1焊角.json", Content: []byte(`{"name":"角焊","current":2}`)},
		{Path: deep + "/x/1焊角.json", Content: []byte(`{"name":"角焊","current":3}`)},
	}}
	// 按重命名导入，两份都应另起名建成。
	got, err := h.WAN.ImportLegacy(ctx, tok, ren)
	// 导入失败或有被拒的就停，超长名不该把它们挡掉。
	if err != nil || len(got.Processes) != 2 || len(got.Rejected) != 0 {
		// 不符即停：深路径同名工艺没有都导进来。
		t.Fatalf("rename %+v %v", got, err)
	}
	// 第一份用截到 64 字的路径名。
	if got.Processes[0].Name != strings.Repeat("深", 64) {
		// 不符即停：路径名没有截到 64 字。
		t.Fatalf("clip %q", got.Processes[0].Name)
	}
	// 第二份截短后再接序号，整名仍是 64 字。
	if got.Processes[1].Name != strings.Repeat("深", 60)+" (2)" {
		// 不符即停：序号前没有先截短原名。
		t.Fatalf("serial %q", got.Processes[1].Name)
	}
	// 原样再导一次，截过名的两份都应按内容沿用。
	again, err := h.WAN.ImportLegacy(ctx, tok, ren)
	// 导入失败或又新建了就停，重导不该出副本。
	if err != nil || len(again.Processes) != 0 || len(again.Skipped) != 2 {
		// 不符即停：截过名的工艺重导没认出来。
		t.Fatalf("again %+v %v", again, err)
	}
	// 第一份应按内容相同跳过。
	if !legacySkipped(again.Skipped, deep+"/1焊角.json", "same content") {
		// 不符即停：截短的路径名没进候选。
		t.Fatalf("again reason %+v", again.Skipped)
	}
}

// 升级前按完整路径名存下的超长工艺名，重导时仍按摘要沿用，不绑到别处同名工艺，也不另出副本。
func TestImportLegacyPreUpgradeLongPathLabel(t *testing.T) {
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
	// 浅路径那份占住短名「1K6.5mmD」。
	user := service.LegacyFile{Path: "User/1K6.5mmD.json", Content: []byte(`{"current":1}`)}
	// 七十一个字的目录，路径名远超 64 字。
	deepDir := "A" + strings.Repeat("深", 70)
	// 深路径下同名、内容不同的一份。
	deep := service.LegacyFile{Path: deepDir + "/1K6.5mmD.json", Content: []byte(`{"current":2}`)}
	// 先导浅路径那份，名字就是文件名。
	first, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Processes: []service.LegacyFile{user}})
	// 导入失败或条数不是 1 就停，不能当通过。
	if err != nil || len(first.Processes) != 1 || first.Processes[0].Name != "1K6.5mmD" {
		// 不符即停：浅路径那份没按文件名建成。
		t.Fatalf("user %+v %v", first, err)
	}
	// 再按另起名导深路径那份，撞短名后按路径另起。
	second, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Rename: true, Processes: []service.LegacyFile{deep}})
	// 导入失败或条数不是 1 就停，不能当通过。
	if err != nil || len(second.Processes) != 1 {
		// 不符即停：深路径那份没导进来。
		t.Fatalf("deep %+v %v", second, err)
	}
	// 升级前的写法：完整路径名，不截短。
	long := deepDir + "/1K6.5mmD"
	// 把名字改回完整路径名，摆出升级前留下的数据。
	if err := h.DB.Exec("UPDATE assets SET name = ? WHERE id = ?", long, second.Processes[0].ID).Error; err != nil {
		// 不符即停：摆不出旧数据就验不了。
		t.Fatal(err)
	}
	// 引用深路径那份的工程，覆盖导入时会按路径重新钉依赖。
	proj := service.LegacyFile{Path: "single_layer/深焊/project_data.json", Content: []byte(`[{"id":"w1","processPath":"` + deep.Path + `","points":[]}]`), Overwrite: true}
	// 整棵树按默认方式重导，深路径那份应按完整路径名和摘要认回原身份。
	again, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Processes: []service.LegacyFile{user, deep}, Projects: []service.LegacyFile{proj}})
	// 导入失败、又新建了工艺或有被拒的就停。
	if err != nil || len(again.Processes) != 0 || len(again.Projects) != 1 || len(again.Rejected) != 0 {
		// 不符即停：升级前的长名没认出来。
		t.Fatalf("again %+v %v", again, err)
	}
	// 两份都该按内容相同跳过，深路径那份不能落成同名内容不同。
	if !legacySkipped(again.Skipped, deep.Path, "same content") || !legacySkipped(again.Skipped, user.Path, "same content") {
		// 不符即停：深路径那份绑到了别处的同名工艺。
		t.Fatalf("again reason %+v", again.Skipped)
	}
	// 工程应钉住深路径那份原身份，不能钉到浅路径那份。
	assertLegacyDeps(t, ctx, h.WAN, tok, again.Projects[0].ID, second.Processes[0].ID.String())
	// 按另起名再导一次，内容相同仍应沿用，不出截短名的副本。
	ren, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Rename: true, Processes: []service.LegacyFile{user, deep}})
	// 导入失败或又新建了就停。
	if err != nil || len(ren.Processes) != 0 || !legacySkipped(ren.Skipped, deep.Path, "same content") {
		// 不符即停：另起名时出了副本。
		t.Fatalf("rename %+v %v", ren, err)
	}
}

// 升级前存下的超长显示名，重导时仍按原写法认同名：默认跳过并沿用，覆盖时改原身份。
func TestImportLegacyPreUpgradeLongName(t *testing.T) {
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
	// 七十个字的显示名，远超 64 字上限。
	long := strings.Repeat("长", 70)
	// 首导时名字会被截到 64 字。
	first, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{
		Processes: []service.LegacyFile{{Path: "p/1.json", Content: []byte(`{"name":"` + long + `","current":1}`)}},
		Projects:  []service.LegacyFile{{Path: "single_layer/x/project_data.json", Name: long, Content: []byte(`[{"id":"w1","processPath":"p/1.json","points":[]}]`)}},
	})
	// 导入失败或条数不对就停，不能当通过。
	if err != nil || len(first.Processes) != 1 || len(first.Projects) != 1 {
		// 不符即停：首导没把工艺和工程都收进来。
		t.Fatalf("first %+v %v", first, err)
	}
	// 把两条的名字都改回完整写法，摆出升级前留下的数据。
	for _, id := range []uuid.UUID{first.Processes[0].ID, first.Projects[0].ID} {
		// 直接改库，服务层已写不出超长名。
		if err := h.DB.Exec("UPDATE assets SET name = ? WHERE id = ?", long, id).Error; err != nil {
			// 不符即停：摆不出旧数据就验不了。
			t.Fatal(err)
		}
	}
	// 同路径换了内容再按默认方式导，应按长名认出同名并跳过。
	changed := service.LegacyFile{Path: "p/1.json", Content: []byte(`{"name":"` + long + `","current":2}`)}
	// 工程原样再导，同名应跳过。
	proj := service.LegacyFile{Path: "single_layer/x/project_data.json", Name: long, Content: []byte(`[{"id":"w1","processPath":"p/1.json","points":[]}]`)}
	// 默认方式重导。
	skip, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Processes: []service.LegacyFile{changed}, Projects: []service.LegacyFile{proj}})
	// 导入失败或新建了截短名的副本就停。
	if err != nil || len(skip.Processes) != 0 || len(skip.Projects) != 0 {
		// 不符即停：长名没认出来，另出了副本。
		t.Fatalf("skip %+v %v", skip, err)
	}
	// 工艺应按同名内容不同跳过，工程按同名跳过。
	if !legacySkipped(skip.Skipped, "p/1.json", "name exists, content differs") || !legacySkipped(skip.Skipped, proj.Path, "name exists") {
		// 不符即停：跳过原因不对。
		t.Fatalf("skip reason %+v", skip.Skipped)
	}
	// 按覆盖再导，应改原身份，不另建。
	over, err := h.WAN.ImportLegacy(ctx, tok, service.LegacyImport{Overwrite: true, Processes: []service.LegacyFile{changed}, Projects: []service.LegacyFile{proj}})
	// 导入失败或条数不对就停。
	if err != nil || len(over.Processes) != 1 || len(over.Projects) != 1 {
		// 不符即停：覆盖没落到原身份上。
		t.Fatalf("over %+v %v", over, err)
	}
	// 覆盖后身份变了就停，长名那条应原地改。
	if over.Processes[0].ID != first.Processes[0].ID || over.Projects[0].ID != first.Projects[0].ID {
		// 不符即停：覆盖另建了一份。
		t.Fatalf("over ids %s %s", over.Processes[0].ID, over.Projects[0].ID)
	}
}
