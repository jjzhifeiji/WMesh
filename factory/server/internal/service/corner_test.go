// 包角工艺和焊接工艺分开选：列表带包角标记，工程任何工艺槽（含包角组）挂包角工艺都拒绝保存。
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
	"wmesh/factory/internal/store"
)

// 单层焊道：焊道工艺只认焊接参数，包角组也只认焊接参数（和焊道同一条底工艺）；网页新建和改正文都拦。
func TestCornerSlotBinding(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
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
	op := mustCreateRole(t, ctx, fac, sa, "op-a", "op-pass", factory.RoleOrgAdmin)
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{}
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
	// 正文没引用包角，但把包角声明成额外依赖也不行：工程只能依赖焊接参数。
	if _, err := fac.CreateFactoryProject(ctx, op.tok, direct, "多钉包角", body(weld.ID, weld.ID), deps, store.WeldKindSingle); !errors.Is(err, domain.ErrCornerSlotMismatch) {
		// 额外依赖里的包角没拦住。
		t.Fatalf("extra corner dep: %v", err)
	}
	// 只依赖焊接参数的工程能建。
	proj, err := fac.CreateFactoryProject(ctx, op.tok, direct, "挂对", body(weld.ID, weld.ID), deps[:1], store.WeldKindSingle)
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

// 示教器新建和盖写工程同样拦包角挂槽；收过焊接工艺模版后网页新建包角工艺正文也原样保留。
func TestCornerPadEntriesAndTemplate(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa-c", "超管C")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，建人要用。
	sa := mustLogin(t, ctx, fac, "sa-c", personPass("sa-c"))
	// 建管理员，网页和示教器都用他。
	op := mustCreateRole(t, ctx, fac, sa, "op-c", "op-pass", factory.RoleOrgAdmin)
	// 编出默认焊接工艺字段表，模拟 WAN 已下发。
	schema, err := contenttpl.Marshal(contenttpl.Default(contenttpl.KindProcess))
	// 编不出就停。
	if err != nil {
		// 把编码错误打出来并停住。
		t.Fatal(err)
	}
	// 留下工厂编号，下发要指明接收厂。
	fid := seed.ID
	// 收下焊接工艺模版，之后新建工艺都会按它套。
	if err := fac.AcceptTemplateDelivery(ctx, factory.TemplateSnapshot{
		// 新取一个模版身份，套默认焊接字段表。
		ID: weldTplID, Kind: factory.KindProcess, Name: "焊接参数", Revision: 1, Schema: schema, Digest: digest.Sum(schema), TargetFactoryID: &fid,
	}); err != nil {
		// 收不下模版就停。
		t.Fatal(err)
	}
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{}
	// 包角正文：顶层 kind 和 layers 不在焊接模版里。
	cornerBody := []byte(`{"kind":"cornerGeometry","name":"包角","layers":[{"length":5}]}`)
	// 网页新建包角工艺。
	corner, err := fac.CreateFactoryProcess(ctx, op.tok, direct, "包角", cornerBody)
	// 建不成就停。
	if err != nil {
		// 把建工艺的错误打出来并停住。
		t.Fatal(err)
	}
	// 读回正文，应与提交的一字不差。
	got, err := fac.ReadAssetContent(ctx, op.tok, corner.ID)
	// 正文被模版改写或读不出就停。
	if err != nil || string(got) != string(cornerBody) {
		// 包角正文被洗成焊接参数，D-05 没修好。
		t.Fatalf("corner body %s err %v", got, err)
	}
	// 发布包角工艺，工程才钉得上。
	corner, err = fac.PublishAsset(ctx, op.tok, corner.ID, corner.Revision)
	// 发布失败或没标成包角就停。
	if err != nil || !corner.Corner {
		// 没标成包角，网页选工艺时分不开。
		t.Fatalf("publish corner %+v %v", corner, err)
	}
	// 新建焊接参数工艺。
	weld, err := fac.CreateFactoryProcess(ctx, op.tok, direct, "焊接", []byte(`{"current":180}`))
	// 建不成就停。
	if err != nil {
		// 把建工艺的错误打出来并停住。
		t.Fatal(err)
	}
	// 发布焊接工艺，工程才钉得上。
	weld, err = fac.PublishAsset(ctx, op.tok, weld.ID, weld.Revision)
	// 发布失败就停。
	if err != nil {
		// 把发布的错误打出来并停住。
		t.Fatal(err)
	}
	// 按焊道工艺和包角组工艺拼一条单层焊道。
	body := func(proc, groupProc uuid.UUID) []byte {
		// 拼出正文。
		return []byte(`[{"templateId":"` + contenttpl.SeedTplSingle + `","kind":"single","name":"焊道","processId":"` + proc.String() +
			`","cornerGroupParams":{"groupId":"g","isMaster":true,"processId":"` + groupProc.String() + `"}}]`)
	}
	// 示教器新建工程，焊道挂包角应拒。
	if _, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProject, "挂错", body(corner.ID, weld.ID), uuid.Nil, "", nil, store.WeldKindSingle); !errors.Is(err, domain.ErrCornerSlotMismatch) {
		// 示教器入口没拦住挂错。
		t.Fatalf("pad create corner slot: %v", err)
	}
	// 示教器新建工程，挂对的能建。
	proj, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProject, "挂对", body(weld.ID, weld.ID), uuid.Nil, "", nil, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 示教器存法的工程被拒了。
		t.Fatalf("pad create right: %v", err)
	}
	// 示教器盖写成包角组挂包角，应拒。
	if _, err := fac.ApplyAppContent(ctx, op.tok, proj.ID, body(weld.ID, corner.ID)); !errors.Is(err, domain.ErrCornerSlotMismatch) {
		// 示教器盖写入口没拦住挂错。
		t.Fatalf("pad apply corner slot: %v", err)
	}
}

