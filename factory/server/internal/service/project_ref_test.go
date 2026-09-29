// 工程焊道引用工艺身份：对齐 deps，不套旧正文。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// 单层模版的固定编号，用来核对焊道引用。
const seedSingleID = "11111111-1111-4111-8111-111111111111"

// 工程焊道只引用工艺身份，不能把旧正文套进去。
func TestProjectProcessRefs(t *testing.T) {
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
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}

	// 新建一份厂级工艺，失败说明起草入口坏了。
	proc, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "工艺", []byte(`{"name":"p","current":180}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	proc, err = fac.PublishAsset(ctx, pe.tok, proc.ID, proc.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 准备这段正文字节，读回对不上说明没写进去。
	stale := []byte(`{"root":"array","templates":[{"id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","kind":"corner","name":"包角","extra":true}]}`)
	// 写模版副本失败就停，否则后面没有可靠结果。
	if _, err := fac.Store().InsertTemplateReplica(ctx, factory.ContentTemplate{
		// 新取一个编号，撞号会让两条记录分不清。
		ID: id.New(), Kind: factory.KindProject, Revision: 1, Schema: stale, Digest: digest.Sum(stale),
	}); err != nil {
		t.Fatal(err)
	}
	// 装上这条依赖，修订或摘要错了工程会对不上。
	dep := factory.AssetDep{ID: proc.ID, Revision: proc.Revision, Digest: proc.Digest}

	// 新建一份厂级工程，失败说明起草入口坏了。
	empty, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "空引用", []byte(`[]`), []factory.AssetDep{dep})
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	emptyBody, err := fac.ReadAssetContent(ctx, pe.tok, empty.ID)
	// 读正文失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把读正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 从焊道里收集工艺编号，漏了说明引用没解析。
	ids, err := contenttpl.CollectProcessIDsFromItems(contenttpl.SeedProjectItems(), emptyBody)
	// 收集工艺号失败或条数不对就停，说明没达预期。
	if err != nil || len(ids) != 0 {
		// 结果是空的，说明该写入的内容没有落下。
		t.Fatalf("empty refs %v %v", ids, err)
	}
	// 结果应和这一步的预期一致，偏离说明行为写偏了。
	if string(emptyBody) != "[]" {
		// 结果是空的，说明该写入的内容没有落下。
		t.Fatalf("empty body %s", emptyBody)
	}
	// 建工程应因依赖不齐被拒绝，放行说明没拦住。
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "错引用", []byte(`[{"templateId":"`+seedSingleID+`","processId":"`+id.New().String()+`"}]`), []factory.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("create mismatch %v", err)
	}
	// 把编号打成文本，方便和路径或正文比对。
	pathBody := []byte(`[{"templateId":"` + seedSingleID + `","processId":"` + proc.ID.String() + `","processPath":"x.json"}]`)
	// 建工程应因越权被拒绝，放行说明没拦住。
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "路径键", pathBody, nil); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("create processPath %v", err)
	}

	// 把编号打成文本，方便和路径或正文比对。
	okBody := []byte(`[{"name":"w","processId":"` + proc.ID.String() + `"}]`)
	// 新建一份厂级工程，失败说明起草入口坏了。
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "对齐", okBody, nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 条数或长度应符合预期，为空或超长说明不全。
	if len(proj.Deps) != 1 || proj.Deps[0].ID != proc.ID {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("pinned %+v", proj.Deps)
	}
	// 改正文应因依赖不齐被拒绝，放行说明没拦住。
	if _, err := fac.UpdateAssetContent(ctx, pe.tok, proj.ID, proj.Revision, []byte(`[{"processId":"`+id.New().String()+`"}]`)); !errors.Is(err, domain.ErrAssetDependency) {
		// 两边结果对不上，说明匹配或引用写偏了。
		t.Fatalf("update mismatch %v", err)
	}
	// 改正文应因越权被拒绝，放行说明没拦住。
	if _, err := fac.UpdateAssetContent(ctx, pe.tok, proj.ID, proj.Revision, []byte(`[{"processId":"`+proc.ID.String()+`","processPath":"x.json"}]`)); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("update processPath %v", err)
	}
	// 读取资产台账，失败说明无权看或已经删除。
	proj, err = fac.GetAsset(ctx, pe.tok, proj.ID)
	// 读台账失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把读台账的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 改依赖应因依赖不齐被拒绝，放行说明没拦住。
	if _, err := fac.SetProjectDeps(ctx, pe.tok, proj.ID, proj.Revision, nil); !errors.Is(err, domain.ErrAssetDependency) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("drop dep %v", err)
	}
	// 留着旧依赖，用来验证改引用后不再认它。
	oldDep := factory.AssetDep{ID: proc.ID, Revision: proc.Revision, Digest: proc.Digest}
	// 改写资产正文，失败说明修订冲突或越权。
	bumped, err := fac.UpdateAssetContent(ctx, pe.tok, proc.ID, proc.Revision, []byte(`{"name":"p2","current":190}`))
	// 改正文失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 修订号应按这次保存前进，没变说明写没落库。
	if bumped.Revision == oldDep.Revision {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatal("process revision")
	}
	// 改写工程依赖，失败说明工艺还不齐。
	followed, err := fac.SetProjectDeps(ctx, pe.tok, proj.ID, proj.Revision, []factory.AssetDep{oldDep})
	// 改依赖失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改依赖的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 修订号应按这次保存前进，没变说明写没落库。
	if len(followed.Deps) != 1 || followed.Deps[0].Revision != bumped.Revision {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("follow %+v want r%d", followed.Deps, bumped.Revision)
	}

	// 留出单层焊道，解析到后再核对模版编号。
	var single contenttpl.ProjectItemSchema
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, it := range contenttpl.SeedProjectItems() {
		// 编成文本失败就停，否则后面没有可靠结果。
		if it.ID == seedSingleID {
			// 记下这条焊道，模版或工艺引用不对会在这里露馅。
			single = it
			break
		}
	}
	// 编成文本再查敏感词，编失败就无法判断泄密。
	raw, err := contenttpl.Marshal(contenttpl.ObjectSchema(single.Fields))
	// 编成文本失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把编成文本的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 留下工厂编号，后面用来核对资产没有串厂。
	fid := seed.ID
	// 收模版失败就停，否则后面没有可靠结果。
	if err := fac.AcceptTemplateDelivery(ctx, factory.TemplateSnapshot{
		// 解析固定编号，失败说明样例编号写坏了。
		ID: uuid.MustParse(seedSingleID), Kind: factory.KindProject, Name: single.Name, Revision: 9, Schema: raw, Digest: digest.Sum(raw),
		TargetFactoryID: &fid,
	}); err != nil {
		t.Fatal(err)
	}
	// 建工程应因依赖不齐被拒绝，放行说明没拦住。
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "模版错引用", []byte(`[{"templateId":"`+seedSingleID+`","extraProcesses":[{"processId":"`+id.New().String()+`"}]}]`), []factory.AssetDep{dep}); !errors.Is(err, domain.ErrAssetDependency) {
		t.Fatalf("template mismatch %v", err)
	}
	// 建工程失败就停，否则后面没有可靠结果。
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "模版对齐", []byte(`[{"templateId":"`+seedSingleID+`","extraProcesses":[{"processId":"`+proc.ID.String()+`"}]}]`), nil); err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 新建一份个人工艺，失败说明个人入口被拒。
	own, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "个人工艺", []byte(`{"name":"mine","current":160}`))
	// 建个人工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建个人工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	own, err = fac.PublishAsset(ctx, pe.tok, own.ID, own.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份厂级工程，失败说明起草入口坏了。
	pinnedPersonal, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "钉个人", []byte(`[{"templateId":"`+seedSingleID+`","processId":"`+own.ID.String()+`"}]`), nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 条数或长度应符合预期，为空或超长说明不全。
	if len(pinnedPersonal.Deps) != 1 || pinnedPersonal.Deps[0].ID != own.ID {
		// 个人稿串到了别人名下，说明归属没保住。
		t.Fatalf("personal pin %+v", pinnedPersonal.Deps)
	}
}
