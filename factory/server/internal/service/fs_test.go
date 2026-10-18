// 本厂目录树：同事看不见别人的个人树，超管能看见；平台副本树只读。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"wmesh/factory/internal/platform/audit"
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
	// 个人树主人用好认的名字，后面查通道回包里有没有漏出它。
	const peLogin = "fsowner-liu"
	// 建人并授角色，供后面步骤使用，失败则前提断了。
	pe := mustCreateRole(t, ctx, fac, sa, peLogin, "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
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
	if !chanHasAsset(chanNodes, mine.ID) || !chanHasAsset(chanNodes, facRow.ID) {
		// 通道看不见个人结构就停，云端缺了这棵树。
		t.Fatal("cloud should see personal structure")
	}
	// 个人工艺挂在个人树上，元数据仍带名称和级别。
	if !chanHasPersonalFile(chanNodes, mine.ID, "我的工艺") {
		// 个人文件的级别或名称对不上就停，云端认不出它。
		t.Fatal("personal file should keep tree level and asset name")
	}
	// 把通道回包编成 JSON，按云端实际收到的样子查。
	raw, err := json.Marshal(chanNodes)
	// 编码失败就停，后面无从查字段。
	if err != nil {
		// 编码失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 回包整段文本，逐个查不该出厂的字段和人名。
	body := string(raw)
	// 创建人、组织、主人字段和主人登录名、显示名、身份都不能出现。
	for _, bad := range []string{`"creator`, `"orgPath"`, `"orgUnitId"`, `"ownerId"`, `"ownerName"`, peLogin, pe.acc.ID.String()} {
		// 回包里出现了人员或组织信息就停，名册出了厂。
		if strings.Contains(body, bad) {
			// 带出人员信息即失败，打出漏出的字段。
			t.Fatalf("channel fs leaks %q: %s", bad, body)
		}
	}
}

// 判断通道目录行里有没有指定的资产。
func chanHasAsset(nodes []factory.ChannelFSNode, id [16]byte) bool {
	// 逐个通道目录行查看，找指向目标资产的文件。
	for _, n := range nodes {
		// 这一行指向目标资产，就算在目录里找到了。
		if n.AssetID != nil && *n.AssetID == id {
			return true
		}
	}
	return false
}

// 判断通道目录里有没有挂在个人树、带着名称的这个文件。
func chanHasPersonalFile(nodes []factory.ChannelFSNode, id [16]byte, name string) bool {
	// 逐个通道目录行查看，找个人树上的目标文件。
	for _, n := range nodes {
		// 资产、树级别和元数据名称都对上，才算找到。
		if n.AssetID != nil && *n.AssetID == id && n.TreeLevel == store.FSTreePersonal && n.Asset != nil && n.Asset.Name == name {
			return true
		}
	}
	return false
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

// 验收平台目录布局只留挂了副本的祖先夹，只装草稿的夹不落地。
func TestPlatformFSLayoutSkipsDraftFolders(t *testing.T) {
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
	// 记下本厂标识，下发的快照要指到这家厂。
	fid := seed.ID
	// 准备一段正文，下发的副本要带着它落地。
	body := []byte("plat-layout")
	// 生成夹 A 的标识，副本会落进这一夹。
	folderA := id.New()
	// 生成夹 B 的标识，它只装没到本厂的草稿。
	folderB := id.New()
	// 生成 B 下子夹的标识，验证嵌套的空夹也会清掉。
	folderC := id.New()
	// 生成草稿资产的标识，本厂从未收到它的副本。
	draftID := id.New()
	// 拼一条挂在夹 A 下的平台工艺成员。
	m := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "布局工艺",
		Status: factory.AssetAvailable, Copyable: false, Revision: 1, Content: body, Digest: digest.Sum(body),
		FSParentID: &folderA, FSPath: []factory.FSFolderHint{{ID: folderA, Name: "标准库"}},
	}
	// 收下平台下发没成功，后面的断言就没有依据。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		// 收下平台下发失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 拼一份整棵布局：A 有副本，B 和它的子夹只挂着草稿。
	layout := factory.PlatformFSLayout{
		Kind: factory.KindProcess,
		Folders: []factory.FSFolderHint{
			{ID: folderA, Name: "标准库"}, {ID: folderB, Name: "草稿夹"}, {ID: folderC, Name: "子夹", ParentID: &folderB},
		},
		Files: []store.FSFileHint{{AssetID: m.ID, ParentID: &folderA}, {AssetID: draftID, ParentID: &folderC}},
	}
	// 同一份布局下发两次，结果都要一样，不能来回闪。
	for round := 1; round <= 2; round++ {
		// 对齐平台目录没成功，后面的断言就没有依据。
		if err := fac.Store().ApplyPlatformFSLayout(ctx, layout); err != nil {
			// 对齐平台目录失败即停，不能把这一步的失败当成通过。
			t.Fatalf("round %d apply: %v", round, err)
		}
		// 按资产找文件节点，核对副本还挂在夹 A 下。
		file, err := fac.Store().FSFileByAsset(ctx, m.ID)
		// 副本文件必须还在夹 A 下面。
		if err != nil || file.ParentID == nil || *file.ParentID != folderA {
			// 文件挂点不对就停，对齐把副本挪丢了。
			t.Fatalf("round %d file %+v %v", round, file, err)
		}
		// 夹 A 挂着副本，必须留下。
		if _, err := fac.Store().FSNodeByID(ctx, folderA); err != nil {
			// 夹 A 不在就停，有副本的祖先被误删了。
			t.Fatalf("round %d folder A: %v", round, err)
		}
		// 逐个核对布局里只装草稿的夹 B 和它的子夹。
		for _, gone := range layout.Folders[1:] {
			// 只装草稿的夹必须查不到，不能留成空夹。
			if _, err := fac.Store().FSNodeByID(ctx, gone.ID); !errors.Is(err, domain.ErrNotFound) {
				// 空夹还在就停，平台树又建出了草稿夹。
				t.Fatalf("round %d draft folder %s: %v", round, gone.Name, err)
			}
		}
		// 草稿没有副本，本厂不该挂出它的文件。
		if _, err := fac.Store().FSFileByAsset(ctx, draftID); !errors.Is(err, domain.ErrNotFound) {
			// 草稿文件挂出来就停，没到的副本不能占位。
			t.Fatalf("round %d draft file: %v", round, err)
		}
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
	// 先登记 C0008 这台焊机，示教器自带的 C 段号才会被认。
	mustClientShortCode(t, ctx, fac, "C0008")
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

// 验收平台夹互换名字、删旧夹再改名后，目录对齐都不再撞名失败。
func TestPlatformFSSwapFolderNames(t *testing.T) {
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
	// 夹 X 起初叫甲，后面和 Y 互换名字。
	folderX := factory.FSFolderHint{ID: id.New(), Name: "甲"}
	// 夹 Y 起初叫乙，后面和 X 互换名字。
	folderY := factory.FSFolderHint{ID: id.New(), Name: "乙"}
	// 拼一条挂在甲夹下的平台工艺。
	m1 := platProc("互换一", 1, []byte("swap-1"), folderX)
	// 拼一条挂在乙夹下的平台工艺。
	m2 := platProc("互换二", 1, []byte("swap-2"), folderY)
	// 先收下第一条，平台树里落出甲夹。
	deliverPlatProc(t, ctx, fac, seed, m1)
	// 再收下第二条，平台树里落出乙夹。
	deliverPlatProc(t, ctx, fac, seed, m2)
	// 拼一份两个夹互换名字后的整棵布局。
	layout := factory.PlatformFSLayout{
		Kind:    factory.KindProcess,
		Folders: []factory.FSFolderHint{{ID: folderX.ID, Name: "乙"}, {ID: folderY.ID, Name: "甲"}},
		Files:   []store.FSFileHint{{AssetID: m1.ID, ParentID: &folderX.ID}, {AssetID: m2.ID, ParentID: &folderY.ID}},
	}
	// 互换名字后按整棵布局对齐，不能再撞名整单失败。
	if err := fac.Store().ApplyPlatformFSLayout(ctx, layout); err != nil {
		// 对齐失败即停，互换名字还是会卡住目录。
		t.Fatalf("swap layout: %v", err)
	}
	// 夹 X 必须改叫乙。
	wantPlatFolder(t, ctx, fac, factory.FSFolderHint{ID: folderX.ID, Name: "乙"})
	// 夹 Y 必须改叫甲。
	wantPlatFolder(t, ctx, fac, factory.FSFolderHint{ID: folderY.ID, Name: "甲"})
	// 第一条仍挂在夹 X 下。
	wantFilePlaced(t, ctx, fac, store.FSFileHint{AssetID: m1.ID, ParentID: &folderX.ID})
	// 第二条仍挂在夹 Y 下。
	wantFilePlaced(t, ctx, fac, store.FSFileHint{AssetID: m2.ID, ParentID: &folderY.ID})

	// 第一条按单份路径把 X 改回甲，此时甲还被 Y 占着。
	m1.FSPath = []factory.FSFolderHint{{ID: folderX.ID, Name: "甲"}}
	// 重收第一条，单份路径对齐也不能撞名失败。
	deliverPlatProc(t, ctx, fac, seed, m1)
	// 第二条按单份路径把 Y 改回乙。
	m2.FSPath = []factory.FSFolderHint{{ID: folderY.ID, Name: "乙"}}
	// 重收第二条，乙已经空出来了。
	deliverPlatProc(t, ctx, fac, seed, m2)
	// 夹 X 必须换回甲。
	wantPlatFolder(t, ctx, fac, folderX)
	// 夹 Y 必须换回乙。
	wantPlatFolder(t, ctx, fac, folderY)

	// 云端删掉夹 X、把 Y 改叫甲，第一条挪回平台根。
	layout = factory.PlatformFSLayout{
		Kind:    factory.KindProcess,
		Folders: []factory.FSFolderHint{{ID: folderY.ID, Name: "甲"}},
		Files:   []store.FSFileHint{{AssetID: m1.ID}, {AssetID: m2.ID, ParentID: &folderY.ID}},
	}
	// 删旧夹再改名后对齐，不能被还没删的旧夹挡住。
	if err := fac.Store().ApplyPlatformFSLayout(ctx, layout); err != nil {
		// 对齐失败即停，旧夹占着名字把目录卡住了。
		t.Fatalf("delete-rename layout: %v", err)
	}
	// 夹 Y 必须改叫甲。
	wantPlatFolder(t, ctx, fac, factory.FSFolderHint{ID: folderY.ID, Name: "甲"})
	// 夹 X 已经空了又不在布局里，必须清掉。
	if _, err := fac.Store().FSNodeByID(ctx, folderX.ID); !errors.Is(err, domain.ErrNotFound) {
		// 旧夹还在就停，平台树留下了让过位的空夹。
		t.Fatalf("old folder X: %v", err)
	}
	// 第一条必须挂回平台根下。
	wantFilePlaced(t, ctx, fac, store.FSFileHint{AssetID: m1.ID})
}

// 验收副本收到改名的新修订后文件名跟上，晚到的旧修订不把名字改回去。
func TestPlatformReplicaRenameFollowsTop(t *testing.T) {
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
	// 第一版叫旧名，不带目录路径，落在平台根下。
	m1 := platProc("旧名", 1, []byte("rename-1"))
	// 收下第一版。
	deliverPlatProc(t, ctx, fac, seed, m1)
	// 文件节点起初叫旧名。
	wantFileName(t, ctx, fac, m1, "旧名")
	// 第三版在云端改叫新名，是目前最高的修订。
	m3 := reviseProc(m1, 3, "新名", []byte("rename-3"))
	// 收下第三版。
	deliverPlatProc(t, ctx, fac, seed, m3)
	// 文件节点名必须跟着改成新名。
	wantFileName(t, ctx, fac, m1, "新名")
	// 第二版是工程钉住的旧修订，晚到一步，名字叫中间名。
	m2 := reviseProc(m1, 2, "中间名", []byte("rename-2"))
	// 收下晚到的第二版。
	deliverPlatProc(t, ctx, fac, seed, m2)
	// 旧修订不能把文件名改回中间名。
	wantFileName(t, ctx, fac, m1, "新名")
	// 第一版重收一次。
	deliverPlatProc(t, ctx, fac, seed, m1)
	// 重收旧修订也不能把名字退回旧名。
	wantFileName(t, ctx, fac, m1, "新名")
}

// 验收副本文件搬进已有同名文件的夹，自己带上编号，不整单失败。
func TestPlatformFileMoveIntoSameName(t *testing.T) {
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
	// 准备一个平台夹，第一条先挂进去。
	folder := factory.FSFolderHint{ID: id.New(), Name: "同名夹"}
	// 拼一条挂在这个夹下、名叫同名的平台工艺。
	m1 := platProc("同名", 1, []byte("dup-1"), folder)
	// 收下第一条。
	deliverPlatProc(t, ctx, fac, seed, m1)
	// 第二条也叫同名，起初没带路径，落在平台根下。
	m2 := platProc("同名", 1, []byte("dup-2"))
	// 收下第二条。
	deliverPlatProc(t, ctx, fac, seed, m2)
	// 第二条在平台根下没人占名，原样叫同名。
	wantFileName(t, ctx, fac, m2, "同名")
	// 第二条身份和修订不变，只改挂到同名夹下。
	m2.FSParentID = &folder.ID
	// 路径带上同名夹，那里已有同名文件。
	m2.FSPath = []factory.FSFolderHint{folder}
	// 重收第二条，搬进去时撞名不能整单失败。
	deliverPlatProc(t, ctx, fac, seed, m2)
	// 第二条必须挂进同名夹。
	file := wantFilePlaced(t, ctx, fac, store.FSFileHint{AssetID: m2.ID, ParentID: &folder.ID})
	// 第二条要带上编号让开，名字仍以同名开头。
	prefix := "同名 "
	// 名字没变或不以同名开头，都说明撞名处理不对。
	if len(file.Name) <= len(prefix) || file.Name[:len(prefix)] != prefix {
		// 名字不对就停，把实际的名字打出来。
		t.Fatalf("moved file name %q", file.Name)
	}
	// 夹里原来的文件保持原名。
	wantFileName(t, ctx, fac, m1, "同名")
}

// 验收闭包前一个成员目录挂不上时，后面的成员照样落库并挂好目录。
func TestPlatformDeliveryFSFailureKeepsLater(t *testing.T) {
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
	// 记下本厂标识，下发的快照要指到这家厂。
	fid := seed.ID
	// 后面的工艺挂在一个正常的夹下。
	procFolder := factory.FSFolderHint{ID: id.New(), Name: "工艺夹"}
	// 拼工程依赖的平台工艺，它排在工程后面。
	proc := platProc("后面的工艺", 1, []byte("later-proc"), procFolder)
	// 工程根带一个含斜杠的坏夹名，目录必然挂不上。
	bad := factory.FSFolderHint{ID: id.New(), Name: "坏/夹"}
	// 准备工程正文，摘要按它算。
	projBody := []byte("bad-fs-proj")
	// 拼工程根成员，钉住后面那条工艺。
	proj := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProject, Level: factory.AssetLevelPlatform, Name: "坏目录工程",
		Status: factory.AssetAvailable, Copyable: false, Revision: 1, Content: projBody, Digest: digest.Sum(projBody),
		Deps:       []factory.AssetDep{{ID: proc.ID, Revision: 1, Digest: proc.Digest}},
		FSParentID: &bad.ID, FSPath: []factory.FSFolderHint{bad},
	}
	// 收下闭包，工程的目录挂不上。
	err = fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProject, proj, []factory.ClosureMember{proc}, &fid, nil))
	// 必须交回坏夹名的错误，不能当作全部挂好了。
	if !errors.Is(err, domain.ErrInvalidName) {
		// 错误不对就停，目录失败被吞掉或换了原因。
		t.Fatalf("accept with bad fs: %v", err)
	}
	// 后面的工艺副本必须照样落库。
	if _, err := fac.Store().LatestReplicaMeta(ctx, proc.ID); err != nil {
		// 工艺副本没落库就停，目录失败挡住了后面的成员。
		t.Fatalf("later replica: %v", err)
	}
	// 后面的工艺必须挂进自己的夹。
	wantFilePlaced(t, ctx, fac, store.FSFileHint{AssetID: proc.ID, ParentID: &procFolder.ID})
	// 工程副本本身也已落库，只是目录没挂上。
	if _, err := fac.Store().LatestReplicaMeta(ctx, proj.ID); err != nil {
		// 工程副本没落库就停，副本写入被目录拖累了。
		t.Fatalf("root replica: %v", err)
	}
	// 拉出审计流水，核对目录失败留了痕迹。
	rows, err := fac.ListAudit(ctx)
	// 拉审计失败就停，否则后面没有可靠结果。
	if err != nil {
		// 拉审计失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 先当没找到目录对齐拒绝。
	denied := false
	// 逐行找目录对齐拒绝的那一条。
	for _, r := range rows {
		// 动作是目录对齐、结果是拒绝，就算找到。
		if r.Action == "apply_fs" && r.Result == audit.Deny {
			// 命中一条就记下。
			denied = true
		}
	}
	// 没有目录对齐拒绝，说明失败没有记下来。
	if !denied {
		// 缺审计就停，目录失败只剩日志。
		t.Fatal("missing apply_fs deny audit")
	}
}

// 拼一条平台工艺成员；给了路径就挂到最后一层夹下。
func platProc(name string, rev int64, body []byte, path ...factory.FSFolderHint) factory.ClosureMember {
	// 先拼出不带路径的成员，身份现发。
	m := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: name,
		Status: factory.AssetAvailable, Copyable: false, Revision: rev, Content: body, Digest: digest.Sum(body),
	}
	// 带了路径才挂进夹，没带就落在平台根下。
	if len(path) > 0 {
		// 另拷一份父夹身份再取址，避免指到调用方的切片。
		parent := path[len(path)-1].ID
		// 文件挂到路径最后一层夹下。
		m.FSParentID = &parent
		// 路径原样带上，供本厂对齐各层夹。
		m.FSPath = path
	}
	return m
}

