// 再升档覆盖平台工艺时也守作业类型在用规则：有平台工程钉着就不许借升档换类型。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 升上来的平台工艺发布后被平台工程钉着，同源换了作业类型再升必须按在用拒绝，平台工艺原样不动。
func TestPlatformRepromoteWeldKindInUse(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立超管并登录，自定义模版这里用不上。
	tok, _ := weldRefWAN(t, ctx, h)
	// 登记工厂「厂A」，快照要挂在这家厂名下。
	fac, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂失败就停，建不成后面没有厂可升档。
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
	// 发布成可用，草稿不能被工程钉。
	pub, err := h.WAN.PublishPlatformAsset(ctx, tok, first.ID, first.Revision)
	// 发布失败就停，后面没有可钉的工艺。
	if err != nil {
		// 不符即停：发布升档工艺失败。
		t.Fatal(err)
	}
	// 准备单层模版正文，和钉住的工艺同类型。
	singleBody := []byte(`[{"templateId":"` + contenttpl.SeedTplSingle + `","name":"单"}]`)
	// 按当前修订钉住升上来的工艺。
	deps := []global.AssetDep{{ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest}}
	// 建单层平台工程钉住它，失败则没有引用可验。
	if proj, err := h.WAN.CreatePlatformProject(ctx, tok, "钉升档工艺", singleBody, deps, store.WeldKindSingle); err != nil || len(proj.Deps) != 1 {
		// 不符即停：工程没钉上升档工艺，后面验不了拦截。
		t.Fatalf("pin promoted %+v %v", proj, err)
	}
	// 同源换成 T 排再升，会把被钉着的工艺换类型，必须按在用拒绝。
	if _, err := h.WAN.PromoteFromSnapshot(ctx, tok, snapOf(store.WeldKindTBar, 2)); !errors.Is(err, domain.ErrWeldKindInUse) {
		// 不符即停：放行了说明钉着它的平台工程会和它类型对不上。
		t.Fatalf("repromote in use: %v", err)
	}
	// 读回平台工艺，看拒绝后类型、修订和状态是否原样。
	kept, err := h.WAN.GetPlatformAsset(ctx, tok, pub.ID)
	// 读失败、类型变了、修订升了或被打回草稿就停。
	if err != nil || kept.WeldKind != store.WeldKindSingle || kept.Revision != pub.Revision || kept.Status != global.AssetAvailable {
		// 不符即停：被拒的再升还是覆盖了平台工艺。
		t.Fatalf("after reject %+v %v", kept, err)
	}
}
