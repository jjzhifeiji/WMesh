// 包角工艺和焊接工艺分开选：列表带包角标记，工程任何工艺槽（含包角组）挂包角工艺都拒绝保存。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 单层焊道：焊道工艺只认焊接参数，包角组也只认焊接参数（和焊道同一条底工艺）；网页新建和改正文都拦。
func TestCornerSlotBinding(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa-a", "超管A")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa-a", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa-a", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	op := mustCreateRole(t, ctx, fac, sa, "op-a", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{Direct: true}
	// 建一条厂级工艺并发布，工程才钉得上。
	mk := func(name string, body []byte) factory.Asset {
		// 标成辅助函数，失败算到调用方。
		t.Helper()
		// 新建厂级工艺。
		p, err := fac.CreateFactoryProcess(ctx, op.tok, direct, name, body)
		// 建工艺失败就停。
		if err != nil {
			// 把建工艺的错误打出来并停住。
			t.Fatalf("create %s: %v", name, err)
		}
		// 发布当前修订。
		p, err = fac.PublishAsset(ctx, op.tok, p.ID, p.Revision)
		// 发布失败就停。
		if err != nil {
			// 把发布的错误打出来并停住。
			t.Fatalf("publish %s: %v", name, err)
		}
		return p
	}
	// 焊接参数工艺。
	weld := mk("焊接", []byte(`{"current":180}`))
	// 示教器写的包角工艺。
	corner := mk("包角", []byte(`{"kind":"cornerGeometry","name":"包角","passes":[{"length":5}]}`))
	// 工艺列表要带上包角标记，网页靠它分开选。
	rows, err := fac.ListAssets(ctx, op.tok, factory.KindProcess)
	// 列不出就停。
	if err != nil {
		// 把列表的错误打出来并停住。
		t.Fatal(err)
	}
	// 按身份记下列表里的标记。
	listed := map[uuid.UUID]bool{}
	// 逐行记下。
	for _, r := range rows {
		// 记下这一行的包角标记。
		listed[r.ID] = r.Corner
	}
	// 标记不对就停。
	if listed[weld.ID] || !listed[corner.ID] {
		// 列表没分开包角和焊接参数。
		t.Fatalf("list corner %v", listed)
	}
	// 按焊道工艺和包角组工艺拼一条单层焊道。
	body := func(proc, groupProc uuid.UUID) []byte {
		// 拼出正文。
		return []byte(`[{"templateId":"` + contenttpl.SeedTplSingle + `","kind":"single","name":"焊道","processId":"` + proc.String() +
			`","cornerGroupParams":{"groupId":"g","isMaster":true,"processId":"` + groupProc.String() + `"}}]`)
	}
	// 两条工艺都钉上，改正文时引用才落在依赖里。
	deps := []factory.AssetDep{{ID: weld.ID, Revision: weld.Revision, Digest: weld.Digest}, {ID: corner.ID, Revision: corner.Revision, Digest: corner.Digest}}
	// 示教器存法：包角组和焊道挂同一条焊接参数，能建。
	proj, err := fac.CreateFactoryProject(ctx, op.tok, direct, "挂对", body(weld.ID, weld.ID), deps, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 示教器存法的工程被拒了。
		t.Fatalf("right binding: %v", err)
	}
	// 焊道挂包角，新建就要拒。
	if _, err := fac.CreateFactoryProject(ctx, op.tok, direct, "焊道挂包角", body(corner.ID, weld.ID), deps, store.WeldKindSingle); !errors.Is(err, domain.ErrCornerSlotMismatch) {
		// 新建没拦住挂错。
		t.Fatalf("create weld slot corner: %v", err)
	}
	// 改正文时焊道、包角组任一处挂包角都要拒。
	for name, raw := range map[string][]byte{"都挂包角": body(corner.ID, corner.ID), "包角组挂包角": body(weld.ID, corner.ID), "焊道挂包角": body(corner.ID, weld.ID)} {
		// 改正文要拒。
		if _, err := fac.UpdateAssetContent(ctx, sa, proj.ID, proj.Revision, raw); !errors.Is(err, domain.ErrCornerSlotMismatch) {
			// 改正文没拦住挂错。
			t.Fatalf("update %s: %v", name, err)
		}
	}
	// 示教器存法的正文照样能改。
	if _, err := fac.UpdateAssetContent(ctx, sa, proj.ID, proj.Revision, body(weld.ID, weld.ID)); err != nil {
		// 示教器存法的改正文被拒了。
		t.Fatalf("update right: %v", err)
	}
}