// 换成同一身份的另一修订，名字和正文随之改。
func reviseProc(m factory.ClosureMember, rev int64, name string, body []byte) factory.ClosureMember {
	// 修订号换成这次要下发的。
	m.Revision = rev
	// 名字换成这一修订在云端的名字。
	m.Name = name
	// 正文换成这一修订的。
	m.Content = body
	// 摘要按新正文重算，对不上会被当成篡改。
	m.Digest = digest.Sum(body)
	return m
}

// 把单条平台工艺封包后收下，收不下即停。
func deliverPlatProc(t *testing.T, ctx context.Context, fac *factory.Service, seed Seeded, m factory.ClosureMember) {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 另拷一份本厂身份再取址，快照要指到这家厂。
	fid := seed.ID
	// 收下平台下发没成功，后面的断言就没有依据。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, m, nil, &fid, nil)); err != nil {
		// 收下失败即停，不能把这一步的失败当成通过。
		t.Fatalf("deliver %s rev %d: %v", m.Name, m.Revision, err)
	}
}

// 核对平台夹还在，并且叫预期的名字。
func wantPlatFolder(t *testing.T, ctx context.Context, fac *factory.Service, want factory.FSFolderHint) {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 按身份读出这个夹。
	n, err := fac.Store().FSNodeByID(ctx, want.ID)
	// 夹不在或名字不对，都说明对齐没有落到位。
	if err != nil || n.NodeKind != store.FSNodeFolder || n.Name != want.Name {
		// 夹不对就停，把实际节点打出来。
		t.Fatalf("folder %s: %+v %v", want.Name, n, err)
	}
}

