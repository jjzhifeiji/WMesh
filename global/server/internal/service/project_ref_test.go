// 工程焊道引用工艺身份；F2 空库三份、已有份不自动插 T 排。
package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
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

// 挂不带工艺槽的自定义模版的多层工程，升档后正文里不留厂级工艺身份。
func TestPromoteCustomTemplateRefs(t *testing.T) {
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
	// 建一份空字段的自定义工程模版，不声明任何工艺槽。
	custom, err := h.WAN.CreateProjectTemplate(ctx, tok, uuid.Nil, "自定义", nil)
	// 建自定义模版失败就停，没有它就复现不了。
	if err != nil {
		// 不符即停：建自定义模版失败。
		t.Fatal(err)
	}
	// 登记工厂「厂A」，建不成后面没有厂可升档。
	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可升档。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 新造厂级工艺身份，升档后正文里不该再出现它。
	srcProc := id.New()
	// 准备工艺正文样本，摘要按它算。
	pbody := []byte(`{"name":"厂工艺","current":180}`)
	// 拼多层工艺的升档快照，作业类型要和工程一致。
	psnap := global.AssetSnapshot{
		SourceID: srcProc, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
		Kind: global.KindProcess, Name: "多层工艺", Content: pbody, Digest: digest.Sum(pbody),
		Copyable: true, Status: global.AssetAvailable, WeldKind: contenttpl.WeldMultilayer,
	}
	// 升档工艺，升不上去工程依赖就对不上。
	plat, err := h.WAN.PromoteFromSnapshot(ctx, tok, psnap)
	// 升档工艺失败就停，升不上去工程依赖就对不上。
	if err != nil {
		// 不符即停：升档工艺失败。
		t.Fatal(err)
	}
	// 发布资产，发不出去后面不能当可用。
	plat, err = h.WAN.PublishPlatformAsset(ctx, tok, plat.ID, plat.Revision)
	// 发布资产失败就停，发不出去后面不能当可用。
	if err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 按工艺身份拼一条挂自定义模版的多层焊道正文。
	body := func(pid string) []byte {
		// 工艺填在基准层和填充层，模版里都没有声明。
		return []byte(`[{"templateId":"` + custom.ID.String() + `","kind":"multi","name":"w","basePath":{"processId":"` + srcProc.String() + `"},"passes":[{"processId":"` + pid + `"}]}]`)
	}
	// 依赖只带这一份厂级工艺。
	deps := []global.AssetDep{{ID: srcProc, Revision: 1, Digest: digest.Sum(pbody)}}
	// 两处都引用依赖里的工艺，正文样本。
	okBody := body(srcProc.String())
	// 拼工程升档快照，作业类型跟源走多层。
	jsnap := global.AssetSnapshot{
		SourceID: id.New(), SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
		Kind: global.KindProject, Name: "自定义多层", Content: okBody, Digest: digest.Sum(okBody),
		Copyable: true, Status: global.AssetAvailable, WeldKind: contenttpl.WeldMultilayer, Deps: deps,
	}
	// 升档工程，升不上去说明标准槽没改写或依赖错配。
	got, err := h.WAN.PromoteFromSnapshot(ctx, tok, jsnap)
	// 升档工程失败就停，不能当通过。
	if err != nil {
		// 不符即停：升档工程失败。
		t.Fatal(err)
	}
	// 读平台正文，读不到就无法比对。
	rewritten, err := h.WAN.ReadPlatformAssetContent(ctx, tok, got.ID)
	// 读平台正文失败就停，读不到就无法比对。
	if err != nil {
		// 不符即停：读平台正文失败。
		t.Fatal(err)
	}
	// 正文里还留着厂级工艺身份就停，说明有槽没改写。
	if bytes.Contains(rewritten, []byte(srcProc.String())) {
		// 不符即停：升档后残留厂级身份。
		t.Fatalf("factory id left %s", rewritten)
	}
	// 两处引用都该换成平台身份，少一处就停。
	if bytes.Count(rewritten, []byte(plat.ID.String())) != 2 {
		// 不符即停：平台身份没有换满两处。
		t.Fatalf("platform id %s", rewritten)
	}
	// 填充层引用一份不在依赖里的工艺，正文样本。
	leak := body(id.New().String())
	// 拼漏依赖的工程升档快照。
	lsnap := global.AssetSnapshot{
		SourceID: id.New(), SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
		Kind: global.KindProject, Name: "漏依赖", Content: leak, Digest: digest.Sum(leak),
		Copyable: true, Status: global.AssetAvailable, WeldKind: contenttpl.WeldMultilayer, Deps: deps,
	}
	// 升档应被拒为依赖不符，放行说明厂级身份会残留。
	if _, err := h.WAN.PromoteFromSnapshot(ctx, tok, lsnap); !errors.Is(err, domain.ErrAssetDependency) {
		// 不符即停：漏依赖的工程升档没被拒。
		t.Fatalf("leak promote %v", err)
	}
}

