// 工艺/工程作业类型：单层焊道、多层焊缝、T排对接；工程与工艺必须同类型。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
	"wmesh/global/internal/store"

	"github.com/google/uuid"
)

// 验作业类型，工程必须和所引用工艺同类型。
func TestPlatformWeldKind(t *testing.T) {
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

	// 新建平台工艺「默认工艺」，建不成后面没有工艺号可对。
	def, err := h.WAN.CreatePlatformProcess(ctx, tok, "默认工艺", []byte(`{"name":"p"}`))
	// 新建平台工艺「默认工艺」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「默认工艺」失败。
		t.Fatal(err)
	}
	// 默认工艺作业类型不是单层就停，这一步不能算通过。
	if def.WeldKind != store.WeldKindSingle {
		// 不符即停：默认工艺作业类型不是单层。
		t.Fatalf("default %s", def.WeldKind)
	}
	// 新建平台工艺「坏类型」应被拒为类型非法，放行或错类都算没拦住。
	if _, err := h.WAN.CreatePlatformProcess(ctx, tok, "坏类型", []byte(`{"name":"p"}`), "nope"); !errors.Is(err, domain.ErrInvalidWeldKind) {
		// 不符即停：新建平台工艺「坏类型」应被拒为类型非法。
		t.Fatalf("invalid: %v", err)
	}
	// 新建平台工艺「多层工艺」，建不成后面没有工艺号可对。
	multiP, err := h.WAN.CreatePlatformProcess(ctx, tok, "多层工艺", []byte(`{"name":"m"}`), "multi")
	// 新建平台工艺「多层工艺」失败就停，建不成后面没有工艺号可对。
	if err != nil {
		// 不符即停：新建平台工艺「多层工艺」失败。
		t.Fatal(err)
	}
	// 多层工艺作业类型不是多层就停，这一步不能算通过。
	if multiP.WeldKind != store.WeldKindMultilayer {
		// 不符即停：多层工艺作业类型不是多层。
		t.Fatalf("alias %s", multiP.WeldKind)
	}
	// 另存工艺「多层副本」，另存失败就没有新草稿。
	copied, err := h.WAN.CopyPlatformProcess(ctx, tok, multiP.ID, "多层副本")
	// 另存工艺「多层副本」失败或作业类型不是多层就停，不能当通过。
	if err != nil || copied.WeldKind != store.WeldKindMultilayer {
		// 不符即停：另存工艺「多层副本」失败或作业类型不是多层。
		t.Fatalf("copy %+v %v", copied, err)
	}

	// 发布资产，发不出去后面不能当可用。
	pub, err := h.WAN.PublishPlatformAsset(ctx, tok, multiP.ID, multiP.Revision)
	// 发布资产失败就停，发不出去后面不能当可用。
	if err != nil {
		// 不符即停：发布资产失败。
		t.Fatal(err)
	}
	// 准备正文样本，后面入库和比对都用它。
	singleBody := []byte(`[{"templateId":"` + contenttpl.SeedTplSingle + `","name":"单"}]`)
	// 新建平台工程「单层工程钉多层」应被拒为类型不一致，放行或错类都算没拦住。
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "单层工程钉多层", singleBody, []global.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}}, store.WeldKindSingle); !errors.Is(err, domain.ErrWeldKindMismatch) {
		t.Fatalf("dep mismatch: %v", err)
	}
	// 准备正文样本，后面入库和比对都用它。
	multiBody := []byte(`[{"templateId":"` + contenttpl.SeedTplMulti + `","name":"多层"}]`)
	// 新建平台工程「单层工程用多层模版」应被拒为类型不一致，放行或错类都算没拦住。
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "单层工程用多层模版", multiBody, nil, store.WeldKindSingle); !errors.Is(err, domain.ErrWeldKindMismatch) {
		// 不符即停：新建平台工程「单层工程用多层模版」应被拒为类型不一致。
		t.Fatalf("content mismatch: %v", err)
	}
	// 新建平台工程「多层工程」，建不成后面没有工程可引用。
	ok, err := h.WAN.CreatePlatformProject(ctx, tok, "多层工程", multiBody, []global.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}}, store.WeldKindMultilayer)
	// 新建平台工程「多层工程」失败就停，建不成后面没有工程可引用。
	if err != nil {
		// 不符即停：新建平台工程「多层工程」失败。
		t.Fatal(err)
	}
	// 这份工程作业类型不是多层就停，这一步不能算通过。
	if ok.WeldKind != store.WeldKindMultilayer {
		// 不符即停：这份工程作业类型不是多层。
		t.Fatalf("project %s", ok.WeldKind)
	}

	// 改作业类型，类型改不了工程会对不上。
	changed, err := h.WAN.SetPlatformWeldKind(ctx, tok, def.ID, def.Revision, store.WeldKindTBar)
	// 改作业类型失败或作业类型不是T排就停，不能当通过。
	if err != nil || changed.WeldKind != store.WeldKindTBar {
		// 不符即停：改作业类型失败或作业类型不是T排。
		t.Fatalf("set kind %+v %v", changed, err)
	}
	// 改作业类型，类型改不了工程会对不上。
	switched, err := h.WAN.SetPlatformWeldKind(ctx, tok, ok.ID, ok.Revision, store.WeldKindSingle)
	// 改作业类型失败或作业类型不是单层就停，不能当通过。
	if err != nil || switched.WeldKind != store.WeldKindSingle {
		// 不符即停：改作业类型失败或作业类型不是单层。
		t.Fatalf("project kind %+v %v", switched, err)
	}
	// 读平台正文，读不到就无法比对。
	cleared, err := h.WAN.ReadPlatformAssetContent(ctx, tok, switched.ID)
	// 读平台正文失败或正文没有清空就停，不能当通过。
	if err != nil || string(cleared) != "[]" {
		// 不符即停：读平台正文失败或正文没有清空。
		t.Fatalf("project body %s %v", cleared, err)
	}
	// 改作业类型应被拒为类型非法，放行或错类都算没拦住。
	if _, err := h.WAN.SetPlatformWeldKind(ctx, tok, changed.ID, changed.Revision, "nope"); !errors.Is(err, domain.ErrInvalidWeldKind) {
		// 不符即停：改作业类型应被拒为类型非法。
		t.Fatalf("bad kind: %v", err)
	}
}

