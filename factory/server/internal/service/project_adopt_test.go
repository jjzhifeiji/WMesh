// 厂级工程引用个人工艺：保存时复制成新的厂级工艺再引用（D-101），加上工艺正文和改名拒空名（D-110）。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// adoptEnv 是这组用例共用的一个厂：超管、管理员（不是超管）和工艺创建人。
type adoptEnv struct {
	ctx   context.Context  // 无取消的上下文
	fac   *factory.Service // 厂服务
	sa    string           // 超管令牌
	admin namedAcc         // 管理员，能写厂级，看不到别人的个人工艺
	owner namedAcc         // 操作员，个人工艺的创建人
}

// newAdoptEnv 开厂并建好三个人。
func newAdoptEnv(t *testing.T) adoptEnv {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 准备无取消的上下文。
	ctx := context.Background()
	// 拉起隔离厂库。
	h := New(t)
	// 建厂并签发超管。
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 开通超管口令。
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 超管登录。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 交回建好的环境。
	return adoptEnv{
		ctx: ctx, fac: fac, sa: sa,
		admin: mustCreateRole(t, ctx, fac, sa, "admin", "admin-pass", factory.RoleOrgAdmin),
		owner: mustCreateRole(t, ctx, fac, sa, "owner", "owner-pass", factory.RoleOperator),
	}
}

// personal 建一条可用的个人工艺。
func (e adoptEnv) personal(t *testing.T, tok, name string) factory.Asset {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 建草稿。
	p, err := e.fac.CreatePersonalProcess(e.ctx, tok, factory.WorkContext{}, name, []byte(`{"name":"`+name+`","current":160}`))
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 发布成可用，才能被依赖。
	p, err = e.fac.PublishAsset(e.ctx, tok, p.ID, p.Revision)
	// 发布失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	return p
}

// factoryProcessCount 数本厂厂级工艺的条数，核对有没有多复制或留下没回收的。
func (e adoptEnv) factoryProcessCount(t *testing.T) int {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 超管列工艺。
	rows, err := e.fac.ListAssets(e.ctx, e.sa, factory.KindProcess)
	// 列表读失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 从零数起。
	n := 0
	// 逐条看级别。
	for _, r := range rows {
		// 只数厂级。
		if r.Level == factory.AssetLevelFactory {
			// 数上这一条。
			n++
		}
	}
	return n
}

// body 读回工程正文。
func (e adoptEnv) body(t *testing.T, projectID uuid.UUID) string {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 超管读正文。
	raw, err := e.fac.ReadAssetContent(e.ctx, e.sa, projectID)
	// 读失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 转成文本方便比对。
	return string(raw)
}

// 网页新建：两条焊道引用同一个人工艺只复制一份，正文和依赖都改成新厂级工艺，个人工艺不动，操作员拿得到。
func TestAdoptOnFactoryCreate(t *testing.T) {
	// 开厂建人。
	e := newAdoptEnv(t)
	// 管理员自己的个人工艺。
	mine := e.personal(t, e.admin.tok, "管理员工艺")
	// 两条焊道引用同一条。
	proj, err := e.fac.CreateFactoryProject(e.ctx, e.admin.tok, factory.WorkContext{}, "厂工程", encWeldBody(mine.ID, mine.ID), nil)
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 只复制一份，依赖里也只有这一条。
	if len(proj.PersonalCopies) != 1 || len(proj.Deps) != 1 || proj.Deps[0].ID != proj.PersonalCopies[0].ID || proj.Deps[0].ID == mine.ID {
		// 把结果打出来。
		t.Fatalf("copies %+v deps %+v", proj.PersonalCopies, proj.Deps)
	}
	// 新工艺身份。
	copyID := proj.Deps[0].ID
	// 回包带改写后的正文，调用方手里的才不旧。
	if proj.SavedContent == nil || strings.Contains(*proj.SavedContent, mine.ID.String()) || strings.Count(*proj.SavedContent, copyID.String()) != 2 {
		// 把结果打出来。
		t.Fatalf("saved content %v", proj.SavedContent)
	}
	// 落库的正文和回包一致。
	if got := e.body(t, proj.ID); got != *proj.SavedContent {
		// 把结果打出来。
		t.Fatalf("stored %s", got)
	}
	// 新工艺是厂级、可用，记着来源。
	got, err := e.fac.GetAsset(e.ctx, e.sa, copyID)
	// 读不到或属性不对就是复制错了。
	if err != nil || got.Level != factory.AssetLevelFactory || got.Status != factory.AssetAvailable || got.SourceID == nil || *got.SourceID != mine.ID || got.CreatorID != e.admin.acc.ID || got.Name != mine.Name || !bytes.Equal(got.Digest, mine.Digest) {
		// 把结果打出来。
		t.Fatalf("copy %+v %v", got, err)
	}
	// 个人工艺原样不动。
	src, err := e.fac.GetAsset(e.ctx, e.sa, mine.ID)
	// 读不到或修订变了就是动了原件。
	if err != nil || src.Level != factory.AssetLevelPersonal || src.Revision != mine.Revision {
		// 把结果打出来。
		t.Fatalf("source %+v %v", src, err)
	}
	// 操作员示教器清单里有这条厂级工艺，缺件来源堵上了。
	mustCreateRole(t, e.ctx, e.fac, e.sa, "op", "op-pass", factory.RoleOperator)
	// 操作员登录平板。
	sess := padSyncLogin(t, e.ctx, e.fac, "op", "op-pass")
	// 拉清单。
	box := padSyncInbox(t, e.ctx, e.fac, sess.Token)
	// 有工艺才打得开工程；个人原件仍不给。
	if !padInboxHas(box, copyID) || padInboxHas(box, mine.ID) {
		// 把结果打出来。
		t.Fatalf("inbox %+v", box.Closures)
	}
}