// 核对副本文件挂在预期的夹下；没给夹就该在平台根下。
func wantFilePlaced(t *testing.T, ctx context.Context, fac *factory.Service, want store.FSFileHint) store.FSNode {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 按资产找文件节点。
	file, err := fac.Store().FSFileByAsset(ctx, want.AssetID)
	// 文件节点找不到或没有挂点，就没法核对。
	if err != nil || file.ParentID == nil {
		// 文件不在就停，副本没有挂进目录。
		t.Fatalf("file %+v %v", file, err)
	}
	// 给了夹就必须挂在这个夹下。
	if want.ParentID != nil {
		// 挂点和预期的夹对不上。
		if *file.ParentID != *want.ParentID {
			// 挂点不对就停，文件没搬到预期的夹。
			t.Fatalf("file parent %s want %s", *file.ParentID, *want.ParentID)
		}
		return file
	}
	// 没给夹就读出父节点，核对它是平台根。
	parent, err := fac.Store().FSNodeByID(ctx, *file.ParentID)
	// 父节点读不出或不是根，文件就没挂回平台根。
	if err != nil || parent.ParentID != nil || parent.TreeLevel != store.FSTreePlatform {
		// 不在平台根下就停，把父节点打出来。
		t.Fatalf("file root parent %+v %v", parent, err)
	}
	return file
}

