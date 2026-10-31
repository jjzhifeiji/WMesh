// 再升档只另起一份厂级草稿，不回写已被工程钉着的那份，作业类型也不受牵连。
package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contenttpl"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 个人工艺升厂级后被厂级工程钉着，源改了作业类型再升应另起新稿，被钉的厂级条目原样不动。
func TestFactoryRepromoteLeavesPinnedCopy(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 开厂建人，自定义模版这里用不上。
	fac, pe, _ := weldRefFactory(t, ctx)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{}
	// 准备单层正文，厂级工程按单层建。
	singleBody := []byte(`[{"templateId":"` + contenttpl.SeedTplSingle + `","name":"单"}]`)
	// 新建一份个人级单层工艺，先升一次厂级。
	proc, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "个人工艺", []byte(`{"name":"p"}`))
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 第一次升厂级，复制出新身份的草稿。
	first, err := fac.PromoteToFactory(ctx, pe.tok, proc.ID, uuid.Nil)
	// 升档失败或作业类型不是单层就停。
	if err != nil || first.WeldKind != store.WeldKindSingle || first.Status != factory.AssetDraft {
		// 第一次升档就没带上作业类型或没落成草稿。
		t.Fatalf("first promote %+v %v", first, err)
	}
	// 草稿不能被钉，先发布升出来的厂级工艺。
	first, err = fac.PublishAsset(ctx, pe.tok, first.ID, first.Revision)
	// 发布失败就停，后面钉不上。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 厂级单层工程钉住升出来的厂级工艺。
	deps := []factory.AssetDep{{ID: first.ID, Revision: first.Revision, Digest: first.Digest}}
	// 建工程失败或依赖没钉上就停。
	if proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "钉厂级工艺", singleBody, deps, store.WeldKindSingle); err != nil || len(proj.Deps) != 1 {
		// 工程没钉上厂级工艺，后面验不了。
		t.Fatalf("pin promoted %+v %v", proj, err)
	}
	// 源本身没人钉，改成 T 排照常成功。
	proc, err = fac.SetAssetWeldKind(ctx, pe.tok, proc.ID, proc.Revision, store.WeldKindTBar)
	// 改焊类失败或没改成就停。
	if err != nil || proc.WeldKind != store.WeldKindTBar {
		// 源改类型被误拦，后面复现不了。
		t.Fatalf("source to tbar %+v %v", proc, err)
	}
	// 再升应另起一份 T 排厂级草稿，不碰被钉着的那份。
	second, err := fac.PromoteToFactory(ctx, pe.tok, proc.ID, uuid.Nil)
	// 再升失败、撞了旧身份或类型不对就停。
	if err != nil || second.ID == first.ID || second.WeldKind != store.WeldKindTBar || second.Status != factory.AssetDraft {
		// 再升没另起新稿，说明还在回写旧稿。
		t.Fatalf("repromote %+v %v", second, err)
	}
	// 读回第一份厂级工艺，看类型和修订是否原样。
	kept, err := fac.GetAsset(ctx, pe.tok, first.ID)
	// 读失败、类型变了或修订升了就停。
	if err != nil || kept.WeldKind != store.WeldKindSingle || kept.Revision != first.Revision {
		// 再升还是改动了被钉着的厂级。
		t.Fatalf("after repromote %+v %v", kept, err)
	}
}