// 没引用个人工艺的厂级工程：回包和正文都和以前一样。
func TestAdoptLeavesOtherLevelsAlone(t *testing.T) {
	// 开厂建人。
	e := newAdoptEnv(t)
	// 建一条厂级工艺。
	fp, err := e.fac.CreateFactoryProcess(e.ctx, e.admin.tok, factory.WorkContext{}, "厂级工艺", []byte(`{"name":"f"}`))
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 发布成可用。
	fp, err = e.fac.PublishAsset(e.ctx, e.admin.tok, fp.ID, fp.Revision)
	// 发布失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 建引用它的厂级工程。
	proj, err := e.fac.CreateFactoryProject(e.ctx, e.admin.tok, factory.WorkContext{}, "厂工程", encWeldBody(fp.ID), nil)
	// 不该复制，也不该带改写正文。
	if err != nil || proj.SavedContent != nil || len(proj.PersonalCopies) != 0 || proj.Deps[0].ID != fp.ID {
		// 把结果打出来。
		t.Fatalf("factory ref %+v %v", proj, err)
	}
	// 个人工艺。
	mine := e.personal(t, e.owner.tok, "我的工艺")
	// 个人工程还是直接钉自己的工艺，不复制。
	own, err := e.fac.CreatePersonalProject(e.ctx, e.owner.tok, factory.WorkContext{}, "我的工程", encWeldBody(mine.ID), nil)
	// 不该复制。
	if err != nil || own.SavedContent != nil || len(own.PersonalCopies) != 0 || own.Deps[0].ID != mine.ID {
		// 把结果打出来。
		t.Fatalf("personal project %+v %v", own, err)
	}
	// 厂级工艺只有原来那一条。
	if n := e.factoryProcessCount(t); n != 1 {
		// 把结果打出来。
		t.Fatalf("factory processes %d", n)
	}
}