// 同一源换了作业类型再升平台级，平台条目的作业类型要跟上。
func TestPlatformPromoteWeldKind(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 固定云端口令，登录和泄密检查都用它。
	const wanPass = "wan-secret"
	// 立云端超管，立不住后面没有人能登录。
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
	// 登记工厂「厂A」，快照要挂在这家厂名下。
	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可升档。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 准备正文样本，两次快照正文相同、只换作业类型。
	body := []byte(`{"name":"焊接","current":180}`)
	// 新造源厂身份，两次快照都指向同一源。
	src := id.New()
	// 按作业类型和源修订拼快照，正文和摘要不变。
	snapOf := func(weld string, rev int64) global.AssetSnapshot {
		// 正文和摘要固定，只让作业类型和修订变化。
		return global.AssetSnapshot{
			SourceID: src, SourceRevision: rev, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProcess, Name: "焊接", Content: body, Digest: digest.Sum(body),
			Copyable: true, WeldKind: weld, Status: global.AssetAvailable,
		}
	}
	// 先用单层快照升档，落成平台草稿。
	first, err := h.WAN.PromoteFromSnapshot(ctx, tok, snapOf(store.WeldKindSingle, 1))
	// 升档失败或作业类型不是单层就停。
	if err != nil || first.WeldKind != store.WeldKindSingle {
		// 不符即停：第一次升档没带上作业类型。
		t.Fatalf("first promote %+v %v", first, err)
	}
	// 再用多层快照升档，正文摘要没变也要覆盖。
	second, err := h.WAN.PromoteFromSnapshot(ctx, tok, snapOf(store.WeldKindMultilayer, 2))
	// 身份、修订、作业类型或草稿状态有一项不对就停。
	if err != nil || second.ID != first.ID || second.Revision != first.Revision+1 ||
		second.WeldKind != store.WeldKindMultilayer || second.Status != global.AssetDraft {
		// 不符即停：只换作业类型的再升被当成没变跳过了。
		t.Fatalf("kind only promote %+v from %+v %v", second, first, err)
	}
	// 摘要和作业类型都没变时再升，应跳过不升修订。
	third, err := h.WAN.PromoteFromSnapshot(ctx, tok, snapOf(store.WeldKindMultilayer, 2))
	// 身份、修订或作业类型变了就停，说明没变也覆盖了。
	if err != nil || third.ID != second.ID || third.Revision != second.Revision || third.WeldKind != store.WeldKindMultilayer {
		// 不符即停：没变的再升也升了修订。
		t.Fatalf("skip promote %+v want %+v %v", third, second, err)
	}
}

