// 加密（不可复制）平台工艺：只在云端配置，厂端人人能用；复制工程时保持原身份，普通工艺复制一份。
package service_test

import (
	"bytes"
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

// encWeldBody 拼一份单层工程正文，每个工艺一条焊道。
func encWeldBody(procs ...uuid.UUID) []byte {
	// 先放开头的方括号。
	out := []byte("[")
	// 逐个工艺拼一条焊道。
	for i, p := range procs {
		// 第二条起要先接逗号。
		if i > 0 {
			// 接上分隔逗号。
			out = append(out, ',')
		}
		// 接上这条焊道。
		out = append(out, []byte(`{"templateId":"`+contenttpl.SeedTplSingle+`","kind":"single","name":"焊道","processId":"`+p.String()+`"}`)...)
	}
	// 收尾的方括号。
	return append(out, ']')
}

// encDepIDs 把依赖收成身份集合。
func encDepIDs(deps []factory.AssetDep) map[uuid.UUID]factory.AssetDep {
	// 按身份装依赖。
	out := make(map[uuid.UUID]factory.AssetDep, len(deps))
	// 逐条收进集合。
	for _, d := range deps {
		// 记下这条依赖。
		out[d.ID] = d
	}
	return out
}

// 个人工程可挂加密平台工艺；普通平台工艺仍须另存个人副本；示教器拉包带上加密工艺正文。
func TestEncryptedPlatformProcessInPersonalProject(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 开通失败就停。
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 把开通的错误打出来并停住。
		t.Fatal(err)
	}
	// 登录并取出令牌。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建一个普通操作员，不是管理员。
	_ = mustCreateRole(t, ctx, fac, sa, "op", "op-pass", factory.RoleOperator)
	// 记下本厂标识，下发的快照要指到这家厂。
	fid := seed.ID
	// 加密平台工艺的正文。
	encBody := []byte(`{"current":166}`)
	// 组一份加密（不可复制）的平台工艺。
	enc := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "加密工艺",
		Status: factory.AssetAvailable, Copyable: false, WeldKind: store.WeldKindSingle, Revision: 1, Content: encBody, Digest: digest.Sum(encBody),
	}
	// 收平台稿失败就停。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, enc, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住。
		t.Fatal(err)
	}
	// 普通平台工艺的正文。
	plainBody := []byte(`{"current":120}`)
	// 组一份可复制的平台工艺。
	plain := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "普通平台工艺",
		Status: factory.AssetAvailable, Copyable: true, WeldKind: store.WeldKindSingle, Revision: 1, Content: plainBody, Digest: digest.Sum(plainBody),
	}
	// 收平台稿失败就停。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, plain, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住。
		t.Fatal(err)
	}
	// 操作员用平板登录。
	sess := padSyncLogin(t, ctx, fac, "op", "op-pass")

	// 示教器上传个人工程挂加密平台工艺，应建成。
	proj, err := fac.CreatePadPersonal(ctx, sess.Token, factory.KindProject, "操作员工程", encWeldBody(enc.ID), uuid.Nil, "", nil, store.WeldKindSingle)
	// 被拒说明个人工程仍不让挂加密平台工艺。
	if err != nil {
		// 把示教器上传的错误打出来并停住。
		t.Fatalf("pad personal with encrypted: %v", err)
	}
	// 依赖应按原身份钉在加密工艺的第一修订。
	if len(proj.Deps) != 1 || proj.Deps[0].ID != enc.ID || proj.Deps[0].Revision != 1 || !bytes.Equal(proj.Deps[0].Digest, enc.Digest) {
		// 依赖没按原身份钉上。
		t.Fatalf("pad deps %+v", proj.Deps)
	}
	// 示教器带着显式依赖上传，也应建成。
	if _, err := fac.CreatePadPersonal(ctx, sess.Token, factory.KindProject, "带依赖", encWeldBody(enc.ID), uuid.Nil, "", proj.Deps, store.WeldKindSingle); err != nil {
		// 显式依赖被拒说明核依赖口径没放开。
		t.Fatalf("pad personal explicit deps: %v", err)
	}
	// 网页新建个人工程挂加密平台工艺，也应建成。
	web, err := fac.CreatePersonalProject(ctx, sess.Token, factory.WorkContext{}, "网页个人工程", encWeldBody(enc.ID), nil, store.WeldKindSingle)
	// 被拒说明网页入口没放开。
	if err != nil || len(web.Deps) != 1 || web.Deps[0].ID != enc.ID {
		// 把网页新建的结果打出来并停住。
		t.Fatalf("web personal with encrypted: %+v %v", web.Deps, err)
	}
	// 普通平台工艺不能直接挂进个人工程，须另存个人副本。
	if _, err := fac.CreatePadPersonal(ctx, sess.Token, factory.KindProject, "挂普通平台", encWeldBody(plain.ID), uuid.Nil, "", nil, store.WeldKindSingle); !errors.Is(err, domain.ErrAssetDependency) {
		// 没拦住说明普通工艺没按复制一份的口径走。
		t.Fatalf("pad personal with copyable platform: %v", err)
	}

	// 示教器清单里要有这份个人工程。
	box := padSyncInbox(t, ctx, fac, sess.Token)
	// 清单缺了个人工程，示教器拉不到。
	if _, ok := padInboxRef(box, proj.ID); !ok {
		// 清单不全就停。
		t.Fatalf("inbox missing personal project")
	}
	// 加密工艺也要在清单里，示教器才拉得到它生成程序。
	if _, ok := padInboxRef(box, enc.ID); !ok {
		// 清单缺了加密工艺就停。
		t.Fatalf("inbox missing encrypted process")
	}
	// 拉这份个人工程的包，工程根单独成包。
	gotProj, err := fac.PadPullClientClosure(ctx, sess.Token, proj.ID)
	// 拉包失败就停。
	if err != nil {
		// 把拉包的错误打出来并停住。
		t.Fatal(err)
	}
	// 用登录人钥解开工程包。
	openedProj, err := factory.OpenTransit(fac.Store().FactoryID(), sess.Account.ID, sess.UnwrapKey, gotProj)
	// 工程根应带着钉死的加密工艺依赖。
	if err != nil || len(openedProj.Members) == 0 || len(openedProj.Members[0].Deps) != 1 || openedProj.Members[0].Deps[0].ID != enc.ID {
		// 工程包不对就停。
		t.Fatalf("project closure %+v %v", openedProj.Members, err)
	}
	// 再拉加密工艺本身。
	gotEnc, err := fac.PadPullClientClosure(ctx, sess.Token, enc.ID)
	// 拉包失败就停。
	if err != nil {
		// 把拉包的错误打出来并停住。
		t.Fatal(err)
	}
	// 用登录人钥解开工艺包。
	openedEnc, err := factory.OpenTransit(fac.Store().FactoryID(), sess.Account.ID, sess.UnwrapKey, gotEnc)
	// 加密工艺正文照给示教器生成程序，可复制标记仍为否。
	if err != nil || len(openedEnc.Members) != 1 || openedEnc.Members[0].Copyable || !bytes.Equal(openedEnc.Members[0].Content, encBody) {
		// 工艺包不对就停。
		t.Fatalf("encrypted closure %+v %v", openedEnc.Members, err)
	}

	// 超管把这份个人工程升厂级，加密工艺跟着按原身份走。
	up, err := fac.PromoteToFactory(ctx, sa, proj.ID, uuid.Nil)
	// 升档失败或依赖变了就停。
	if err != nil || len(up.Deps) != 1 || up.Deps[0].ID != enc.ID {
		// 把升档结果打出来并停住。
		t.Fatalf("promote personal with encrypted: %+v %v", up.Deps, err)
	}
}

