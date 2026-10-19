// 再升档覆盖厂级工艺时也守作业类型在用规则：有工程钉着就不许借升档换类型。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 个人工艺升厂级后被厂级工程钉着，源改了作业类型再升必须按在用拒绝，厂级条目原样不动。
func TestFactoryRepromoteWeldKindInUse(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 开厂建人，自定义模版这里用不上。
	fac, pe, _ := weldRefFactory(t, ctx)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}
	// 准备单层正文，厂级工程按单层建。
	singleBody := []byte(`[{"templateId":"` + contenttpl.SeedTplSingle + `","name":"单"}]`)
	// 新建一份个人级单层工艺，先升一次厂级。
	proc, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "个人工艺", []byte(`{"name":"p"}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 第一次升厂级，复制出新身份。
	first, err := fac.PromoteToFactory(ctx, pe.tok, proc.ID)
	// 升档失败或作业类型不是单层就停。
	if err != nil || first.WeldKind != store.WeldKindSingle {
		// 第一次升档就没带上作业类型。
		t.Fatalf("first promote %+v %v", first, err)
	}
	// 厂级单层工程钉住升出来的厂级工艺。
	deps := []factory.AssetDep{{ID: first.ID, Revision: first.Revision, Digest: first.Digest}}
	// 建工程失败或依赖没钉上就停。
	if proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "钉厂级工艺", singleBody, deps, store.WeldKindSingle); err != nil || len(proj.Deps) != 1 {
		// 工程没钉上厂级工艺，后面验不了拦截。
		t.Fatalf("pin promoted %+v %v", proj, err)
	}
	// 源本身没人钉，改成 T 排照常成功。
	proc, err = fac.SetAssetWeldKind(ctx, pe.tok, proc.ID, proc.Revision, store.WeldKindTBar)
	// 改焊类失败或没改成就停。
	if err != nil || proc.WeldKind != store.WeldKindTBar {
		// 源改类型被误拦，后面复现不了。
		t.Fatalf("source to tbar %+v %v", proc, err)
	}
	// 再升会把 T 排写到被钉着的厂级工艺上，必须按在用拒绝。
	if _, err := fac.PromoteToFactory(ctx, pe.tok, proc.ID); !errors.Is(err, domain.ErrWeldKindInUse) {
		// 放行了说明钉着它的工程会和它类型对不上。
		t.Fatalf("repromote in use: %v", err)
	}
	// 读回厂级工艺，看拒绝后类型和修订是否原样。
	kept, err := fac.GetAsset(ctx, pe.tok, first.ID)
	// 读失败、类型变了或修订升了就停。
	if err != nil || kept.WeldKind != store.WeldKindSingle || kept.Revision != first.Revision {
		// 被拒的再升还是覆盖了厂级。
		t.Fatalf("after reject %+v %v", kept, err)
	}
}
