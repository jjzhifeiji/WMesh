// 工艺/工程作业类型：单层焊道、多层焊缝、T排对接；工程与工艺必须同类型。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 个人级改了作业类型再升厂级：每次复制一份新草稿，作业类型按源现在的，旧副本不动。
func TestFactoryPromoteWeldKind(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下初始超管，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa-a", "超管A")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := seedPass(ctx, fac, "sa-a", "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa-a", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	pe := mustCreateRole(t, ctx, fac, sa, "pe-a", "pe-pass", factory.RoleOrgAdmin)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{}
	// 准备单层正文，个人工程先按单层建。
	singleBody := []byte(`[{"templateId":"` + contenttpl.SeedTplSingle + `","name":"单"}]`)
	// 准备多层正文，改类型后按多层重写。
	multiBody := []byte(`[{"templateId":"` + contenttpl.SeedTplMulti + `","name":"多层"}]`)

	// 新建一份个人级单层工艺，先升一次厂级。
	proc, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "个人工艺", []byte(`{"name":"p"}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 第一次升厂级，复制出新身份的草稿。
	first, err := fac.PromoteToFactory(ctx, pe.tok, proc.ID, uuid.Nil)
	// 升档失败、不是新身份或作业类型不是单层就停。
	if err != nil || first.ID == proc.ID || first.Status != store.AssetDraft || first.WeldKind != store.WeldKindSingle {
		// 第一次升档就没带上作业类型。
		t.Fatalf("first promote %+v %v", first, err)
	}
	// 源只改作业类型，正文摘要保持不变。
	proc, err = fac.SetAssetWeldKind(ctx, pe.tok, proc.ID, proc.Revision, store.WeldKindMultilayer)
	// 改焊类失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改焊类的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 再升每次都复制一份新草稿，作业类型按源现在的。
	second, err := fac.PromoteToFactory(ctx, pe.tok, proc.ID, uuid.Nil)
	// 身份没换、不是首个修订或作业类型没跟上就停。
	if err != nil || second.ID == first.ID || second.Revision != 1 || second.WeldKind != store.WeldKindMultilayer {
		// 再升覆盖了旧副本或没带上新类型。
		t.Fatalf("kind only promote %+v from %+v %v", second, first, err)
	}
	// 源没变再升，也要再复制一份，不跳过。
	third, err := fac.PromoteToFactory(ctx, pe.tok, proc.ID, uuid.Nil)
	// 身份和前两份撞了就停。
	if err != nil || third.ID == second.ID || third.ID == first.ID {
		// 没变的再升被跳过或覆盖了。
		t.Fatalf("again promote %+v want new %v", third, err)
	}
	// 第一份副本不受后面升档影响，仍是单层。
	if got, err := fac.GetAsset(ctx, pe.tok, first.ID); err != nil || got.WeldKind != store.WeldKindSingle || got.Revision != first.Revision {
		// 旧副本被后面的升档改了。
		t.Fatalf("first copy changed %+v %v", got, err)
	}
	// 草稿要先发布才能被工程钉住。
	second, err = fac.PublishAsset(ctx, pe.tok, second.ID, second.Revision)
	// 发布失败就停。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 厂级工艺按新类型应能钉进多层工程。
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "钉升档工艺", multiBody, []factory.AssetDep{{ID: second.ID, Revision: second.Revision, Digest: second.Digest}}, store.WeldKindMultilayer); err != nil {
		// 厂级工艺还留着旧类型，钉不进新类型工程。
		t.Fatalf("pin promoted: %v", err)
	}

	// 新建一份个人级单层工程，先升一次厂级。
	proj, err := fac.CreatePersonalProject(ctx, pe.tok, direct, "个人工程", singleBody, nil, store.WeldKindSingle)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 第一次升厂级，复制出新身份的草稿。
	pFirst, err := fac.PromoteToFactory(ctx, pe.tok, proj.ID, uuid.Nil)
	// 升档失败或作业类型不是单层就停。
	if err != nil || pFirst.WeldKind != store.WeldKindSingle {
		// 第一次升档就没带上作业类型。
		t.Fatalf("first project promote %+v %v", pFirst, err)
	}
	// 源改成多层，旧单层正文会被清成空焊道。
	proj, err = fac.SetAssetWeldKind(ctx, pe.tok, proj.ID, proj.Revision, store.WeldKindMultilayer)
	// 改焊类失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改焊类的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 再按多层写正文，模拟改类型后重新编辑。
	proj, err = fac.UpdateAssetContent(ctx, pe.tok, proj.ID, proj.Revision, multiBody)
	// 改正文失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 再升复制一份新草稿，正文和作业类型按源现在的。
	pSecond, err := fac.PromoteToFactory(ctx, pe.tok, proj.ID, uuid.Nil)
	// 身份没换、作业类型或摘要有一项不对就停。
	if err != nil || pSecond.ID == pFirst.ID ||
		pSecond.WeldKind != store.WeldKindMultilayer || !bytes.Equal(pSecond.Digest, proj.Digest) {
		// 正文换了作业类型却没跟上。
		t.Fatalf("project promote %+v from %+v %v", pSecond, proj, err)
	}
	// 准备一份改过的多层正文，验厂级能按新类型改。
	edited := []byte(`[{"templateId":"` + contenttpl.SeedTplMulti + `","name":"多层改"}]`)
	// 厂级工程按新类型改正文应能保存。
	if _, err := fac.UpdateAssetContent(ctx, pe.tok, pSecond.ID, pSecond.Revision, edited); err != nil {
		// 厂级还是旧类型，按新类型改正文被拒。
		t.Fatalf("edit promoted project: %v", err)
	}
}

// 作业类型要合法，工程和工艺类型不一致则拒绝。
func TestFactoryWeldKind(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下初始超管，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa-a", "超管A")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := seedPass(ctx, fac, "sa-a", "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa-a", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	pe := mustCreateRole(t, ctx, fac, sa, "pe-a", "pe-pass", factory.RoleOrgAdmin)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{}

	// 新建一份厂级工艺，失败说明起草入口坏了。
	def, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "默认工艺", []byte(`{"name":"p"}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 作业类型应与预期一致，不对说明没按类型落库。
	if def.WeldKind != store.WeldKindSingle {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("default %s", def.WeldKind)
	}
	// 建工艺应因焊类非法被拒绝，放行说明没拦住。
	if _, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "坏类型", []byte(`{"name":"p"}`), "nope"); !errors.Is(err, domain.ErrInvalidWeldKind) {
		// 非法输入被收下了，说明校验没有生效。
		t.Fatalf("invalid: %v", err)
	}
	// 新建一份厂级工艺，失败说明起草入口坏了。
	multiP, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "多层工艺", []byte(`{"name":"m"}`), "multi")
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 作业类型应与预期一致，不对说明没按类型落库。
	if multiP.WeldKind != store.WeldKindMultilayer {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("alias %s", multiP.WeldKind)
	}
	// 复制出一份工艺，失败说明源稿不可复制或越权。
	copied, err := fac.CopyProcess(ctx, pe.tok, multiP.ID, "多层副本")
	// 复制工艺失败或焊类不对就停，说明没达预期。
	if err != nil || copied.WeldKind != store.WeldKindMultilayer {
		// 复制结果和源稿缠在一起，说明没有独立成稿。
		t.Fatalf("copy %+v %v", copied, err)
	}

	// 发布当前修订，失败说明状态不允许发布。
	pub, err := fac.PublishAsset(ctx, pe.tok, multiP.ID, multiP.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 准备这段正文字节，读回对不上说明没写进去。
	singleBody := []byte(`[{"templateId":"` + contenttpl.SeedTplSingle + `","name":"单"}]`)
	// 建工程应因类型不一致被拒绝，放行说明没拦住。
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "单层工程钉多层", singleBody, []factory.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}}, store.WeldKindSingle); !errors.Is(err, domain.ErrWeldKindMismatch) {
		t.Fatalf("dep mismatch: %v", err)
	}
	// 准备这段正文字节，读回对不上说明没写进去。
	multiBody := []byte(`[{"templateId":"` + contenttpl.SeedTplMulti + `","name":"多层"}]`)
	// 建工程应因类型不一致被拒绝，放行说明没拦住。
	if _, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "单层工程用多层模版", multiBody, nil, store.WeldKindSingle); !errors.Is(err, domain.ErrWeldKindMismatch) {
		// 两边结果对不上，说明匹配或引用写偏了。
		t.Fatalf("content mismatch: %v", err)
	}
	// 新建一份厂级工程，失败说明起草入口坏了。
	ok, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "多层工程", multiBody, []factory.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}}, store.WeldKindMultilayer)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 作业类型应与预期一致，不对说明没按类型落库。
	if ok.WeldKind != store.WeldKindMultilayer {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("project %s", ok.WeldKind)
	}

	// 修改作业类型，失败说明类型非法或冲突。
	changed, err := fac.SetAssetWeldKind(ctx, pe.tok, def.ID, def.Revision, store.WeldKindTBar)
	// 改焊类失败或焊类不对就停，说明没达预期。
	if err != nil || changed.WeldKind != store.WeldKindTBar {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("set kind %+v %v", changed, err)
	}
	// 修改作业类型，失败说明类型非法或冲突。
	switched, err := fac.SetAssetWeldKind(ctx, pe.tok, ok.ID, ok.Revision, store.WeldKindSingle)
	// 改焊类失败或焊类不对就停，说明没达预期。
	if err != nil || switched.WeldKind != store.WeldKindSingle {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("project kind %+v %v", switched, err)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	cleared, err := fac.ReadAssetContent(ctx, pe.tok, switched.ID)
	// 打成文本失败或结果不符就停，说明没达预期。
	if err != nil || string(cleared) != "[]" {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("project body %s %v", cleared, err)
	}
	// 改焊类应因焊类非法被拒绝，放行说明没拦住。
	if _, err := fac.SetAssetWeldKind(ctx, pe.tok, changed.ID, changed.Revision, "nope"); !errors.Is(err, domain.ErrInvalidWeldKind) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("bad kind: %v", err)
	}

	// 新建一份单层厂级工艺，给多层工程误引用用。
	singleP, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "单层工艺", []byte(`{"name":"s"}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布单层工艺，可用后才钉得进工程。
	singleP, err = fac.PublishAsset(ctx, pe.tok, singleP.ID, singleP.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份不带引用的多层工程，给示教器盖回用。
	padProj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "示教器多层工程", multiBody, nil, store.WeldKindMultilayer)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 记下盖回前的正文，拒绝后要比对没被改。
	before, err := fac.ReadAssetContent(ctx, pe.tok, padProj.ID)
	// 读正文失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把读正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 按多层模版拼一份焊道引用指定工艺的正文。
	refBody := func(processID string, x int) []byte {
		// 焊道里写工艺身份和横坐标，便于区分两次盖回。
		return []byte(`[{"templateId":"` + contenttpl.SeedTplMulti + `","name":"多层","passes":[{"processId":"` + processID + `","valX":` + strconv.Itoa(x) + `}]}]`)
	}
	// 示教器新引用单层工艺应被拒，放行说明没核作业类型。
	if _, err := fac.ApplyAppContent(ctx, sa, padProj.ID, refBody(singleP.ID.String(), 1)); !errors.Is(err, domain.ErrWeldKindMismatch) {
		// 单层参数会下发到多层焊缝上，必须拦住。
		t.Fatalf("pad single ref: %v", err)
	}
	// 读回台账，拒绝后修订和依赖都不该变。
	kept, err := fac.GetAsset(ctx, sa, padProj.ID)
	// 读台账失败或修订、依赖变了就停。
	if err != nil || kept.Revision != padProj.Revision || len(kept.Deps) != 0 {
		// 被拒的盖回改到了台账，说明拒绝前就落库了。
		t.Fatalf("pad reject meta %+v %v", kept, err)
	}
	// 读回正文，拒绝后应和盖回前一样。
	after, err := fac.ReadAssetContent(ctx, pe.tok, padProj.ID)
	// 读正文失败或正文变了就停。
	if err != nil || !bytes.Equal(after, before) {
		// 被拒的正文落了库，说明拒绝没挡住写入。
		t.Fatalf("pad reject body %s %v", after, err)
	}
	// 先当没有拒绝审计，找到再改成有。
	denied := false
	// 逐条看审计，找这份工程的盖回拒绝。
	for _, r := range mustRows(t, ctx, fac) {
		// 动作、结果和对象都对上才算这次的拒绝。
		if r.Action == "apply_app" && r.Result == audit.Deny && strings.HasPrefix(r.Target, padProj.ID.String()) {
			// 记下已见到拒绝审计。
			denied = true
		}
	}
	// 没见到拒绝审计就停，拒绝原因查不到。
	if !denied {
		// 拒绝没留审计，事后查不出谁盖过。
		t.Fatal("missing apply_app deny audit")
	}
	// 改成引用多层工艺再盖，应能盖上并钉进依赖。
	applied, err := fac.ApplyAppContent(ctx, sa, padProj.ID, refBody(pub.ID.String(), 1))
	// 盖回失败或依赖不对就停，说明同类型工艺被误拦。
	if err != nil || applied.Revision != padProj.Revision+1 || len(applied.Deps) != 1 || applied.Deps[0].ID != pub.ID {
		// 同类型的新引用被拒，说明核对写偏了。
		t.Fatalf("pad multi ref %+v %v", applied, err)
	}

	// 在用工艺服务层已改不了类型，直接改库模拟旧数据里类型已对不上。
	forceWeldKind(t, ctx, fac, pub, store.WeldKindSingle)
	// 只改点坐标再盖回，旧依赖不在这里拦，应能盖上。
	moved, err := fac.ApplyAppContent(ctx, sa, padProj.ID, refBody(pub.ID.String(), 2))
	// 盖回失败或依赖变了就停，说明旧依赖被误核。
	if err != nil || moved.Revision != applied.Revision+1 || len(moved.Deps) != 1 || moved.Deps[0].ID != pub.ID {
		// 示教器没碰的旧依赖挡住了盖回。
		t.Fatalf("pad old dep %+v %v", moved, err)
	}
}

// weldRefTemplateSchema 自定义工程模版：带一个工艺槽，正文认不出作业类型。
const weldRefTemplateSchema = `{"root":"object","fields":[{"key":"name","label":"名称","type":"string","default":""},{"key":"proc","label":"工艺","type":"process"}]}`

// weldRefBody 拼一条挂自定义模版、工艺槽填指定工艺的工程正文。
func weldRefBody(tplID, processID uuid.UUID) []byte {
	// 不写焊缝种类，作业类型核对只能放过。
	return []byte(`[{"templateId":"` + tplID.String() + `","name":"w","proc":"` + processID.String() + `"}]`)
}

// weldRefFactory 开厂、建人并收下自定义模版，交回厂服务、人员和模版身份。
func weldRefFactory(t *testing.T, ctx context.Context) (*factory.Service, namedAcc, uuid.UUID) {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下初始超管，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOrgAdmin)
	// 新取自定义模版身份，和三份种子区分开。
	tplID := id.New()
	// 留下工厂编号，收模版要对准本厂。
	fid := seed.ID
	// 收自定义模版失败就停，没有它就复现不了。
	if err := fac.AcceptTemplateDelivery(ctx, factory.TemplateSnapshot{
		// 记下身份、类型和修订，摘要按字段表算。
		ID: tplID, Kind: factory.KindProject, Name: "自定义", Revision: 1,
		Schema: []byte(weldRefTemplateSchema), Digest: digest.Sum([]byte(weldRefTemplateSchema)),
		TargetFactoryID: &fid,
	}); err != nil {
		t.Fatal(err)
	}
	return fac, pe, tplID
}

// weldRefProcess 建一条厂级工艺并发布，交回可用修订。
func weldRefProcess(t *testing.T, ctx context.Context, fac *factory.Service, tok, name, weld string) factory.Asset {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 按指定作业类型建厂级工艺。
	p, err := fac.CreateFactoryProcess(ctx, tok, factory.WorkContext{}, name, []byte(`{"name":"p"}`), weld)
	// 建工艺失败就停，后面没有工艺可钉。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，草稿不能当依赖。
	p, err = fac.PublishAsset(ctx, tok, p.ID, p.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	return p
}

// 自定义模版的多层工程改成单层：正文引用被筛掉的工艺就清空，之后改正文不再被拒。
func TestFactoryWeldKindDropsCustomRefs(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 开厂建人并收下自定义模版。
	fac, pe, tplID := weldRefFactory(t, ctx)
	// 建一条可用的多层工艺，给工程正文引用。
	multi := weldRefProcess(t, ctx, fac, pe.tok, "多层工艺", store.WeldKindMultilayer)
	// 建多层工程，正文引用会自动钉进依赖。
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, factory.WorkContext{}, "自定义多层", weldRefBody(tplID, multi.ID), nil, store.WeldKindMultilayer)
	// 建工程失败或依赖没钉上就停。
	if err != nil || len(proj.Deps) != 1 || proj.Deps[0].ID != multi.ID {
		// 自定义模版的多层工程没建好。
		t.Fatalf("create %+v %v", proj, err)
	}
	// 改成单层，多层工艺会被筛掉。
	switched, err := fac.SetAssetWeldKind(ctx, pe.tok, proj.ID, proj.Revision, store.WeldKindSingle)
	// 改作业类型失败、依赖没清空或类型不对就停。
	if err != nil || len(switched.Deps) != 0 || switched.WeldKind != store.WeldKindSingle {
		// 被筛掉的依赖还留着。
		t.Fatalf("switch %+v %v", switched, err)
	}
	// 读回资产正文，看是否换成空焊道。
	body, err := fac.ReadAssetContent(ctx, pe.tok, switched.ID)
	// 正文没换成空焊道就停，说明还引用被筛掉的工艺。
	if err != nil || string(body) != "[]" {
		// 正文残留被筛掉的工艺引用。
		t.Fatalf("body %s %v", body, err)
	}
	// 按自定义模版重写一份不带工艺的正文。
	edited := []byte(`[{"templateId":"` + tplID.String() + `","name":"新"}]`)
	// 之后改正文应能保存，被拒说明还卡着旧引用。
	if _, err := fac.UpdateAssetContent(ctx, pe.tok, switched.ID, switched.Revision, edited); err != nil {
		// 改类型后改正文被拒。
		t.Fatalf("edit after switch: %v", err)
	}
}

// 改工程作业类型时，引用的工艺没被筛掉，正文原样保留。
func TestFactoryWeldKindKeepsCustomRefs(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 开厂建人并收下自定义模版。
	fac, pe, tplID := weldRefFactory(t, ctx)
	// 正文引用的多层工艺，后面会改类型跟着工程走。
	used := weldRefProcess(t, ctx, fac, pe.tok, "在用工艺", store.WeldKindMultilayer)
	// 只挂依赖、正文不引用的多层工艺，改单层时会被筛掉。
	spare := weldRefProcess(t, ctx, fac, pe.tok, "备用工艺", store.WeldKindMultilayer)
	// 备用工艺按当前修订钉进依赖。
	deps := []factory.AssetDep{{ID: spare.ID, Revision: spare.Revision, Digest: spare.Digest}}
	// 建多层工程，正文只引用在用工艺。
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, factory.WorkContext{}, "自定义多层", weldRefBody(tplID, used.ID), deps, store.WeldKindMultilayer)
	// 建工程失败或依赖不是两条就停。
	if err != nil || len(proj.Deps) != 2 {
		// 两条工艺没都钉上。
		t.Fatalf("create %+v %v", proj, err)
	}
	// 记下改类型前的正文，后面逐字比对。
	before, err := fac.ReadAssetContent(ctx, pe.tok, proj.ID)
	// 读正文失败就停，读不到就无法比对。
	if err != nil {
		// 把读正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 在用工艺直接在库里改成单层，模拟旧数据，工程改单层时它能留下。
	used = forceWeldKind(t, ctx, fac, used, store.WeldKindSingle)
	// 工程改单层，只有备用工艺被筛掉。
	proj, err = fac.SetAssetWeldKind(ctx, pe.tok, proj.ID, proj.Revision, store.WeldKindSingle)
	// 改作业类型失败或依赖不是只剩在用工艺就停。
	if err != nil || len(proj.Deps) != 1 || proj.Deps[0].ID != used.ID {
		// 筛依赖结果不对。
		t.Fatalf("to single %+v %v", proj, err)
	}
	// 读回正文，引用不在被筛部分应原样留下。
	kept, err := fac.ReadAssetContent(ctx, pe.tok, proj.ID)
	// 正文被改动就停，说明误清了没悬空的引用。
	if err != nil || !bytes.Equal(kept, before) {
		// 引用未被筛掉却清了正文。
		t.Fatalf("partial drop body %s %v", kept, err)
	}
	// 在用工艺再直接在库里改成 T 排，工程跟着改时一条都不筛。
	used = forceWeldKind(t, ctx, fac, used, store.WeldKindTBar)
	// 工程改 T 排，依赖全部留下。
	proj, err = fac.SetAssetWeldKind(ctx, pe.tok, proj.ID, proj.Revision, store.WeldKindTBar)
	// 改作业类型失败或依赖变了就停。
	if err != nil || len(proj.Deps) != 1 || proj.Deps[0].ID != used.ID {
		// 没有该筛的依赖却变了。
		t.Fatalf("to tbar %+v %v", proj, err)
	}
	// 读回正文，没有依赖被筛掉应原样留下。
	same, err := fac.ReadAssetContent(ctx, pe.tok, proj.ID)
	// 正文被改动就停，说明没筛依赖也清了正文。
	if err != nil || !bytes.Equal(same, before) {
		// 没有依赖被筛掉却清了正文。
		t.Fatalf("no drop body %s %v", same, err)
	}
}

// forceWeldKind 绕过服务层直接在库里改工艺作业类型，摆出旧数据里类型对不上的情形。
func forceWeldKind(t *testing.T, ctx context.Context, fac *factory.Service, a factory.Asset, weld string) factory.Asset {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 只改作业类型这一列，修订和正文都不动；在用工艺走正常写入会被拒。
	if err := fac.Store().TamperAssetWeldKind(ctx, a.ID, weld); err != nil {
		// 把改库的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 读回改后的元数据交给调用方。
	out, err := fac.Store().GovernedAssetMetaByID(ctx, a.ID)
	// 读回失败就停，后面拿不到当前行。
	if err != nil {
		// 把读回的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	return out
}

// 工艺仍被工程钉着时不许换作业类型；同类型不拦，没人钉着的照常能改。
func TestFactoryWeldKindInUse(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 开厂建人，自定义模版这里用不上。
	fac, pe, _ := weldRefFactory(t, ctx)
	// 建一条可用的多层工艺，给工程钉住。
	pinned := weldRefProcess(t, ctx, fac, pe.tok, "在用工艺", store.WeldKindMultilayer)
	// 再建一条没人钉的多层工艺，验没被引用时能改。
	spare := weldRefProcess(t, ctx, fac, pe.tok, "空闲工艺", store.WeldKindMultilayer)
	// 准备多层模版正文，和钉住的工艺同类型。
	multiBody := []byte(`[{"templateId":"` + contenttpl.SeedTplMulti + `","name":"多层"}]`)
	// 按当前修订钉住在用工艺。
	deps := []factory.AssetDep{{ID: pinned.ID, Revision: pinned.Revision, Digest: pinned.Digest}}
	// 建多层工程钉住在用工艺，失败则没有引用可验。
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, factory.WorkContext{}, "钉工艺工程", multiBody, deps, store.WeldKindMultilayer)
	// 建工程失败或依赖没钉上就停。
	if err != nil || len(proj.Deps) != 1 || proj.Deps[0].ID != pinned.ID {
		// 工程没钉上在用工艺，后面验不了拦截。
		t.Fatalf("create %+v %v", proj, err)
	}
	// 在用工艺改成单层应被拒为工艺在用、不能改类型。
	if _, err := fac.SetAssetWeldKind(ctx, pe.tok, pinned.ID, pinned.Revision, store.WeldKindSingle); !errors.Is(err, domain.ErrWeldKindInUse) {
		// 放行了说明钉着它的工程会和它类型对不上。
		t.Fatalf("in use: %v", err)
	}
	// 读回在用工艺，看拒绝后类型和修订是否原样。
	kept, err := fac.GetAsset(ctx, pe.tok, pinned.ID)
	// 读失败、类型变了或修订升了就停。
	if err != nil || kept.WeldKind != store.WeldKindMultilayer || kept.Revision != pinned.Revision {
		// 被拒的改类型还是落了库。
		t.Fatalf("after reject %+v %v", kept, err)
	}
	// 用别名改成同一类型，类型没变不该被拦。
	same, err := fac.SetAssetWeldKind(ctx, pe.tok, pinned.ID, pinned.Revision, "multi")
	// 被拒或类型变了就停，说明同类型也被误拦。
	if err != nil || same.WeldKind != store.WeldKindMultilayer {
		// 同类型保存不该看引用。
		t.Fatalf("same kind %+v %v", same, err)
	}
	// 没人钉的工艺改成单层应照常成功。
	free, err := fac.SetAssetWeldKind(ctx, pe.tok, spare.ID, spare.Revision, store.WeldKindSingle)
	// 失败或类型没改成单层就停。
	if err != nil || free.WeldKind != store.WeldKindSingle {
		// 没被引用的工艺也被拦了。
		t.Fatalf("unreferenced %+v %v", free, err)
	}
}
