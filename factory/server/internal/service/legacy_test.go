// F2：厂端导入旧工艺/工程；缺路径整份工程拒绝，已入工艺保留。
package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 导入旧工艺和工程，引用对齐且不把旧正文套入。
func TestImportLegacy(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	pe := mustCreateRole(t, ctx, fac, saTok, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 建人并授好角色，失败说明后面没有账号可用。
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 建人并授好角色，失败说明后面没有账号可用。
	oa := mustCreateRole(t, ctx, fac, saTok, "oa", "oa-pass", factory.RoleOrgAdmin, factory.ScopeFactory, nil)

	// 准备这段正文字节，读回对不上说明没写进去。
	procBody := []byte(`{"name":"角焊","current":170}`)
	// 准备这段正文字节，读回对不上说明没写进去。
	okProj := []byte(`[{"id":"w1","name":"焊道","processPath":"1焊角.json","points":[]}]`)
	// 准备这段正文字节，读回对不上说明没写进去。
	emptyProj := []byte(`[{"id":"w2","name":"空","processPath":"","points":[]}]`)
	// 准备这段正文字节，读回对不上说明没写进去。
	badProj := []byte(`[{"id":"w3","name":"缺","processPath":"no-such.json","points":[]}]`)
	// 准备这段正文字节，读回对不上说明没写进去。
	multiProj := []byte(`[{"id":"m1","name":"多层","basePath":{"processPath":"1焊角.json","points":[]},"passes":[{"id":"p1","process":{"current":1},"processPath":""}]}]`)
	// 准备这段正文字节，读回对不上说明没写进去。
	tbarProj := []byte(`[{"id":"t1","name":"T","points":[{"id":"a","type":"GROOVE_A_LOWER"}]}]`)
	// 准备这段正文字节，读回对不上说明没写进去。
	rootBody := []byte(`{"name":"打底","current":160}`)
	// 准备这段正文字节，读回对不上说明没写进去。
	capBody := []byte(`{"name":"盖面","current":180}`)

	// 装好旧版导入包，缺工艺或工程则导入结果是空的。
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
	// 导入旧包应因未登录被拒绝，放行说明没拦住。
	if _, err := fac.ImportLegacy(ctx, "", in); !errors.Is(err, domain.ErrUnauthorized) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("anon import: %v", err)
	}
	// 导入旧包应因越权被拒绝，放行说明没拦住。
	if _, err := fac.ImportLegacy(ctx, op.tok, factory.LegacyImport{}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("op import: %v", err)
	}
	// 导入旧包应因越权被拒绝，放行说明没拦住。
	if _, err := fac.ImportLegacy(ctx, pe.tok, factory.LegacyImport{}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("pe import: %v", err)
	}
	// 导入旧版工艺包，失败说明包结构对不上。
	empty, err := fac.ImportLegacy(ctx, oa.tok, factory.LegacyImport{})
	// 导入旧包失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把导入旧包的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 条数或长度应符合预期，为空或超长说明不全。
	if len(empty.Processes) != 0 || len(empty.Projects) != 0 {
		// 结果是空的，说明该写入的内容没有落下。
		t.Fatalf("oa empty %+v", empty)
	}

	// 导入旧版工艺包，失败说明包结构对不上。
	got, err := fac.ImportLegacy(ctx, saTok, in)
	// 导入旧包失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把导入旧包的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 角焊被单层和多层共用，按类型拆成两份，共四份工艺。
	if len(got.Processes) != 4 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("processes %d rejected %+v", len(got.Processes), got.Rejected)
	}
	// 条数或长度应符合预期，为空或超长说明不全。
	if len(got.Projects) != 4 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("projects %d rejected %+v", len(got.Projects), got.Rejected)
	}
	// 导入结果里工艺、工程和拒绝项都应在，缺了说明没拆开。
	if got.Processes == nil || got.Projects == nil || got.Rejected == nil || got.Skipped == nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("nil slices %+v", got)
	}
	// 名称或编号应带上预期片段，对不上说明规则偏了。
	if len(got.Rejected) != 1 || !strings.Contains(got.Rejected[0].Path, "坏") {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("rejected %+v", got.Rejected)
	}

	// 先留空工艺编号，扫到角焊再填上供工程引用。
	var weldID string
	// 多层那份角焊的编号，扫到再填上。
	var multiID string
	// 逐条看导入结果，缺了应在的工艺或工程即失败。
	for _, p := range got.Processes {
		// 级别应是预期那一档，升错或没升都算失败。
		if p.Name == "角焊" {
			// 把编号打成文本，方便和路径或正文比对。
			weldID = p.ID.String()
			// 级别应是预期那一档，升错或没升都算失败。
			if p.Status != factory.AssetAvailable || p.Level != factory.AssetLevelFactory || p.WeldKind != store.WeldKindSingle {
				// 断言没通过就停，结果和这条用例的预期不一致。
				t.Fatalf("process %+v", p)
			}
		}
		// 多层工程引用的那份应带多层后缀，作业类型是多层。
		if p.Name == "角焊-多层" {
			// 把编号打成文本，方便和正文比对。
			multiID = p.ID.String()
			// 作业类型不是多层说明拆份没按类型来。
			if p.WeldKind != store.WeldKindMultilayer {
				// 断言没通过就停，结果和这条用例的预期不一致。
				t.Fatalf("multi process %+v", p)
			}
		}
	}
	// 应能找到角焊工艺，没有说明导入把工艺丢掉了。
	if weldID == "" || multiID == "" {
		// 应能找到两份角焊，没有说明共用工艺没拆份。
		t.Fatalf("missing 角焊 %+v", got.Processes)
	}

	// 逐条看导入结果，缺了应在的工艺或工程即失败。
	for _, proj := range got.Projects {
		// 读回资产正文，失败说明无权读或稿已经没了。
		body, err := fac.ReadAssetContent(ctx, pe.tok, proj.ID)
		// 读正文失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把读正文的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 正文里不应残留这段，还在说明旧结构没清掉。
		if bytes.Contains(body, []byte("processPath")) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("processPath left in %s %s", proj.Name, body)
		}
		// 正文里不应残留这段，还在说明旧结构没清掉。
		if bytes.Contains(body, []byte(`"process":{`)) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("nested process left in %s %s", proj.Name, body)
		}
		// 读取资产台账，失败说明无权看或已经删除。
		full, err := fac.GetAsset(ctx, pe.tok, proj.ID)
		// 读台账失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把读台账的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 按工程类型分头核对引用，对不上说明导入没拆开。
		switch {
		// 平焊应接上单层模版和角焊，缺了说明引用没对齐。
		case strings.Contains(proj.Name, "平焊") || proj.Name == "平焊":
			// 正文里应能对上这段，对不上说明没写进去。
			if !bytes.Contains(body, []byte(weldID)) || !bytes.Contains(body, []byte(contenttpl.SeedTplSingle)) {
				// 断言没通过就停，结果和这条用例的预期不一致。
				t.Fatalf("single %s", body)
			}
			// 条数或长度应符合预期，为空或超长说明不全。
			if len(full.Deps) != 1 || full.Deps[0].ID.String() != weldID {
				// 依赖和焊道引用不一致，说明工程没有对齐。
				t.Fatalf("single deps %+v", full.Deps)
			}
		// 空工程应只留空引用，还嵌着旧稿说明没拆干净。
		case proj.Name == "空":
			// 正文里应能对上这段，对不上说明没写进去。
			if !bytes.Contains(body, []byte(`"processId":""`)) {
				// 结果是空的，说明该写入的内容没有落下。
				t.Fatalf("empty %s", body)
			}
			// 条数或长度应符合预期，为空或超长说明不全。
			if len(full.Deps) != 0 {
				// 结果是空的，说明该写入的内容没有落下。
				t.Fatalf("empty deps %+v", full.Deps)
			}
		// 多层应接上多层模版和角焊，缺了说明类型没对齐。
		case strings.Contains(string(body), contenttpl.SeedTplMulti) || proj.Name == "多层":
			// 正文里应钉多层那份角焊，对不上说明没按类型取。
			if !bytes.Contains(body, []byte(contenttpl.SeedTplMulti)) || !bytes.Contains(body, []byte(multiID)) {
				// 断言没通过就停，结果和这条用例的预期不一致。
				t.Fatalf("multi %s", body)
			}
			// 多层工程作业类型应是多层，只钉多层那份。
			if full.WeldKind != store.WeldKindMultilayer || len(full.Deps) != 1 || full.Deps[0].ID.String() != multiID {
				// 断言没通过就停，结果和这条用例的预期不一致。
				t.Fatalf("multi deps %s %+v", full.WeldKind, full.Deps)
			}
		// T排应展开间隙和根工艺，缺了说明旧结构还在。
		case bytes.Contains(body, []byte(contenttpl.SeedTplTBar)):
			// 正文里应能对上这段，对不上说明没写进去。
			if !bytes.Contains(body, []byte("gapBands")) || !bytes.Contains(body, []byte("rootProcessId")) {
				// 断言没通过就停，结果和这条用例的预期不一致。
				t.Fatalf("tbar %s", body)
			}
			// 条数或长度应符合预期，为空或超长说明不全。
			if len(full.Deps) < 2 || full.WeldKind != store.WeldKindTBar {
				// 依赖和焊道引用不一致，说明工程没有对齐。
				t.Fatalf("tbar deps %s %+v", full.WeldKind, full.Deps)
			}
			// T 排工程的依赖都应是 T 排那份。
			assertLegacyDepKinds(t, ctx, fac, saTok, full.Deps, store.WeldKindTBar)
		}
	}

	// 拉出审计流水，失败则无法核对有没有记账。
	rows, err := fac.ListAudit(ctx)
	// 拉审计失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把拉审计的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 审计应留下允许或拒绝，缺了说明这步没记账。
	if bad := audit.Incomplete(rows); len(bad) > 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("incomplete %#v", bad[0])
	}
	// 把审计打成可搜文本，打不出则查不了泄密。
	dump := audit.Dump(rows)
	// 审计里不应出现口令或正文，出现了就算泄密。
	if !audit.ContainsAny(dump, "import_legacy") {
		// 应存在的记录没有出现，说明这一步没落下。
		t.Fatalf("missing import_legacy: %s", dump)
	}
	// 审计里不应出现口令或正文，出现了就算泄密。
	if audit.ContainsAny(dump, "pe-pass", "sa-pass", string(procBody)) {
		// 结果里出现了不该有的敏感词，说明已经泄密。
		t.Fatalf("secret or body leaked")
	}
}

