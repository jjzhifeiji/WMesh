// 本厂目录树：同事看不见别人的个人树，超管能看见；平台副本树只读。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 验收个人树只对本人和超管可见，平台树不能写。
func TestFactoryFS(t *testing.T) {
	// 准备贯穿本用例的上下文，不设截止时间。
	ctx := context.Background()
	// 起一套隔离厂库，起不来则本用例没有库可测。
	h := New(t)
	// 开通本厂，供后面步骤使用，失败则前提断了。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登录取会话，供后面步骤使用，失败则前提断了。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，供后面步骤使用，失败则前提断了。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 建人并授角色，供后面步骤使用，失败则前提断了。
	other := mustCreateRole(t, ctx, fac, sa, "ot", "ot-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 用直属上下文写工艺，不把它挂到车间。
	direct := factory.WorkContext{Direct: true}

	// 建个人工艺，供后面步骤使用，失败则前提断了。
	mine, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "我的工艺", []byte("p"))
	// 建个人工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建个人工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 建厂级工艺，供后面步骤使用，失败则前提断了。
	facRow, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "厂级工艺", []byte("f"))
	// 建厂级工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建厂级工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}

	// 列出可见目录，供后面步骤使用，失败则前提断了。
	peNodes, err := fac.ListFS(ctx, pe.tok, factory.KindProcess)
	// 列出可见目录没成功，后面的断言就没有依据。
	if err != nil {
		// 列出可见目录失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 列出可见目录，供后面步骤使用，失败则前提断了。
	otNodes, err := fac.ListFS(ctx, other.tok, factory.KindProcess)
	// 列出可见目录没成功，后面的断言就没有依据。
	if err != nil {
		// 列出可见目录失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 列出可见目录，供后面步骤使用，失败则前提断了。
	saNodes, err := fac.ListFS(ctx, sa, factory.KindProcess)
	// 列出可见目录没成功，后面的断言就没有依据。
	if err != nil {
		// 列出可见目录失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 个人工艺只有本人和超管可见，同事必须看不见。
	if !fsHasAsset(peNodes, mine.ID) || !fsHasAsset(saNodes, mine.ID) || fsHasAsset(otNodes, mine.ID) {
		// 可见性不对就停，个人目录没有按人隔开。
		t.Fatalf("personal visibility pe=%v sa=%v ot=%v", fsHasAsset(peNodes, mine.ID), fsHasAsset(saNodes, mine.ID), fsHasAsset(otNodes, mine.ID))
	}
	// 厂级工艺必须出现在同事能看见的目录里。
	if !fsHasAsset(otNodes, facRow.ID) {
		// 同事看不见厂级工艺就停，厂级树被藏起来了。
		t.Fatal("factory tree should be public")
	}

	// 留出厂级根夹，找到之后再往里面建夹。
	var factoryRoot factory.FSNodeView
	// 逐个目录节点查看，用来认出根夹或目标资产。
	for _, n := range peNodes {
		// 无父节点的厂级夹就是厂级根，先把它认出来。
		if n.NodeKind == store.FSNodeFolder && n.TreeLevel == store.FSTreeFactory && n.ParentID == nil {
			// 命中没有父节点的厂级夹时，把它记下来。
			factoryRoot = n
		}
	}
	// 目录里必须有厂级根夹，否则后面没法往下建。
	if factoryRoot.ID == [16]byte{} {
		t.Fatal("missing factory root")
	}
	// 新建文件夹，供后面步骤使用，失败则前提断了。
	folder, err := fac.CreateFSFolder(ctx, pe.tok, factoryRoot.ID, "公用夹")
	// 新建文件夹没成功，后面的断言就没有依据。
	if err != nil {
		// 新建文件夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 留出平台根夹，用来验证这里不能新建。
	var platRoot factory.FSNodeView
	// 逐个目录节点查看，用来认出根夹或目标资产。
	for _, n := range peNodes {
		// 无父节点的平台夹就是平台根，先把它认出来。
		if n.NodeKind == store.FSNodeFolder && n.TreeLevel == store.FSTreePlatform && n.ParentID == nil {
			// 命中没有父节点的平台夹时，把它记下来。
			platRoot = n
		}
	}
	// 在平台根下新建夹必须因越权被拒绝。
	if _, err := fac.CreateFSFolder(ctx, pe.tok, platRoot.ID, "不能写"); !errors.Is(err, domain.ErrForbidden) {
		// 平台树能被写就停，下发来的目录不是只读。
		t.Fatalf("platform mkdir: %v", err)
	}

	// 按资产找文件节点，供后面步骤使用，失败则前提断了。
	file, err := fac.Store().FSFileByAsset(ctx, facRow.ID)
	// 按资产找文件节点没成功，后面的断言就没有依据。
	if err != nil {
		// 按资产找文件节点失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 移动目录节点没成功，后面的断言就没有依据。
	if _, err := fac.MoveFSNode(ctx, pe.tok, file.ID, folder.ID); err != nil {
		// 移动目录节点失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}

	// 按通道列出目录，供后面步骤使用，失败则前提断了。
	chanNodes, err := fac.ListFSForChannel(ctx, factory.KindProcess)
	// 按通道列出目录没成功，后面的断言就没有依据。
	if err != nil {
		// 按通道列出目录失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 通道视角必须能看见个人工艺所在的目录结构。
	if !fsHasAsset(chanNodes, mine.ID) {
		// 通道看不见个人结构就停，云端缺了这棵树。
		t.Fatal("cloud should see personal structure")
	}
}

// 验收平台下发的目录路径会落到本厂树上。
func TestPlatformFSPathFromWAN(t *testing.T) {
	// 准备贯穿本用例的上下文，不设截止时间。
	ctx := context.Background()
	// 起一套隔离厂库，起不来则本用例没有库可测。
	h := New(t)
	// 开通本厂，供后面步骤使用，失败则前提断了。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登录取会话，供后面步骤使用，失败则前提断了。
	tok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 记下本厂标识，下发的快照要指到这家厂。
	fid := seed.ID
	// 准备一段正文，读回或检索时要拿它对照。
	body := []byte("plat-fs")
	// 生成一个新标识，用来当目录、资产或客户端。
	folderID := id.New()
	// 拼一条带着目录路径的平台工艺成员。
	m := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台工艺",
		Status: factory.AssetAvailable, Copyable: false, Revision: 1, Content: body, Digest: digest.Sum(body),
		FSParentID: &folderID, FSPath: []factory.FSFolderHint{{ID: folderID, Name: "标准库"}},
	}
	// 收下平台下发没成功，后面的断言就没有依据。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		// 收下平台下发失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 列出可见目录，供后面步骤使用，失败则前提断了。
	nodes, err := fac.ListFS(ctx, tok, factory.KindProcess)
	// 列出可见目录没成功，后面的断言就没有依据。
	if err != nil {
		// 列出可见目录失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 按资产找文件节点，供后面步骤使用，失败则前提断了。
	file, err := fac.Store().FSFileByAsset(ctx, m.ID)
	// 文件必须挂在下发带来的标准库夹下面。
	if err != nil || file.ParentID == nil || *file.ParentID != folderID {
		// 父夹不对就停，平台给的路径没有落地。
		t.Fatalf("file parent %+v %v", file, err)
	}
	// 目录里必须有这次下发过来的标准库夹。
	if !fsHasFolder(nodes, folderID, "标准库") {
		// 标准库夹不在就停，路径没有同步下来。
		t.Fatal("missing synced folder")
	}

	// 生成一个新标识，用来当目录、资产或客户端。
	next := id.New()
	// 把父夹改成新的焊接夹，看目录会不会跟上。
	m.FSParentID = &next
	// 配上新的路径名，空下来的旧夹应当消失。
	m.FSPath = []factory.FSFolderHint{{ID: next, Name: "焊接"}}
	// 收下平台下发没成功，后面的断言就没有依据。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		// 收下平台下发失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 按资产找文件节点，供后面步骤使用，失败则前提断了。
	file, err = fac.Store().FSFileByAsset(ctx, m.ID)
	// 再次下发后，文件必须改挂到新的焊接夹下。
	if err != nil || file.ParentID == nil || *file.ParentID != next {
		// 还挂在旧夹就停，目录没有跟上新的路径。
		t.Fatalf("moved %+v %v", file, err)
	}
	// 列出可见目录，供后面步骤使用，失败则前提断了。
	nodes, err = fac.ListFS(ctx, tok, factory.KindProcess)
	// 列出可见目录没成功，后面的断言就没有依据。
	if err != nil {
		// 列出可见目录失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 已经没人用的旧夹必须从目录树上消失。
	if fsHasFolder(nodes, folderID, "标准库") {
		// 空的旧夹还在就停，目录没有收干净。
		t.Fatal("empty old folder should be gone")
	}
	// 新下发的焊接夹必须出现在目录树上。
	if !fsHasFolder(nodes, next, "焊接") {
		// 新夹不在就停，路径没有换成焊接这一夹。
		t.Fatal("missing new folder")
	}
}

// 验收平板个人工艺放进子夹后，闭包里带上路径。
func TestPadNestedFS(t *testing.T) {
	// 准备贯穿本用例的上下文，不设截止时间。
	ctx := context.Background()
	// 起一套隔离厂库，起不来则本用例没有库可测。
	h := New(t)
	// 开通本厂，供后面步骤使用，失败则前提断了。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登录取会话，供后面步骤使用，失败则前提断了。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，供后面步骤使用，失败则前提断了。
	op := mustCreateRole(t, ctx, fac, sa, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 准备一段正文，读回或检索时要拿它对照。
	body := []byte(`{"name":"mine","current":170}`)
	// 建平板个人工艺，供后面步骤使用，失败则前提断了。
	got, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "平板工艺", body, id.New(), "GY-C0008-000001", nil)
	// 建平板个人工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建平板个人工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 列出可见目录，供后面步骤使用，失败则前提断了。
	nodes, err := fac.ListFS(ctx, op.tok, factory.KindProcess)
	// 列出可见目录没成功，后面的断言就没有依据。
	if err != nil {
		// 列出可见目录失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 留出个人根夹，后面在它下面建现场夹。
	var personalRoot factory.FSNodeView
	// 逐个目录节点查看，用来认出根夹或目标资产。
	for _, n := range nodes {
		// 无父节点的个人夹就是个人根，先把它认出来。
		if n.NodeKind == store.FSNodeFolder && n.TreeLevel == store.FSTreePersonal && n.ParentID == nil {
			// 命中没有父节点的个人夹时，把它记下来。
			personalRoot = n
		}
	}
	// 个人根夹必须存在，否则后面没有地方建现场夹。
	if personalRoot.ID == [16]byte{} {
		t.Fatal("missing personal root")
	}
	// 新建文件夹，供后面步骤使用，失败则前提断了。
	folder, err := fac.CreateFSFolder(ctx, op.tok, personalRoot.ID, "现场夹")
	// 新建文件夹没成功，后面的断言就没有依据。
	if err != nil {
		// 新建文件夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 把资产放进文件夹没成功，后面的断言就没有依据。
	if _, err := fac.MoveAssetInto(ctx, op.tok, got.ID, folder.ID); err != nil {
		// 把资产放进文件夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 拉平板下发闭包，供后面步骤使用，失败则前提断了。
	packed, err := fac.PadPullClientClosure(ctx, op.tok, got.ID)
	// 拉平板下发闭包没成功，后面的断言就没有依据。
	if err != nil {
		// 拉平板下发闭包失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 拉下来的闭包里必须只有刚才那一条成员。
	if len(packed.Snapshot.Members) != 1 {
		// 成员条数不对就停，闭包多带或少带了内容。
		t.Fatalf("members %d", len(packed.Snapshot.Members))
	}
	// 取出闭包里的那条成员，核对它的夹和路径。
	m := packed.Snapshot.Members[0]
	// 成员的父夹必须是刚才放进去的现场夹。
	if m.FSParentID == nil || *m.FSParentID != folder.ID {
		// 父夹不对就停，下发没有带上目录位置。
		t.Fatalf("parent %+v", m.FSParentID)
	}
	// 成员路径必须只有现场夹这一层名字。
	if len(m.FSPath) != 1 || m.FSPath[0].Name != "现场夹" {
		// 路径不对就停，平板侧对不上这个夹名。
		t.Fatalf("path %+v", m.FSPath)
	}
}

// 判断这些目录节点里有没有指定的资产。
func fsHasAsset(nodes []factory.FSNodeView, id [16]byte) bool {
	// 逐个目录节点查看，用来认出根夹或目标资产。
	for _, n := range nodes {
		// 这个节点指向目标资产，就算在目录里找到了。
		if n.AssetID != nil && *n.AssetID == id {
			return true
		}
	}
	return false
}

// 判断目录里有没有指定名称的这个夹。
func fsHasFolder(nodes []factory.FSNodeView, id [16]byte, name string) bool {
	// 逐个目录节点查看，用来认出根夹或目标资产。
	for _, n := range nodes {
		// 标识、种类和名称都对上，才算找到这个夹。
		if n.ID == id && n.NodeKind == store.FSNodeFolder && n.Name == name {
			return true
		}
	}
	return false
}

// 验收文件和文件夹复制，以及跨树复制会被拒绝。
func TestCopyFS(t *testing.T) {
	// 准备贯穿本用例的上下文，不设截止时间。
	ctx := context.Background()
	// 起一套隔离厂库，起不来则本用例没有库可测。
	h := New(t)
	// 开通本厂，供后面步骤使用，失败则前提断了。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 开通本厂没成功，后面的断言就没有依据。
	if err != nil {
		// 开通本厂失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 激活账号没成功，后面的断言就没有依据。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活账号失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 登录取会话，供后面步骤使用，失败则前提断了。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授角色，供后面步骤使用，失败则前提断了。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 用直属上下文写源工艺，不把它挂到车间。
	direct := factory.WorkContext{Direct: true}
	// 建厂级工艺，供后面步骤使用，失败则前提断了。
	src, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "厂级源", []byte("copy-fs"))
	// 建厂级工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建厂级工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 列出可见目录，供后面步骤使用，失败则前提断了。
	nodes, err := fac.ListFS(ctx, pe.tok, factory.KindProcess)
	// 列出可见目录没成功，后面的断言就没有依据。
	if err != nil {
		// 列出可见目录失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 留出厂级根夹，复制和建夹都从这里开始。
	var factoryRoot factory.FSNodeView
	// 逐个目录节点查看，用来认出根夹或目标资产。
	for _, n := range nodes {
		// 无父节点的厂级夹就是厂级根，先把它认出来。
		if n.NodeKind == store.FSNodeFolder && n.TreeLevel == store.FSTreeFactory && n.ParentID == nil {
			// 命中没有父节点的厂级夹时，把它记下来。
			factoryRoot = n
		}
	}
	// 目录里必须有厂级根夹，否则后面没法往下建。
	if factoryRoot.ID == [16]byte{} {
		t.Fatal("missing factory root")
	}
	// 新建文件夹，供后面步骤使用，失败则前提断了。
	alpha, err := fac.CreateFSFolder(ctx, pe.tok, factoryRoot.ID, "甲")
	// 新建文件夹没成功，后面的断言就没有依据。
	if err != nil {
		// 新建文件夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 新建文件夹，供后面步骤使用，失败则前提断了。
	beta, err := fac.CreateFSFolder(ctx, pe.tok, factoryRoot.ID, "乙")
	// 新建文件夹没成功，后面的断言就没有依据。
	if err != nil {
		// 新建文件夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 按资产找文件节点，供后面步骤使用，失败则前提断了。
	file, err := fac.Store().FSFileByAsset(ctx, src.ID)
	// 按资产找文件节点没成功，后面的断言就没有依据。
	if err != nil {
		// 按资产找文件节点失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 复制厂级根夹必须因越权被拒绝。
	if _, err := fac.CopyFSNode(ctx, pe.tok, factoryRoot.ID, beta.ID); !errors.Is(err, domain.ErrForbidden) {
		// 根夹能被复制就停，整棵目录树可以被搬走。
		t.Fatalf("copy root: %v", err)
	}
	// 复制目录节点，供后面步骤使用，失败则前提断了。
	dup, err := fac.CopyFSNode(ctx, pe.tok, file.ID, alpha.ID)
	// 复制出来的文件必须进甲夹，并且是一条新资产。
	if err != nil || dup.ParentID == nil || *dup.ParentID != alpha.ID || dup.AssetID == nil || *dup.AssetID == src.ID {
		// 没进甲夹或还是原资产就停，复制没有分开。
		t.Fatalf("copy file %+v %v", dup, err)
	}
	// 按资产找文件节点，供后面步骤使用，失败则前提断了。
	still, err := fac.Store().FSFileByAsset(ctx, src.ID)
	// 复制之后，源文件节点必须还在原来的位置。
	if err != nil || still.ID != file.ID {
		// 源节点变了就停，复制被做成了移动。
		t.Fatalf("src moved %+v %v", still, err)
	}
	// 移动目录节点没成功，后面的断言就没有依据。
	if _, err := fac.MoveFSNode(ctx, pe.tok, file.ID, alpha.ID); err != nil {
		// 移动目录节点失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 复制目录节点，供后面步骤使用，失败则前提断了。
	folderCopy, err := fac.CopyFSNode(ctx, pe.tok, alpha.ID, beta.ID)
	// 复制甲夹必须挂到乙夹下面，并且仍是文件夹。
	if err != nil || folderCopy.ParentID == nil || *folderCopy.ParentID != beta.ID || folderCopy.NodeKind != store.FSNodeFolder {
		// 夹没有挂到乙或种类变了就停，复制夹失败。
		t.Fatalf("copy folder %+v %v", folderCopy, err)
	}
	// 列出夹内子节点，供后面步骤使用，失败则前提断了。
	kids, err := fac.Store().ListFSChildren(ctx, folderCopy.ID)
	// 复制出来的夹里必须带着原来的两个子节点。
	if err != nil || len(kids) != 2 {
		// 子节点不是两个就停，复制夹的时候没有带上孩子。
		t.Fatalf("folder copy kids %d %v", len(kids), err)
	}
	// 列出夹内子节点，供后面步骤使用，失败则前提断了。
	origKids, err := fac.Store().ListFSChildren(ctx, alpha.ID)
	// 原夹里的子节点必须还是两个，不能被搬走。
	if err != nil || len(origKids) != 2 {
		// 原夹的孩子变了就停，复制把源夹掏空了。
		t.Fatalf("src folder kids %d %v", len(origKids), err)
	}
	// 把夹复制到自己里面必须因成环被拒绝。
	if _, err := fac.CopyFSNode(ctx, pe.tok, alpha.ID, alpha.ID); !errors.Is(err, domain.ErrCycle) {
		// 能复制进自己就停，目录可以被绕成环。
		t.Fatalf("into self: %v", err)
	}
	// 建个人工艺，供后面步骤使用，失败则前提断了。
	mine, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "个人源", []byte("p"))
	// 建个人工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建个人工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 按资产找文件节点，供后面步骤使用，失败则前提断了。
	mineFile, err := fac.Store().FSFileByAsset(ctx, mine.ID)
	// 按资产找文件节点没成功，后面的断言就没有依据。
	if err != nil {
		// 按资产找文件节点失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 个人文件复制进厂级根必须因越权被拒绝。
	if _, err := fac.CopyFSNode(ctx, pe.tok, mineFile.ID, factoryRoot.ID); !errors.Is(err, domain.ErrForbidden) {
		// 个人能进厂级树就停，两棵目录串到一起了。
		t.Fatalf("personal into factory: %v", err)
	}
	// 准备可复制的平台正文，复制后应变成厂级。
	platBody := []byte("plat-copy-fs")
	// 拼一条可复制的平台工艺，准备收进本厂再复制。
	m := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台可复制",
		Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: platBody, Digest: digest.Sum(platBody),
	}
	// 记下本厂标识，下发的时候要指到这家厂。
	fid := seed.ID
	// 收下平台下发没成功，后面的断言就没有依据。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		// 收下平台下发失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 按资产找文件节点，供后面步骤使用，失败则前提断了。
	platFile, err := fac.Store().FSFileByAsset(ctx, m.ID)
	// 按资产找文件节点没成功，后面的断言就没有依据。
	if err != nil {
		// 按资产找文件节点失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 复制目录节点，供后面步骤使用，失败则前提断了。
	fromPlat, err := fac.CopyFSNode(ctx, pe.tok, platFile.ID, beta.ID)
	// 可复制的平台文件必须能够复制进乙夹。
	if err != nil || fromPlat.ParentID == nil || *fromPlat.ParentID != beta.ID || fromPlat.AssetID == nil {
		// 平台文件复制失败就停，允许复制没有生效。
		t.Fatalf("platform copy %+v %v", fromPlat, err)
	}
	// 读资产元数据，供后面步骤使用，失败则前提断了。
	copied, err := fac.GetAsset(ctx, pe.tok, *fromPlat.AssetID)
	// 从平台复制出来的必须是厂级新资产，不能还是原件。
	if err != nil || copied.Level != factory.AssetLevelFactory || copied.ID == m.ID {
		// 还是平台原件或级别不对就停，复制没有变成厂级。
		t.Fatalf("platform copy asset %+v %v", copied, err)
	}
}
