// 平板保存即为可用；管理后台新建仍是草稿。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 平板保存后应为可用，管理后台新建则仍是草稿。
func TestPadSaveLandsAvailable(t *testing.T) {
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
	// 建管理员，厂级工程由它起草发布。
	pe := mustCreateRole(t, ctx, fac, sa, "pe-a", "pe-pass", factory.RoleOrgAdmin)
	// 建人并授好角色，失败说明后面没有账号可用。
	op := mustCreateRole(t, ctx, fac, sa, "op-a", "op-pass", factory.RoleOperator)
	// 不指定文件夹，后面新建资产都挂在对应树根。
	direct := factory.WorkContext{}
	// 准备这段正文字节，读回对不上说明没写进去。
	body := []byte(`{"name":"mine","current":170}`)

	// 新建一份个人工艺，失败说明个人入口被拒。
	draft, err := fac.CreatePersonalProcess(ctx, pe.tok, direct, "后台草稿", body)
	// 建个人工艺失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建个人工艺的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 状态应符合这一步的预期，停在旧状态说明没流转。
	if draft.Status != factory.AssetDraft {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("admin create: %+v", draft)
	}

	// 先登记 C0008 这台焊机，示教器自带的 C 段号才会被认。
	mustClientShortCode(t, ctx, fac, "C0008")
	// 新取一个编号，撞号会让两条记录分不清。
	localID := id.New()
	// 在平板上新建个人稿，失败说明会话或上下文不对。
	got, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "平板工艺", body, localID, "GY-C0008-000001", nil)
	// 平板建稿失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把平板建稿的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 作者应是本人，串到别人说明归属没保住。
	if got.ID != localID || got.Status != factory.AssetAvailable || got.Level != factory.AssetLevelPersonal ||
		got.CreatorID != op.acc.ID || got.Code != "GY-C0008-000001" || !got.Copyable || got.Content != nil {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("pad create: %+v", got)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	opened, err := fac.ReadAssetContent(ctx, op.tok, got.ID)
	// 读正文失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(opened, body) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("read %q %v", opened, err)
	}
	// 读回资产正文，失败说明无权读或稿已经没了。
	asSA, err := fac.ReadAssetContent(ctx, sa, got.ID)
	// 读正文失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(asSA, body) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("sa read %q %v", asSA, err)
	}
	// 准备这段正文字节，读回对不上说明没写进去。
	nextBody := []byte(`{"name":"mine","current":180}`)
	// 改写资产正文，失败说明修订冲突或越权。
	edited, err := fac.UpdateAssetContent(ctx, sa, got.ID, got.Revision, nextBody)
	// 改正文失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 换成改过的稿，后面要核对修订已经前进。
	got = edited
	// 换成新正文，读回不一致说明没写进去。
	body = nextBody
	// 读正文应因越权被拒绝，放行说明没拦住。
	if _, err := fac.ReadAssetContent(ctx, pe.tok, got.ID); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("pe read: %v", err)
	}

	// 平板建稿应因编号冲突被拒绝，放行说明没拦住。
	if _, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "重号", body, id.New(), "GY-C0008-000001", nil); !errors.Is(err, domain.ErrAssetCodeConflict) {
		// 资产编号和预期不一致，说明发号规则偏了。
		t.Fatalf("dup code: %v", err)
	}

	// 把编号打成文本，方便和路径或正文比对。
	projBody := []byte(`[{"processId":"` + got.ID.String() + `"}]`)
	// 新建一份厂级工程，失败说明起草入口坏了。
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, direct, "厂工程", []byte(`[]`), nil)
	// 建工程失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建工程的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 发布当前修订，失败说明状态不允许发布。
	proj, err = fac.PublishAsset(ctx, pe.tok, proj.ID, proj.Revision)
	// 发布失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把发布的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 装上这条依赖，修订或摘要错了工程会对不上。
	dep := factory.AssetDep{ID: got.ID, Revision: got.Revision, Digest: got.Digest}
	// 超管改写厂级工程依赖钉上操作员的个人工艺，失败说明工艺还不齐。
	pinned, err := fac.SetProjectDeps(ctx, sa, proj.ID, proj.Revision, []factory.AssetDep{dep})
	// 改依赖失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改依赖的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 改写资产正文，失败说明修订冲突或越权。
	updated, err := fac.UpdateAssetContent(ctx, sa, pinned.ID, pinned.Revision, projBody)
	// 改正文失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改正文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 修订号应按这次保存前进，没变说明写没落库。
	if updated.Revision <= pinned.Revision || !bytes.Equal(updated.Digest, digest.Sum(projBody)) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("update %+v", updated)
	}
	// 平板建稿应因越权被拒绝，放行说明没拦住。
	if _, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProject, "坏工程", []byte(`{"processPath":""}`), uuid.Nil, "", nil); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("processPath: %v", err)
	}
	// 平板建稿应因找不到被拒绝，放行说明没拦住。
	if _, err := fac.CreatePadPersonal(ctx, op.tok, "other", "坏", body, uuid.Nil, "", nil); !errors.Is(err, domain.ErrNotFound) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("kind: %v", err)
	}
}