// 缺路径时整份工程拒绝，已经导入的工艺要保留。
func TestImportLegacyMissingKeepsProcess(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 导入旧版工艺包，失败说明包结构对不上。
	got, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{
		Processes: []factory.LegacyFile{{Path: "keep.json", Content: []byte(`{"name":"留","current":1}`)}},
		Projects:  []factory.LegacyFile{{Path: "gone/project_data.json", Content: []byte(`[{"processPath":"missing.json"}]`)}},
	})
	// 导入旧包失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把导入旧包的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 条数或长度应符合预期，为空或超长说明不全。
	if len(got.Processes) != 1 || len(got.Projects) != 0 || len(got.Rejected) != 1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%+v", got)
	}
	// 导入旧版工艺包，失败说明包结构对不上。
	empty, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{})
	// 导入旧包失败或完整性不对就停，说明没达预期。
	if err != nil || empty.Processes == nil || empty.Projects == nil || empty.Rejected == nil || empty.Skipped == nil {
		// 结果是空的，说明该写入的内容没有落下。
		t.Fatalf("empty %+v %v", empty, err)
	}
	// 状态应符合这一步的预期，停在旧状态说明没流转。
	if got.Processes[0].Name != "留" || got.Processes[0].Status != factory.AssetAvailable {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("kept %+v", got.Processes[0])
	}
}