// 核对副本文件节点叫预期的名字。
func wantFileName(t *testing.T, ctx context.Context, fac *factory.Service, m factory.ClosureMember, name string) {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 按资产找文件节点。
	file, err := fac.Store().FSFileByAsset(ctx, m.ID)
	// 文件不在或名字不对，都说明名字没有同步对。
	if err != nil || file.Name != name {
		// 名字不对就停，把实际节点打出来。
		t.Fatalf("file name want %q got %+v %v", name, file, err)
	}
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

	// 新建丙夹，装一好一坏两份工艺，考整夹预检。
	gamma, err := fac.CreateFSFolder(ctx, pe.tok, factoryRoot.ID, "丙")
	// 新建丙夹没成功，后面的断言就没有依据。
	if err != nil {
		// 新建丙夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 丙下再建子夹，坏文件藏在里面考递归预检。
	gammaSub, err := fac.CreateFSFolder(ctx, pe.tok, gamma.ID, "丙子")
	// 新建子夹没成功，后面的断言就没有依据。
	if err != nil {
		// 新建子夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 建一份可复制的厂级工艺，旧逻辑会先把它复制出去。
	good, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "好工艺", []byte("good"))
	// 建好工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建好工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 建一份不可复制的厂级工艺，整夹复制应被它挡住。
	locked, err := fac.CreateProcess(ctx, pe.tok, direct, factory.AssetLevelFactory, "锁定工艺", []byte("locked"), false)
	// 建锁定工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建锁定工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 好工艺挂进丙夹，失败则前提断了。
	if _, err := fac.MoveAssetInto(ctx, pe.tok, good.ID, gamma.ID); err != nil {
		// 挂进丙夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 锁定工艺挂进丙子，失败则前提断了。
	if _, err := fac.MoveAssetInto(ctx, pe.tok, locked.ID, gammaSub.ID); err != nil {
		// 挂进丙子失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 记下复制前的节点数和资产数，复制被拒后要原样不变。
	nodesBefore, assetsBefore := fsTotals(t, ctx, fac, factory.KindProcess)
	// 夹里混着不可复制的工艺，整夹复制必须被拒。
	if _, err := fac.CopyFSNode(ctx, pe.tok, gamma.ID, beta.ID); !errors.Is(err, domain.ErrAssetNotCopyable) {
		// 没被拒就停，不可复制的工艺跟着夹被带走了。
		t.Fatalf("copy folder with locked: %v", err)
	}
	// 复制被拒之后再数一遍节点和资产。
	nodesAfter, assetsAfter := fsTotals(t, ctx, fac, factory.KindProcess)
	// 节点数或资产数变了，说明被拒前已经建了半截。
	if nodesAfter != nodesBefore || assetsAfter != assetsBefore {
		// 留下了半截文件夹或副本就停，复制不是整体操作。
		t.Fatalf("locked copy left nodes %d->%d assets %d->%d", nodesBefore, nodesAfter, assetsBefore, assetsAfter)
	}

	// 新建丁夹，只放一份已停用的工艺。
	delta, err := fac.CreateFSFolder(ctx, pe.tok, factoryRoot.ID, "丁")
	// 新建丁夹没成功，后面的断言就没有依据。
	if err != nil {
		// 新建丁夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 建一份厂级工艺，稍后发布再停用。
	off, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "停用工艺", []byte("off"))
	// 建工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 先发布成可用，才能停用。
	pub, err := fac.PublishAsset(ctx, pe.tok, off.ID, off.Revision)
	// 发布没成功，后面的断言就没有依据。
	if err != nil {
		// 发布失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 把工艺停用，复制时应被挡住。
	if _, err := fac.DisableAsset(ctx, pe.tok, off.ID, pub.Revision); err != nil {
		// 停用失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 停用工艺挂进丁夹，失败则前提断了。
	if _, err := fac.MoveAssetInto(ctx, pe.tok, off.ID, delta.ID); err != nil {
		// 挂进丁夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 记下复制前的节点数和资产数，复制被拒后要原样不变。
	nodesBefore, assetsBefore = fsTotals(t, ctx, fac, factory.KindProcess)
	// 夹里有停用的工艺，整夹复制必须被拒。
	if _, err := fac.CopyFSNode(ctx, pe.tok, delta.ID, beta.ID); !errors.Is(err, domain.ErrAssetNotAvailable) {
		// 没被拒就停，停用的工艺跟着夹被复制出去了。
		t.Fatalf("copy folder with disabled: %v", err)
	}
	// 复制被拒之后再数一遍节点和资产。
	nodesAfter, assetsAfter = fsTotals(t, ctx, fac, factory.KindProcess)
	// 节点数或资产数变了，说明被拒前已经建了空文件夹。
	if nodesAfter != nodesBefore || assetsAfter != assetsBefore {
		// 留下了空文件夹就停，复制不是整体操作。
		t.Fatalf("disabled copy left nodes %d->%d assets %d->%d", nodesBefore, nodesAfter, assetsBefore, assetsAfter)
	}

	// 列出工程目录，顺带建好工程树的各级根。
	projNodes, err := fac.ListFS(ctx, pe.tok, factory.KindProject)
	// 列出工程目录没成功，后面的断言就没有依据。
	if err != nil {
		// 列出工程目录失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 留出工程厂级根夹，工程复制的用例从这里建。
	var projRoot factory.FSNodeView
	// 逐个目录节点查看，用来认出工程厂级根。
	for _, n := range projNodes {
		// 无父节点的厂级夹就是工程厂级根，先把它认出来。
		if n.NodeKind == store.FSNodeFolder && n.TreeLevel == store.FSTreeFactory && n.ParentID == nil {
			// 命中没有父节点的厂级夹时，把它记下来。
			projRoot = n
		}
	}
	// 工程目录里必须有厂级根夹，否则后面没法往下建。
	if projRoot.ID == [16]byte{} {
		// 缺工程厂级根即停，后面的用例没有落脚处。
		t.Fatal("missing project factory root")
	}
	// 建一份厂级工程，拿它的文件节点试复制。
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "厂工程", []byte(`[]`), nil)
	// 建厂级工程没成功，后面的断言就没有依据。
	if err != nil {
		// 建厂级工程失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 按资产找工程的文件节点，失败则前提断了。
	projFile, err := fac.Store().FSFileByAsset(ctx, proj.ID)
	// 找不到工程文件节点，后面的断言就没有依据。
	if err != nil {
		// 找工程文件节点失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 工程树里建一个空夹当复制源。
	projEmpty, err := fac.CreateFSFolder(ctx, pe.tok, projRoot.ID, "工程空夹")
	// 新建工程空夹没成功，后面的断言就没有依据。
	if err != nil {
		// 新建工程空夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 工程树里再建一个夹当粘贴目标。
	projDest, err := fac.CreateFSFolder(ctx, pe.tok, projRoot.ID, "工程目标")
	// 新建工程目标夹没成功，后面的断言就没有依据。
	if err != nil {
		// 新建工程目标夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 记下复制前工程树的节点数和资产数。
	nodesBefore, assetsBefore = fsTotals(t, ctx, fac, factory.KindProject)
	// 工程树里复制文件必须因越权被拒。
	if _, err := fac.CopyFSNode(ctx, pe.tok, projFile.ID, projDest.ID); !errors.Is(err, domain.ErrForbidden) {
		// 没被拒就停，工程树还开着复制口子。
		t.Fatalf("copy project file: %v", err)
	}
	// 工程树里复制空文件夹也必须因越权被拒。
	if _, err := fac.CopyFSNode(ctx, pe.tok, projEmpty.ID, projDest.ID); !errors.Is(err, domain.ErrForbidden) {
		// 没被拒就停，工程树里能复制出文件夹。
		t.Fatalf("copy project folder: %v", err)
	}
	// 两次被拒之后再数一遍工程树。
	nodesAfter, assetsAfter = fsTotals(t, ctx, fac, factory.KindProject)
	// 节点数或资产数变了，说明被拒前留下了文件夹。
	if nodesAfter != nodesBefore || assetsAfter != assetsBefore {
		// 工程树多出东西就停，被拒的复制留了痕迹。
		t.Fatalf("project copy left nodes %d->%d assets %d->%d", nodesBefore, nodesAfter, assetsBefore, assetsAfter)
	}
	// 读出目标夹的下级，核对里面仍是空的。
	destKids, err := fac.Store().ListFSChildren(ctx, projDest.ID)
	// 目标夹里有东西或读不出，都说明复制留了痕迹。
	if err != nil || len(destKids) != 0 {
		// 目标夹不空就停，被拒的复制留下了文件夹。
		t.Fatalf("project dest kids %d %v", len(destKids), err)
	}

	// 超管列目录，能看见同事的个人树，也顺带建好自己的个人根。
	saNodes, err := fac.ListFS(ctx, sa, factory.KindProcess)
	// 超管列目录没成功，后面的断言就没有依据。
	if err != nil {
		// 超管列目录失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 留出同事的个人根，超管要往这里粘贴。
	var peRoot factory.FSNodeView
	// 逐个目录节点查看，认出同事的个人根。
	for _, n := range saNodes {
		// 无父节点、主人是同事的个人夹，就是同事的个人根。
		if n.NodeKind == store.FSNodeFolder && n.TreeLevel == store.FSTreePersonal && n.ParentID == nil && n.OwnerID != nil && *n.OwnerID == pe.acc.ID {
			// 命中同事的个人根时，把它记下来。
			peRoot = n
		}
	}
	// 超管必须能看见同事的个人根，否则没法验粘贴。
	if peRoot.ID == [16]byte{} {
		// 缺同事个人根即停，后面的用例没有落脚处。
		t.Fatal("missing pe personal root")
	}
	// 同事在自己树里建个人夹，用来装个人工艺。
	peFolder, err := fac.CreateFSFolder(ctx, pe.tok, peRoot.ID, "个人夹")
	// 新建个人夹没成功，后面的断言就没有依据。
	if err != nil {
		// 新建个人夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 把前面的个人工艺挪进个人夹，夹里就有一份文件。
	if _, err := fac.MoveFSNode(ctx, pe.tok, mineFile.ID, peFolder.ID); err != nil {
		// 挪进个人夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 记下同事个人根下的下级，被拒后不能多出空夹。
	peKidsBefore, err := fac.Store().ListFSChildren(ctx, peRoot.ID)
	// 下级读不出就没法比数量。
	if err != nil {
		// 读同事个人根失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 记下超管个人树里的文件数，被拒后不能多出草稿。
	saFilesBefore := personalFiles(t, ctx, fac, seed.SuperAdminID)
	// 记下复制前全部工艺目录的节点数和资产数。
	nodesBefore, assetsBefore = fsTotals(t, ctx, fac, factory.KindProcess)
	// 记下复制前已有的审计，事后只看新增的几条。
	auditBefore := auditIDs(t, ctx, fac)
	// 超管把同事的个人文件粘进同事的个人根，必须被拒。
	if _, err := fac.CopyFSNode(ctx, sa, mineFile.ID, peRoot.ID); !errors.Is(err, domain.ErrForbidden) {
		// 没被拒就停，副本会落到超管名下贴不过去。
		t.Fatalf("admin copy file into other personal: %v", err)
	}
	// 超管把同事的个人夹粘进同事的个人根，也必须被拒。
	if _, err := fac.CopyFSNode(ctx, sa, peFolder.ID, peRoot.ID); !errors.Is(err, domain.ErrForbidden) {
		// 没被拒就停，同事树里会先留下空夹。
		t.Fatalf("admin copy folder into other personal: %v", err)
	}
	// 拉出审计流水，核对被拒前没有先建副本或文件夹。
	rows, err := fac.ListAudit(ctx)
	// 拉审计失败就停，否则后面没有可靠结果。
	if err != nil {
		// 拉审计失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 数出两次粘贴新增的审计条数。
	fresh := 0
	// 逐行看审计，只看这两次粘贴新增的。
	for _, r := range rows {
		// 之前就有的审计跳过，不算这次的。
		if _, ok := auditBefore[r.ID]; ok {
			continue
		}
		// 新增一条就记一笔，最后核对条数。
		fresh++
		// 新增的只能是复制拒绝，有别的说明动手后才拒。
		if r.Action != "copy_fs" || r.Result != audit.Deny {
			// 出现建资产、建夹或回收的记录就停，拒绝来得太晚。
			t.Fatalf("unexpected audit %s %s", r.Action, r.Result)
		}
	}
	// 两次粘贴各记一条拒绝，不多不少。
	if fresh != 2 {
		// 条数不对就停，拒绝没记全或多做了事。
		t.Fatalf("fresh audit rows %d", fresh)
	}
	// 两次被拒之后再数一遍工艺目录。
	nodesAfter, assetsAfter = fsTotals(t, ctx, fac, factory.KindProcess)
	// 节点数或资产数变了，说明被拒前已经建了东西。
	if nodesAfter != nodesBefore || assetsAfter != assetsBefore {
		// 多出节点或资产就停，被拒的复制留了痕迹。
		t.Fatalf("other personal copy left nodes %d->%d assets %d->%d", nodesBefore, nodesAfter, assetsBefore, assetsAfter)
	}
	// 超管个人树的文件数必须不变，不能留下草稿。
	if got := personalFiles(t, ctx, fac, seed.SuperAdminID); got != saFilesBefore {
		// 超管树里多了文件就停，被拒前先建了副本。
		t.Fatalf("admin personal files %d->%d", saFilesBefore, got)
	}
	// 再读同事个人根的下级，核对没有多出空夹。
	peKidsAfter, err := fac.Store().ListFSChildren(ctx, peRoot.ID)
	// 下级数变了或读不出，都说明被拒的复制留了痕迹。
	if err != nil || len(peKidsAfter) != len(peKidsBefore) {
		// 同事个人根多出下级就停，被拒前先建了文件夹。
		t.Fatalf("pe root kids %d->%d %v", len(peKidsBefore), len(peKidsAfter), err)
	}
	// 同事本人在自己树里复制个人夹，照常可以。
	ownCopy, err := fac.CopyFSNode(ctx, pe.tok, peFolder.ID, peRoot.ID)
	// 主人自己的粘贴必须成功，并落在个人根下。
	if err != nil || ownCopy.ParentID == nil || *ownCopy.ParentID != peRoot.ID {
		// 主人自己都贴不进就停，拒绝收得太宽了。
		t.Fatalf("owner copy folder %+v %v", ownCopy, err)
	}
}

// 验收 64 字的文件夹和工艺复制回原夹：截短原名再接副本尾巴，不报同名。
func TestCopyFSLongName(t *testing.T) {
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
	// 建 64 字的厂级工艺，作为复制源文件。
	src, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, strings.Repeat("工", 64), []byte("long-fs"))
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
	// 留出厂级根夹，原文件夹建在它下面。
	var factoryRoot factory.FSNodeView
	// 逐个目录节点查看，用来认出厂级根夹。
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
	// 建原文件夹，长名夹和长名工艺都放这里，再复制回这里。
	home, err := fac.CreateFSFolder(ctx, pe.tok, factoryRoot.ID, "长名")
	// 建原文件夹没成功，后面的断言就没有依据。
	if err != nil {
		// 建原文件夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 夹名正好 64 字，第 59 字是空格，考截口处去空白。
	folderName := strings.Repeat("甲", 58) + " " + strings.Repeat("乙", 5)
	// 建长名文件夹，上限以内的名字应能建成。
	long, err := fac.CreateFSFolder(ctx, pe.tok, home.ID, folderName)
	// 建长名文件夹没成功，后面的断言就没有依据。
	if err != nil {
		// 建 64 字文件夹失败即停，不能当成通过。
		t.Fatal(err)
	}
	// 长名工艺挂进原文件夹，和长名夹同级。
	file, err := fac.MoveAssetInto(ctx, pe.tok, src.ID, home.ID)
	// 挂不进去就停，后面的断言就没有依据。
	if err != nil {
		// 长名工艺挂进原文件夹失败即停，不能当成通过。
		t.Fatal(err)
	}
	// 两个源各复制两回，名字应依次是副本和副本 (2)。
	cases := []struct {
		src  store.FSNode // 复制源节点
		want []string     // 两回复制依次应得的名字
	}{
		{long, []string{strings.Repeat("甲", 58) + " - 副本", strings.Repeat("甲", 55) + " - 副本 (2)"}},
		{file, []string{strings.Repeat("工", 59) + " - 副本", strings.Repeat("工", 55) + " - 副本 (2)"}},
	}
	// 逐个源核对，每回复制都要截短原名后接尾巴。
	for _, c := range cases {
		// 依次复制，名字按副本、副本 (2) 往下排。
		for _, want := range c.want {
			// 复制回原文件夹，同名已在，应截短原名后接尾巴。
			dup, err := fac.CopyFSNode(ctx, pe.tok, c.src.ID, home.ID)
			// 复制失败或没落在原文件夹就停，长名仍被当成同名。
			if err != nil || dup.ParentID == nil || *dup.ParentID != home.ID {
				// 长名复制回原文件夹失败即停，不能当成通过。
				t.Fatalf("copy long %q: %+v %v", c.src.Name, dup, err)
			}
			// 新名不对或超过上限就停，截短规则没生效。
			if dup.Name != want || utf8.RuneCountInString(dup.Name) > store.FSNameMax {
				// 复制出的名字不对即停，不能当成通过。
				t.Fatalf("copy name %q want %q", dup.Name, want)
			}
		}
	}
}

// 记下当前全部审计的标识，供事后挑出新增的几条。
func auditIDs(t *testing.T, ctx context.Context, fac *factory.Service) map[[16]byte]struct{} {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 读出全部审计，读不到就没法比。
	rows, err := fac.ListAudit(ctx)
	// 审计读失败就没法比新增。
	if err != nil {
		// 审计读失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 按条数准备标识集合。
	ids := make(map[[16]byte]struct{}, len(rows))
	// 逐行记下标识，后面据此认出新增的。
	for _, r := range rows {
		// 这一条已存在，记进集合。
		ids[r.ID] = struct{}{}
	}
	return ids
}

// 数出某人个人工艺树里的文件数，核对被拒的复制没在他名下留草稿。
func personalFiles(t *testing.T, ctx context.Context, fac *factory.Service, owner [16]byte) int {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 读出工艺目录全部节点，读不到就没法数。
	nodes, err := fac.Store().ListFSNodes(ctx, factory.KindProcess)
	// 目录读失败就没法数文件。
	if err != nil {
		// 目录读失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 从零开始数，只算这个人个人树里的文件。
	count := 0
	// 逐个节点看，挑出这个人个人树里的文件。
	for _, n := range nodes {
		// 是个人树、主人对得上、又是文件，才算一份。
		if n.TreeLevel == store.FSTreePersonal && n.OwnerID != nil && *n.OwnerID == owner && n.NodeKind == store.FSNodeFile {
			// 命中一份就加一。
			count++
		}
	}
	return count
}

// 数出某类目录的节点数和本厂资产数，核对被拒的复制什么都没多。
func fsTotals(t *testing.T, ctx context.Context, fac *factory.Service, kind string) (int, int) {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 读出该类全部目录节点，读不到就没法比。
	nodes, err := fac.Store().ListFSNodes(ctx, kind)
	// 目录读失败就没法比数量。
	if err != nil {
		// 目录读失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 读出本厂全部资产，读不到就没法比。
	assets, err := fac.Store().ListGovernedAssets(ctx)
	// 资产读失败就没法比数量。
	if err != nil {
		// 资产读失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 交回两个数量，由调用方比对复制前后。
	return len(nodes), len(assets)
}

// 验删文件夹先整体预检：后面的工艺仍被引用，前面的也不能先被删掉。
func TestFactoryFSDeleteFolderAllOrNothing(t *testing.T) {
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
	// 用直属上下文写工艺，不把它挂到车间。
	direct := factory.WorkContext{Direct: true}
	// 名字排前面的工艺，不被任何工程引用。
	free, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "a-未引用", []byte("free"))
	// 建未引用工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建未引用工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 名字排后面的工艺，稍后被工程钉住。
	used, err := fac.CreateFactoryProcess(ctx, pe.tok, direct, "b-被引用", []byte("used"))
	// 建被引用工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建被引用工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 发布后才能被工程依赖。
	pub, err := fac.PublishAsset(ctx, pe.tok, used.ID, used.Revision)
	// 发布没成功，后面的断言就没有依据。
	if err != nil {
		// 发布失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 建一条钉住后一个工艺的工程，造出引用。
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "钉工程", []byte(`[]`), []factory.AssetDep{{
		ID: pub.ID, Revision: pub.Revision, Digest: pub.Digest,
	}})
	// 建工程没成功，后面的断言就没有依据。
	if err != nil {
		// 建工程失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 列出可见目录，供后面步骤使用，失败则前提断了。
	nodes, err := fac.ListFS(ctx, pe.tok, factory.KindProcess)
	// 列出可见目录没成功，后面的断言就没有依据。
	if err != nil {
		// 列出可见目录失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 留出厂级根夹，待删夹要建在它下面。
	var factoryRoot factory.FSNodeView
	// 逐个目录节点查看，用来认出厂级根。
	for _, n := range nodes {
		// 无父节点的厂级夹就是厂级根，先把它认出来。
		if n.NodeKind == store.FSNodeFolder && n.TreeLevel == store.FSTreeFactory && n.ParentID == nil {
			// 命中没有父节点的厂级夹时，把它记下来。
			factoryRoot = n
		}
	}
	// 目录里必须有厂级根夹，否则后面没法往下建。
	if factoryRoot.ID == [16]byte{} {
		// 缺厂级根即停，后面的用例没有落脚处。
		t.Fatal("missing factory root")
	}
	// 建要删的文件夹，两个工艺都放进去。
	folder, err := fac.CreateFSFolder(ctx, pe.tok, factoryRoot.ID, "待删夹")
	// 新建文件夹没成功，后面的断言就没有依据。
	if err != nil {
		// 新建文件夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 逐个把两个工艺挂进文件夹。
	for _, a := range []factory.Asset{free, used} {
		// 挂进文件夹失败即停，两个工艺须同在一夹。
		if _, err := fac.MoveAssetInto(ctx, pe.tok, a.ID, folder.ID); err != nil {
			// 挂进文件夹失败即停，不能把这一步的失败当成通过。
			t.Fatal(err)
		}
	}
	// 删文件夹应整体被拒为仍被引用，放行或错类都算没拦住。
	if err := fac.DeleteFSNode(ctx, pe.tok, folder.ID); !errors.Is(err, domain.ErrReferenced) {
		// 没被拒或错类就停，删除不是整体操作。
		t.Fatalf("delete folder: %v", err)
	}
	// 逐个确认两个工艺都还在，并且仍挂在文件夹下。
	for _, a := range []factory.Asset{free, used} {
		// 读不到资产就说明已被删掉。
		if _, err := fac.GetAsset(ctx, pe.tok, a.ID); err != nil {
			// 工艺被删就停，前一个先被物理删除了。
			t.Fatalf("asset %s gone: %v", a.Name, err)
		}
		// 按资产找文件节点，应仍在原文件夹。
		file, err := fac.Store().FSFileByAsset(ctx, a.ID)
		// 找不到或不在文件夹下就停，目录被动过。
		if err != nil || file.ParentID == nil || *file.ParentID != folder.ID {
			// 文件节点不在原文件夹就停，被拒前已经动了目录。
			t.Fatalf("file %s %+v %v", a.Name, file, err)
		}
	}
	// 文件夹节点本身也应还在。
	if _, err := fac.Store().FSNodeByID(ctx, folder.ID); err != nil {
		// 文件夹没了就停，被拒的删除仍删掉了节点。
		t.Fatalf("folder gone: %v", err)
	}
	// 删掉钉住它的工程，解除阻塞。
	if err := fac.DeleteAsset(ctx, pe.tok, proj.ID); err != nil {
		// 删工程失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 阻塞解除后再删文件夹应成功。
	if err := fac.DeleteFSNode(ctx, pe.tok, folder.ID); err != nil {
		// 解除引用后仍删不掉就停，预检拦得太宽了。
		t.Fatalf("delete after unref: %v", err)
	}
	// 逐个确认两个工艺都已连带删掉。
	for _, a := range []factory.Asset{free, used} {
		// 读资产应被拒为找不到，还在就是没连带删。
		if _, err := fac.GetAsset(ctx, pe.tok, a.ID); !errors.Is(err, domain.ErrNotFound) {
			// 工艺还在就停，整夹删除没有连带删资产。
			t.Fatalf("asset %s should be gone: %v", a.Name, err)
		}
	}
	// 文件夹节点也应已删掉。
	if _, err := fac.Store().FSNodeByID(ctx, folder.ID); !errors.Is(err, domain.ErrNotFound) {
		// 文件夹还在就停，整夹删除没删掉节点。
		t.Fatalf("folder should be gone: %v", err)
	}
}

// 验收网页新建挂进文件夹：同名自动带编号；目录不在或是别人的个人夹就整笔不建。
func TestCreateAssetIntoFolder(t *testing.T) {
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
	// 登录取超管会话，后面拿它试管理员越主人。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建新建资产的人，他往厂级夹里建工艺。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 建同事，他的个人夹给别人当挂不上的目标。
	other := mustCreateRole(t, ctx, fac, sa, "ot", "ot-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 取厂级工艺树的根，公用夹要建在它下面。
	root, err := fac.Store().EnsureFSRoot(ctx, factory.KindProcess, store.FSTreeFactory, nil)
	// 取根失败就停，没有根建不了文件夹。
	if err != nil {
		// 取厂级根失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 建公用夹，两条同名工艺都建到这里。
	folder, err := fac.CreateFSFolder(ctx, pe.tok, root.ID, "公用夹")
	// 建公用夹失败就停，没有目标夹验不了挂目录。
	if err != nil {
		// 建公用夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 工作位置带上目标夹，新建时同一笔挂进去。
	into := factory.WorkContext{Direct: true, FolderID: folder.ID}
	// 第一条建进公用夹，名字应原样占位。
	first, err := fac.CreateFactoryProcess(ctx, pe.tok, into, "同名工艺", []byte("a"))
	// 第一条建不成就停，挂目录的前提断了。
	if err != nil {
		// 建第一条失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 第二条同名再建进去，应成功并自动带编号。
	second, err := fac.CreateFactoryProcess(ctx, pe.tok, into, "同名工艺", []byte("b"))
	// 同名被拒说明还按 409 挡新建。
	if err != nil {
		// 同名建进文件夹失败即停，重名没有自动带编号。
		t.Fatalf("same name into folder: %v", err)
	}
	// 两条依次应得的文件名：原名，原名加编号。
	want := []struct {
		row  factory.Asset // 新建出的资产
		name string        // 目录里应得的文件名
	}{{first, "同名工艺"}, {second, "同名工艺 " + second.Code}}
	// 逐条核对文件节点落在公用夹且名字对得上。
	for _, w := range want {
		// 按资产找文件节点，看它挂在哪里。
		file, err := fac.Store().FSFileByAsset(ctx, w.row.ID)
		// 没挂进公用夹或名字不对，说明没在同一笔挂好。
		if err != nil || file.ParentID == nil || *file.ParentID != folder.ID || file.Name != w.name {
			// 文件挂点或名字不对即停。
			t.Fatalf("file %+v want %q in %s: %v", file, w.name, folder.ID, err)
		}
	}
	// 取同事的个人根，在下面建同事夹。
	otherRoot, err := fac.Store().EnsureFSRoot(ctx, factory.KindProcess, store.FSTreePersonal, &other.acc.ID)
	// 取同事根失败就停，没有别人的夹可试。
	if err != nil {
		// 取同事个人根失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 同事自己建的个人夹，别人建的个人资产挂不进去。
	otherFolder, err := fac.CreateFSFolder(ctx, other.tok, otherRoot.ID, "同事夹")
	// 建同事夹失败就停，前提断了。
	if err != nil {
		// 建同事夹失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 记下被拒之前的节点数和资产数。
	nodes0, assets0 := fsTotals(t, ctx, fac, factory.KindProcess)
	// 三种挂不上的目标：目录不在、同事不可写、管理员可写但主人不是本人。
	cases := []struct {
		tok    string   // 新建人的会话
		folder [16]byte // 目标文件夹
		want   error    // 应得的拒绝
	}{
		{pe.tok, id.New(), domain.ErrNotFound},
		{pe.tok, otherFolder.ID, domain.ErrForbidden},
		{sa, otherFolder.ID, domain.ErrForbidden},
	}
	// 逐个目标试新建个人工艺，都应整笔不建。
	for _, c := range cases {
		// 建个人工艺到挂不上的目标，应按原因拒绝。
		_, err := fac.CreatePersonalProcess(ctx, c.tok, factory.WorkContext{Direct: true, FolderID: c.folder}, "挂不上", []byte("c"))
		// 放行或错误不对，说明挂目录没在建资产前把关。
		if !errors.Is(err, c.want) {
			// 拒绝原因不对即停。
			t.Fatalf("create into %x: %v want %v", c.folder, err, c.want)
		}
		// 再数一遍节点和资产，被拒的不应留下任何东西。
		nodes, assets := fsTotals(t, ctx, fac, factory.KindProcess)
		// 多出资产或节点，说明挂不上时没整笔回滚。
		if nodes != nodes0 || assets != assets0 {
			// 留下了半截即停。
			t.Fatalf("rejected create left rows: nodes %d->%d assets %d->%d", nodes0, nodes, assets0, assets)
		}
	}
}

// 验收节点组织管理员碰不了下属个人级，整厂管理员和超管仍能读。
func TestPersonalAssetNodeAdminDenied(t *testing.T) {
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
	// 登录取超管会话，后面建人建车间并对照读正文。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建车间，个人工艺记在它下面，节点管理员只管它。
	shop, err := fac.CreateOrgUnit(ctx, sa, "车间", nil)
	// 建车间没成功，后面的断言就没有依据。
	if err != nil {
		// 建车间失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 建车间操作员，他是这条个人工艺的主人。
	owner := mustCreateRole(t, ctx, fac, sa, "owner", "owner-pass", factory.RoleOperator, factory.ScopeOrgUnit, &shop.ID)
	// 把主人分到车间，才能用车间当工作上下文。
	if err := fac.Assign(ctx, sa, owner.acc.ID, shop.ID); err != nil {
		// 分配失败即停，主人没落在车间里。
		t.Fatal(err)
	}
	// 主人用车间上下文建个人工艺，资产记在车间下。
	mine, err := fac.CreatePersonalProcess(ctx, owner.tok, factory.WorkContext{OrgUnitID: &shop.ID}, "车间个人工艺", []byte("p"))
	// 建个人工艺没成功，后面的断言就没有依据。
	if err != nil {
		// 建个人工艺失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 资产须记在车间下，否则测不到节点管理员越权。
	if mine.OrgUnitID == nil || *mine.OrgUnitID != shop.ID {
		// 没记到车间即停，本用例的前提不成立。
		t.Fatalf("asset org unit %v want %s", mine.OrgUnitID, shop.ID)
	}
	// 主人先读一遍正文，后面拿它对照别人读到的。
	want, err := fac.ReadAssetContent(ctx, owner.tok, mine.ID)
	// 主人读不到自己的正文，后面的对照就没有依据。
	if err != nil {
		// 主人读正文失败即停，不能把这一步的失败当成通过。
		t.Fatal(err)
	}
	// 建只管车间的组织管理员，他不该碰下属个人级。
	shopAdmin := mustCreateRole(t, ctx, fac, sa, "oashop", "oashop-pass", factory.RoleOrgAdmin, factory.ScopeOrgUnit, &shop.ID)
	// 建整厂组织管理员，他和超管一样能打开个人级。
	facAdmin := mustCreateRole(t, ctx, fac, sa, "oafac", "oafac-pass", factory.RoleOrgAdmin, factory.ScopeFactory, nil)
	// 节点管理员读下属个人级正文必须被拒。
	if _, err := fac.ReadAssetContent(ctx, shopAdmin.tok, mine.ID); !errors.Is(err, domain.ErrForbidden) {
		// 能读到即停，个人级参数漏给了节点管理员。
		t.Fatalf("shop admin read: %v", err)
	}
	// 节点管理员改下属个人级正文必须被拒。
	if _, err := fac.UpdateAssetContent(ctx, shopAdmin.tok, mine.ID, mine.Revision, []byte("x")); !errors.Is(err, domain.ErrForbidden) {
		// 能改即停，别人的个人级被节点管理员覆盖了。
		t.Fatalf("shop admin update: %v", err)
	}
	// 节点管理员另存下属个人级工艺必须被拒。
	if _, err := fac.CopyProcess(ctx, shopAdmin.tok, mine.ID, "节点另存"); !errors.Is(err, domain.ErrForbidden) {
		// 能另存即停，个人级正文借复制漏了出去。
		t.Fatalf("shop admin copy: %v", err)
	}
	// 节点管理员删下属个人级必须被拒。
	if err := fac.DeleteAsset(ctx, shopAdmin.tok, mine.ID); !errors.Is(err, domain.ErrForbidden) {
		// 能删即停，别人的个人级被节点管理员删了。
		t.Fatalf("shop admin delete: %v", err)
	}
	// 整厂组织管理员和超管都应读到原样正文。
	for _, tok := range []string{facAdmin.tok, sa} {
		// 用整厂管理员身份读个人级正文。
		got, err := fac.ReadAssetContent(ctx, tok, mine.ID)
		// 读不到或内容变了，说明整厂口径被收窄或被改过。
		if err != nil || string(got) != string(want) {
			// 读失败或正文不对即停，打出实际结果。
			t.Fatalf("factory admin read: %q %v want %q", got, err, want)
		}
	}
}