// 网页保存的三步（先改依赖、再改正文、再改依赖）手里都是旧身份，只复制一份，最后都对回副本。
func TestAdoptWebSaveSteps(t *testing.T) {
	// 开厂建人。
	e := newAdoptEnv(t)
	// 管理员自己的个人工艺。
	mine := e.personal(t, e.admin.tok, "管理员工艺")
	// 先建一份空工程。
	proj, err := e.fac.CreateFactoryProject(e.ctx, e.admin.tok, factory.WorkContext{}, "厂工程", []byte(`[]`), nil)
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 依赖声明成这份个人工艺。
	dep := factory.AssetDep{ID: mine.ID, Revision: mine.Revision, Digest: mine.Digest}
	// 第一步改依赖。
	step1, err := e.fac.SetProjectDeps(e.ctx, e.admin.tok, proj.ID, proj.Revision, []factory.AssetDep{dep})
	// 这一步就复制出副本。
	if err != nil || len(step1.PersonalCopies) != 1 || len(step1.Deps) != 1 || step1.Deps[0].ID == mine.ID {
		// 把结果打出来。
		t.Fatalf("step1 %+v %v", step1, err)
	}
	// 副本身份。
	copyID := step1.Deps[0].ID
	// 第二步改正文，手里还写着个人工艺。
	step2, err := e.fac.UpdateAssetContent(e.ctx, e.admin.tok, proj.ID, step1.Revision, encWeldBody(mine.ID))
	// 对回副本，不再复制，回包带改写后的正文。
	if err != nil || len(step2.PersonalCopies) != 0 || step2.SavedContent == nil || !strings.Contains(*step2.SavedContent, copyID.String()) || strings.Contains(*step2.SavedContent, mine.ID.String()) {
		// 把结果打出来。
		t.Fatalf("step2 %+v %v", step2, err)
	}
	// 第三步再改依赖，手里还是个人工艺。
	step3, err := e.fac.SetProjectDeps(e.ctx, e.admin.tok, proj.ID, step2.Revision, []factory.AssetDep{dep})
	// 依赖还是副本，不再复制。
	if err != nil || len(step3.PersonalCopies) != 0 || len(step3.Deps) != 1 || step3.Deps[0].ID != copyID {
		// 把结果打出来。
		t.Fatalf("step3 %+v %v", step3, err)
	}
	// 从头到尾只多出一条厂级工艺。
	if n := e.factoryProcessCount(t); n != 1 {
		// 把结果打出来。
		t.Fatalf("factory processes %d", n)
	}
}

// 示教器：另存厂级工程和盖正文走同一规则；旧正文再传对回已有副本，不再复制；回包摘要和本机不同，示教器据此重拉。
func TestAdoptPadPaths(t *testing.T) {
	// 开厂建人。
	e := newAdoptEnv(t)
	// 管理员在示教器上建的个人工艺。
	mine, err := e.fac.CreatePadPersonal(e.ctx, e.admin.tok, factory.KindProcess, "本机工艺", []byte(`{"name":"本机工艺","current":170}`), id.New(), "", nil)
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 本机正文写的是个人工艺。
	stale := encWeldBody(mine.ID)
	// 示教器身份新建厂级工程，依赖也带个人工艺。
	proj, err := e.fac.CreatePad(e.ctx, e.admin.tok, factory.AssetLevelFactory, factory.KindProject, "厂工程", stale, id.New(), "", []factory.AssetDep{{ID: mine.ID, Revision: mine.Revision, Digest: mine.Digest}})
	// 应当复制一份，回包带新正文。
	if err != nil || len(proj.PersonalCopies) != 1 || proj.SavedContent == nil || len(proj.Deps) != 1 || proj.Deps[0].ID == mine.ID {
		// 把结果打出来。
		t.Fatalf("pad create %+v %v", proj, err)
	}
	// 副本身份。
	copyID := proj.Deps[0].ID
	// 回包丢了或本机还没重拉：下一轮把旧正文再盖一遍。
	again, err := e.fac.ApplyAppContent(e.ctx, e.admin.tok, proj.ID, stale)
	// 对回已有副本，不再复制。
	if err != nil || len(again.PersonalCopies) != 0 || again.SavedContent == nil || !strings.Contains(*again.SavedContent, copyID.String()) {
		// 把结果打出来。
		t.Fatalf("pad apply stale %+v %v", again, err)
	}
	// 依赖也还是副本。
	if len(again.Deps) != 1 || again.Deps[0].ID != copyID {
		// 把结果打出来。
		t.Fatalf("pad apply deps %+v", again.Deps)
	}
	// 个人工艺又来一条，示教器改正文引用它。
	second, err := e.fac.CreatePadPersonal(e.ctx, e.admin.tok, factory.KindProcess, "本机工艺二", []byte(`{"name":"本机工艺二","current":171}`), id.New(), "", nil)
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 盖一份同时引用副本和新个人工艺的正文。
	mixed, err := e.fac.ApplyAppContent(e.ctx, e.admin.tok, proj.ID, encWeldBody(copyID, second.ID))
	// 只复制新的那条。
	if err != nil || len(mixed.PersonalCopies) != 1 || mixed.PersonalCopies[0].SourceID != second.ID || len(mixed.Deps) != 2 {
		// 把结果打出来。
		t.Fatalf("pad apply mixed %+v %v", mixed, err)
	}
	// 厂级工艺一共两条。
	if n := e.factoryProcessCount(t); n != 2 {
		// 把结果打出来。
		t.Fatalf("factory processes %d", n)
	}
	// 示教器新建撞号：工程没落成，刚复制的要回收。
	third, err := e.fac.CreatePadPersonal(e.ctx, e.admin.tok, factory.KindProcess, "本机工艺三", []byte(`{"name":"本机工艺三"}`), id.New(), "", nil)
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 厂级工程号用工艺已占的号，一定撞。
	if _, err := e.fac.CreatePad(e.ctx, e.admin.tok, factory.AssetLevelFactory, factory.KindProject, "撞号", encWeldBody(third.ID), id.New(), third.Code, nil); err == nil {
		// 不该建成。
		t.Fatal("code conflict accepted")
	}
	// 还是两条，没留下半截。
	if n := e.factoryProcessCount(t); n != 2 {
		// 把结果打出来。
		t.Fatalf("factory processes after conflict %d", n)
	}
}

