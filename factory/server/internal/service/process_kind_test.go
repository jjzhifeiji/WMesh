// 工艺种类：按所属模版套用、种类定了不能换、只有焊接参数分适用作业、工程只依赖焊接参数、示教器看不到自建种类。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 云端下发了多份工艺模版后，厂端按种类建工艺，并守住三条约束。
func TestProcessKinds(t *testing.T) {
	// 准备本测上下文。
	ctx := context.Background()
	// 起隔离厂库。
	h := New(t)
	// 建厂和初始超管。
	seed, fac, err := h.Provision(ctx, "sa-k", "超管K")
	// 建不成就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 超管登录。
	sa := mustLogin(t, ctx, fac, "sa-k", personPass("sa-k"))
	// 建一个能建厂级件的管理员，同时拿来登示教器。
	op := mustCreateRole(t, ctx, fac, sa, "op-k", "op-pass", factory.RoleOrgAdmin)
	// 本厂身份，投递模版要对上。
	fid := seed.ID
	// 自建种类的模版身份。
	customID := id.New()
	// 预置包角的模版身份。
	cornerTplID := uuid.MustParse(contenttpl.SeedTplCorner)
	// 投递一份工艺模版的小工具。
	deliver := func(tplID uuid.UUID, name string, fields []contenttpl.Field) {
		// 标成辅助函数，失败算到调用方。
		t.Helper()
		// 编成规范字段表。
		schema, err := contenttpl.Marshal(contenttpl.ObjectSchema(fields))
		// 编不出就停。
		if err != nil {
			// 把错误打出来。
			t.Fatal(err)
		}
		// 组一份投给本厂的模版。
		snap := factory.TemplateSnapshot{ID: tplID, Kind: factory.KindProcess, Name: name, Revision: 1, Schema: schema, Digest: digest.Sum(schema), TargetFactoryID: &fid}
		// 像云端通道那样送达。
		if err := fac.AcceptTemplateDelivery(ctx, snap); err != nil {
			// 收不下就停。
			t.Fatalf("deliver %s: %v", name, err)
		}
	}
	// 逐份送达预置两份。
	for _, s := range contenttpl.SeedProcessTemplates() {
		// 送达这一份。
		deliver(uuid.MustParse(s.ID), s.Name, s.Fields)
	}
	// 再送达一份自建种类，字段只有名称和振幅。
	deliver(customID, "摆动参数", []contenttpl.Field{
		{Key: "name", Label: "名称", Type: contenttpl.TypeString, Default: ""},
		{Key: "amp", Label: "振幅", Type: contenttpl.TypeString, Unit: "mm", Default: 4.0},
	})
	// 厂端应列出三份工艺模版。
	if tpls, err := fac.ListProcessTemplates(ctx, op.tok); err != nil || len(tpls) != 3 {
		// 把份数打出来。
		t.Fatalf("process templates %d %v", len(tpls), err)
	}
	// 直属厂根的工作上下文。
	direct := factory.WorkContext{}
	// 读正文并解成对象的小工具。
	body := func(assetID uuid.UUID) map[string]any {
		// 标成辅助函数，失败算到调用方。
		t.Helper()
		// 读正文。
		raw, err := fac.ReadAssetContent(ctx, op.tok, assetID)
		// 读不到就停。
		if err != nil {
			// 把错误打出来。
			t.Fatal(err)
		}
		// 准备接住对象。
		var obj map[string]any
		// 解不开就停。
		if err := json.Unmarshal(raw, &obj); err != nil {
			// 把坏正文打出来。
			t.Fatalf("body %s: %v", raw, err)
		}
		return obj
	}
	// 网页新建包角：故意带上多层作业类型和一个多余键。
	corner, err := fac.CreateFactoryProcess(ctx, op.tok, direct, "包角一号", []byte(`{"kind":"cornerGeometry","name":"包角一号","passes":[{"length":5}],"junk":1}`), store.WeldKindMultilayer)
	// 建不成就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 应标成包角、归到预置包角，适用作业一律记单层。
	if !corner.Corner || corner.TemplateID != contenttpl.SeedTplCorner || corner.WeldKind != store.WeldKindSingle {
		// 把结果打出来。
		t.Fatalf("corner asset %+v", corner)
	}
	// 读回包角正文。
	got := body(corner.ID)
	// 取逐层几何。
	passes, _ := got["passes"].([]any)
	// kind 留着、多余键删掉、层数不变。
	if got["kind"] != contenttpl.CornerKind || got["junk"] != nil || len(passes) != 1 {
		// 把正文打出来。
		t.Fatalf("corner body %v", got)
	}
	// 这一层缺的两项应补默认。
	if pass, _ := passes[0].(map[string]any); pass["flatOffset"] != float64(0) || pass["upwardOffset"] != float64(0) {
		// 把这一层打出来。
		t.Fatalf("corner pass %v", pass)
	}
	// 示教器写的包角正文三项齐全，上送后一个字节都不该变。
	padBody := []byte(`{"kind":"cornerGeometry","name":"平板包角","passes":[{"length":8.0,"flatOffset":6.0,"upwardOffset":1.5}]}`)
	// 走示教器新建入口。
	padCorner, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "平板包角", padBody, uuid.Nil, "", nil)
	// 建不成或没标包角就停。
	if err != nil || !padCorner.Corner || padCorner.TemplateID != contenttpl.SeedTplCorner {
		// 把结果打出来。
		t.Fatalf("pad corner %+v %v", padCorner, err)
	}
	// 读回示教器那条的原文。
	if raw, err := fac.ReadAssetContent(ctx, op.tok, padCorner.ID); err != nil || string(raw) != string(padBody) {
		// 正文被改写了。
		t.Fatalf("pad corner body %s %v", raw, err)
	}
	// 自建种类：正文写了所属身份。
	custom, err := fac.CreateFactoryProcess(ctx, op.tok, direct, "摆动一号", []byte(`{"templateId":"`+customID.String()+`","amp":3,"junk":true}`), store.WeldKindTBar)
	// 建不成就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 归到自建那份，不算包角，适用作业记单层。
	if custom.TemplateID != customID.String() || custom.Corner || custom.WeldKind != store.WeldKindSingle {
		// 把结果打出来。
		t.Fatalf("custom asset %+v", custom)
	}
	// 自建正文：身份留着、振幅保留、多余键删掉。
	if got = body(custom.ID); got["templateId"] != customID.String() || got["amp"] != float64(3) || got["junk"] != nil {
		// 把正文打出来。
		t.Fatalf("custom body %v", got)
	}
	// 焊接参数照旧，可以指定适用作业。
	weld, err := fac.CreateFactoryProcess(ctx, op.tok, direct, "焊接一号", []byte(`{"name":"焊接一号","current":190}`), store.WeldKindMultilayer)
	// 建不成或归属不对就停。
	if err != nil || weld.TemplateID != contenttpl.SeedTplWeld || weld.WeldKind != store.WeldKindMultilayer {
		// 把结果打出来。
		t.Fatalf("weld asset %+v %v", weld, err)
	}
	// 改正文不许换种类：焊接参数改成包角正文被拒。
	if _, err := fac.UpdateAssetContent(ctx, op.tok, weld.ID, weld.Revision, []byte(`{"kind":"cornerGeometry","name":"焊接一号","passes":[{"length":5}]}`)); !errors.Is(err, domain.ErrProcessKindFixed) {
		// 没拦住。
		t.Fatalf("weld to corner: %v", err)
	}
	// 包角去掉标记变成焊接参数也被拒。
	if _, err := fac.UpdateAssetContent(ctx, op.tok, corner.ID, corner.Revision, []byte(`{"name":"包角一号","passes":[{"length":5}]}`)); !errors.Is(err, domain.ErrProcessKindFixed) {
		// 没拦住。
		t.Fatalf("corner to weld: %v", err)
	}
	// 在正文里显式写预置焊接参数的身份再加包角标记，想瞒过种类判定：包角标记说了算，照样被拒。
	if _, err := fac.UpdateAssetContent(ctx, op.tok, weld.ID, weld.Revision, []byte(`{"templateId":"`+contenttpl.SeedTplWeld+`","kind":"cornerGeometry","name":"焊接一号","passes":[{"length":5}]}`)); !errors.Is(err, domain.ErrProcessKindFixed) {
		// 没拦住。
		t.Fatalf("weld to corner by templateId: %v", err)
	}
	// 包角去掉标记、改写预置包角的身份也不行：没有标记就不是包角。
	if _, err := fac.UpdateAssetContent(ctx, op.tok, corner.ID, corner.Revision, []byte(`{"templateId":"`+contenttpl.SeedTplCorner+`","name":"包角一号","passes":[{"length":5}]}`)); !errors.Is(err, domain.ErrProcessKindFixed) {
		// 没拦住。
		t.Fatalf("corner to weld by templateId: %v", err)
	}
	// 示教器盖正文同样不许换种类。
	if _, err := fac.ApplyAppContent(ctx, op.tok, padCorner.ID, []byte(`{"name":"平板包角","current":180}`)); !errors.Is(err, domain.ErrProcessKindFixed) {
		// 没拦住。
		t.Fatalf("pad corner to weld: %v", err)
	}
	// 种类不变的改正文照常能改。
	corner, err = fac.UpdateAssetContent(ctx, op.tok, corner.ID, corner.Revision, []byte(`{"kind":"cornerGeometry","name":"包角一号","passes":[{"length":6,"flatOffset":5,"upwardOffset":0}]}`))
	// 改不成就停。
	if err != nil {
		// 把错误打出来。
		t.Fatalf("corner edit: %v", err)
	}
	// 包角不许改适用作业。
	if _, err := fac.SetAssetWeldKind(ctx, op.tok, corner.ID, corner.Revision, store.WeldKindTBar); !errors.Is(err, domain.ErrProcessKindFixed) {
		// 没拦住。
		t.Fatalf("corner weld kind: %v", err)
	}
	// 自建种类也不许改。
	if _, err := fac.SetAssetWeldKind(ctx, op.tok, custom.ID, custom.Revision, store.WeldKindTBar); !errors.Is(err, domain.ErrProcessKindFixed) {
		// 没拦住。
		t.Fatalf("custom weld kind: %v", err)
	}
	// 焊接参数可以改。
	if weld, err = fac.SetAssetWeldKind(ctx, op.tok, weld.ID, weld.Revision, store.WeldKindSingle); err != nil {
		// 把错误打出来。
		t.Fatalf("weld weld kind: %v", err)
	}
	// 三条都发布，后面要拿来当依赖和下发。
	for _, p := range []*factory.Asset{&weld, &corner, &custom} {
		// 发布这一条。
		pub, err := fac.PublishAsset(ctx, op.tok, p.ID, p.Revision)
		// 发布不了就停。
		if err != nil {
			// 把错误打出来。
			t.Fatalf("publish %s: %v", p.Name, err)
		}
		// 记下发布后的修订。
		*p = pub
	}
	// 一条只引用焊接参数的单层焊道。
	weldLine := []byte(`[{"templateId":"` + contenttpl.SeedTplSingle + `","kind":"single","name":"焊道","processId":"` + weld.ID.String() + `"}]`)
	// 把一条工艺收成依赖声明。
	dep := func(a factory.Asset) factory.AssetDep {
		return factory.AssetDep{ID: a.ID, Revision: a.Revision, Digest: a.Digest}
	}
	// 工程把自建种类声明成依赖被拒。
	if _, err := fac.CreateFactoryProject(ctx, op.tok, direct, "钉自建", weldLine, []factory.AssetDep{dep(weld), dep(custom)}, store.WeldKindSingle); !errors.Is(err, domain.ErrCornerSlotMismatch) {
		// 没拦住。
		t.Fatalf("project custom dep: %v", err)
	}
	// 只依赖焊接参数的工程能建。
	proj, err := fac.CreateFactoryProject(ctx, op.tok, direct, "只钉焊接", weldLine, []factory.AssetDep{dep(weld)}, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 把错误打出来。
		t.Fatalf("project weld dep: %v", err)
	}
	// 事后改依赖把包角加进去也被拒。
	if _, err := fac.SetProjectDeps(ctx, op.tok, proj.ID, proj.Revision, []factory.AssetDep{dep(weld), dep(corner)}); !errors.Is(err, domain.ErrCornerSlotMismatch) {
		// 没拦住。
		t.Fatalf("set deps corner: %v", err)
	}
	// 登示教器，看它拿到的清单。
	sess, err := fac.LoginPad(ctx, "op-k", "op-pass")
	// 登不上就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 读示教器清单。
	box, err := fac.PadClientInbox(ctx, sess.Token)
	// 读不到就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 记下清单里有哪些身份。
	seen := map[uuid.UUID]bool{}
	// 逐条记。
	for _, ref := range box.Closures {
		// 记下这一条。
		seen[ref.AssetID] = true
	}
	// 焊接参数和包角要发给示教器，自建种类不发。
	if !seen[weld.ID] || !seen[corner.ID] || seen[custom.ID] {
		// 把清单打出来。
		t.Fatalf("pad inbox weld=%v corner=%v custom=%v", seen[weld.ID], seen[corner.ID], seen[custom.ID])
	}
	// 云端删了自建种类：按全集撤下，厂端只剩预置两份。
	if err := fac.SyncTemplateSet(ctx, factory.KindProcess, true, []uuid.UUID{weldTplID, cornerTplID}); err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 再列应是两份。
	if tpls, err := fac.ListProcessTemplates(ctx, op.tok); err != nil || len(tpls) != 2 {
		// 把份数打出来。
		t.Fatalf("after retire %d %v", len(tpls), err)
	}
	// 模版撤下后再按这个种类新建，正文原样保存，仍归到那个身份。
	raw, err := fac.CreateFactoryProcess(ctx, op.tok, direct, "摆动二号", []byte(`{"templateId":"`+customID.String()+`","amp":5,"junk":true}`))
	// 建不成或归属不对就停。
	if err != nil || raw.TemplateID != customID.String() {
		// 把结果打出来。
		t.Fatalf("custom without template %+v %v", raw, err)
	}
	// 没有模版就不套，多余键还在。
	if got = body(raw.ID); got["junk"] != true {
		// 把正文打出来。
		t.Fatalf("raw custom body %v", got)
	}
}