// 示教器建厂级工程可钉已收平台副本和同事个人工艺，个人级仍按本人规则拒绝。
func TestPadCreateProjectDepsByLevel(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa-d", "超管D")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := seedPass(ctx, fac, "sa-d", "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出超管令牌，建厂级工程要用管理员。
	sa := mustLogin(t, ctx, fac, "sa-d", "sa-pass")
	// 建工艺的同事，他的个人工艺给别人的工程去钉。
	op := mustCreateRole(t, ctx, fac, sa, "op-d", "op-pass", factory.RoleOperator)
	// 另一名操作员，用来试个人级工程钉别人的工艺。
	pe := mustCreateRole(t, ctx, fac, sa, "pe-d", "pe-pass", factory.RoleOperator)
	// 留出本厂身份，收平台下发时要指明目标厂。
	fid := seed.ID

	// 准备一条平台工艺的正文。
	platBody := []byte("plat-body")
	// 组装平台工艺，号是云端 W 段。
	plat := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "平台焊",
		Code: "GY-W-000001", Status: factory.AssetAvailable, Copyable: true, Revision: 1,
		Content: platBody, Digest: digest.Sum(platBody),
	}
	// 收下平台工艺失败就停，后面没有平台副本可钉。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, plat, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住。
		t.Fatal(err)
	}
	// 同事在示教器上建一条个人工艺，建成即可用。
	mine, err := fac.CreatePadPersonal(ctx, op.tok, factory.KindProcess, "同事工艺", []byte(`{"name":"op","current":170}`), id.New(), "", nil)
	// 建个人工艺失败就停，后面没有同事工艺可钉。
	if err != nil {
		// 把建个人工艺的错误打出来并停住。
		t.Fatal(err)
	}
	// 平台副本的依赖，修订和摘要钉到收下的那版。
	platDep := factory.AssetDep{ID: plat.ID, Revision: plat.Revision, Digest: plat.Digest}
	// 同事个人工艺的依赖，钉到建成时的修订。
	mineDep := factory.AssetDep{ID: mine.ID, Revision: mine.Revision, Digest: mine.Digest}

	// 管理员在示教器上建厂级工程，同时钉两条依赖。
	proj, err := fac.CreatePad(ctx, sa, factory.AssetLevelFactory, factory.KindProject, "厂级工程", []byte(`[]`), id.New(), "", []factory.AssetDep{platDep, mineDep})
	// 被拒说明厂级工程仍按个人级规则核依赖。
	if err != nil {
		// 把建厂级工程的错误打出来并停住。
		t.Fatalf("factory project: %v", err)
	}
	// 读回工程元数据，核对级别、状态和依赖。
	got, err := fac.GetAsset(ctx, sa, proj.ID)
	// 读不回或级别状态不对，说明没按厂级建成。
	if err != nil || got.Level != factory.AssetLevelFactory || got.Status != factory.AssetAvailable || len(got.Deps) != 2 {
		// 把读回的工程打出来并停住。
		t.Fatalf("factory project meta %+v %v", got, err)
	}
	// 按身份收好期望的依赖，逐条比对修订和摘要。
	want := map[uuid.UUID]factory.AssetDep{platDep.ID: platDep, mineDep.ID: mineDep}
	// 逐条核对读回的依赖，钉错修订或摘要都算失败。
	for _, d := range got.Deps {
		// 按身份找期望依赖，找不到说明钉进了别的工艺。
		w, ok := want[d.ID]
		// 身份、修订或摘要不对，说明依赖没钉准。
		if !ok || d.Revision != w.Revision || !bytes.Equal(d.Digest, w.Digest) {
			// 把对不上的依赖打出来并停住。
			t.Fatalf("factory project dep %+v want %+v", d, w)
		}
	}

	// 个人级只钉同事的个人工艺，留出身份好核没建成。
	colleagueID := id.New()
	// 个人工程不能钉别人的个人工艺，应按依赖不合格拒绝。
	if _, err := fac.CreatePad(ctx, pe.tok, factory.AssetLevelPersonal, factory.KindProject, "钉同事", []byte(`[]`), colleagueID, "", []factory.AssetDep{mineDep}); !errors.Is(err, domain.ErrAssetDependency) {
		// 放行或错误不对，说明个人级规则被放宽了。
		t.Fatalf("personal project with colleague dep: %v", err)
	}
	// 被拒的工程不应落库，读回应当找不到。
	if _, err := fac.GetAsset(ctx, sa, colleagueID); !errors.Is(err, domain.ErrNotFound) {
		// 找到了说明被拒的工程仍被写进库。
		t.Fatalf("rejected colleague project exists: %v", err)
	}
	// 个人级钉平台副本，留出身份好核没建成。
	platOnlyID := id.New()
	// 个人工程不能钉平台副本，只要求被拒。
	if _, err := fac.CreatePad(ctx, pe.tok, factory.AssetLevelPersonal, factory.KindProject, "钉平台", []byte(`[]`), platOnlyID, "", []factory.AssetDep{platDep}); err == nil {
		// 放行说明个人工程能钉平台副本了。
		t.Fatal("personal project with platform dep accepted")
	}
	// 被拒的工程不应落库，读回应当找不到。
	if _, err := fac.GetAsset(ctx, sa, platOnlyID); !errors.Is(err, domain.ErrNotFound) {
		// 找到了说明被拒的工程仍被写进库。
		t.Fatalf("rejected platform project exists: %v", err)
	}
}