// 别人的个人工艺：管理员新加拒绝、不公开参数；超管可以复制；存量的不挡别的改动，也不复制。
func TestAdoptColleaguePersonal(t *testing.T) {
	// 开厂建人。
	e := newAdoptEnv(t)
	// 同事的个人工艺。
	his := e.personal(t, e.owner.tok, "同事工艺")
	// 管理员新建工程引用它，看不到正文就不能复制。
	if _, err := e.fac.CreateFactoryProject(e.ctx, e.admin.tok, factory.WorkContext{}, "越权", encWeldBody(his.ID), nil); !errors.Is(err, domain.ErrAssetDependency) {
		// 把结果打出来。
		t.Fatalf("admin colleague %v", err)
	}
	// 没留下任何副本。
	if n := e.factoryProcessCount(t); n != 0 {
		// 把结果打出来。
		t.Fatalf("factory processes %d", n)
	}
	// 存量：修复前就钉着同事个人工艺的厂级工程，直接落库造出来。
	old, err := e.fac.Store().InsertGovernedAssetInto(e.ctx, factory.Asset{
		Kind: factory.KindProject, Level: factory.AssetLevelFactory, Name: "存量工程", Status: factory.AssetAvailable, Copyable: true, WeldKind: "single",
		Content: encWeldBody(his.ID), Digest: digest.Sum(encWeldBody(his.ID)), CreatorID: e.admin.acc.ID,
		Deps: []factory.AssetDep{{ID: his.ID, Revision: his.Revision, Digest: his.Digest}},
	}, uuid.Nil, false)
	// 造不出就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 管理员改这份工程别的地方：同一条引用照旧，能存，不复制。
	saved, err := e.fac.UpdateAssetContent(e.ctx, e.admin.tok, old.ID, old.Revision, encWeldBody(his.ID, his.ID))
	// 不该拒，也不该复制。
	if err != nil || len(saved.PersonalCopies) != 0 || len(saved.Deps) != 1 || saved.Deps[0].ID != his.ID {
		// 把结果打出来。
		t.Fatalf("admin keeps old %+v %v", saved, err)
	}
	// 超管存同一份：看得到正文，这时转成厂级。
	converted, err := e.fac.UpdateAssetContent(e.ctx, e.sa, old.ID, saved.Revision, encWeldBody(his.ID))
	// 应当复制一份并改写。
	if err != nil || len(converted.PersonalCopies) != 1 || len(converted.Deps) != 1 || converted.Deps[0].ID == his.ID || converted.SavedContent == nil {
		// 把结果打出来。
		t.Fatalf("sa converts %+v %v", converted, err)
	}
}

