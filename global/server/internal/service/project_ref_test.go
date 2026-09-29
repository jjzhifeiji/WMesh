// 工程焊道引用工艺身份；F2 空库三份、已有份不自动插 T 排。
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

	"github.com/google/uuid"
)

// 单层焊道模版身份，工程引用时拿它当键。
const seedSingleID = "11111111-1111-4111-8111-111111111111"

// 焊道按工艺身份引用，拿编号当身份必须拒绝。
func TestProjectProcessRefs(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 固定云端口令，登录和泄密检查都用它。
	const wanPass = "wan-secret"
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		// 立云端超管失败就停，立不住后面没有人能登录。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", wanPass)
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 新建平台工艺「工艺」，建不成后面没有工艺号可对。
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "工艺", []byte(`{"name":"p","current":180}`))
	// 新建平台工艺「工艺」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「工艺」失败。
		t.Fatal(err)
	}
	// 发布资产，发不出去后面不能当可用。
	proc, err = h.WAN.PublishPlatformAsset(ctx, tok, proc.ID, proc.Revision)
	// 发布资产失败就停，发不出去后面不能当可用。
	if err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 钉住依赖的身份修订和摘要，错了工程建不成。
	dep := global.AssetDep{ID: proc.ID, Revision: proc.Revision, Digest: proc.Digest}

	// 新建平台工程「空引用」，建不成后面没有工程可引用。
	empty, err := h.WAN.CreatePlatformProject(ctx, tok, "空引用", []byte(`[]`), []global.AssetDep{dep})
	// 新建平台工程「空引用」失败就停，建不成后面没有工程可引用。
	if err != nil {
		// 不符即停：新建平台工程「空引用」失败。
		t.Fatal(err)
	}
	// 读平台正文，读不到就无法比对。
	emptyBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, empty.ID)
	// 读平台正文失败就停，读不到就无法比对。
	if err != nil {
		// 不符即停：读平台正文失败。
		t.Fatal(err)
	}
	// 从焊道收集工艺身份，收空说明引用没写上。
	ids, err := contenttpl.CollectProcessIDsFromItems(contenttpl.SeedProjectItems(), emptyBody)
	// 收集焊道里的工艺身份失败或条数不是0就停，不能当通过。
	if err != nil || len(ids) != 0 {
		// 不符即停：收集焊道里的工艺身份失败或条数不是0。
		t.Fatalf("empty refs %v %v", ids, err)
	}
	// 这里正文没有清空就停，这一步不能算通过。
	if string(emptyBody) != "[]" {
		// 不符即停：这里正文没有清空。
		t.Fatalf("empty body %s", emptyBody)
	}
	// 新造一个身份，后面用来区分是不是同一份。
	bad := []byte(`[{"templateId":"` + seedSingleID + `","processId":"` + id.New().String() + `"}]`)
	// 新建平台工程「错引用」应被拒为依赖不符，放行或错类都算没拦住。
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "错引用", bad, []global.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("create mismatch %v", err)
	}
	// 准备正文样本，后面入库和比对都用它。
	pathBody := []byte(`[{"templateId":"` + seedSingleID + `","processId":"` + proc.ID.String() + `","processPath":"x.json"}]`)
	// 新建平台工程「路径键」应被拒为越权，放行或错类都算没拦住。
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "路径键", pathBody, nil); !errors.Is(err, domain.ErrForbidden) {
		// 不符即停：新建平台工程「路径键」应被拒为越权。
		t.Fatalf("create processPath %v", err)
	}

	// 准备正文样本，后面入库和比对都用它。
	okBody := []byte(`[{"templateId":"` + seedSingleID + `","name":"w","processId":"` + proc.ID.String() + `"}]`)
	// 新建平台工程「对齐」，建不成后面没有工程可引用。
	proj, err := h.WAN.CreatePlatformProject(ctx, tok, "对齐", okBody, nil)
	// 新建平台工程「对齐」失败就停，建不成后面没有工程可引用。
	if err != nil {
		// 不符即停：新建平台工程「对齐」失败。
		t.Fatal(err)
	}
	// 这份工程条数不是1或身份变了就停，不能当通过。
	if len(proj.Deps) != 1 || proj.Deps[0].ID != proc.ID {
		// 不符即停：这份工程条数不是1或身份变了。
		t.Fatalf("pinned %+v", proj.Deps)
	}
	// 改平台正文应被拒为依赖不符，放行或错类都算没拦住。
	if _, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, proj.ID, proj.Revision, []byte(`[{"templateId":"`+seedSingleID+`","processId":"`+id.New().String()+`"}]`)); !errors.Is(err, domain.ErrAssetDependency) {
		// 不符即停：改平台正文应被拒为依赖不符。
		t.Fatalf("update mismatch %v", err)
	}
	// 正文还留着旧路径就停，导入没有换成身份。
	if _, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, proj.ID, proj.Revision, []byte(`[{"templateId":"`+seedSingleID+`","processId":"`+proc.ID.String()+`","processPath":"x.json"}]`)); !errors.Is(err, domain.ErrForbidden) {
		// 不符即停：正文还留着旧路径。
		t.Fatalf("update processPath %v", err)
	}
	// 读取资产，读不到就无法核对原件。
	proj, err = h.WAN.GetPlatformAsset(ctx, tok, proj.ID)
	// 读取资产失败就停，读不到就无法核对原件。
	if err != nil {
		// 不符即停：读取资产失败。
		t.Fatal(err)
	}
	// 改工程依赖应被拒为依赖不符，放行或错类都算没拦住。
	if _, err := h.WAN.SetPlatformProjectDeps(ctx, tok, proj.ID, proj.Revision, nil); !errors.Is(err, domain.ErrAssetDependency) {
		// 不符即停：改工程依赖应被拒为依赖不符。
		t.Fatalf("drop dep %v", err)
	}

	// 登记工厂「厂A」，建不成后面没有厂可授权。
	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 新造一个身份，后面用来区分是不是同一份。
	srcProc := id.New()
	// 准备正文样本，后面入库和比对都用它。
	pbody := []byte(`{"name":"厂工艺","current":180}`)
	// 拼升档快照，身份或摘要错了会被拒或串档。
	psnap := global.AssetSnapshot{
		SourceID: srcProc, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
		Kind: global.KindProcess, Name: "厂工艺", Content: pbody, Digest: digest.Sum(pbody),
		Copyable: true, Status: global.AssetAvailable,
	}
	// 升档，升不上去档位就断了。
	plat, err := h.WAN.PromoteFromSnapshot(ctx, tok, psnap)
	// 升档失败就停，升不上去档位就断了。
	if err != nil {
		// 不符即停：升档失败。
		t.Fatal(err)
	}
	// 发布资产，发不出去后面不能当可用。
	plat, err = h.WAN.PublishPlatformAsset(ctx, tok, plat.ID, plat.Revision)
	// 发布资产失败就停，发不出去后面不能当可用。
	if err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 把结果打成文本，以便检查有没有人员字段。
	projJSON, err := json.Marshal([]map[string]any{{
		"name":      "升档工程",
		"processId": srcProc.String(),
		"keep":      true,
	}})
	// 导出字段表失败就停，导不出就没法套正文。
	if err != nil {
		// 不符即停：导出字段表失败。
		t.Fatal(err)
	}
	// 新造一个身份，后面用来区分是不是同一份。
	projSrc := id.New()
	// 拼升档快照，身份或摘要错了会被拒或串档。
	jsnap := global.AssetSnapshot{
		SourceID: projSrc, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
		Kind: global.KindProject, Name: "厂工程", Content: projJSON, Digest: digest.Sum(projJSON),
		Copyable: true, Status: global.AssetAvailable,
		Deps: []global.AssetDep{{ID: srcProc, Revision: 1, Digest: digest.Sum(pbody)}},
	}
	// 升档，升不上去档位就断了。
	got, err := h.WAN.PromoteFromSnapshot(ctx, tok, jsnap)
	// 升档失败就停，升不上去档位就断了。
	if err != nil {
		// 不符即停：升档失败。
		t.Fatal(err)
	}
	// 返回值条数不是1或身份变了就停，不能当通过。
	if len(got.Deps) != 1 || got.Deps[0].ID != plat.ID {
		// 不符即停：返回值条数不是1或身份变了。
		t.Fatalf("deps %+v", got.Deps)
	}
	// 读平台正文，读不到就无法比对。
	rewritten, err := h.WAN.ReadPlatformAssetContent(ctx, tok, got.ID)
	// 读平台正文失败就停，读不到就无法比对。
	if err != nil {
		// 不符即停：读平台正文失败。
		t.Fatal(err)
	}
	// 从焊道收集工艺身份，收空说明引用没写上。
	refs, err := contenttpl.CollectProcessIDsFromItems(contenttpl.SeedProjectItems(), rewritten)
	// 收集焊道里的工艺身份失败或条数不是1就停，不能当通过。
	if err != nil || len(refs) != 1 || refs[0] != plat.ID.String() {
		// 不符即停：收集焊道里的工艺身份失败或条数不是1。
		t.Fatalf("rewritten refs %v %s", refs, rewritten)
	}
	// 先留出位置，循环里找到再填，找不到就失败。
	var welds []map[string]any
	// 解开焊道失败就停，解不开就看不到焊道字段。
	if err := json.Unmarshal(rewritten, &welds); err != nil || welds[0]["keep"] != true {
		// 不符即停：解开焊道失败。
		t.Fatalf("keep %s", rewritten)
	}
	// 升档，升不上去档位就断了。
	again, err := h.WAN.PromoteFromSnapshot(ctx, tok, jsnap)
	// 升档失败或身份变了就停，不能当通过。
	if err != nil || again.ID != got.ID || again.Revision != got.Revision {
		// 不符即停：升档失败或身份变了。
		t.Fatalf("skip %+v %v", again, err)
	}
	// 拼升档快照，身份或摘要错了会被拒或串档。
	missing := global.AssetSnapshot{
		SourceID: id.New(), SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
		Kind: global.KindProject, Name: "缺工艺", Content: projJSON, Digest: digest.Sum(projJSON),
		Copyable: true, Status: global.AssetAvailable,
		Deps: []global.AssetDep{{ID: id.New(), Revision: 1, Digest: digest.Sum(pbody)}},
	}
	// 升档应被拒为依赖不符，放行或错类都算没拦住。
	if _, err := h.WAN.PromoteFromSnapshot(ctx, tok, missing); !errors.Is(err, domain.ErrAssetDependency) {
		// 不符即停：升档应被拒为依赖不符。
		t.Fatalf("missing process %v", err)
	}

	// 读平台正文，读不到就无法比对。
	oldBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proj.ID)
	// 读平台正文失败就停，读不到就无法比对。
	if err != nil {
		// 不符即停：读平台正文失败。
		t.Fatal(err)
	}
	// 列工程模版，模版列表读不到就对不了种子。
	list, err := h.WAN.ListProjectTemplates(ctx, tok)
	// 列工程模版失败或条数不是3就停，不能当通过。
	if err != nil || len(list) != 3 {
		// 不符即停：列工程模版失败或条数不是3。
		t.Fatalf("list %d %v", len(list), err)
	}
	// 先当没找到，循环里命中再改成真。
	foundTBar := false
	// 逐条查看结果，漏看一条会把结论判错。
	for _, row := range list {
		// 正文出现这段才进这支，用来分种类核对。
		if row.ID.String() == contenttpl.SeedTplTBar && bytes.Contains(row.Schema, []byte("gapBands")) && !bytes.Contains(row.Schema, []byte("processPath")) {
			// 标成已命中，最后仍为假说明名单里没有。
			foundTBar = true
		}
	}
	// T排模版没有出现就停，名单或种子丢了。
	if !foundTBar {
		// 不符即停：T排模版没有出现。
		t.Fatalf("empty db missing tbar: %+v", list)
	}
	// 先占一个候选，循环里再改成真正要的那条。
	single := list[0]
	// 逐条查看结果，漏看一条会把结论判错。
	for _, row := range list {
		// 条件成立才做这一支，漏进来会算错样本。
		if row.ID.String() == seedSingleID {
			// 先占一个候选，循环里再改成真正要的那条。
			single = row
			break
		}
	}
	// 改工程模版，工程模版改不了种子会对不上。
	renamed, err := h.WAN.UpdateProjectTemplate(ctx, tok, single.ID, single.Revision, "单层焊道改名", single.Schema)
	// 改工程模版失败或修订没有跟上就停，不能当通过。
	if err != nil || renamed.Revision != single.Revision+1 {
		// 不符即停：改工程模版失败或修订没有跟上。
		t.Fatalf("rename %+v %v", renamed, err)
	}
	// 读平台正文，读不到就无法比对。
	still, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proj.ID)
	// 读平台正文失败或正文不一致就停，不能当通过。
	if err != nil || !bytes.Equal(still, oldBody) {
		// 不符即停：读平台正文失败或正文不一致。
		t.Fatalf("old body changed %s", still)
	}
	// 改工程模版应被拒为模版不合法，放行或错类都算没拦住。
	if _, err := h.WAN.UpdateProjectTemplate(ctx, tok, renamed.ID, renamed.Revision, "", renamed.Schema); !errors.Is(err, domain.ErrTemplateInvalid) {
		// 不符即停：改工程模版应被拒为模版不合法。
		t.Fatalf("empty name %v", err)
	}
	// 建工程模版「坡口」，工程模版建不成列表对不齐。
	created, err := h.WAN.CreateProjectTemplate(ctx, tok, uuid.Nil, "坡口", nil)
	// 建工程模版「坡口」失败就停，工程模版建不成列表对不齐。
	if err != nil {
		// 不符即停：建工程模版「坡口」失败。
		t.Fatal(err)
	}
	// 删工程模版失败就停，删不掉就验不了会不会补回。
	if err := h.WAN.DeleteProjectTemplate(ctx, tok, created.ID); err != nil {
		// 不符即停：删工程模版失败。
		t.Fatal(err)
	}
	// 新造一个身份，后面用来区分是不是同一份。
	extraBad := []byte(`[{"templateId":"` + seedSingleID + `","extraProcesses":[{"processId":"` + id.New().String() + `"}]}]`)
	// 新建平台工程「模版错引用」应被拒为依赖不符，放行或错类都算没拦住。
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "模版错引用", extraBad, []global.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("template mismatch %v", err)
	}
	// 准备正文样本，后面入库和比对都用它。
	extraOK := []byte(`[{"templateId":"` + seedSingleID + `","extraProcesses":[{"processId":"` + proc.ID.String() + `"}]}]`)
	// 新建平台工程「模版对齐」失败就停，建不成后面没有工程可引用。
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "模版对齐", extraOK, nil); err != nil {
		// 不符即停：新建平台工程「模版对齐」失败。
		t.Fatal(err)
	}
	// 列工程模版，模版列表读不到就对不了种子。
	listed, err := h.WAN.ListProjectTemplates(ctx, tok)
	// 列工程模版失败就停，模版列表读不到就对不了种子。
	if err != nil {
		// 不符即停：列工程模版失败。
		t.Fatal(err)
	}
	// 先当没找到，循环里命中再改成真。
	found := false
	// 逐条查看结果，漏看一条会把结论判错。
	for _, row := range listed {
		// 正文出现这段才进这支，用来分种类核对。
		if bytes.Contains(row.Schema, []byte(`"processId"`)) && !bytes.Contains(row.Schema, []byte("processPath")) {
			// 标成已命中，最后仍为假说明名单里没有。
			found = true
			break
		}
	}
	// 查找结果没有出现就停，名单或种子丢了。
	if !found {
		// 不符即停：查找结果没有出现。
		t.Fatalf("seed schema missing processId")
	}
	// 列工程模版应被拒为未登录，放行或错类都算没拦住。
	if _, err := h.WAN.ListProjectTemplates(ctx, "bad"); !errors.Is(err, domain.ErrUnauthorized) && !errors.Is(err, domain.ErrInvalidCredentials) {
		// 不符即停：列工程模版应被拒为未登录。
		t.Fatalf("anon list %v", err)
	}

	// 把身份收成文本，后面比对焊道引用靠它。
	pack, err := h.WAN.CreatePlatformProject(ctx, tok, "组包", []byte(`[{"templateId":"`+seedSingleID+`","processId":"`+proc.ID.String()+`"}]`), nil)
	// 新建平台工程「组包」失败就停，建不成后面没有工程可引用。
	if err != nil {
		// 不符即停：新建平台工程「组包」失败。
		t.Fatal(err)
	}
	// 发布资产，发不出去后面不能当可用。
	pack, err = h.WAN.PublishPlatformAsset(ctx, tok, pack.ID, pack.Revision)
	// 发布资产失败就停，发不出去后面不能当可用。
	if err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 授权给厂失败就停，授权失败厂侧用不了。
	if err := h.WAN.GrantFactoryAsset(ctx, tok, pack.ID, fac.Factory.ID); err != nil {
		// 不符即停：授权给厂失败。
		t.Fatal(err)
	}
	// 下发到厂，下发失败厂侧没有这份。
	snap, err := h.WAN.DistributeToFactory(ctx, tok, pack.ID, fac.Factory.ID)
	// 下发到厂失败或条数不是2就停，不能当通过。
	if err != nil || len(snap.Members) != 2 {
		// 不符即停：下发到厂失败或条数不是2。
		t.Fatalf("members %+v %v", snap, err)
	}
}