// 示教器建到已删的夹或同事的个人夹时退回所属树根，照样建成；自己的夹照常挂进去。
func TestPadCreateFolderFallsBackToRoot(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa-f", "超管F")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := seedPass(ctx, fac, "sa-f", "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录取超管会话，后面拿它试管理员建到同事夹。
	sa := mustLogin(t, ctx, fac, "sa-f", "sa-pass")
	// 示教器上的操作员，在自己的个人树里建。
	op := mustCreateRole(t, ctx, fac, sa, "op-f", "op-pass", factory.RoleOperator)
	// 同事，他的个人夹给管理员当目标。
	mate := mustCreateRole(t, ctx, fac, sa, "mate-f", "mate-pass", factory.RoleOperator)
	// 准备这段正文字节，示教器每条都用它。
	body := []byte(`{"name":"pad","current":170}`)
	// 取操作员的个人根，夹都建在它下面。
	opRoot, err := fac.Store().EnsureFSRoot(ctx, factory.KindProcess, store.FSTreePersonal, &op.acc.ID)
	// 取个人根失败就停，没有地方建夹。
	if err != nil {
		// 把取个人根的错误打出来并停住。
		t.Fatal(err)
	}
	// 建一个留着的夹，正常挂目录要落在这里。
	keep, err := fac.CreateFSFolder(ctx, op.tok, opRoot.ID, "现场夹")
	// 建夹失败就停，前提断了。
	if err != nil {
		// 把建夹的错误打出来并停住。
		t.Fatal(err)
	}
	// 再建一个夹随即删掉，模拟示教器拿着旧目录。
	gone, err := fac.CreateFSFolder(ctx, op.tok, opRoot.ID, "旧夹")
	// 建夹失败就停，前提断了。
	if err != nil {
		// 把建夹的错误打出来并停住。
		t.Fatal(err)
	}
	// 删掉旧夹失败就停，后面验不了退回根。
	if err := fac.DeleteFSNode(ctx, op.tok, gone.ID); err != nil {
		// 把删夹的错误打出来并停住。
		t.Fatal(err)
	}
	// 管理员要建到的同事个人根。
	mateRoot, err := fac.Store().EnsureFSRoot(ctx, factory.KindProcess, store.FSTreePersonal, &mate.acc.ID)
	// 取同事根失败就停，没有别人的夹可试。
	if err != nil {
		// 把取同事根的错误打出来并停住。
		t.Fatal(err)
	}
	// 同事自己建的个人夹，管理员的个人资产挂不进去。
	mateFolder, err := fac.CreateFSFolder(ctx, mate.tok, mateRoot.ID, "同事夹")
	// 建同事夹失败就停，前提断了。
	if err != nil {
		// 把建同事夹的错误打出来并停住。
		t.Fatal(err)
	}
	// 三种情形：自己的夹、已删的夹、管理员建到同事夹。
	cases := []struct {
		tok    string   // 示教器登录的会话
		name   string   // 新建资产的名字
		folder [16]byte // 示教器带来的父文件夹
		home   [16]byte // 应落的文件夹；空则落新建人的个人根
	}{
		{op.tok, "进夹", keep.ID, keep.ID},
		{op.tok, "夹已删", gone.ID, [16]byte{}},
		{sa, "管理员个人", mateFolder.ID, [16]byte{}},
	}
	// 逐个情形新建，都应成功并落到该落的地方。
	for _, c := range cases {
		// 示教器按个人级新建并带上父文件夹。
		row, err := fac.CreatePadIn(ctx, c.tok, factory.AssetLevelPersonal, factory.KindProcess, c.name, body, id.New(), "", nil, c.folder)
		// 被拒说明挂不上时没退回根，示教器会一直重传。
		if err != nil {
			// 把新建的错误打出来并停住。
			t.Fatalf("pad create %s: %v", c.name, err)
		}
		// 先按应落的夹算，空的换成新建人的个人根。
		home := c.home
		// 应落根时按创建人取个人根，管理员落他自己的根。
		if home == [16]byte{} {
			// 取新建人的个人根，确认资产没进别人的树。
			own, err := fac.Store().EnsureFSRoot(ctx, factory.KindProcess, store.FSTreePersonal, &row.CreatorID)
			// 取根失败就停，没法核挂点。
			if err != nil {
				// 把取根的错误打出来并停住。
				t.Fatal(err)
			}
			// 改按新建人的个人根核对。
			home = own.ID
		}
		// 按资产找文件节点，看它挂在哪里。
		file, err := fac.Store().FSFileByAsset(ctx, row.ID)
		// 挂点或主人不对，说明退回的不是本人的树根。
		if err != nil || file.ParentID == nil || *file.ParentID != home || file.OwnerID == nil || *file.OwnerID != row.CreatorID {
			// 把文件节点打出来并停住。
			t.Fatalf("pad %s file %+v want parent %x: %v", c.name, file, home, err)
		}
	}
}
