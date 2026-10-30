// 包角工艺和焊接工艺分开选：列表带包角标记，工程任何工艺槽（含包角组）挂包角工艺都拒绝保存。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	global "wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 单层焊道：焊道工艺只认焊接参数，包角组也只认焊接参数（和焊道同一条底工艺）；新建和改正文都拦。
func TestCornerSlotBinding(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 固定云端口令。
	const wanPass = "wan-secret"
	// 立云端超管失败就停。
	if err := h.WAN.BootstrapAdmin(ctx, "w", wanPass); err != nil {
		// 立不住后面没有人能登录。
		t.Fatal(err)
	}
	// 登录拿会话。
	tok, err := h.WAN.Login(ctx, "w", wanPass)
	// 登录失败就停。
	if err != nil {
		// 没有票后面接口都进不去。
		t.Fatal(err)
	}
	// 焊接参数工艺。
	weld, err := h.WAN.CreatePlatformProcess(ctx, tok, "焊接", []byte(`{"current":180}`))
	// 建不成就停。
	if err != nil {
		// 把建工艺的错误打出来并停住。
		t.Fatal(err)
	}
	// 网页新建会按焊接参数模版套正文，包角只能从示教器升上来；这里直接写库摆一条包角正文。
	cornerBody := []byte(`{"kind":"cornerGeometry","name":"包角","passes":[{"length":5}]}`)
	// 先建一条占位工艺。
	corner, err := h.WAN.CreatePlatformProcess(ctx, tok, "包角", []byte(`{}`))
	// 建不成就停。
	if err != nil {
		// 把建工艺的错误打出来并停住。
		t.Fatal(err)
	}
	// 走存储层改成包角正文，包角标记随正文算。
	corner, err = store.Open(h.DB).UpdateAsset(ctx, corner.ID, corner.Revision, store.AssetWrite{
		Name: corner.Name, Content: cornerBody, Digest: digest.Sum(cornerBody), Copyable: corner.Copyable, Status: corner.Status,
	})
	// 改不了或没认出包角就停。
	if err != nil || !corner.Corner {
		// 存储层没按正文定包角。
		t.Fatalf("corner write %+v %v", corner, err)
	}
	// 两条都发布，工程才钉得上。
	for _, p := range []*global.Asset{&weld, &corner} {
		// 发布当前修订。
		got, err := h.WAN.PublishPlatformAsset(ctx, tok, p.ID, p.Revision)
		// 发布失败就停。
		if err != nil {
			// 把发布的错误打出来并停住。
			t.Fatalf("publish %s: %v", p.Name, err)
		}
		// 换成发布后的行。
		*p = got
	}
	// 工艺列表要带上包角标记，网页靠它分开选。
	rows, err := h.WAN.ListPlatformAssets(ctx, tok, global.KindProcess)
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
	deps := []global.AssetDep{{ID: weld.ID, Revision: weld.Revision, Digest: weld.Digest}, {ID: corner.ID, Revision: corner.Revision, Digest: corner.Digest}}
	// 示教器存法：包角组和焊道挂同一条焊接参数，能建。
	proj, err := h.WAN.CreatePlatformProject(ctx, tok, "挂对", body(weld.ID, weld.ID), deps)
	// 建不成就停。
	if err != nil {
		// 示教器存法的工程被拒了。
		t.Fatalf("right binding: %v", err)
	}
	// 焊道挂包角，新建就要拒。
	if _, err := h.WAN.CreatePlatformProject(ctx, tok, "焊道挂包角", body(corner.ID, weld.ID), deps); !errors.Is(err, domain.ErrCornerSlotMismatch) {
		// 新建没拦住挂错。
		t.Fatalf("create weld slot corner: %v", err)
	}
	// 改正文时焊道、包角组任一处挂包角都要拒。
	for name, raw := range map[string][]byte{"都挂包角": body(corner.ID, corner.ID), "包角组挂包角": body(weld.ID, corner.ID), "焊道挂包角": body(corner.ID, weld.ID)} {
		// 改正文要拒。
		if _, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, proj.ID, proj.Revision, raw); !errors.Is(err, domain.ErrCornerSlotMismatch) {
			// 改正文没拦住挂错。
			t.Fatalf("update %s: %v", name, err)
		}
	}
	// 示教器存法的正文照样能改。
	if _, err := h.WAN.UpdatePlatformAssetContent(ctx, tok, proj.ID, proj.Revision, body(weld.ID, weld.ID)); err != nil {
		// 示教器存法的改正文被拒了。
		t.Fatalf("update right: %v", err)
	}
}
