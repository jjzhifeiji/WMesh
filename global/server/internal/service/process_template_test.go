// 工艺模版多份：预置两份、自建种类、按所属模版套用、包角骨架、删除与按原身份补建。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

// 读一条平台工艺的正文并解成对象。
func processBody(t *testing.T, ctx context.Context, h *Harness, tok string, assetID uuid.UUID) map[string]any {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 读正文。
	raw, err := h.WAN.ReadPlatformAssetContent(ctx, tok, assetID)
	// 读不到就停。
	if err != nil {
		// 把读正文的错误打出来。
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

// 工艺模版可以有多份：空库落预置两份，管理员能自建种类；新建工艺按正文所属的那份套用。
func TestProcessTemplates(t *testing.T) {
	// 准备本测上下文。
	ctx := context.Background()
	// 起云端库和服务。
	h := New(t)
	// 立云端超管失败就停。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 立不住后面没有人能登录。
		t.Fatal(err)
	}
	// 登录拿会话。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录失败就停。
	if err != nil {
		// 没有票后面接口都进不去。
		t.Fatal(err)
	}
	// 空库首次列出应是预置两份。
	seeds, err := h.WAN.ListProcessTemplates(ctx, tok)
	// 列不出或不是两份就停。
	if err != nil || len(seeds) != 2 {
		// 把实得份数打出来。
		t.Fatalf("seeds %d %v", len(seeds), err)
	}
	// 预置包角的身份。
	cornerID := uuid.MustParse(contenttpl.SeedTplCorner)
	// 自建一个种类，字段只有名称和振幅。
	custom, err := h.WAN.CreateProcessTemplate(ctx, tok, uuid.Nil, "摆动参数", []byte(`{"root":"object","fields":[{"key":"name","label":"名称","type":"string","default":""},{"key":"amp","label":"振幅","type":"string","unit":"mm","default":4}]}`))
	// 建不成就停。
	if err != nil || custom.Revision != 1 || custom.Kind != global.KindProcess {
		// 把结果打出来。
		t.Fatalf("create custom %+v %v", custom, err)
	}
	// 同名再建应被拒。
	if _, err := h.WAN.CreateProcessTemplate(ctx, tok, uuid.Nil, "摆动参数", nil); !errors.Is(err, domain.ErrTemplateInvalid) {
		// 同名没拦住。
		t.Fatalf("dup name: %v", err)
	}
	// 自己指定一个非预置身份应被拒。
	if _, err := h.WAN.CreateProcessTemplate(ctx, tok, id.New(), "乱指身份", nil); !errors.Is(err, domain.ErrTemplateInvalid) {
		// 非预置身份没拦住。
		t.Fatalf("free id: %v", err)
	}
	// 模版顶层占用保留键应被拒。
	if _, err := h.WAN.CreateProcessTemplate(ctx, tok, uuid.Nil, "占保留键", []byte(`{"root":"object","fields":[{"key":"kind","label":"k","type":"string","default":""}]}`)); !errors.Is(err, domain.ErrTemplateInvalid) {
		// 保留键没拦住。
		t.Fatalf("reserved key: %v", err)
	}
	// 按自建种类新建工艺：正文写了所属身份，多余的键会被删。
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "摆动一号", []byte(`{"templateId":"`+custom.ID.String()+`","amp":3,"junk":true}`))
	// 建不成就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 所属模版应记成自建那份，且不算包角。
	if proc.TemplateID != custom.ID.String() || proc.Corner {
		// 归属不对。
		t.Fatalf("custom asset %+v", proc)
	}
	// 读回正文核对。
	body := processBody(t, ctx, h, tok, proc.ID)
	// 所属身份要留着，振幅保留，名称补默认，多余键删掉。
	if body["templateId"] != custom.ID.String() || body["amp"] != float64(3) || body["name"] != "" || body["junk"] != nil {
		// 把正文打出来。
		t.Fatalf("custom body %v", body)
	}
	// 新建包角：按预置包角套，kind 要留住，缺的层参数补默认。
	corner, err := h.WAN.CreatePlatformProcess(ctx, tok, "包角一号", []byte(`{"kind":"cornerGeometry","name":"c","passes":[{"length":5}],"junk":1}`))
	// 建不成就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 应标成包角并归到预置包角。
	if !corner.Corner || corner.TemplateID != contenttpl.SeedTplCorner {
		// 归属不对。
		t.Fatalf("corner asset %+v", corner)
	}
	// 读回正文核对。
	body = processBody(t, ctx, h, tok, corner.ID)
	// 取出逐层几何。
	passes, _ := body["passes"].([]any)
	// kind 留着、多余键删掉、只有一层。
	if body["kind"] != contenttpl.CornerKind || body["junk"] != nil || body["templateId"] != nil || len(passes) != 1 {
		// 把正文打出来。
		t.Fatalf("corner body %v", body)
	}
	// 这一层缺的两项应补上默认。
	pass, _ := passes[0].(map[string]any)
	// 长度保留，另两项补零。
	if pass["length"] != float64(5) || pass["flatOffset"] != float64(0) || pass["upwardOffset"] != float64(0) {
		// 把这一层打出来。
		t.Fatalf("corner pass %v", pass)
	}
	// 焊接参数照旧：不写所属身份，归到预置焊接参数。
	weld, err := h.WAN.CreatePlatformProcess(ctx, tok, "焊接一号", []byte(`{"name":"w","current":190}`))
	// 建不成或归属不对就停。
	if err != nil || weld.TemplateID != contenttpl.SeedTplWeld || weld.Corner {
		// 把结果打出来。
		t.Fatalf("weld asset %+v %v", weld, err)
	}
	// 焊接正文里不应多出所属身份或包角标记。
	if body = processBody(t, ctx, h, tok, weld.ID); body["templateId"] != nil || body["kind"] != nil {
		// 把正文打出来。
		t.Fatalf("weld body %v", body)
	}
	// 改正文不许换种类：焊接参数改成包角正文被拒。
	if _, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, weld.ID, weld.Revision, []byte(`{"kind":"cornerGeometry","name":"w","passes":[{"length":5}]}`)); !errors.Is(err, domain.ErrProcessKindFixed) {
		// 没拦住。
		t.Fatalf("weld to corner: %v", err)
	}
	// 包角去掉标记变成焊接参数也被拒。
	if _, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, corner.ID, corner.Revision, []byte(`{"name":"c","passes":[{"length":5}]}`)); !errors.Is(err, domain.ErrProcessKindFixed) {
		// 没拦住。
		t.Fatalf("corner to weld: %v", err)
	}
	// 在正文里显式写预置焊接参数的身份再加包角标记，想瞒过种类判定：包角标记说了算，照样被拒。
	if _, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, weld.ID, weld.Revision, []byte(`{"templateId":"`+contenttpl.SeedTplWeld+`","kind":"cornerGeometry","name":"w","passes":[{"length":5}]}`)); !errors.Is(err, domain.ErrProcessKindFixed) {
		// 没拦住。
		t.Fatalf("weld to corner by templateId: %v", err)
	}
	// 包角去掉标记、改写预置包角的身份也不行：没有标记就不是包角。
	if _, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, corner.ID, corner.Revision, []byte(`{"templateId":"`+contenttpl.SeedTplCorner+`","name":"c","passes":[{"length":5}]}`)); !errors.Is(err, domain.ErrProcessKindFixed) {
		// 没拦住。
		t.Fatalf("corner to weld by templateId: %v", err)
	}
	// 包角和自建种类不许改适用作业。
	for _, a := range []global.Asset{corner, proc} {
		// 改成 T 排应被拒。
		if _, err := h.WAN.SetPlatformWeldKind(ctx, tok, a.ID, a.Revision, "tbar"); !errors.Is(err, domain.ErrProcessKindFixed) {
			// 没拦住。
			t.Fatalf("weld kind of %s: %v", a.Name, err)
		}
	}
	// 焊接参数可以改适用作业。
	if _, err := h.WAN.SetPlatformWeldKind(ctx, tok, weld.ID, weld.Revision, "multilayer"); err != nil {
		// 把错误打出来。
		t.Fatalf("weld kind of weld: %v", err)
	}
	// 取预置包角当前修订。
	var cornerTpl global.ContentTemplate
	// 在预置两份里找包角。
	for _, s := range seeds {
		// 身份对上就记下。
		if s.ID == cornerID {
			// 记下这一份。
			cornerTpl = s
		}
	}
	// 预置包角的骨架不许改掉：去掉逐层几何应被拒。
	if _, err := h.WAN.UpdateProcessTemplate(ctx, tok, cornerID, cornerTpl.Revision, "包角", []byte(`{"root":"object","fields":[{"key":"name","label":"名称","type":"string","default":""}]}`)); !errors.Is(err, domain.ErrTemplateInvalid) {
		// 骨架没拦住。
		t.Fatalf("corner skeleton: %v", err)
	}
	// 在骨架之外加字段是允许的。
	grown, err := h.WAN.UpdateProcessTemplate(ctx, tok, cornerID, cornerTpl.Revision, "包角", []byte(`{"root":"object","fields":[{"key":"name","label":"名称","type":"string","default":""},{"key":"note","label":"备注","type":"string","default":"无"},{"key":"passes","label":"逐层几何","type":"array","items":{"key":"pass","label":"包角层","type":"object","fields":[{"key":"length","label":"焊道长度","type":"string","default":0},{"key":"flatOffset","label":"平焊偏移","type":"string","default":0},{"key":"upwardOffset","label":"立焊偏移","type":"string","default":0}]}}]}`))
	// 改不成或修订没升就停。
	if err != nil || grown.Revision != cornerTpl.Revision+1 {
		// 把结果打出来。
		t.Fatalf("grow corner %+v %v", grown, err)
	}
	// 下发给厂的模版里应带上全部工艺模版：预置两份加自建一份。
	snaps, err := h.WAN.SnapshotsForFactory(ctx, id.New())
	// 组不出就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 数一数工艺模版份数。
	procTpls := 0
	// 逐份看种类。
	for _, s := range snaps {
		// 是工艺就计数。
		if s.Kind == global.KindProcess {
			// 多一份。
			procTpls++
		}
	}
	// 应是三份。
	if procTpls != 3 {
		// 把份数打出来。
		t.Fatalf("process snapshots %d", procTpls)
	}
	// 预置的也能删。
	if err := h.WAN.DeleteProcessTemplate(ctx, tok, cornerID); err != nil {
		// 删不掉就停。
		t.Fatal(err)
	}
	// 删后再列只剩两份，不会补回。
	if left, err := h.WAN.ListProcessTemplates(ctx, tok); err != nil || len(left) != 2 {
		// 把份数打出来。
		t.Fatalf("after delete %d %v", len(left), err)
	}
	// 包角模版没了，新建包角照样能建，正文原样留着，归属仍是预置包角的身份。
	raw, err := h.WAN.CreatePlatformProcess(ctx, tok, "包角二号", []byte(`{"kind":"cornerGeometry","name":"d","passes":[{"length":6}],"junk":1}`))
	// 建不成或归属不对就停。
	if err != nil || raw.TemplateID != contenttpl.SeedTplCorner || !raw.Corner {
		// 把结果打出来。
		t.Fatalf("corner without template %+v %v", raw, err)
	}
	// 没有模版就不套，多余键还在。
	if body = processBody(t, ctx, h, tok, raw.ID); body["junk"] != float64(1) {
		// 把正文打出来。
		t.Fatalf("raw corner body %v", body)
	}
	// 按原身份补建，修订要接在删除前之后，厂端才不会当成旧副本。
	back, err := h.WAN.CreateProcessTemplate(ctx, tok, cornerID, "", nil)
	// 补建不成或修订没接上就停。
	if err != nil || back.ID != cornerID || back.Revision != grown.Revision+1 || back.Name != "包角" {
		// 把结果打出来。
		t.Fatalf("recreate corner %+v %v", back, err)
	}
	// 未登录不能改模版。
	if _, err := h.WAN.CreateProcessTemplate(ctx, "bad", uuid.Nil, "x", nil); !errors.Is(err, domain.ErrUnauthorized) && !errors.Is(err, domain.ErrInvalidCredentials) {
		// 没拦住未登录。
		t.Fatalf("anon: %v", err)
	}
}