// 包角工艺和普通工艺同一套可复制口径：平台不可复制的两种都不给看、不给另存、不给示教器盖，拉包都标不可复制；可复制的包角照常看、另存、盖写；示教器可建、可盖个人包角。
func TestCornerFollowsCopyable(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa-k", "超管K")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，建人要用。
	sa := mustLogin(t, ctx, fac, "sa-k", personPass("sa-k"))
	// 建管理员，厂级盖写和另存权限都够，拒绝只能来自可复制口径。
	op := mustCreateRole(t, ctx, fac, sa, "op-k", "op-pass", factory.RoleOrgAdmin)
	// 留下工厂编号，下发要指明接收厂。
	fid := seed.ID
	// 包角正文。
	cornerBody := []byte(`{"kind":"cornerGeometry","name":"包角","passes":[{"length":5}]}`)
	// 普通焊接参数正文。
	weldBody := []byte(`{"current":180}`)
	// 按正文和可复制标记收一条平台工艺。
	deliver := func(name string, body []byte, copyable bool) factory.ClosureMember {
		// 标成辅助函数，失败算到调用方。
		t.Helper()
		// 组装平台工艺成员。
		m := factory.ClosureMember{
			ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: name,
			Status: factory.AssetAvailable, Copyable: copyable, Revision: 1, Content: body, Digest: digest.Sum(body),
		}
		// 收不下就停。
		if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
			// 把收平台稿的错误打出来并停住。
			t.Fatalf("deliver %s: %v", name, err)
		}
		return m
	}
	// 平台加密的包角和普通工艺。
	locked := map[string]factory.ClosureMember{
		"包角": deliver("加密包角", cornerBody, false),
		"普通": deliver("加密焊接", weldBody, false),
	}
	// 两种加密工艺逐条走同一套核对，结果必须一样。
	for name, m := range locked {
		// 厂端读正文要拒。
		if _, err := fac.ReadAssetContent(ctx, op.tok, m.ID); !errors.Is(err, domain.ErrForbidden) {
			// 加密件参数露给厂端。
			t.Fatalf("%s read: %v", name, err)
		}
		// 另存要按不可复制拒。
		if _, err := fac.CopyProcess(ctx, op.tok, m.ID, name+"副本"); !errors.Is(err, domain.ErrAssetNotCopyable) {
			// 加密件能另存出明文副本。
			t.Fatalf("%s copy: %v", name, err)
		}
		// 示教器盖写要拒。
		if _, err := fac.ApplyAppContent(ctx, op.tok, m.ID, weldBody); !errors.Is(err, domain.ErrForbidden) {
			// 示教器能改加密件。
			t.Fatalf("%s apply: %v", name, err)
		}
		// 示教器拉包应能拉到，且标不可复制，示教器据此不给看不给改。
		got, err := fac.PadPullClientClosure(ctx, op.tok, m.ID)
		// 拉不到或没标不可复制就停。
		if err != nil || got.Snapshot.Copyable {
			// 拉包口径和普通加密件不一致。
			t.Fatalf("%s pull copyable=%v err %v", name, got.Snapshot.Copyable, err)
		}
	}

	// 平台不加密的包角。
	open := deliver("明文包角", cornerBody, true)
	// 厂端能读正文，且原样不被焊接模版改写。
	got, err := fac.ReadAssetContent(ctx, op.tok, open.ID)
	// 读不出或被改写就停。
	if err != nil || string(got) != string(cornerBody) {
		// 明文包角被误拦或被改写。
		t.Fatalf("open read %s %v", got, err)
	}
	// 另存成本厂厂级，照常带包角标记。
	cp, err := fac.CopyProcess(ctx, op.tok, open.ID, "明文包角副本")
	// 另存失败或没标包角就停。
	if err != nil || !cp.Corner {
		// 明文包角另存被误拦。
		t.Fatalf("open copy %+v %v", cp, err)
	}
	// 示教器盖写本厂副本照常。
	if _, err := fac.ApplyAppContent(ctx, op.tok, cp.ID, []byte(`{"kind":"cornerGeometry","name":"包角","passes":[{"length":6}]}`)); err != nil {
		// 示教器盖明文包角被误拦。
		t.Fatalf("open apply: %v", err)
	}

	// 示教器新建个人包角照常。
	mine, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "平板包角", cornerBody, uuid.Nil, "", nil)
	// 建不成或没标包角就停。
	if err != nil || !mine.Corner {
		// 示教器建包角被误拦。
		t.Fatalf("pad create corner %+v %v", mine, err)
	}
	// 示教器盖自己的包角照常。
	if _, err := fac.ApplyAppContent(ctx, op.tok, mine.ID, []byte(`{"kind":"cornerGeometry","name":"包角","passes":[{"length":7}]}`)); err != nil {
		// 示教器盖个人包角被误拦。
		t.Fatalf("pad apply corner: %v", err)
	}
}