// 校验没过或修订冲突时一条都不复制，草稿不能被依赖，作业类型不符也拦在复制前。
func TestAdoptNoLeftovers(t *testing.T) {
	// 开厂建人。
	e := newAdoptEnv(t)
	// 管理员自己的个人工艺。
	mine := e.personal(t, e.admin.tok, "管理员工艺")
	// 建一份空工程。
	proj, err := e.fac.CreateFactoryProject(e.ctx, e.admin.tok, factory.WorkContext{}, "厂工程", []byte(`[]`), nil)
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 拿旧修订改依赖，应回冲突。
	dep := factory.AssetDep{ID: mine.ID, Revision: mine.Revision, Digest: mine.Digest}
	// 期望修订故意写错。
	if _, err := e.fac.SetProjectDeps(e.ctx, e.admin.tok, proj.ID, proj.Revision+5, []factory.AssetDep{dep}); !errors.Is(err, domain.ErrRevisionConflict) {
		// 把结果打出来。
		t.Fatalf("conflict %v", err)
	}
	// 草稿个人工艺。
	draft, err := e.fac.CreatePersonalProcess(e.ctx, e.admin.tok, factory.WorkContext{}, "草稿", []byte(`{"name":"草稿"}`))
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 草稿不能被依赖。
	if _, err := e.fac.CreateFactoryProject(e.ctx, e.admin.tok, factory.WorkContext{}, "钉草稿", encWeldBody(draft.ID), nil); !errors.Is(err, domain.ErrAssetNotAvailable) {
		// 把结果打出来。
		t.Fatalf("draft %v", err)
	}
	// 多层工程引用单层个人工艺，作业类型不符。
	if _, err := e.fac.CreateFactoryProject(e.ctx, e.admin.tok, factory.WorkContext{}, "类型不符", []byte(`[]`), []factory.AssetDep{dep}, "multilayer"); !errors.Is(err, domain.ErrWeldKindMismatch) {
		// 把结果打出来。
		t.Fatalf("weld kind %v", err)
	}
	// 全程没有留下副本。
	if n := e.factoryProcessCount(t); n != 0 {
		// 把结果打出来。
		t.Fatalf("factory processes %d", n)
	}
}

// 改工艺正文 name 为空或全空白拒绝；没这个键的旧正文放行；已有空名数据读不受影响。
func TestProcessBodyNameRequired(t *testing.T) {
	// 开厂建人。
	e := newAdoptEnv(t)
	// 建厂级工艺，正文名字是空的（修复前留下的数据也长这样）。
	row, err := e.fac.CreateFactoryProcess(e.ctx, e.admin.tok, factory.WorkContext{}, "空名工艺", []byte(`{"name":"","current":1}`))
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 已有空名数据照常读。
	if _, err := e.fac.ReadAssetContent(e.ctx, e.admin.tok, row.ID); err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 空串、半角空白、全角空白、空值都拒绝，网页和示教器两条路都试。
	for _, bad := range []string{`{"name":"","current":2}`, `{"name":"  ","current":2}`, `{"name":"　","current":2}`, `{"name":null,"current":2}`} {
		// 网页改正文。
		if _, err := e.fac.UpdateAssetContent(e.ctx, e.admin.tok, row.ID, row.Revision, []byte(bad)); !errors.Is(err, domain.ErrInvalidName) {
			// 把结果打出来。
			t.Fatalf("web %s %v", bad, err)
		}
		// 示教器盖正文。
		if _, err := e.fac.ApplyAppContent(e.ctx, e.admin.tok, row.ID, []byte(bad)); !errors.Is(err, domain.ErrInvalidName) {
			// 把结果打出来。
			t.Fatalf("app %s %v", bad, err)
		}
	}
	// 没有 name 键的旧正文放行。
	next, err := e.fac.UpdateAssetContent(e.ctx, e.admin.tok, row.ID, row.Revision, []byte(`{"current":3}`))
	// 不该拒。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 有名字的放行。
	if _, err := e.fac.UpdateAssetContent(e.ctx, e.admin.tok, row.ID, next.Revision, []byte(`{"name":"有名","current":4}`)); err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
}

