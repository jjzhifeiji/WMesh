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
	// 条数或长度应符合预期，为空或超长说明不全。
	if len(got.Processes) != 3 {
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
	// 逐条看导入结果，缺了应在的工艺或工程即失败。
	for _, p := range got.Processes {
		// 级别应是预期那一档，升错或没升都算失败。
		if p.Name == "角焊" {
			// 把编号打成文本，方便和路径或正文比对。
			weldID = p.ID.String()
			// 级别应是预期那一档，升错或没升都算失败。
			if p.Status != factory.AssetAvailable || p.Level != factory.AssetLevelFactory {
				// 断言没通过就停，结果和这条用例的预期不一致。
				t.Fatalf("process %+v", p)
			}
		}
	}
	// 应能找到角焊工艺，没有说明导入把工艺丢掉了。
	if weldID == "" {
		// 应能找到角焊工艺，没有说明导入把工艺丢掉了。
		t.Fatal("missing 角焊")
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
			// 正文里应能对上这段，对不上说明没写进去。
			if !bytes.Contains(body, []byte(contenttpl.SeedTplMulti)) || !bytes.Contains(body, []byte(weldID)) {
				// 断言没通过就停，结果和这条用例的预期不一致。
				t.Fatalf("multi %s", body)
			}
		// T排应展开间隙和根工艺，缺了说明旧结构还在。
		case bytes.Contains(body, []byte(contenttpl.SeedTplTBar)):
			// 正文里应能对上这段，对不上说明没写进去。
			if !bytes.Contains(body, []byte("gapBands")) || !bytes.Contains(body, []byte("rootProcessId")) {
				// 断言没通过就停，结果和这条用例的预期不一致。
				t.Fatalf("tbar %s", body)
			}
			// 条数或长度应符合预期，为空或超长说明不全。
			if len(full.Deps) < 2 {
				// 依赖和焊道引用不一致，说明工程没有对齐。
				t.Fatalf("tbar deps %+v", full.Deps)
			}
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