// T 排工程缺间隙带且本批没有缝隙目录工艺时整份拒绝；自带间隙带或同批带上工艺的照旧入库。
func TestImportLegacyTBarNeedsGapBands(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 不带间隙带的 T 排工程，只能靠本批缝隙目录补带。
	tbar := factory.LegacyFile{Path: "tbar/对接/project_data.json", Content: []byte(`[{"id":"t1","name":"T","points":[{"id":"a","type":"GROOVE_A_LOWER"}]}]`)}
	// 正文自带间隙带的 T 排工程，不靠目录补。
	own := factory.LegacyFile{Path: "tbar/自带/project_data.json", Content: []byte(`[{"id":"t2","name":"T","points":[{"id":"a","type":"GROOVE_A_LOWER"}],"gapBands":[{"minGap":3,"maxGap":4,"layer":1,"rootProcessId":"","capProcessId":""}]}]`)}
	// 带了空间隙带的 T 排工程，和没带一样要靠本批缝隙目录补。
	empty := factory.LegacyFile{Path: "tbar/空带/project_data.json", Content: []byte(`[{"id":"t3","name":"T","points":[{"id":"a","type":"GROOVE_A_LOWER"}],"gapBands":[]}]`)}
	// 本批只有一份普通工艺，没有缝隙目录。
	plain := factory.LegacyFile{Path: "1焊角.json", Content: []byte(`{"name":"角焊","current":170}`)}
	// 引用普通工艺的单层工程，核对别的工程不受牵连。
	single := factory.LegacyFile{Path: "single_layer/平焊/project_data.json", Content: []byte(`[{"id":"w1","processPath":"1焊角.json","points":[]}]`)}
	// 先导工艺和工程，但不带缝隙目录那几份工艺。
	first, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{Processes: []factory.LegacyFile{plain}, Projects: []factory.LegacyFile{tbar, empty, own, single}})
	// 导入报错或条数不对就停，缺带和空带的两份 T 排应被拒。
	if err != nil || len(first.Processes) != 1 || len(first.Projects) != 2 || len(first.Rejected) != 2 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("first %+v %v", first, err)
	}
	// 按路径收起被拒的份，后面逐份核对原因。
	rejected := map[string]string{}
	// 逐条收路径和原因。
	for _, r := range first.Rejected {
		// 路径对到原因，后面按路径查。
		rejected[r.Path] = r.Reason
	}
	// 被拒的应是缺带和空带那两份，原因提示把工艺和工程一起导入。
	for _, path := range []string{tbar.Path, empty.Path} {
		// 没被拒或原因不对就停，空带不能算自带。
		if !strings.HasPrefix(rejected[path], "tbar gap bands missing") {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("rejected %s %+v", path, first.Rejected)
		}
	}
	// 读出本厂全部资产，核对缺带和空带两份都没有入库。
	rows, err := fac.Store().ListGovernedAssets(ctx)
	// 读资产失败就停，否则没法确认没入库。
	if err != nil {
		// 把读资产的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 逐条找被拒两份的名字，找到说明拒绝了还入了库。
	for _, a := range rows {
		// 工程里出现「对接」或「空带」说明被拒那份落了库。
		if a.Kind == factory.KindProject && (a.Name == "对接" || a.Name == "空带") {
			// 断言没通过就停，拒绝的工程不该入库。
			t.Fatalf("rejected project stored %+v", a)
		}
	}
	// 缝隙目录里的打底和盖面。
	gaps := []factory.LegacyFile{
		{Path: "Standard/T排立对接/1H-3-4/打底.json", Content: []byte(`{"name":"打底","current":160}`)},
		{Path: "Standard/T排立对接/1H-3-4/盖面.json", Content: []byte(`{"name":"盖面","current":180}`)},
	}
	// 缝隙目录工艺和缺带、空带工程一起导入，应照旧成功。
	second, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{Processes: gaps, Projects: []factory.LegacyFile{tbar, empty}})
	// 导入报错或条数不对就停，同批带上工艺不该再拒。
	if err != nil || len(second.Processes) != 2 || len(second.Projects) != 2 || len(second.Rejected) != 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("second %+v %v", second, err)
	}
	// 按名字收起本批工艺身份，后面核对缝隙带。
	ids := map[string]string{}
	// 逐条收名字和身份。
	for _, p := range second.Processes {
		// 名字对到身份文本。
		ids[p.Name] = p.ID.String()
	}
	// 逐份读回 T 排工程正文，缺带和空带的都该补上缝隙带。
	for _, p := range second.Projects {
		// 读回这份工程正文。
		body, err := fac.ReadAssetContent(ctx, saTok, p.ID)
		// 读正文失败就停，否则后面没有可靠结果。
		if err != nil {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatal(err)
		}
		// 缝隙带应钉上同批那份打底和盖面，空带不能原样留下。
		assertLegacyBands(t, body, []string{ids["打底"], ids["盖面"]})
	}
}