// 删掉 T 排后不会自动补回，已有三份保持原样。
func TestExistingProjectTemplatesKeepThree(t *testing.T) {
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
	// 列工程模版，模版列表读不到就对不了种子。
	list, err := h.WAN.ListProjectTemplates(ctx, tok)
	// 列工程模版失败或条数不是3就停，不能当通过。
	if err != nil || len(list) != 3 {
		// 不符即停：列工程模版失败或条数不是3。
		t.Fatalf("seed %d %v", len(list), err)
	}
	// 先留出位置，循环里找到再填，找不到就失败。
	var tbarID = list[0].ID
	// 先当没找到，循环里命中再改成真。
	found := false
	// 逐条查看结果，漏看一条会把结论判错。
	for _, row := range list {
		// 条件成立才做这一支，漏进来会算错样本。
		if row.ID.String() == contenttpl.SeedTplTBar {
			// 先占一个候选，循环里再改成真正要的那条。
			tbarID = row.ID
			// 标成已命中，最后仍为假说明名单里没有。
			found = true
		}
	}
	// 查找结果没有出现就停，名单或种子丢了。
	if !found {
		// 不符即停：查找结果没有出现。
		t.Fatal("empty db missing tbar")
	}
	// 删工程模版失败就停，删不掉就验不了会不会补回。
	if err := h.WAN.DeleteProjectTemplate(ctx, tok, tbarID); err != nil {
		// 不符即停：删工程模版失败。
		t.Fatal(err)
	}
	// 列工程模版，模版列表读不到就对不了种子。
	again, err := h.WAN.ListProjectTemplates(ctx, tok)
	// 列工程模版失败或条数不是2就停，不能当通过。
	if err != nil || len(again) != 2 {
		// 不符即停：列工程模版失败或条数不是2。
		t.Fatalf("after delete %d %v", len(again), err)
	}
	// 逐项检查，漏一项这一步就不能算通过。
	for _, row := range again {
		// 删掉的T排又出现就停，说明被自动补回了。
		if row.ID.String() == contenttpl.SeedTplTBar {
			// 不符即停：删掉的T排又出现。
			t.Fatal("auto reinserted tbar")
		}
	}
	// 解析模版身份，解析失败就对不上种子。
	seedID, err := uuid.Parse(contenttpl.SeedTplTBar)
	// 解析身份失败就停，解析失败就对不上种子。
	if err != nil {
		// 不符即停：解析身份失败。
		t.Fatal(err)
	}
	// 建工程模版「T排对接」，工程模版建不成列表对不齐。
	got, err := h.WAN.CreateProjectTemplate(ctx, tok, seedID, "T排对接", nil)
	// 建工程模版「T排对接」失败或身份变了就停，不能当通过。
	if err != nil || got.ID != seedID {
		// 不符即停：建工程模版「T排对接」失败或身份变了。
		t.Fatalf("rebuild %+v %v", got, err)
	}
	// 这里正文缺了应有内容就停，这一步不能算通过。
	if !bytes.Contains(got.Schema, []byte("gapBands")) {
		// 不符即停：这里正文缺了应有内容。
		t.Fatalf("seed fields %s", got.Schema)
	}
	// 建工程模版「T排对接」应被拒为模版不合法，放行或错类都算没拦住。
	if _, err := h.WAN.CreateProjectTemplate(ctx, tok, seedID, "T排对接", nil); !errors.Is(err, domain.ErrTemplateInvalid) {
		// 不符即停：建工程模版「T排对接」应被拒为模版不合法。
		t.Fatalf("dup seed %v", err)
	}
}
