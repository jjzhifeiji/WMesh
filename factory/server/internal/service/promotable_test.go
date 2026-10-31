// 通道升档：列出本厂全部级别，不分状态。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 升档名单要含本厂全部级别，漏级别说明没列全。
func TestPromotableForChannel(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
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
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{}
	// 准备这段正文字节，读回对不上说明没写进去。
	body := []byte("factory-body")

	// 新建一份厂级工艺，失败说明起草入口坏了。
	draft, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "草稿", body)
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份厂级工艺，失败说明起草入口坏了。
	avail, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "可升", body)
	// 建工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布失败就停，否则后面没有可靠结果。
	if _, err := fac.PublishAsset(ctx, pe.tok, avail.ID, avail.Revision); err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份个人工艺，失败说明个人入口被拒。
	personal, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "个人", body)
	// 建个人工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建个人工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布失败就停，否则后面没有可靠结果。
	if _, err := fac.PublishAsset(ctx, pe.tok, personal.ID, personal.Revision); err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 准备这段正文字节，读回对不上说明没写进去。
	platBody := []byte("plat-copy")
	// 装一条平台级成员，用来验证升档名单的边界。
	plat := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台副本",
		Status: factory.AssetAvailable, Copyable: false, Revision: 5, Content: platBody, Digest: digest.Sum(platBody),
	}
	// 收平台稿失败就停，否则后面没有可靠结果。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, plat, nil, &seed.ID, nil)); err != nil {
		// 把收平台稿的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 列出可升档资产，失败说明通道或权限不对。
	rows, err := fac.ListPromotable(ctx, factory.KindProcess)
	// 列可升档失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把列可升档的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 按名称记下状态或级别，漏记会把名单核对错。
	seen := map[string]string{}
	// 按名称记下级别，漏记会把升档结果看错。
	levels := map[string]string{}
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, r := range rows {
		// 记下这条的状态，和预期不一致就说明名单偏了。
		seen[r.Name] = r.Status
		// 记下这条的级别，升错档会在这里对不上。
		levels[r.Name] = r.Level
	}
	// 结果应和这一步的预期一致，偏离说明行为写偏了。
	if seen["草稿"] != factory.AssetDraft || seen["可升"] != factory.AssetAvailable || seen["个人"] != factory.AssetAvailable || seen["平台副本"] != factory.AssetAvailable {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%+v", rows)
	}
	// 级别应是预期那一档，升错或没升都算失败。
	if levels["草稿"] != factory.AssetLevelFactory || levels["个人"] != factory.AssetLevelPersonal || levels["平台副本"] != factory.AssetLevelPlatform {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("levels %+v", levels)
	}

	// 做快照失败或状态不对就停，说明没达预期。
	if snap, err := fac.SnapshotForChannel(ctx, draft.ID); err != nil || snap.Status != factory.AssetDraft || !contentcrypt.IsEnvelope(snap.Content) {
		// 状态还停在草稿，说明保存没有落成可用。
		t.Fatalf("draft: %+v %v", snap, err)
	}
	// 做快照失败或结果不符就停，说明没达预期。
	if snap, err := fac.SnapshotForChannel(ctx, personal.ID); err != nil || snap.SourceID != personal.ID || !contentcrypt.IsEnvelope(snap.Content) {
		// 个人稿串到了别人名下，说明归属没保住。
		t.Fatalf("personal: %+v %v", snap, err)
	}
	// 做快照应因越权被拒绝，放行说明没拦住。
	if _, err := fac.SnapshotForChannel(ctx, plat.ID); !errors.Is(err, domain.ErrForbidden) {
		// 副本没有按预期同步，说明下发没有写进厂。
		t.Fatalf("platform replica: %v", err)
	}
	// 按通道做下发快照，失败说明没有可发的稿。
	snap, err := fac.SnapshotForChannel(ctx, avail.ID)
	// 认信封失败或结果不符就停，说明没达预期。
	if err != nil || snap.SourceID != avail.ID || !contentcrypt.IsEnvelope(snap.Content) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%+v %v", snap, err)
	}
}