// 同名再导入应另立新稿，不能把原来那条覆盖掉。
func TestImportLegacySameName(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 准备这段正文字节，读回对不上说明没写进去。
	proc := factory.LegacyFile{Path: "1焊角.json", Content: []byte(`{"name":"角焊","current":170}`)}
	// 准备这段正文字节，读回对不上说明没写进去。
	proj := factory.LegacyFile{Path: "single_layer/平焊/project_data.json", Content: []byte(`[{"id":"w1","processPath":"1焊角.json","points":[]}]`)}
	// 导入旧版工艺包，失败说明包结构对不上。
	first, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{Processes: []factory.LegacyFile{proc}, Projects: []factory.LegacyFile{proj}})
	// 导入旧包失败或条数不对就停，说明没达预期。
	if err != nil || len(first.Processes) != 1 || len(first.Projects) != 1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("first %+v %v", first, err)
	}
	// 导入旧版工艺包，失败说明包结构对不上。
	skip, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{
		Processes: []factory.LegacyFile{proc},
		Projects: []factory.LegacyFile{
			proj,
			{Path: "single_layer/另焊/project_data.json", Content: []byte(`[{"id":"w2","processPath":"1焊角.json","points":[]}]`)},
		},
	})
	// 这一步失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把这一步的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 条数或长度应符合预期，为空或超长说明不全。
	if len(skip.Processes) != 0 || len(skip.Projects) != 1 || len(skip.Skipped) != 2 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("skip %+v", skip)
	}
	// 内容没变的工艺按摘要沿用，原因应写内容相同。
	if !legacySkipped(skip.Skipped, "1焊角.json", "same content") {
		// 原因不对说明没按摘要沿用。
		t.Fatalf("skip reason %+v", skip.Skipped)
	}
	// 结果应和这一步的预期一致，偏离说明行为写偏了。
	if skip.Projects[0].Name != "另焊" {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("new project %+v", skip.Projects[0])
	}
	// 读取资产台账，失败说明无权看或已经删除。
	full, err := fac.GetAsset(ctx, saTok, skip.Projects[0].ID)
	// 读台账失败或条数不对就停，说明没达预期。
	if err != nil || len(full.Deps) != 1 || full.Deps[0].ID != first.Processes[0].ID {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("pin %+v %v", full.Deps, err)
	}
	// 导入旧版工艺包，失败说明包结构对不上。
	over, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{
		Overwrite: true,
		Processes: []factory.LegacyFile{{Path: "1焊角.json", Content: []byte(`{"name":"角焊","current":999}`)}},
		Projects:  []factory.LegacyFile{proj},
	})
	// 导入旧包失败或条数不对就停，说明没达预期。
	if err != nil || len(over.Processes) != 1 || len(over.Projects) != 1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("over %+v %v", over, err)
	}
	// 应是另一条记录而不是同一条，撞号说明没另立。
	if over.Processes[0].ID != first.Processes[0].ID || over.Projects[0].ID != first.Projects[0].ID {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("id changed %s %s", over.Processes[0].ID, over.Projects[0].ID)
	}
	// 修订号应按这次保存前进，没变说明写没落库。
	if over.Processes[0].Revision <= first.Processes[0].Revision {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("rev %d", over.Processes[0].Revision)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	body, err := fac.ReadAssetContent(ctx, saTok, over.Processes[0].ID)
	// 读正文失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Contains(body, []byte("999")) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("content %s %v", body, err)
	}
	// 导入旧版工艺包，失败说明包结构对不上。
	ren, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{
		Rename: true,
		Processes: []factory.LegacyFile{
			{Path: "yy/1焊角.json", Content: []byte(`{"name":"角焊","current":1}`)},
		},
		Projects: []factory.LegacyFile{
			{Path: "single_layer/平焊/multilayer_data.json", Content: []byte(`[{"id":"m","processPath":"yy/1焊角.json","points":[]}]`)},
		},
	})
	// 这一步失败或条数不对就停，说明没达预期。
	if err != nil || len(ren.Processes) != 1 || len(ren.Projects) != 1 || len(ren.Skipped) != 0 {
		// 名称没有改成预期，说明这次改名没生效。
		t.Fatalf("rename %+v %v", ren, err)
	}
	// 应是另一条记录而不是同一条，撞号说明没另立。
	if ren.Processes[0].ID == first.Processes[0].ID || ren.Processes[0].Name != "yy/1焊角" {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("process name %+v", ren.Processes[0])
	}
	// 应是另一条记录而不是同一条，撞号说明没另立。
	if ren.Projects[0].ID == first.Projects[0].ID || ren.Projects[0].Name != "single_layer/平焊-多层" {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("project name %+v", ren.Projects[0])
	}
	// 导入旧版工艺包，失败说明包结构对不上。
	one, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{
		Processes: []factory.LegacyFile{
			{Path: "1焊角.json", Content: []byte(`{"name":"角焊","current":1}`)},
			{Path: "zz/1焊角.json", Content: []byte(`{"name":"角焊","current":2}`), Rename: true},
		},
		Projects: []factory.LegacyFile{
			{Path: "single_layer/平焊/project_data.json", Content: proj.Content, Overwrite: true},
		},
	})
	// 这一步失败或条数不对就停，说明没达预期。
	if err != nil || len(one.Processes) != 1 || len(one.Projects) != 1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("one %+v %v", one, err)
	}
	// 应是另一条记录而不是同一条，撞号说明没另立。
	if one.Processes[0].Name != "zz/1焊角" || one.Projects[0].ID != first.Projects[0].ID {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("one names %+v %+v", one.Processes[0], one.Projects[0])
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	kept, err := fac.ReadAssetContent(ctx, saTok, first.Processes[0].ID)
	// 读正文失败或正文不同就停，说明没达预期。
	if err != nil || bytes.Contains(kept, []byte("\"current\":2")) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("process should stay %s %v", kept, err)
	}
}