// weldRefTemplateSchema 自定义工程模版：带一个工艺槽，正文认不出作业类型。
const weldRefTemplateSchema = `{"root":"object","fields":[{"key":"name","label":"名称","type":"string","default":""},{"key":"proc","label":"工艺","type":"process"}]}`

// weldRefBody 拼一条挂自定义模版、工艺槽填指定工艺的工程正文。
func weldRefBody(tplID, processID uuid.UUID) []byte {
	// 不写焊缝种类，作业类型核对只能放过。
	return []byte(`[{"templateId":"` + tplID.String() + `","name":"w","proc":"` + processID.String() + `"}]`)
}

// weldRefWAN 立超管、登录并建好自定义模版，交回会话和模版身份。
func weldRefWAN(t *testing.T, ctx context.Context, h *Harness) (string, uuid.UUID) {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
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
	// 建一份带工艺槽的自定义工程模版。
	tpl, err := h.WAN.CreateProjectTemplate(ctx, tok, uuid.Nil, "自定义", []byte(weldRefTemplateSchema))
	// 建自定义模版失败就停，没有它就复现不了。
	if err != nil {
		// 不符即停：建自定义模版失败。
		t.Fatal(err)
	}
	return tok, tpl.ID
}

// weldRefProcess 建一条平台工艺并发布，交回可用修订。
func weldRefProcess(t *testing.T, ctx context.Context, h *Harness, tok, name, weld string) global.Asset {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 按指定作业类型建平台工艺。
	p, err := h.WAN.CreatePlatformProcess(ctx, tok, name, []byte(`{"name":"p"}`), weld)
	// 建工艺失败就停，后面没有工艺可钉。
	if err != nil {
		// 不符即停：建平台工艺失败。
		t.Fatal(err)
	}
	// 发布资产，发不出去工程钉不上。
	p, err = h.WAN.PublishPlatformAsset(ctx, tok, p.ID, p.Revision)
	// 发布失败就停，草稿不能当依赖。
	if err != nil {
		// 不符即停：发布平台工艺失败。
		t.Fatal(err)
	}
	return p
}

// 自定义模版的多层工程改成单层：正文引用被筛掉的工艺就清空，之后改正文不再被拒。
func TestPlatformWeldKindDropsCustomRefs(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立超管并建好自定义模版。
	tok, tplID := weldRefWAN(t, ctx, h)
	// 建一条可用的多层工艺，给工程正文引用。
	multi := weldRefProcess(t, ctx, h, tok, "多层工艺", store.WeldKindMultilayer)
	// 建多层工程，正文引用会自动钉进依赖。
	proj, err := h.WAN.CreatePlatformProject(ctx, tok, "自定义多层", weldRefBody(tplID, multi.ID), nil, store.WeldKindMultilayer)
	// 建工程失败或依赖没钉上就停。
	if err != nil || len(proj.Deps) != 1 || proj.Deps[0].ID != multi.ID {
		// 不符即停：自定义模版的多层工程没建好。
		t.Fatalf("create %+v %v", proj, err)
	}
	// 改成单层，多层工艺会被筛掉。
	switched, err := h.WAN.SetPlatformWeldKind(ctx, tok, proj.ID, proj.Revision, store.WeldKindSingle)
	// 改作业类型失败就停，不能当通过。
	if err != nil {
		// 不符即停：改工程作业类型失败。
		t.Fatal(err)
	}
	// 读回台账，看依赖和作业类型。
	meta, err := h.WAN.GetPlatformAsset(ctx, tok, switched.ID)
	// 依赖没清空或作业类型不是单层就停。
	if err != nil || len(meta.Deps) != 0 || meta.WeldKind != store.WeldKindSingle {
		// 不符即停：被筛掉的依赖还留着。
		t.Fatalf("meta %+v %v", meta, err)
	}
	// 读平台正文，读不到就无法比对。
	body, err := h.WAN.ReadPlatformAssetContent(ctx, tok, meta.ID)
	// 正文没换成空焊道就停，说明还引用被筛掉的工艺。
	if err != nil || string(body) != "[]" {
		// 不符即停：正文残留被筛掉的工艺引用。
		t.Fatalf("body %s %v", body, err)
	}
	// 按自定义模版重写一份不带工艺的正文。
	edited := []byte(`[{"templateId":"` + tplID.String() + `","name":"新"}]`)
	// 之后改正文应能保存，被拒说明还卡着旧引用。
	if _, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, meta.ID, meta.Revision, edited); err != nil {
		// 不符即停：改类型后改正文被拒。
		t.Fatalf("edit after switch: %v", err)
	}
}