// 改名拒空名、去两端空白；停用的网页不能改，示教器同步能改，和它能盖正文一致。
func TestRenameAssetRules(t *testing.T) {
	// 开厂建人。
	e := newAdoptEnv(t)
	// 操作员的个人工艺。
	mine := e.personal(t, e.owner.tok, "原名")
	// 空名和全空白拒绝。
	for _, bad := range []string{"", "   ", "　"} {
		// 改名应拒。
		if _, err := e.fac.RenameAsset(e.ctx, e.owner.tok, mine.ID, mine.Revision, bad); !errors.Is(err, domain.ErrInvalidName) {
			// 把结果打出来。
			t.Fatalf("blank %q %v", bad, err)
		}
	}
	// 两端空白去掉。
	row, err := e.fac.RenameAsset(e.ctx, e.owner.tok, mine.ID, mine.Revision, "  新名 ")
	// 不该拒，名字去了空白。
	if err != nil || row.Name != "新名" {
		// 把结果打出来。
		t.Fatalf("trim %+v %v", row, err)
	}
	// 停用这条。
	row, err = e.fac.DisableAsset(e.ctx, e.owner.tok, row.ID, row.Revision)
	// 停用失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 网页令牌改停用的还是拒绝。
	if _, err := e.fac.RenameAsset(e.ctx, e.owner.tok, row.ID, row.Revision, "网页改"); !errors.Is(err, domain.ErrAssetNotAvailable) {
		// 把结果打出来。
		t.Fatalf("web disabled %v", err)
	}
	// 示教器会话。
	sess, err := e.fac.LoginPad(e.ctx, "owner", "owner-pass")
	// 登录失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 示教器同步能改，否则这件每轮都报没收下。
	renamed, err := e.fac.RenameAsset(e.ctx, sess.Token, row.ID, row.Revision, "本机改")
	// 不该拒，状态保持停用。
	if err != nil || renamed.Name != "本机改" || renamed.Status != factory.AssetDisabled {
		// 把结果打出来。
		t.Fatalf("app disabled %+v %v", renamed, err)
	}
}

// 个人工艺复制之后真改过参数，再选它要重新复制；只改名不算落后，复制不了时对回已有副本。
func TestAdoptRecopiesWhenSourceChanged(t *testing.T) {
	// 开厂建人。
	e := newAdoptEnv(t)
	// 管理员自己的个人工艺。
	mine := e.personal(t, e.admin.tok, "管理员工艺")
	// 建一份空工程。
	proj, err := e.fac.CreateFactoryProject(e.ctx, e.admin.tok, factory.WorkContext{}, "厂工程", []byte(`[]`), nil)
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 第一次引用，复制出第一份。
	first, err := e.fac.SetProjectDeps(e.ctx, e.admin.tok, proj.ID, proj.Revision, []factory.AssetDep{{ID: mine.ID, Revision: mine.Revision, Digest: mine.Digest}})
	// 这一步就复制出副本。
	if err != nil || len(first.Deps) != 1 {
		// 把结果打出来。
		t.Fatalf("first %+v %v", first, err)
	}
	// 第一份副本身份。
	oldCopy := first.Deps[0].ID
	// 个人工艺只改名：修订升高，摘要不变。
	renamed, err := e.fac.RenameAsset(e.ctx, e.admin.tok, mine.ID, mine.Revision, "改过名")
	// 改名失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 再传写着个人工艺的正文，只改过名就对回第一份。
	same, err := e.fac.UpdateAssetContent(e.ctx, e.admin.tok, proj.ID, first.Revision, encWeldBody(mine.ID))
	// 不复制，正文改指第一份。
	if err != nil || len(same.PersonalCopies) != 0 || same.SavedContent == nil || !strings.Contains(*same.SavedContent, oldCopy.String()) {
		// 把结果打出来。
		t.Fatalf("rename only %+v %v", same, err)
	}
	// 个人工艺改了参数。
	edited, err := e.fac.UpdateAssetContent(e.ctx, e.admin.tok, mine.ID, renamed.Revision, []byte(`{"name":"改过名","current":200}`))
	// 改失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 再选一次这条个人工艺，网页先把它加进依赖。
	again, err := e.fac.SetProjectDeps(e.ctx, e.admin.tok, proj.ID, same.Revision, []factory.AssetDep{same.Deps[0], {ID: mine.ID, Revision: edited.Revision, Digest: edited.Digest}})
	// 复制出新的一份，带新参数。
	if err != nil || len(again.PersonalCopies) != 1 || again.PersonalCopies[0].ID == oldCopy {
		// 把结果打出来。
		t.Fatalf("recopy %+v %v", again, err)
	}
	// 第二份副本身份。
	newCopy := again.PersonalCopies[0].ID
	// 新副本读出来是新参数。
	raw, err := e.fac.ReadAssetContent(e.ctx, e.admin.tok, newCopy)
	// 读失败或还是旧参数都不对。
	if err != nil || !strings.Contains(string(raw), "200") {
		// 把结果打出来。
		t.Fatalf("new copy %s %v", raw, err)
	}
	// 同一次保存的下一步还写着个人工艺，对回新的这份，不再复制。
	next, err := e.fac.UpdateAssetContent(e.ctx, e.admin.tok, proj.ID, again.Revision, encWeldBody(mine.ID))
	// 不复制，正文改指新的这份。
	if err != nil || len(next.PersonalCopies) != 0 || next.SavedContent == nil || !strings.Contains(*next.SavedContent, newCopy.String()) {
		// 把结果打出来。
		t.Fatalf("next step %+v %v", next, err)
	}
	// 个人工艺又改了参数。
	edited2, err := e.fac.UpdateAssetContent(e.ctx, e.admin.tok, mine.ID, edited.Revision, []byte(`{"name":"改过名","current":210}`))
	// 改失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 然后停用，复制不了了。
	if _, err := e.fac.DisableAsset(e.ctx, e.admin.tok, mine.ID, edited2.Revision); err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 旧正文再传不被拒，对回已有副本，也不复制。
	last, err := e.fac.UpdateAssetContent(e.ctx, e.admin.tok, proj.ID, next.Revision, encWeldBody(mine.ID))
	// 不复制也不拒。
	if err != nil || len(last.PersonalCopies) != 0 {
		// 把结果打出来。
		t.Fatalf("disabled source %+v %v", last, err)
	}
	// 全程只多出两份厂级工艺。
	if n := e.factoryProcessCount(t); n != 2 {
		// 把结果打出来。
		t.Fatalf("factory processes %d", n)
	}
}