// 首批同名不同路径的工艺各有身份；原样重导沿用，覆盖时各改各的。
func TestImportLegacyPathIdentity(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 标准目录那份同名工艺，偏移 1.5。
	std := factory.LegacyFile{Path: "Standard/1T1.2/1P-1F/1K6.5mmD.json", Content: []byte(`{"name":"1K6.5mmD","offsetX":"1.5","current":250}`)}
	// 用户目录那份同名工艺，偏移 10。
	usr := factory.LegacyFile{Path: "User/1K6.5mmD.json", Content: []byte(`{"name":"1K6.5mmD","offsetX":"10","current":250}`)}
	// T 排两个缝隙目录各放一份打底和盖面，名字都一样；后一个目录放前面验排序。
	gaps := []factory.LegacyFile{
		{Path: "Standard/6T1.2-T排立对接/1H-4-5/打底.json", Content: []byte(`{"name":"打底","current":165}`)},
		{Path: "Standard/6T1.2-T排立对接/1H-4-5/盖面.json", Content: []byte(`{"name":"盖面","current":185}`)},
		{Path: "Standard/6T1.2-T排立对接/1H-3-4/打底.json", Content: []byte(`{"name":"打底","current":160}`)},
		{Path: "Standard/6T1.2-T排立对接/1H-3-4/盖面.json", Content: []byte(`{"name":"盖面","current":180}`)},
	}
	// 用户目录那份放最前，排序后仍应由标准目录留原名。
	procs := append([]factory.LegacyFile{usr, std}, gaps...)
	// 引用用户目录那份工艺的单层工程。
	userBody := []byte(`[{"id":"w1","processPath":"User/1K6.5mmD.json","points":[]}]`)
	// 不带缝隙带的 T 排工程，导入时按目录补带。
	tbarBody := []byte(`[{"id":"t1","name":"T","points":[{"id":"a","type":"GROOVE_A_LOWER"}]}]`)
	// 首批工程：两份同名不同路径的工程，加上单层和 T 排各一份。
	projs := []factory.LegacyFile{
		{Path: "single_layer/b/平焊/project_data.json", Content: userBody},
		{Path: "single_layer/a/平焊/project_data.json", Content: userBody},
		{Path: "single_layer/用户/project_data.json", Content: userBody},
		{Path: "tbar/对接/project_data.json", Content: tbarBody},
	}
	// 首批开着覆盖导入，本批同名也不能互相改写。
	first, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{Overwrite: true, Processes: procs, Projects: projs})
	// 导入失败或条数不对就停，说明本批同名被并掉了。
	if err != nil || len(first.Processes) != 6 || len(first.Projects) != 4 || len(first.Skipped) != 0 || len(first.Rejected) != 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
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
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("missing %s in %v", n, ids)
		}
	}
	// 标准目录那份的正文不能被用户目录那份改写。
	stdBody, err := fac.ReadAssetContent(ctx, saTok, first.Processes[0].ID)
	// 读正文失败或被改写就停，说明开覆盖时盖了本批刚建的。
	if err != nil || first.Processes[0].ID.String() != ids["1K6.5mmD"] || !bytes.Contains(stdBody, []byte(`"1.5"`)) || bytes.Contains(stdBody, []byte(`"10"`)) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("std body %s %v", stdBody, err)
	}
	// 按名字收起工程，后面逐份核对。
	projByName := map[string]factory.Asset{}
	// 逐份收名字和工程。
	for _, p := range first.Projects {
		// 名字对到工程。
		projByName[p.Name] = p
	}
	// 两份同名工程应各自入库，后一份按路径另起名。
	if projByName["平焊"].ID == projByName["single_layer/b/平焊"].ID || projByName["single_layer/b/平焊"].Name == "" {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("projects %+v", first.Projects)
	}
	// 单层工程应钉用户目录那份工艺。
	assertLegacyDeps(t, ctx, fac, saTok, projByName["用户"].ID, ids["User/1K6.5mmD"])
	// 读 T 排工程正文，核对缝隙带。
	tbar, err := fac.ReadAssetContent(ctx, saTok, projByName["对接"].ID)
	// 读正文失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把读正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 两条缝隙带的打底和盖面应各是各的目录那份。
	assertLegacyBands(t, tbar, []string{ids["打底"], ids["盖面"], ids["Standard/6T1.2-T排立对接/1H-4-5/打底"], ids["Standard/6T1.2-T排立对接/1H-4-5/盖面"]})

	// 原样再导一次，另带两份新工程核对路径对照。
	again, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{
		Processes: procs,
		Projects: append(slices.Clone(projs),
			factory.LegacyFile{Path: "single_layer/用户二/project_data.json", Content: userBody},
			factory.LegacyFile{Path: "tbar/对接二/project_data.json", Content: tbarBody},
		),
	})
	// 导入失败或新建了工艺就停，说明重导出了副本。
	if err != nil || len(again.Processes) != 0 || len(again.Projects) != 2 || len(again.Skipped) != 10 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("again %+v %v", again, err)
	}
	// 每份工艺都应按摘要沿用，原因写内容相同。
	for _, f := range procs {
		// 缺了说明这份没按摘要找回首导的身份。
		if !legacySkipped(again.Skipped, f.Path, "same content") {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("not same content %s %+v", f.Path, again.Skipped)
		}
	}
	// 按名字收起新工程。
	againByName := map[string]factory.Asset{}
	// 逐份收名字和工程。
	for _, p := range again.Projects {
		// 名字对到工程。
		againByName[p.Name] = p
	}
	// 新单层工程仍应钉用户目录那份。
	assertLegacyDeps(t, ctx, fac, saTok, againByName["用户二"].ID, ids["User/1K6.5mmD"])
	// 读新 T 排工程正文，核对缝隙带。
	tbar2, err := fac.ReadAssetContent(ctx, saTok, againByName["对接二"].ID)
	// 读正文失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把读正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 路径对照不变，缝隙带仍钉首导那几份。
	assertLegacyBands(t, tbar2, []string{ids["打底"], ids["盖面"], ids["Standard/6T1.2-T排立对接/1H-4-5/打底"], ids["Standard/6T1.2-T排立对接/1H-4-5/盖面"]})

	// 两份都改了内容，开覆盖再导。
	over, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{
		Overwrite: true,
		Processes: []factory.LegacyFile{
			{Path: usr.Path, Content: []byte(`{"name":"1K6.5mmD","offsetX":"12","current":250}`)},
			{Path: std.Path, Content: []byte(`{"name":"1K6.5mmD","offsetX":"2.5","current":250}`)},
		},
	})
	// 导入失败或条数不对就停，说明覆盖没各改各的。
	if err != nil || len(over.Processes) != 2 || len(over.Skipped) != 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("over %+v %v", over, err)
	}
	// 每份覆盖后的身份和名字都应不变，正文各自更新。
	for _, p := range over.Processes {
		// 身份应是首导那份，变了说明新建了工艺。
		if ids[p.Name] != p.ID.String() {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("identity changed %+v", p)
		}
		// 读回正文，核对改的是自己那份。
		body, err := fac.ReadAssetContent(ctx, saTok, p.ID)
		// 读正文失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把读正文的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 标准目录那份应改成 2.5，用户目录那份改成 12。
		want := map[string]string{"1K6.5mmD": `"2.5"`, "User/1K6.5mmD": `"12"`}[p.Name]
		// 正文对不上说明覆盖串到了别的身份。
		if want == "" || !bytes.Contains(body, []byte(want)) {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("over body %s %s", p.Name, body)
		}
	}
}