// 厂级工程钉已下发的平台工艺也能升档：平台身份原样保留并重钉当前修订，草稿和停用拒绝。
func TestPromotePinnedPlatformProcess(t *testing.T) {
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
	// 登记工厂「厂A」，建不成后面没有厂可升档。
	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可升档。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 新建平台工艺，厂级工程直接钉它的平台身份。
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "平台工艺", []byte(`{"name":"p","current":180}`))
	// 新建平台工艺失败就停，没有它就复现不了。
	if err != nil {
		// 不符即停：新建平台工艺失败。
		t.Fatal(err)
	}
	// 发布平台工艺，下发到厂的就是这一版。
	proc, err = h.WAN.PublishPlatformAsset(ctx, tok, proc.ID, proc.Revision)
	// 发布平台工艺失败就停，发不出去厂里拿不到。
	if err != nil {
		// 不符即停：发布平台工艺失败。
		t.Fatal(err)
	}
	// 厂里钉的是下发时的修订和摘要。
	pinned := global.AssetDep{ID: proc.ID, Revision: proc.Revision, Digest: proc.Digest}
	// 云端随后改了正文，修订往前走一版。
	cur, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, proc.ID, proc.Revision, []byte(`{"name":"p","current":200}`))
	// 改平台工艺正文失败就停，验不了重钉当前修订。
	if err != nil {
		// 不符即停：改平台工艺正文失败。
		t.Fatal(err)
	}
	// 按工艺身份拼一条单层焊道正文。
	weld := func(pid uuid.UUID) []byte {
		// 焊道直接引用给定的工艺身份。
		return []byte(`[{"templateId":"` + seedSingleID + `","name":"w","processId":"` + pid.String() + `"}]`)
	}
	// 按正文和依赖拼工程升档快照。
	projSnap := func(name string, body []byte, deps ...global.AssetDep) global.AssetSnapshot {
		// 源身份每次新造，避免撞上已升过的工程。
		return global.AssetSnapshot{
			SourceID: id.New(), SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProject, Name: name, Content: body, Digest: digest.Sum(body),
			Copyable: true, Status: global.AssetAvailable, Deps: deps,
		}
	}
	// 只钉平台工艺的工程正文。
	body := weld(proc.ID)
	// 升档工程，修复前这里报依赖错配。
	got, err := h.WAN.PromoteFromSnapshot(ctx, tok, projSnap("钉平台", body, pinned))
	// 升档失败就停，说明平台身份仍按源身份去找。
	if err != nil {
		// 不符即停：钉平台工艺的工程升档失败。
		t.Fatal(err)
	}
	// 依赖须指向原平台身份和当前修订、摘要。
	if len(got.Deps) != 1 || got.Deps[0].ID != proc.ID || got.Deps[0].Revision != cur.Revision || !bytes.Equal(got.Deps[0].Digest, cur.Digest) {
		// 不符即停：依赖没有重钉到当前修订。
		t.Fatalf("deps %+v want %s rev=%d", got.Deps, proc.ID, cur.Revision)
	}
	// 读平台正文，读不到就无法比对。
	rewritten, err := h.WAN.ReadPlatformAssetContent(ctx, tok, got.ID)
	// 读平台正文失败或正文变了就停，平台引用不该被改写。
	if err != nil || !bytes.Equal(rewritten, body) {
		// 不符即停：正文里的平台引用被改动。
		t.Fatalf("body %s %v", rewritten, err)
	}

	// 新造厂级工艺身份，用来验两种依赖混钉。
	srcProc := id.New()
	// 准备厂级工艺正文样本，摘要按它算。
	pbody := []byte(`{"name":"厂工艺","current":160}`)
	// 先把厂级工艺升成平台级，工程依赖才找得到。
	plat, err := h.WAN.PromoteFromSnapshot(ctx, tok, global.AssetSnapshot{
		SourceID: srcProc, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
		Kind: global.KindProcess, Name: "厂工艺", Content: pbody, Digest: digest.Sum(pbody),
		Copyable: true, Status: global.AssetAvailable,
	})
	// 升档厂级工艺失败就停，混钉验不了。
	if err != nil {
		// 不符即停：升档厂级工艺失败。
		t.Fatal(err)
	}
	// 发布升档出来的平台工艺，草稿不能当依赖。
	plat, err = h.WAN.PublishPlatformAsset(ctx, tok, plat.ID, plat.Revision)
	// 发布失败就停，混钉验不了。
	if err != nil {
		// 不符即停：发布升档工艺失败。
		t.Fatal(err)
	}
	// 两条焊道，一条钉平台工艺，一条钉厂级工艺。
	mixed := []byte(`[{"templateId":"` + seedSingleID + `","name":"a","processId":"` + proc.ID.String() + `"},{"templateId":"` + seedSingleID + `","name":"b","processId":"` + srcProc.String() + `"}]`)
	// 厂级工艺依赖钉的是厂里的源身份。
	factoryDep := global.AssetDep{ID: srcProc, Revision: 1, Digest: digest.Sum(pbody)}
	// 升档混钉工程，两条路都要走通。
	both, err := h.WAN.PromoteFromSnapshot(ctx, tok, projSnap("混钉", mixed, pinned, factoryDep))
	// 升档失败就停，说明有一条路没走通。
	if err != nil {
		// 不符即停：混钉工程升档失败。
		t.Fatal(err)
	}
	// 平台工艺原身份保留，厂级工艺换成升档后的平台身份。
	if len(both.Deps) != 2 || both.Deps[0].ID != proc.ID || both.Deps[1].ID != plat.ID {
		// 不符即停：混钉依赖改写不对。
		t.Fatalf("mixed deps %+v", both.Deps)
	}
	// 读混钉工程正文，读不到就无法比对。
	mixedBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, both.ID)
	// 读平台正文失败就停，读不到就无法比对。
	if err != nil {
		// 不符即停：读混钉工程正文失败。
		t.Fatal(err)
	}
	// 从焊道收集工艺身份，看引用改得对不对。
	refs, err := contenttpl.CollectProcessIDsFromItems(contenttpl.SeedProjectItems(), mixedBody)
	// 收集失败或引用不是平台身份就停。
	if err != nil || len(refs) != 2 || refs[0] != proc.ID.String() || refs[1] != plat.ID.String() {
		// 不符即停：混钉正文引用改写不对。
		t.Fatalf("mixed refs %v %s", refs, mixedBody)
	}

	// 新建一份只留草稿的平台工艺。
	draft, err := h.WAN.CreatePlatformProcess(ctx, tok, "草稿工艺", []byte(`{"name":"d","current":170}`))
	// 新建草稿工艺失败就停，验不了草稿拒绝。
	if err != nil {
		// 不符即停：新建草稿工艺失败。
		t.Fatal(err)
	}
	// 钉草稿工艺的工程升档应报依赖错配。
	if _, err := h.WAN.PromoteFromSnapshot(ctx, tok, projSnap("钉草稿", weld(draft.ID), global.AssetDep{ID: draft.ID, Revision: draft.Revision, Digest: draft.Digest})); !errors.Is(err, domain.ErrAssetDependency) {
		// 不符即停：钉草稿工艺的工程没被拒。
		t.Fatalf("draft promote %v", err)
	}
	// 新建一份之后会停用的平台工艺。
	off, err := h.WAN.CreatePlatformProcess(ctx, tok, "停用工艺", []byte(`{"name":"o","current":190}`))
	// 新建失败就停，验不了停用拒绝。
	if err != nil {
		// 不符即停：新建待停用工艺失败。
		t.Fatal(err)
	}
	// 先发布，只有可用才能停用。
	off, err = h.WAN.PublishPlatformAsset(ctx, tok, off.ID, off.Revision)
	// 发布失败就停，停用不了。
	if err != nil {
		// 不符即停：发布待停用工艺失败。
		t.Fatal(err)
	}
	// 厂里钉的是停用前的可用版。
	offDep := global.AssetDep{ID: off.ID, Revision: off.Revision, Digest: off.Digest}
	// 停用平台工艺，之后不能再当依赖。
	if _, err := h.WAN.DisablePlatformAsset(ctx, tok, off.ID, off.Revision); err != nil {
		// 不符即停：停用平台工艺失败。
		t.Fatal(err)
	}
	// 钉已停用工艺的工程升档应报依赖错配。
	if _, err := h.WAN.PromoteFromSnapshot(ctx, tok, projSnap("钉停用", weld(off.ID), offDep)); !errors.Is(err, domain.ErrAssetDependency) {
		// 不符即停：钉停用工艺的工程没被拒。
		t.Fatalf("disabled promote %v", err)
	}
}