// 复制工程：加密平台工艺保持原身份，普通工艺复制一份并改指新身份，依赖和正文一致。
func TestCopyProjectKeepsEncryptedCopiesPlain(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 开通失败就停。
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 把开通的错误打出来并停住。
		t.Fatal(err)
	}
	// 登录并取出令牌。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建一个组织管理员，用来建厂级工程。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOrgAdmin)
	// 建一个普通操作员，用来复制个人工程。
	op := mustCreateRole(t, ctx, fac, sa, "op", "op-pass", factory.RoleOperator)
	// 记下本厂标识，下发的快照要指到这家厂。
	fid := seed.ID
	// 加密平台工艺的正文。
	encBody := []byte(`{"current":166}`)
	// 组一份加密（不可复制）的平台工艺。
	enc := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "加密工艺",
		Status: factory.AssetAvailable, Copyable: false, WeldKind: store.WeldKindSingle, Revision: 1, Content: encBody, Digest: digest.Sum(encBody),
	}
	// 收平台稿失败就停。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, enc, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住。
		t.Fatal(err)
	}
	// 普通厂级工艺的正文。
	plainBody := []byte(`{"current":180}`)
	// 建一份厂级工艺。
	plain, err := fac.CreateFactoryProcess(ctx, pe.tok, factory.WorkContext{}, "厂级工艺", plainBody, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 把建工艺的错误打出来并停住。
		t.Fatal(err)
	}
	// 发布厂级工艺，工程才钉得上。
	plain, err = fac.PublishAsset(ctx, pe.tok, plain.ID, plain.Revision)
	// 发布失败就停。
	if err != nil {
		// 把发布的错误打出来并停住。
		t.Fatal(err)
	}
	// 建一份厂级工程，同时挂加密平台工艺和普通厂级工艺。
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, factory.WorkContext{}, "源工程", encWeldBody(enc.ID, plain.ID), nil, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 把建工程的错误打出来并停住。
		t.Fatal(err)
	}
	// 复制前先数一下资产，复制后应只多出工程和一份工艺。
	before, err := fac.ListAssets(ctx, sa, "")
	// 列不出就停。
	if err != nil {
		// 把列表错误打出来并停住。
		t.Fatal(err)
	}

	// 复制工程，走网页复制入口。
	cp, err := fac.CopyAsset(ctx, pe.tok, proj.ID, "工程副本")
	// 复制失败就停。
	if err != nil {
		// 把复制的错误打出来并停住。
		t.Fatalf("copy project: %v", err)
	}
	// 副本是新身份的厂级工程草稿。
	if cp.ID == proj.ID || cp.Kind != factory.KindProject || cp.Level != factory.AssetLevelFactory || cp.Status != factory.AssetDraft || cp.Name != "工程副本" {
		// 副本元数据不对就停。
		t.Fatalf("copy row %+v", cp)
	}
	// 依赖收成集合便于核对。
	deps := encDepIDs(cp.Deps)
	// 加密工艺按原身份和原修订保留。
	if d, ok := deps[enc.ID]; len(cp.Deps) != 2 || !ok || d.Revision != 1 || !bytes.Equal(d.Digest, enc.Digest) {
		// 加密工艺没保持原引用。
		t.Fatalf("encrypted dep not kept %+v", cp.Deps)
	}
	// 普通工艺不应再按原身份引用。
	if _, ok := deps[plain.ID]; ok {
		// 普通工艺没复制。
		t.Fatalf("plain dep not copied %+v", cp.Deps)
	}
	// 找出普通工艺的新身份。
	var newPlain factory.AssetDep
	// 逐条找出不是加密工艺的那一条。
	for _, d := range cp.Deps {
		// 不是加密工艺的就是复制出的普通工艺。
		if d.ID != enc.ID {
			// 记下新身份。
			newPlain = d
		}
	}
	// 读复制出的工艺元数据。
	np, err := fac.GetAsset(ctx, pe.tok, newPlain.ID)
	// 复制出的工艺应是可用的厂级可复制工艺，修订摘要和依赖一致。
	if err != nil || np.Kind != factory.KindProcess || np.Level != factory.AssetLevelFactory || np.Status != factory.AssetAvailable ||
		!np.Copyable || np.Revision != newPlain.Revision || !bytes.Equal(np.Digest, newPlain.Digest) {
		// 复制出的工艺不对就停。
		t.Fatalf("copied process %+v %v", np, err)
	}
	// 读复制出的工艺正文。
	npBody, err := fac.ReadAssetContent(ctx, pe.tok, newPlain.ID)
	// 正文应和原件一样。
	if err != nil || !bytes.Equal(npBody, plainBody) {
		// 正文不同就停。
		t.Fatalf("copied process body %q %v", npBody, err)
	}
	// 读工程副本正文，核对引用已改指。
	body, err := fac.ReadAssetContent(ctx, pe.tok, cp.ID)
	// 读不到就停。
	if err != nil {
		// 把读正文的错误打出来并停住。
		t.Fatal(err)
	}
	// 收集副本正文里的工艺引用。
	refs, err := contenttpl.CollectProcessIDsFromItems(contenttpl.SeedProjectItems(), body)
	// 引用应正好是加密工艺和新工艺。
	if err != nil || len(refs) != 2 || refs[0] != enc.ID.String() || refs[1] != newPlain.ID.String() {
		// 正文引用和依赖对不上。
		t.Fatalf("copy refs %v %v", refs, err)
	}
	// 源工程依赖不动。
	src, err := fac.GetAsset(ctx, pe.tok, proj.ID)
	// 源工程的依赖应仍是原来两条。
	if err != nil || len(src.Deps) != 2 || encDepIDs(src.Deps)[plain.ID].ID != plain.ID {
		// 源工程被改了就停。
		t.Fatalf("source deps changed %+v %v", src.Deps, err)
	}
	// 复制后再数资产。
	after, err := fac.ListAssets(ctx, sa, "")
	// 应只多出工程副本和一份工艺。
	if err != nil || len(after) != len(before)+2 {
		// 多建或少建了资产。
		t.Fatalf("assets %d -> %d %v", len(before), len(after), err)
	}

	// 操作员建一份自己的个人工艺。
	mine, err := fac.CreatePersonalProcess(ctx, op.tok, factory.WorkContext{}, "我的工艺", plainBody, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 把建工艺的错误打出来并停住。
		t.Fatal(err)
	}
	// 发布个人工艺，工程才钉得上。
	mine, err = fac.PublishAsset(ctx, op.tok, mine.ID, mine.Revision)
	// 发布失败就停。
	if err != nil {
		// 把发布的错误打出来并停住。
		t.Fatal(err)
	}
	// 操作员建个人工程挂加密工艺和自己的工艺。
	pp, err := fac.CreatePersonalProject(ctx, op.tok, factory.WorkContext{}, "个人源工程", encWeldBody(enc.ID, mine.ID), nil, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 把建工程的错误打出来并停住。
		t.Fatal(err)
	}
	// 操作员复制自己的个人工程。
	pcp, err := fac.CopyAsset(ctx, op.tok, pp.ID, "个人副本")
	// 复制失败就停。
	if err != nil {
		// 把复制的错误打出来并停住。
		t.Fatalf("copy personal project: %v", err)
	}
	// 依赖收成集合便于核对。
	pdeps := encDepIDs(pcp.Deps)
	// 个人副本：加密工艺保持原身份，自己的工艺复制成新的个人工艺。
	if _, ok := pdeps[enc.ID]; pcp.Level != factory.AssetLevelPersonal || len(pcp.Deps) != 2 || !ok {
		// 个人副本依赖不对就停。
		t.Fatalf("personal copy %+v", pcp)
	}
	// 自己的工艺不应再按原身份引用。
	if _, ok := pdeps[mine.ID]; ok {
		// 普通工艺没复制。
		t.Fatalf("personal plain not copied %+v", pcp.Deps)
	}
	// 别人不能复制这份个人工程。
	if _, err := fac.CopyAsset(ctx, pe.tok, pp.ID, "偷复制"); !errors.Is(err, domain.ErrForbidden) {
		// 没拦住说明个人工程归属没保住。
		t.Fatalf("other copies personal project: %v", err)
	}
}