// 共用工艺按作业类型拆份，工程和工艺的作业类型都对，导入后网页和示教器都能改。
func TestImportLegacyWeldKind(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 单层和多层工程共用的一份工艺，另有 T 排目录和 6T1.2 样例目录。
	procs := []factory.LegacyFile{
		{Path: "User/1K6.5mmD.json", Content: []byte(`{"name":"1K6.5mmD","current":250}`)},
		{Path: "Standard/T排立对接/1H-3-4/打底.json", Content: []byte(`{"name":"打底","current":160}`)},
		{Path: "Standard/T排立对接/1H-3-4/盖面.json", Content: []byte(`{"name":"盖面","current":180}`)},
		{Path: "Standard/6T1.2/2H-4-5/打底.json", Content: []byte(`{"name":"打底","current":165}`)},
		{Path: "其它.json", Content: []byte(`{"name":"其它","current":100}`)},
	}
	// 多层工程正文，基准层引用共用那份工艺。
	multiBody := []byte(`[{"id":"m1","basePath":{"processPath":"User/1K6.5mmD.json","points":[]},"passes":[]}]`)
	// 单层、多层、T 排各一份，另加一份混了单层和多层的工程。
	projs := []factory.LegacyFile{
		{Path: "single_layer/单/project_data.json", Content: []byte(`[{"id":"s1","processPath":"User/1K6.5mmD.json","points":[]}]`)},
		{Path: "multi_layer/多/multilayer_data.json", Content: multiBody},
		{Path: "tbar/对接/project_data.json", Content: []byte(`[{"id":"t1","points":[{"id":"a","type":"GROOVE_A_LOWER"}]}]`)},
		{Path: "single_layer/混/project_data.json", Content: []byte(`[{"id":"a","processPath":"User/1K6.5mmD.json","points":[]},{"id":"b","basePath":{"processPath":"User/1K6.5mmD.json","points":[]},"passes":[]}]`)},
	}
	// 首次导入整批，混了焊缝的那份应被拒。
	got, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{Processes: procs, Projects: projs})
	// 导入失败或条数不对就停，说明拆份或拒绝不对。
	if err != nil || len(got.Processes) != 6 || len(got.Projects) != 3 || len(got.Rejected) != 1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("first %+v %v", got, err)
	}
	// 混了焊缝的工程整份拒绝，原因写作业类型不符。
	if got.Rejected[0].Path != "single_layer/混/project_data.json" || got.Rejected[0].Reason != domain.ErrWeldKindMismatch.Error() {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("rejected %+v", got.Rejected)
	}
	// 按名字收起工艺，后面核对身份和作业类型。
	procByName := map[string]factory.Asset{}
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
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("process %s want %s in %+v", n, k, got.Processes)
		}
	}
	// 按名字收起工程，后面逐份核对。
	projByName := map[string]factory.Asset{}
	// 逐份收名字和工程。
	for _, p := range got.Projects {
		// 名字对到工程。
		projByName[p.Name] = p
	}
	// 每份工程该有的作业类型。
	for n, k := range map[string]string{"单": store.WeldKindSingle, "多": store.WeldKindMultilayer, "对接": store.WeldKindTBar} {
		// 作业类型不对说明新建工程没按正文推断。
		if projByName[n].WeldKind != k {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("project %s want %s got %+v", n, k, projByName[n])
		}
	}
	// 单层工程钉单层那份共用工艺。
	assertLegacyDeps(t, ctx, fac, saTok, projByName["单"].ID, procByName["1K6.5mmD"].ID.String())
	// 多层工程钉带多层后缀那份。
	assertLegacyDeps(t, ctx, fac, saTok, projByName["多"].ID, procByName["1K6.5mmD-多层"].ID.String())
	// 读取 T 排工程台账，核对依赖都是 T 排。
	tbarFull, err := fac.GetAsset(ctx, saTok, projByName["对接"].ID)
	// 读台账失败或依赖条数不对就停。
	if err != nil || len(tbarFull.Deps) != 3 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("tbar deps %+v %v", tbarFull.Deps, err)
	}
	// T 排工程的依赖全是 T 排那份。
	assertLegacyDepKinds(t, ctx, fac, saTok, tbarFull.Deps, store.WeldKindTBar)
	// 读 T 排工程正文，核对缝隙带。
	tbarBody, err := fac.ReadAssetContent(ctx, saTok, projByName["对接"].ID)
	// 读正文失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把读正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// T 排目录那条带在前，6T1.2 样例目录只有打底。
	assertLegacyBands(t, tbarBody, []string{procByName["Standard/T排立对接/1H-3-4/打底"].ID.String(), procByName["盖面"].ID.String(), procByName["打底"].ID.String(), ""})
	// 多层和 T 排工程导入后网页改正文、示教器盖回都要能过。
	for _, n := range []string{"多", "对接"} {
		// 读回工程正文，原样改一次。
		body, err := fac.ReadAssetContent(ctx, saTok, projByName[n].ID)
		// 读正文失败就停，否则后面没有可靠结果。
		if err != nil {
			// 把读正文的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 网页改正文，作业类型不符会被拒。
		row, err := fac.UpdateAssetContent(ctx, saTok, projByName[n].ID, projByName[n].Revision, body)
		// 改正文失败就停，说明导入的作业类型和正文不符。
		if err != nil {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("update %s: %v", n, err)
		}
		// 示教器盖回正文，作业类型不符会被拒。
		if _, err := fac.ApplyAppContent(ctx, saTok, row.ID, body); err != nil {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("apply %s: %v", n, err)
		}
	}

	// 只重试那份多层工程，工艺照旧全部上送。
	retry, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{
		Processes: procs,
		Projects:  []factory.LegacyFile{{Path: "multi_layer/多/multilayer_data.json", Content: multiBody, Overwrite: true}},
	})
	// 导入失败、新建了工艺或工程没盖上就停，说明重试多出了副本。
	if err != nil || len(retry.Processes) != 0 || len(retry.Projects) != 1 || len(retry.Rejected) != 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("retry %+v %v", retry, err)
	}
	// 共用那份按多层找回首导的带后缀那份。
	if !legacySkipped(retry.Skipped, "User/1K6.5mmD.json", "same content") || retry.Projects[0].ID != projByName["多"].ID {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("retry skipped %+v %+v", retry.Skipped, retry.Projects)
	}
	// 重试后多层工程仍钉带多层后缀那份。
	assertLegacyDeps(t, ctx, fac, saTok, projByName["多"].ID, procByName["1K6.5mmD-多层"].ID.String())
}