// 改工程作业类型时，引用的工艺没被筛掉，正文原样保留。
func TestPlatformWeldKindKeepsCustomRefs(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立超管并建好自定义模版。
	tok, tplID := weldRefWAN(t, ctx, h)
	// 正文引用的多层工艺，后面会改类型跟着工程走。
	used := weldRefProcess(t, ctx, h, tok, "在用工艺", store.WeldKindMultilayer)
	// 只挂依赖、正文不引用的多层工艺，改单层时会被筛掉。
	spare := weldRefProcess(t, ctx, h, tok, "备用工艺", store.WeldKindMultilayer)
	// 备用工艺按当前修订钉进依赖。
	deps := []global.AssetDep{{ID: spare.ID, Revision: spare.Revision, Digest: spare.Digest}}
	// 建多层工程，正文只引用在用工艺。
	proj, err := h.WAN.CreatePlatformProject(ctx, tok, "自定义多层", weldRefBody(tplID, used.ID), deps, store.WeldKindMultilayer)
	// 建工程失败或依赖不是两条就停。
	if err != nil || len(proj.Deps) != 2 {
		// 不符即停：两条工艺没都钉上。
		t.Fatalf("create %+v %v", proj, err)
	}
	// 记下改类型前的正文，后面逐字比对。
	before, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proj.ID)
	// 读正文失败就停，读不到就无法比对。
	if err != nil {
		// 不符即停：读平台正文失败。
		t.Fatal(err)
	}
	// 在用工艺直接在库里改成单层，模拟旧数据，工程改单层时它能留下。
	forcePlatformWeldKind(t, ctx, h, used.ID, store.WeldKindSingle)
	// 工程改单层，只有备用工艺被筛掉。
	proj, err = h.WAN.SetPlatformWeldKind(ctx, tok, proj.ID, proj.Revision, store.WeldKindSingle)
	// 改作业类型失败或依赖不是只剩在用工艺就停。
	if err != nil || len(proj.Deps) != 1 || proj.Deps[0].ID != used.ID {
		// 不符即停：筛依赖结果不对。
		t.Fatalf("to single %+v %v", proj, err)
	}
	// 读平台正文，引用不在被筛部分应原样留下。
	kept, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proj.ID)
	// 正文被改动就停，说明误清了没悬空的引用。
	if err != nil || !bytes.Equal(kept, before) {
		// 不符即停：引用未被筛掉却清了正文。
		t.Fatalf("partial drop body %s %v", kept, err)
	}
	// 在用工艺再直接在库里改成 T 排，工程跟着改时一条都不筛。
	forcePlatformWeldKind(t, ctx, h, used.ID, store.WeldKindTBar)
	// 工程改 T 排，依赖全部留下。
	proj, err = h.WAN.SetPlatformWeldKind(ctx, tok, proj.ID, proj.Revision, store.WeldKindTBar)
	// 改作业类型失败或依赖变了就停。
	if err != nil || len(proj.Deps) != 1 || proj.Deps[0].ID != used.ID {
		// 不符即停：没有该筛的依赖却变了。
		t.Fatalf("to tbar %+v %v", proj, err)
	}
	// 读平台正文，没有依赖被筛掉应原样留下。
	same, err := h.WAN.ReadPlatformAssetContent(ctx, tok, proj.ID)
	// 正文被改动就停，说明没筛依赖也清了正文。
	if err != nil || !bytes.Equal(same, before) {
		// 不符即停：没有依赖被筛掉却清了正文。
		t.Fatalf("no drop body %s %v", same, err)
	}
}