// 加密平台工艺只在云端配置：厂端网页和示教器改参数、改名、改类型、另存都拒，读正文 403。
func TestEncryptedPlatformProcessReadOnlyAtFactory(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 开通失败就停。
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 把开通的错误打出来并停住。
		t.Fatal(err)
	}
	// 登录并取出令牌，超管是厂里权限最大的人。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建一个操作员，走示教器入口。
	_ = mustCreateRole(t, ctx, fac, sa, "op", "op-pass", factory.RoleOperator)
	// 记下本厂标识，下发的快照要指到这家厂。
	fid := seed.ID
	// 加密平台工艺的正文。
	encBody := []byte(`{"current":166}`)
	// 组一份加密（不可复制）的平台工艺。
	enc := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "加密工艺",
		Status: factory.AssetAvailable, Copyable: false, WeldKind: store.WeldKindSingle, Revision: 1, Content: encBody, Digest: digest.Sum(encBody),
	}
	// 收平台稿失败就停。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, enc, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住。
		t.Fatal(err)
	}
	// 新参数正文，用来试改。
	next := []byte(`{"current":999}`)
	// 超管在网页改参数应拒。
	if _, err := fac.UpdateAssetContent(ctx, sa, enc.ID, 1, next); !errors.Is(err, domain.ErrForbidden) {
		// 没拦住说明厂端能改加密工艺参数。
		t.Fatalf("web update: %v", err)
	}
	// 超管在网页改名应拒。
	if _, err := fac.RenameAsset(ctx, sa, enc.ID, 1, "改名"); !errors.Is(err, domain.ErrForbidden) {
		// 没拦住说明厂端能改加密工艺。
		t.Fatalf("web rename: %v", err)
	}
	// 超管在网页改作业类型应拒。
	if _, err := fac.SetAssetWeldKind(ctx, sa, enc.ID, 1, store.WeldKindMultilayer); !errors.Is(err, domain.ErrForbidden) {
		// 没拦住说明厂端能改加密工艺。
		t.Fatalf("web weld kind: %v", err)
	}
	// 超管停用也应拒，状态由云端管。
	if _, err := fac.DisableAsset(ctx, sa, enc.ID, 1); !errors.Is(err, domain.ErrForbidden) {
		// 没拦住说明厂端能改加密工艺状态。
		t.Fatalf("web disable: %v", err)
	}
	// 另存应拒，加密工艺不能复制。
	if _, err := fac.CopyAsset(ctx, sa, enc.ID, "另存"); !errors.Is(err, domain.ErrAssetNotCopyable) {
		// 没拦住说明加密工艺能被另存出参数。
		t.Fatalf("web copy: %v", err)
	}
	// 超管读正文应 403。
	if _, err := fac.ReadAssetContent(ctx, sa, enc.ID); !errors.Is(err, domain.ErrForbidden) {
		// 没拦住说明厂端能看加密工艺参数。
		t.Fatalf("web read: %v", err)
	}
	// 操作员用平板登录。
	sess := padSyncLogin(t, ctx, fac, "op", "op-pass")
	// 示教器盖写加密工艺应拒。
	if _, err := fac.ApplyAppContent(ctx, sess.Token, enc.ID, next); !errors.Is(err, domain.ErrForbidden) {
		// 没拦住说明示教器能改加密工艺参数。
		t.Fatalf("pad apply: %v", err)
	}
	// 示教器读正文也应 403。
	if _, err := fac.ReadAssetContent(ctx, sess.Token, enc.ID); !errors.Is(err, domain.ErrForbidden) {
		// 没拦住说明示教器能看加密工艺参数。
		t.Fatalf("pad read: %v", err)
	}
	// 读回元数据核对原件没被改动。
	meta, err := fac.GetAsset(ctx, sa, enc.ID)
	// 修订、名字或可复制变了说明被改过。
	if err != nil || meta.Revision != 1 || meta.Name != enc.Name || meta.Copyable {
		// 原件被改了就停。
		t.Fatalf("encrypted changed %+v %v", meta, err)
	}
}