// 平台级目标已停用时再升档必须被拒，不能被源悄悄改回草稿。
func TestPromoteDisabledTargetRejected(t *testing.T) {
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
	// 登记工厂「厂A」，建不成后面没有厂可升档。
	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可升档。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 厂级源身份固定，两次升档都指向同一条平台级。
	srcID := id.New()
	// 按正文和修订拼厂级工艺快照。
	procSnap := func(rev int64, body []byte) global.AssetSnapshot {
		// 源身份不变，只换修订和正文。
		return global.AssetSnapshot{
			SourceID: srcID, SourceRevision: rev, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProcess, Name: "厂工艺", Content: body, Digest: digest.Sum(body),
			Copyable: true, Status: global.AssetAvailable,
		}
	}
	// 第一版正文，第一次升档用它。
	first := []byte(`{"name":"厂工艺","current":150}`)
	// 第一次升档落成平台级草稿。
	plat, err := h.WAN.PromoteFromSnapshot(ctx, tok, procSnap(1, first))
	// 第一次升档失败就停，没有目标验不了停用。
	if err != nil {
		// 不符即停：第一次升档失败。
		t.Fatal(err)
	}
	// 先发布，只有可用才能停用。
	plat, err = h.WAN.PublishPlatformAsset(ctx, tok, plat.ID, plat.Revision)
	// 发布失败就停，停用不了。
	if err != nil {
		// 不符即停：发布平台工艺失败。
		t.Fatal(err)
	}
	// 管理员停用这条平台级。
	off, err := h.WAN.DisablePlatformAsset(ctx, tok, plat.ID, plat.Revision)
	// 停用失败就停，验不了停用目标。
	if err != nil {
		// 不符即停：停用平台工艺失败。
		t.Fatal(err)
	}
	// 源改了正文后再升，修复前会被覆盖成草稿。
	if _, err := h.WAN.PromoteFromSnapshot(ctx, tok, procSnap(2, []byte(`{"name":"厂工艺","current":160}`))); !errors.Is(err, domain.ErrAssetNotAvailable) {
		// 不符即停：停用目标被改过正文的快照覆盖。
		t.Fatalf("changed promote onto disabled: %v", err)
	}
	// 正文没变再升也要拒，不能当幂等放行。
	if _, err := h.WAN.PromoteFromSnapshot(ctx, tok, procSnap(1, first)); !errors.Is(err, domain.ErrAssetNotAvailable) {
		// 不符即停：停用目标被同摘要快照放行。
		t.Fatalf("same promote onto disabled: %v", err)
	}
	// 读回平台级，核对修订、摘要和状态都没动。
	got, err := h.WAN.GetPlatformAsset(ctx, tok, plat.ID)
	// 读取失败或任一项变了就停。
	if err != nil || got.Status != global.AssetDisabled || got.Revision != off.Revision || !bytes.Equal(got.Digest, off.Digest) {
		// 不符即停：停用目标被升档改动。
		t.Fatalf("disabled target changed %+v %v", got, err)
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

// 起云端服务并登录超管，交回夹具和会话票。
func projectTplAdmin(t *testing.T, ctx context.Context) (*Harness, string) {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
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
	return h, tok
}

// 改掉单层种子的字段后再列表，字段和修订都不被代码改回。
func TestSeedTemplateEditKept(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起服务并登录，拿到夹具和会话票。
	h, tok := projectTplAdmin(t, ctx)
	// 列工程模版，空库首次应落下三份种子。
	list, err := h.WAN.ListProjectTemplates(ctx, tok)
	// 列工程模版失败或条数不是3就停，不能当通过。
	if err != nil || len(list) != 3 {
		// 不符即停：列工程模版失败或条数不是3。
		t.Fatalf("seed %d %v", len(list), err)
	}
	// 先当没找到，循环里命中再填上单层那份。
	var single global.ContentTemplate
	// 逐条查看结果，找出单层种子。
	for _, row := range list {
		// 身份对上单层种子才取这一份。
		if row.ID.String() == seedSingleID {
			// 记下单层种子，后面拿它来改。
			single = row
		}
	}
	// 单层种子没出现就停，空库种子不全。
	if single.ID == uuid.Nil {
		// 不符即停：单层种子没出现。
		t.Fatal("empty db missing single")
	}
	// 把单层字段改成空对象，和代码种子明显不同。
	edited, err := h.WAN.UpdateProjectTemplate(ctx, tok, single.ID, single.Revision, single.Name, []byte(`{"root":"object"}`))
	// 改工程模版失败或修订没有加一就停，不能当通过。
	if err != nil || edited.Revision != single.Revision+1 || bytes.Equal(edited.Schema, single.Schema) {
		// 不符即停：改工程模版失败或修订不对。
		t.Fatalf("edit %+v %v", edited, err)
	}
	// 连列两次，确认读路径不会把字段改回去。
	for i := 0; i < 2; i++ {
		// 再列工程模版，读路径不该写库。
		again, err := h.WAN.ListProjectTemplates(ctx, tok)
		// 列工程模版失败或条数不是3就停，不能当通过。
		if err != nil || len(again) != 3 {
			// 不符即停：再列工程模版失败或条数不对。
			t.Fatalf("relist %d %v", len(again), err)
		}
		// 逐条找回单层那份核对字段和修订。
		for _, row := range again {
			// 不是单层这份就跳过，别的种子不受影响。
			if row.ID != single.ID {
				continue
			}
			// 修订或字段被改回就停，说明读路径还在覆盖种子。
			if row.Revision != edited.Revision || !bytes.Equal(row.Schema, edited.Schema) {
				// 不符即停：管理员的修改被读路径撤销。
				t.Fatalf("seed reverted rev=%d schema=%s", row.Revision, row.Schema)
			}
		}
	}
}

// 三份种子删光后列表为空，下发只剩工艺，带种子身份仍能手动补建。
func TestSeedTemplatesStayDeleted(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起服务并登录，拿到夹具和会话票。
	h, tok := projectTplAdmin(t, ctx)
	// 列工程模版，空库首次应落下三份种子。
	list, err := h.WAN.ListProjectTemplates(ctx, tok)
	// 列工程模版失败或条数不是3就停，不能当通过。
	if err != nil || len(list) != 3 {
		// 不符即停：列工程模版失败或条数不是3。
		t.Fatalf("seed %d %v", len(list), err)
	}
	// 逐份删掉三份种子，删到零份。
	for _, row := range list {
		// 删工程模版失败就停，删不掉就验不了会不会补回。
		if err := h.WAN.DeleteProjectTemplate(ctx, tok, row.ID); err != nil {
			// 不符即停：删工程模版失败。
			t.Fatal(err)
		}
	}
	// 删光后再列，应仍是空表。
	empty, err := h.WAN.ListProjectTemplates(ctx, tok)
	// 列失败或又冒出模版就停，说明种子被补回了。
	if err != nil || len(empty) != 0 {
		// 不符即停：删光后种子被自动补回。
		t.Fatalf("after delete all %d %v", len(empty), err)
	}
	// 组给厂的模版快照，删光后只该剩工艺一份。
	snaps, err := h.WAN.SnapshotsForFactory(ctx, id.New())
	// 组快照失败或不是只剩工艺就停，不能当通过。
	if err != nil || len(snaps) != 1 || snaps[0].Kind != global.KindProcess {
		// 不符即停：下发快照里又带上了工程种子。
		t.Fatalf("snapshots %+v %v", snaps, err)
	}
	// 解析单层种子身份，手动补建要用它。
	seedID, err := uuid.Parse(seedSingleID)
	// 解析身份失败就停，解析失败就对不上种子。
	if err != nil {
		// 不符即停：解析身份失败。
		t.Fatal(err)
	}
	// 带种子身份手动补建单层，空名空字段按种子填。
	rebuilt, err := h.WAN.CreateProjectTemplate(ctx, tok, seedID, "", nil)
	// 补建失败或身份变了就停，不能当通过。
	if err != nil || rebuilt.ID != seedID || rebuilt.Revision != 1 {
		// 不符即停：带种子身份补建失败。
		t.Fatalf("rebuild %+v %v", rebuilt, err)
	}
	// 补建后再列，只该有手动补的这一份。
	after, err := h.WAN.ListProjectTemplates(ctx, tok)
	// 列失败或条数不是1就停，说明另外两份被顺带补回。
	if err != nil || len(after) != 1 || after[0].ID != seedID {
		// 不符即停：补建一份却冒出了别的种子。
		t.Fatalf("after rebuild %+v %v", after, err)
	}
}

// 只删单层种子或三份删光后，网页按种子身份新建的焊道仍落进正文。
func TestCreateProjectAfterSeedsDeleted(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起服务并登录，拿到夹具和会话票。
	h, tok := projectTplAdmin(t, ctx)
	// 列工程模版，空库首次应落下三份种子。
	list, err := h.WAN.ListProjectTemplates(ctx, tok)
	// 列工程模版失败或条数不是3就停，不能当通过。
	if err != nil || len(list) != 3 {
		// 不符即停：列工程模版失败或条数不是3。
		t.Fatalf("seed %d %v", len(list), err)
	}
	// 网页新建单层工程时 emptyWeld 落下的那条焊道，固定挂单层种子。
	weld := []byte(`[{"id":"w1","templateId":"` + seedSingleID + `","kind":"single","name":"焊道 1","points":[],"processId":"","isEnabled":true}]`)
	// 新建一份单层工程并读回正文；raw 为真时正文应原样留下，否则按种子字段补齐。
	check := func(name string, raw bool) {
		// 失败栈指到用例，避免停在闭包里面。
		t.Helper()
		// 按单层新建平台工程，正文就是那一条焊道。
		proj, err := h.WAN.CreatePlatformProject(ctx, tok, name, weld, nil)
		// 新建失败就停，删了种子也该能建。
		if err != nil {
			// 不符即停：新建平台工程失败。
			t.Fatalf("%s create %v", name, err)
		}
		// 读回平台正文，核对焊道还在。
		body, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proj.ID)
		// 读正文失败就停，读不到就无法比对。
		if err != nil {
			// 不符即停：读平台正文失败。
			t.Fatalf("%s read %v", name, err)
		}
		// 准备接读回的各条焊道。
		var items []map[string]any
		// 解不开、条数不是一或身份变了就停，说明焊道被吞。
		if err := json.Unmarshal(body, &items); err != nil || len(items) != 1 || items[0]["templateId"] != seedSingleID || items[0]["name"] != "焊道 1" {
			// 不符即停：新建正文丢了焊道。
			t.Fatalf("%s body %s %v", name, body, err)
		}
		// 零份时应原样留下，不该再按模版改写。
		if raw && !bytes.Equal(body, weld) {
			// 不符即停：零份时正文被改写。
			t.Fatalf("%s not raw %s", name, body)
		}
		// 缺份时应按种子内置字段补上缺的选中点。
		if _, ok := items[0]["selectedPointIndex"]; !raw && !ok {
			// 不符即停：没按种子字段套正文。
			t.Fatalf("%s seed fields %s", name, body)
		}
	}
	// 先只删单层种子，多层和 T 排还在。
	if err := h.WAN.DeleteProjectTemplate(ctx, tok, uuid.MustParse(seedSingleID)); err != nil {
		// 不符即停：删单层种子失败。
		t.Fatal(err)
	}
	// 缺了单层那份，按内置字段套也要留下焊道。
	check("缺单层", false)
	// 再删掉其余两份，删到零份。
	for _, row := range list {
		// 单层那份已经删过，跳过。
		if row.ID.String() == seedSingleID {
			continue
		}
		// 删工程模版失败就停，删不掉就验不了零份。
		if err := h.WAN.DeleteProjectTemplate(ctx, tok, row.ID); err != nil {
			// 不符即停：删工程模版失败。
			t.Fatal(err)
		}
	}
	// 零份时正文原样留下，不被套成空数组。
	check("零份", true)
}

// 空库并发列表：库里只落三份，每个请求都拿到三份且不报错。
func TestSeedTemplatesConcurrentList(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起服务并登录，拿到夹具和会话票。
	h, tok := projectTplAdmin(t, ctx)
	// 同时发起的列表请求数，够让抢标记撞上。
	const workers = 8
	// 等全部请求回来再核对。
	var wg sync.WaitGroup
	// 每个请求拿到的份数，按序号各占一格。
	counts := make([]int, workers)
	// 每个请求的错误，按序号各占一格。
	errs := make([]error, workers)
	// 先拦住全部请求，放闸后一起冲。
	start := make(chan struct{})
	// 逐个起列表请求，尽量同时打到空库。
	for i := 0; i < workers; i++ {
		// 登记一个待回来的请求。
		wg.Add(1)
		// 起一个并发列表请求，结果落到自己的格子。
		go func(i int) {
			// 请求回来就销账，主线才能往下核。
			defer wg.Done()
			// 等放闸再列，让请求尽量撞在一起。
			<-start
			// 列工程模版，结果记进自己那一格。
			rows, err := h.WAN.ListProjectTemplates(ctx, tok)
			// 记下份数，后面逐个核对是不是三份。
			counts[i] = len(rows)
			// 记下错误，后面不能有修订冲突或撞键。
			errs[i] = err
		}(i)
	}
	// 放闸，全部请求一起去列。
	close(start)
	// 等全部请求回来。
	wg.Wait()
	// 逐个核对每个请求的结果。
	for i := 0; i < workers; i++ {
		// 报错或不是三份就停，并发种子没收住。
		if errs[i] != nil || counts[i] != 3 {
			// 不符即停：并发列表报错或份数不对。
			t.Fatalf("worker %d got %d %v", i, counts[i], errs[i])
		}
	}
	// 直接读库里的工程模版行，核对没有多插。
	rows, err := h.WAN.Store().TemplatesByKind(ctx, global.KindProject)
	// 读库失败或不是三份就停，说明种子落了多遍。
	if err != nil || len(rows) != 3 {
		// 不符即停：库里工程模版份数不对。
		t.Fatalf("stored %d %v", len(rows), err)
	}
	// 逐份核对修订，并发读不该把种子升修订。
	for _, row := range rows {
		// 修订不是一就停，说明读路径还在改写种子。
		if row.Revision != 1 {
			// 不符即停：种子修订被读路径抬高。
			t.Fatalf("seed revision %s %d", row.Name, row.Revision)
		}
	}
}