// forcePlatformWeldKind 绕过服务层直接在库里改工艺作业类型，摆出旧数据里类型对不上的情形。
func forcePlatformWeldKind(t *testing.T, ctx context.Context, h *Harness, assetID uuid.UUID, weld string) {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 只改作业类型这一列，修订和正文都不动。
	if err := h.DB.WithContext(ctx).Exec("UPDATE assets SET weld_kind = ? WHERE id = ?", weld, assetID).Error; err != nil {
		// 不符即停：改库失败，旧数据情形摆不出来。
		t.Fatal(err)
	}
}

// 工艺仍被平台工程钉着时不许换作业类型；同类型不拦，没人钉着的照常能改。
func TestPlatformWeldKindInUse(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立超管并登录，自定义模版这里用不上。
	tok, _ := weldRefWAN(t, ctx, h)
	// 建一条可用的多层工艺，给工程钉住。
	pinned := weldRefProcess(t, ctx, h, tok, "在用工艺", store.WeldKindMultilayer)
	// 再建一条没人钉的多层工艺，验没被引用时能改。
	spare := weldRefProcess(t, ctx, h, tok, "空闲工艺", store.WeldKindMultilayer)
	// 准备多层模版正文，和钉住的工艺同类型。
	multiBody := []byte(`[{"templateId":"` + contenttpl.SeedTplMulti + `","name":"多层"}]`)
	// 按当前修订钉住在用工艺。
	deps := []global.AssetDep{{ID: pinned.ID, Revision: pinned.Revision, Digest: pinned.Digest}}
	// 建多层工程钉住在用工艺，失败则没有引用可验。
	proj, err := h.WAN.CreatePlatformProject(ctx, tok, "钉工艺工程", multiBody, deps, store.WeldKindMultilayer)
	// 建工程失败或依赖没钉上就停。
	if err != nil || len(proj.Deps) != 1 || proj.Deps[0].ID != pinned.ID {
		// 不符即停：工程没钉上在用工艺，后面验不了拦截。
		t.Fatalf("create %+v %v", proj, err)
	}
	// 在用工艺改成单层应被拒为工艺在用、不能改类型。
	if _, err := h.WAN.SetPlatformWeldKind(ctx, tok, pinned.ID, pinned.Revision, store.WeldKindSingle); !errors.Is(err, domain.ErrWeldKindInUse) {
		// 不符即停：放行了说明钉着它的工程会和它类型对不上。
		t.Fatalf("in use: %v", err)
	}
	// 读回在用工艺，看拒绝后类型和修订是否原样。
	kept, err := h.WAN.GetPlatformAsset(ctx, tok, pinned.ID)
	// 读失败、类型变了或修订升了就停。
	if err != nil || kept.WeldKind != store.WeldKindMultilayer || kept.Revision != pinned.Revision {
		// 不符即停：被拒的改类型还是落了库。
		t.Fatalf("after reject %+v %v", kept, err)
	}
	// 用别名改成同一类型，类型没变不该被拦。
	same, err := h.WAN.SetPlatformWeldKind(ctx, tok, pinned.ID, pinned.Revision, "multi")
	// 被拒或类型变了就停，说明同类型也被误拦。
	if err != nil || same.WeldKind != store.WeldKindMultilayer {
		// 不符即停：同类型保存不该看引用。
		t.Fatalf("same kind %+v %v", same, err)
	}
	// 没人钉的工艺改成单层应照常成功。
	free, err := h.WAN.SetPlatformWeldKind(ctx, tok, spare.ID, spare.Revision, store.WeldKindSingle)
	// 失败或类型没改成单层就停。
	if err != nil || free.WeldKind != store.WeldKindSingle {
		// 不符即停：没被引用的工艺也被拦了。
		t.Fatalf("unreferenced %+v %v", free, err)
	}
}