// 库里已有旧导入的单层工艺和工程，开覆盖重导后多层工程改成多层并钉多层那份。
func TestImportLegacyWeldKindOverwrite(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 单层和多层工程共用的旧工艺。
	proc := factory.LegacyFile{Path: "User/1K6.5mmD.json", Content: []byte(`{"name":"1K6.5mmD","current":250}`)}
	// 引用共用工艺的单层工程。
	single := factory.LegacyFile{Path: "single_layer/单/project_data.json", Content: []byte(`[{"id":"s1","processPath":"User/1K6.5mmD.json","points":[]}]`)}
	// 先只导单层工程，库里留下单层那份工艺。
	old, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{Processes: []factory.LegacyFile{proc}, Projects: []factory.LegacyFile{single}})
	// 导入失败或条数不对就停，否则后面没有旧数据。
	if err != nil || len(old.Processes) != 1 || len(old.Projects) != 1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("old %+v %v", old, err)
	}
	// 读回旧工艺台账，拿修订和摘要钉依赖。
	oldProc, err := fac.GetAsset(ctx, saTok, old.Processes[0].ID)
	// 读台账失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把读台账的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 旧导入落下的多层工程：正文是多层，作业类型却是单层。
	oldBody := []byte(`[{"id":"m1","kind":"multi","templateId":"` + contenttpl.SeedTplMulti + `","basePath":{"processId":"` + oldProc.ID.String() + `","points":[]},"passes":[]}]`)
	// 绕过服务直接落一条旧导入的工程，模拟修复前的数据。
	oldProj, err := fac.Store().InsertGovernedAsset(ctx, factory.Asset{
		Kind: factory.KindProject, Level: factory.AssetLevelFactory, Name: "多", Status: factory.AssetAvailable,
		Copyable: true, WeldKind: store.WeldKindSingle, Content: oldBody, Digest: digest.Sum(oldBody), CreatorID: seed.SuperAdminID,
		Deps: []factory.AssetDep{{ID: oldProc.ID, Revision: oldProc.Revision, Digest: oldProc.Digest}},
	})
	// 落旧工程失败就停，否则后面没有旧数据。
	if err != nil {
		// 把落库的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开覆盖重导单层和多层工程。
	over, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{
		Overwrite: true,
		Processes: []factory.LegacyFile{proc},
		Projects: []factory.LegacyFile{
			single,
			{Path: "multi_layer/多/multilayer_data.json", Content: []byte(`[{"id":"m1","basePath":{"processPath":"User/1K6.5mmD.json","points":[]},"passes":[]}]`)},
		},
	})
	// 导入失败或条数不对就停，说明多层那份没拆出来或工程没盖上。
	if err != nil || len(over.Processes) != 1 || len(over.Projects) != 2 || len(over.Rejected) != 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("over %+v %v", over, err)
	}
	// 新拆出的应是带多层后缀的多层工艺，单层那份沿用。
	if over.Processes[0].Name != "1K6.5mmD-多层" || over.Processes[0].WeldKind != store.WeldKindMultilayer || !legacySkipped(over.Skipped, proc.Path, "same content") {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("over process %+v %+v", over.Processes, over.Skipped)
	}
	// 读回旧工程台账，核对作业类型和依赖。
	full, err := fac.GetAsset(ctx, saTok, oldProj.ID)
	// 读台账失败、类型没改或依赖不对就停。
	if err != nil || full.WeldKind != store.WeldKindMultilayer || len(full.Deps) != 1 || full.Deps[0].ID != over.Processes[0].ID {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("over project %+v %v", full, err)
	}
	// 单层工程仍钉单层那份旧工艺。
	assertLegacyDeps(t, ctx, fac, saTok, old.Projects[0].ID, oldProc.ID.String())
}