// 云端把加密工艺改回普通：已挂它的个人工程照常能拉能盖写；按原身份新钉被拒，另存个人副本后再钉可以。
func TestEncryptedPlatformProcessTurnedPlain(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 开通失败就停。
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 把开通的错误打出来并停住。
		t.Fatal(err)
	}
	// 登录并取出令牌。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建一个普通操作员。
	_ = mustCreateRole(t, ctx, fac, sa, "op", "op-pass", factory.RoleOperator)
	// 记下本厂标识，下发的快照要指到这家厂。
	fid := seed.ID
	// 工艺正文。
	encBody := []byte(`{"current":166}`)
	// 第一修订是加密的。
	enc := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "曾加密工艺",
		Status: factory.AssetAvailable, Copyable: false, WeldKind: store.WeldKindSingle, Revision: 1, Content: encBody, Digest: digest.Sum(encBody),
	}
	// 收平台稿失败就停。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, enc, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住。
		t.Fatal(err)
	}
	// 操作员用平板登录。
	sess := padSyncLogin(t, ctx, fac, "op", "op-pass")
	// 加密时挂进个人工程。
	proj, err := fac.CreatePadPersonal(ctx, sess.Token, factory.KindProject, "老工程", encWeldBody(enc.ID), uuid.Nil, "", nil, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 把建工程的错误打出来并停住。
		t.Fatal(err)
	}
	// 云端改回普通，下发第二修订。
	plain := enc
	// 修订升一。
	plain.Revision = 2
	// 改成可复制。
	plain.Copyable = true
	// 收平台稿失败就停。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, plain, nil, &fid, nil)); err != nil {
		// 把收平台稿的错误打出来并停住。
		t.Fatal(err)
	}

	// 老工程照常能拉。
	if _, err := fac.PadPullClientClosure(ctx, sess.Token, proj.ID); err != nil {
		// 改回普通后老工程拉不到。
		t.Fatalf("pull old project: %v", err)
	}
	// 工艺本身也能拉到新修订。
	gotProc, err := fac.PadPullClientClosure(ctx, sess.Token, enc.ID)
	// 拉不到或不是新修订就停。
	if err != nil || gotProc.Snapshot.Revision != 2 {
		// 改回普通后工艺拉不到。
		t.Fatalf("pull process: %+v %v", gotProc.Snapshot.Revision, err)
	}
	// 老工程原样盖写也照常，旧依赖不重钉。
	if _, err := fac.ApplyAppContent(ctx, sess.Token, proj.ID, encWeldBody(enc.ID)); err != nil {
		// 改回普通后老工程盖写不了。
		t.Fatalf("apply old project: %v", err)
	}
	// 带着加密时钉的旧依赖重传，也照常收下。
	if _, err := fac.CreatePadPersonal(ctx, sess.Token, factory.KindProject, "带旧依赖", encWeldBody(enc.ID), uuid.Nil, "", proj.Deps, store.WeldKindSingle); err != nil {
		// 加密时钉的旧依赖被拒了。
		t.Fatalf("old encrypted dep: %v", err)
	}
	// 按原身份新钉进个人工程应拒：现在是普通工艺，要先另存个人副本。
	if _, err := fac.CreatePadPersonal(ctx, sess.Token, factory.KindProject, "新钉原身份", encWeldBody(enc.ID), uuid.Nil, "", nil, store.WeldKindSingle); !errors.Is(err, domain.ErrAssetDependency) {
		// 没拦住说明普通工艺没按复制一份走。
		t.Fatalf("pin plain by original id: %v", err)
	}
	// 示教器另存个人副本。
	fork, err := fac.CreatePadPersonal(ctx, sess.Token, factory.KindProcess, "曾加密工艺", encBody, uuid.Nil, "", nil, store.WeldKindSingle)
	// 另存失败就停。
	if err != nil {
		// 把另存的错误打出来并停住。
		t.Fatal(err)
	}
	// 老工程改指个人副本后盖写，应收下。
	row, err := fac.ApplyAppContent(ctx, sess.Token, proj.ID, encWeldBody(fork.ID))
	// 盖写失败或依赖里没有副本就停。
	if err != nil || encDepIDs(row.Deps)[fork.ID].ID != fork.ID {
		// 改指副本后盖写不了。
		t.Fatalf("apply forked: %+v %v", row.Deps, err)
	}
	// 新工程钉个人副本，应建成。
	if _, err := fac.CreatePadPersonal(ctx, sess.Token, factory.KindProject, "钉副本", encWeldBody(fork.ID), uuid.Nil, "", nil, store.WeldKindSingle); err != nil {
		// 钉个人副本被拒。
		t.Fatalf("pin fork: %v", err)
	}
}