// 升档名单不分状态，把草稿或停用漏掉即失败。
func TestPromotableProjectsAllStatuses(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa", "超管")
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
	// 标明直接作业上下文，后面新建资产都挂在这里。
	direct := factory.WorkContext{}
	// 准备这段正文字节，读回对不上说明没写进去。
	body := []byte("job")

	// 新建一份厂级工程，失败说明起草入口坏了。
	draft, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "草稿工程", body, nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份厂级工程，失败说明起草入口坏了。
	avail, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "可用工程", body, nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	avail, err = fac.PublishAsset(ctx, pe.tok, avail.ID, avail.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份厂级工程，失败说明起草入口坏了。
	off, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "停用工程", body, nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	off, err = fac.PublishAsset(ctx, pe.tok, off.ID, off.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 停用这份资产，失败说明仍被引用或越权。
	off, err = fac.DisableAsset(ctx, pe.tok, off.ID, off.Revision)
	// 停用资产失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把停用资产的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新建一份个人工程，失败说明个人入口被拒。
	personal, err := fac.CreatePersonalProject(ctx, pe.tok, direct, "个人工程", body, nil)
	// 建个人工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建个人工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}

	// 列出可升档资产，失败说明通道或权限不对。
	rows, err := fac.ListPromotable(ctx, factory.KindProject)
	// 列可升档失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把列可升档的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 按名称记下状态或级别，漏记会把名单核对错。
	seen := map[string]string{}
	// 逐条检查这一批结果，任一条偏离即判失败。
	for _, r := range rows {
		// 记下这条的状态，和预期不一致就说明名单偏了。
		seen[r.Name] = r.Status
	}
	// 结果应和这一步的预期一致，偏离说明行为写偏了。
	if seen["草稿工程"] != factory.AssetDraft || seen["可用工程"] != factory.AssetAvailable || seen["停用工程"] != factory.AssetDisabled || seen["个人工程"] != factory.AssetDraft {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("%+v", rows)
	}

	// 做快照失败或结果不符就停，说明没达预期。
	if snap, err := fac.SnapshotForChannel(ctx, personal.ID); err != nil || snap.SourceID != personal.ID {
		// 个人稿串到了别人名下，说明归属没保住。
		t.Fatalf("personal: %+v %v", snap, err)
	}
	// 做快照失败或状态不对就停，说明没达预期。
	if snap, err := fac.SnapshotForChannel(ctx, draft.ID); err != nil || snap.Status != factory.AssetDraft || !contentcrypt.IsEnvelope(snap.Content) {
		// 状态还停在草稿，说明保存没有落成可用。
		t.Fatalf("draft snap %+v %v", snap, err)
	}
	// 做快照失败或状态不对就停，说明没达预期。
	if snap, err := fac.SnapshotForChannel(ctx, off.ID); err != nil || snap.Status != factory.AssetDisabled {
		// 停用之后仍能操作，说明停用没有生效。
		t.Fatalf("disabled snap %+v %v", snap, err)
	}
}

// 升档落进所选厂级文件夹成草稿，再升另起一份；非创建人的管理员升不了别人的个人级。
func TestPromoteIntoFolderAsDraft(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下超管，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 改测试口令失败就停，否则后面没有可靠结果。
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 把改口令的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建管理员，他自己的个人级可以升到厂级。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOrgAdmin)
	// 再建一位管理员，他不是源的创建人。
	pe2 := mustCreateRole(t, ctx, fac, sa, "pe2", "pe2-pass", factory.RoleOrgAdmin)
	// 列出可见目录，用来认出厂级根夹。
	nodes, err := fac.ListFS(ctx, pe.tok, factory.KindProcess)
	// 列目录失败就停，后面没法建夹。
	if err != nil {
		// 把列目录的错误打出来并停住。
		t.Fatal(err)
	}
	// 留出厂级根夹，建目标夹要从这里开始。
	var factoryRoot factory.FSNodeView
	// 逐个目录节点查看，认出厂级根。
	for _, n := range nodes {
		// 无父节点的厂级夹就是厂级根。
		if n.NodeKind == store.FSNodeFolder && n.TreeLevel == store.FSTreeFactory && n.ParentID == nil {
			// 命中厂级根就记下来。
			factoryRoot = n
		}
	}
	// 目录里必须有厂级根夹，否则后面没法往下建。
	if factoryRoot.ID == uuid.Nil {
		// 认不出厂级根就停，后面没法建夹。
		t.Fatal("missing factory root")
	}
	// 在厂级根下建目标夹，升档要落到这里。
	folder, err := fac.CreateFSFolder(ctx, pe.tok, factoryRoot.ID, "升档夹")
	// 建夹失败就停，后面没有目标可选。
	if err != nil {
		// 把建夹的错误打出来并停住。
		t.Fatal(err)
	}
	// 新建一份个人工艺，作为升档的源。
	src, err := fac.CreatePersonalProcess(ctx, pe.tok, factory.WorkContext{}, "个人源", []byte(`{"name":"src"}`))
	// 建个人工艺失败就停，否则后面没有源可升。
	if err != nil {
		// 把建个人工艺的错误打出来并停住。
		t.Fatal(err)
	}
	// 升到所选文件夹，应落成新身份的厂级草稿。
	first, err := fac.PromoteToFactory(ctx, pe.tok, src.ID, folder.ID)
	// 升档失败或没落成厂级草稿就停。
	if err != nil || first.ID == src.ID || first.Level != factory.AssetLevelFactory || first.Status != factory.AssetDraft {
		// 升出来的不是厂级草稿，口径偏了。
		t.Fatalf("promote into folder %+v %v", first, err)
	}
	// 再升一次，应另起一份，不回写第一份。
	second, err := fac.PromoteToFactory(ctx, pe.tok, src.ID, folder.ID)
	// 再升失败或撞了第一份的身份或编号就停。
	if err != nil || second.ID == first.ID || second.Code == first.Code || second.Status != factory.AssetDraft {
		// 再升没另起一份，说明还在覆盖旧稿。
		t.Fatalf("second promote %+v %v", second, err)
	}
	// 重新列目录，核对两份都挂在所选夹下。
	nodes, err = fac.ListFS(ctx, pe.tok, factory.KindProcess)
	// 列目录失败就停，后面核不了落点。
	if err != nil {
		// 把列目录的错误打出来并停住。
		t.Fatal(err)
	}
	// 记下落在所选夹下的资产。
	under := map[uuid.UUID]bool{}
	// 逐个节点看文件挂在哪个夹下。
	for _, n := range nodes {
		// 只看挂在目标夹下的文件。
		if n.AssetID != nil && n.ParentID != nil && *n.ParentID == folder.ID {
			// 记上这份资产。
			under[*n.AssetID] = true
		}
	}
	// 两份升出来的厂级都要在所选夹下。
	if !under[first.ID] || !under[second.ID] {
		// 有一份没落进所选夹，说明落点不对。
		t.Fatalf("promoted not in folder: %v", under)
	}
	// 不是创建人的管理员升别人的个人级必须被拒。
	if _, err := fac.PromoteToFactory(ctx, pe2.tok, src.ID, folder.ID); !errors.Is(err, domain.ErrForbidden) {
		// 放行说明别人的个人级被借升档拿走了。
		t.Fatalf("non-creator admin promote: %v", err)
	}
}