// 共用工艺拆份后跳过行按类型分开；按带回的类型和后缀重试，只动对应那份。
func TestImportLegacyRetrySplitCopy(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 单层和多层工程共用的旧工艺。
	proc := factory.LegacyFile{Path: "User/1K6.5mmD.json", Content: []byte(`{"name":"1K6.5mmD","current":250}`)}
	// 引用共用工艺的单层和多层工程各一份。
	projs := []factory.LegacyFile{
		{Path: "single_layer/单/project_data.json", Content: []byte(`[{"id":"s1","processPath":"User/1K6.5mmD.json","points":[]}]`)},
		{Path: "multi_layer/多/multilayer_data.json", Content: []byte(`[{"id":"m1","basePath":{"processPath":"User/1K6.5mmD.json","points":[]},"passes":[]}]`)},
	}
	// 首导把共用工艺拆成单层和带多层后缀的两份。
	first, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{Processes: []factory.LegacyFile{proc}, Projects: projs})
	// 导入失败或条数不对就停，说明没拆成两份。
	if err != nil || len(first.Processes) != 2 || len(first.Projects) != 2 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("first %+v %v", first, err)
	}
	// 按名字收起两份工艺。
	byName := map[string]factory.Asset{}
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
	same, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{Processes: []factory.LegacyFile{proc}, Projects: projs})
	// 重导失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把重导的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 两份各占一行，靠作业类型和后缀分开，不再是两条一样的。
	wantSame := []factory.LegacyReject{
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
	changed := factory.LegacyFile{Path: proc.Path, Content: []byte(`{"name":"1K6.5mmD","current":260}`)}
	// 不开覆盖再导，两份都按同名内容不同跳过。
	diff, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{Processes: []factory.LegacyFile{changed}, Projects: projs})
	// 导入失败或新建了工艺就停，同名不开覆盖应只跳过。
	if err != nil || len(diff.Processes) != 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("diff %+v %v", diff, err)
	}
	// 多层那份的跳过行，带多层类型和后缀。
	multiRow := factory.LegacyReject{Path: proc.Path, Reason: "name exists, content differs", WeldKind: store.WeldKindMultilayer, Suffix: "-多层"}
	// 没有这一行就停，网页没法单独重试多层那份。
	if !slices.Contains(diff.Skipped, multiRow) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("diff skipped %+v", diff.Skipped)
	}
	// 重试前读单层那份台账，后面核对没被顺带改掉。
	before, err := fac.GetAsset(ctx, saTok, singleCopy.ID)
	// 读台账失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把读台账的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 照网页重试多层那行：只带这份工艺，带回类型和后缀并开覆盖。
	retry, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{Processes: []factory.LegacyFile{{
		Path: changed.Path, Content: changed.Content, Overwrite: true, WeldKind: multiRow.WeldKind, Suffix: multiRow.Suffix,
	}}})
	// 失败或盖的不是多层那份就停，说明后缀那份仍盖不到。
	if err != nil || len(retry.Processes) != 1 || retry.Processes[0].ID != multiCopy.ID || len(retry.Rejected) != 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("retry %+v %v", retry, err)
	}
	// 重试后再读单层那份台账。
	after, err := fac.GetAsset(ctx, saTok, singleCopy.ID)
	// 读失败或修订、摘要变了就停，说明单层那份被顺带覆盖。
	if err != nil || after.Revision != before.Revision || !bytes.Equal(after.Digest, before.Digest) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("single changed %+v %v", after, err)
	}
	// 单层那行按重命名重试，应按路径另起一份单层。
	ren, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{Processes: []factory.LegacyFile{{
		Path: changed.Path, Content: changed.Content, Rename: true, WeldKind: store.WeldKindSingle,
	}}})
	// 失败、名字或作业类型不对就停，说明重命名没按单层那份来。
	if err != nil || len(ren.Processes) != 1 || ren.Processes[0].Name != "User/1K6.5mmD" || ren.Processes[0].WeldKind != store.WeldKindSingle {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("rename %+v %v", ren, err)
	}
	// 带回的类型不认、后缀对不上或只带后缀，都整份拒绝。
	bad, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{Processes: []factory.LegacyFile{
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
func assertLegacyDepKinds(t *testing.T, ctx context.Context, fac *factory.Service, tok string, deps []factory.AssetDep, kind string) {
	// 标记为辅助函数，报错行指向调用处。
	t.Helper()
	// 逐条依赖读回工艺台账。
	for _, d := range deps {
		// 读取依赖的工艺台账。
		p, err := fac.GetAsset(ctx, tok, d.ID)
		// 读不到或作业类型不对就停，说明钉错了那份。
		if err != nil || p.WeldKind != kind {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("dep %s kind %s want %s %v", d.ID, p.WeldKind, kind, err)
		}
	}
}

// legacySkipped 跳过清单里有这条路径且原因相同。
func legacySkipped(list []factory.LegacyReject, path, reason string) bool {
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
func assertLegacyDeps(t *testing.T, ctx context.Context, fac *factory.Service, tok string, projectID uuid.UUID, processID string) {
	// 标记为辅助函数，报错行指向调用处。
	t.Helper()
	// 读取资产台账，失败说明无权看或已经删除。
	full, err := fac.GetAsset(ctx, tok, projectID)
	// 读台账失败或依赖不对就停，说明钉错了工艺。
	if err != nil || len(full.Deps) != 1 || full.Deps[0].ID.String() != processID {
		// 断言没通过就停，结果和这条用例的预期不一致。
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
		// 断言没通过就停，结果和这条用例的预期不一致。
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
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("bands %v want %v", got, want)
	}
}

// 单份被拒只给业务原话；库写不进（名字含 0x00）时对外只写 internal error，不带 SQLSTATE。
func TestImportLegacyRejectReasonHidesInternal(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 名字含 0x00 的工艺，库会以编码错误拒写。
	nulProc := factory.LegacyFile{Path: "nul.json", Name: "坏\x00艺", Content: []byte(`{"name":"坏","current":1}`)}
	// 名字含 0x00 的工程，正文没问题，同样写不进库。
	nulProj := factory.LegacyFile{Path: "nul/project_data.json", Name: "坏\x00程", Content: []byte(`[{"id":"w1","points":[]}]`)}
	// 正文不是数组的工程，按业务拒绝。
	notArr := factory.LegacyFile{Path: "obj/project_data.json", Content: []byte(`{}`)}
	// 引用不存在工艺的工程，按依赖缺失拒绝。
	gone := factory.LegacyFile{Path: "gone/project_data.json", Content: []byte(`[{"processPath":"missing.json"}]`)}
	// 一批导入，四份都该被拒，整批不报错。
	got, err := fac.ImportLegacy(ctx, saTok, factory.LegacyImport{Processes: []factory.LegacyFile{nulProc}, Projects: []factory.LegacyFile{nulProj, notArr, gone}})
	// 导入报错或条数不对就停，单份库错不该连累整批。
	if err != nil || len(got.Processes) != 0 || len(got.Projects) != 0 || len(got.Rejected) != 4 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%+v %v", got, err)
	}
	// 按路径记下期望原因：库错一律笼统，业务错误给原话。
	want := map[string]string{
		nulProc.Path: "internal error",
		nulProj.Path: "internal error",
		notArr.Path:  domain.ErrForbidden.Error(),
		gone.Path:    domain.ErrAssetDependency.Error(),
	}
	// 逐份核对原因，库原文一个字都不能出现。
	for _, r := range got.Rejected {
		// 原因不符或带了 SQLSTATE 就停，说明内部错误漏到了响应里。
		if r.Reason != want[r.Path] || strings.Contains(r.Reason, "SQLSTATE") {
			// 断言没通过就停，结果和这条用例的预期不一致。
			t.Fatalf("reject %s: %q want %q", r.Path, r.Reason, want[r.Path])
		}
	}
}