// 工程写进库后记审计失败：接口回错，但工程已钉着副本，副本不能被回收掉。
func TestAdoptKeepsCopyWhenAuditFailsAfterWrite(t *testing.T) {
	// 准备无取消的上下文。
	ctx := context.Background()
	// 另开一座厂库，用例要直连库装触发器。
	admin := testpg.Open(t)
	// 建空库。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_fac")
	// 迁好库。
	db := testpg.OpenMigrated(t, dsn)
	// 按一家新厂装上厂服务。
	fac := factory.NewService(store.Open(db, id.New()))
	// 夹具不连 WAN，自签租约才能写正文。
	if err := fac.Store().GrantLocalLease(ctx); err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 写本厂短码才能发号。
	if err := fac.Store().PutFactoryShortCode(ctx, "F01"); err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 写入首个超管。
	if _, err := fac.AcceptEnrollment(ctx, id.New(), "sa-a", "超管A", personPass("sa-a")); err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 超管登录。
	sa := mustLogin(t, ctx, fac, "sa-a", personPass("sa-a"))
	// 建管理员。
	pe := mustCreateRole(t, ctx, fac, sa, "pe-a", "pe-pass", factory.RoleOrgAdmin)
	// 借用夹具的方法建可用个人工艺。
	mine := adoptEnv{ctx: ctx, fac: fac, sa: sa, admin: pe, owner: pe}.personal(t, pe.tok, "管理员工艺")
	// 建一份空工程。
	proj, err := fac.CreateFactoryProject(ctx, pe.tok, factory.WorkContext{}, "厂工程", []byte(`[]`), nil)
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 只让「改工程成功」这条审计写失败。
	if err := db.Exec(`CREATE FUNCTION fail_update_audit() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'audit down'; END; $$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_update_audit BEFORE INSERT ON audit_events FOR EACH ROW
		WHEN (NEW.action = 'update_asset' AND NEW.result = 'allow') EXECUTE FUNCTION fail_update_audit();`).Error; err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 改依赖，工程能写进库，记审计失败。
	if _, err := fac.SetProjectDeps(ctx, pe.tok, proj.ID, proj.Revision, []factory.AssetDep{{ID: mine.ID, Revision: mine.Revision, Digest: mine.Digest}}); err == nil {
		// 触发器没起作用，这条用例就没验到。
		t.Fatal("audit failure should surface")
	}
	// 恢复审计。
	if err := db.Exec(`DROP TRIGGER fail_update_audit ON audit_events`).Error; err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 工程已经改了依赖。
	got, err := fac.GetAsset(ctx, pe.tok, proj.ID)
	// 读不到或没钉上副本，这条用例的前提就不成立。
	if err != nil || len(got.Deps) != 1 || got.Deps[0].ID == mine.ID {
		// 把结果打出来。
		t.Fatalf("project %+v %v", got, err)
	}
	// 被钉着的副本还在，正文读得到。
	if _, err := fac.ReadAssetContent(ctx, pe.tok, got.Deps[0].ID); err != nil {
		// 把错误打出来。
		t.Fatalf("copy recycled: %v", err)
	}
}
