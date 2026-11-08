// D-102 边界：依赖只记声明，复制、改依赖按身份取当前版，停用的已声明依赖不挡路。
package service_test

import (
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

// 边界用例共用的厂、操作人和厂身份。
type latestEdgeEnv struct {
	ctx context.Context  // 无取消的上下文
	fac *factory.Service // 被测厂端服务
	tok string           // 组织管理员令牌
	fid uuid.UUID        // 本厂身份
}

// 建厂并交回组织管理员的令牌。
func newLatestEdgeEnv(t *testing.T) latestEdgeEnv {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 开通失败就停，否则登录不了。
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 把开通的错误打出来并停住。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建组织管理员，用来建厂级工程。
	pe := mustCreateRole(t, ctx, fac, sa, "pe", "pe-pass", factory.RoleOrgAdmin)
	return latestEdgeEnv{ctx: ctx, fac: fac, tok: pe.tok, fid: seed.ID}
}

// 造一份已送达的单层平台工艺成员。
func (e latestEdgeEnv) deliverProc(t *testing.T, pid uuid.UUID, rev int64, body, status string, copyable bool) factory.ClosureMember {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 摘要跟正文走，验收要核。
	m := factory.ClosureMember{
		ID: pid, Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "边界工艺",
		Status: status, Copyable: copyable, WeldKind: store.WeldKindSingle, Revision: rev, Content: []byte(body), Digest: digest.Sum([]byte(body)),
	}
	// 收不下就停，后面没有副本可用。
	if err := e.fac.AcceptPlatformDelivery(e.ctx, sealSnap(factory.KindProcess, m, nil, &e.fid, nil)); err != nil {
		// 把验收的错误打出来并停住。
		t.Fatal(err)
	}
	return m
}

// 依赖加密平台工艺的工程，工艺升版后另存：新工程声明的是当前版。
func TestCopyProjectEncryptedDepFollowsLatest(t *testing.T) {
	// 建厂和操作人。
	e := newLatestEdgeEnv(t)
	// 第一修订的加密平台工艺。
	v1 := e.deliverProc(t, id.New(), 1, `{"current":201}`, factory.AssetAvailable, false)
	// 工程声明时就是第一修订。
	proj, err := e.fac.CreateFactoryProject(e.ctx, e.tok, factory.WorkContext{}, "源工程", encWeldBody(v1.ID), nil, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 把建工程的错误打出来并停住。
		t.Fatal(err)
	}
	// 工艺升到第二修订。
	v2 := e.deliverProc(t, v1.ID, 2, `{"current":202}`, factory.AssetAvailable, false)
	// 另存工程。
	cp, err := e.fac.CopyAsset(e.ctx, e.tok, proj.ID, "副本")
	// 工艺升版不该挡住另存。
	if err != nil {
		// 把复制的错误打出来并停住。
		t.Fatalf("copy: %v", err)
	}
	// 另存是一次新声明，记当前修订。
	if len(cp.Deps) != 1 || cp.Deps[0].ID != v1.ID || cp.Deps[0].Revision != v2.Revision {
		// 把实际依赖打出来并停住。
		t.Fatalf("deps %+v", cp.Deps)
	}
}

// 工程副本声明的旧修订本厂没收到（上回收到一半），只有更高修订：另存照样能成。
func TestCopyReplicaProjectDeclaredRevisionAbsent(t *testing.T) {
	// 建厂和操作人。
	e := newLatestEdgeEnv(t)
	// 工艺身份，第一修订从未送达。
	pid := id.New()
	// 第一修订只算摘要，不进本厂。
	oldBody := []byte(`{"current":211}`)
	// 只有第二修订到了。
	v2 := e.deliverProc(t, pid, 2, `{"current":212}`, factory.AssetAvailable, false)
	// 工程正文引用这条工艺。
	body := encWeldBody(pid)
	// 工程根声明第一修订。
	root := factory.ClosureMember{
		ID: id.New(), Kind: factory.KindProject, Level: factory.AssetLevelPlatform, Name: "平台工程", Status: factory.AssetAvailable,
		Copyable: true, WeldKind: store.WeldKindSingle, Revision: 1, Content: body, Digest: digest.Sum(body),
		Deps: []factory.AssetDep{{ID: pid, Revision: 1, Digest: digest.Sum(oldBody)}},
	}
	// 直接落副本，模拟收到一半的工程根。
	if _, err := e.fac.Store().InsertReplica(e.ctx, factory.AssetReplica{
		// 落成平台级只读副本。
		ID: root.ID, Revision: 1, Kind: root.Kind, Level: root.Level, Name: root.Name, Status: root.Status,
		Copyable: true, WeldKind: root.WeldKind, Content: root.Content, Digest: root.Digest, Deps: root.Deps,
	}); err != nil {
		// 把落库的错误打出来并停住。
		t.Fatal(err)
	}
	// 另存这份平台工程。
	cp, err := e.fac.CopyAsset(e.ctx, e.tok, root.ID, "副本")
	// 声明的旧修订不在不该挡住。
	if err != nil {
		// 把复制的错误打出来并停住。
		t.Fatalf("copy: %v", err)
	}
	// 新工程记当前修订。
	if len(cp.Deps) != 1 || cp.Deps[0].Revision != v2.Revision {
		// 把实际依赖打出来并停住。
		t.Fatalf("deps %+v", cp.Deps)
	}
}

// 加密工艺最新修订已停用：不管本厂有没有留着旧修订，另存都按停用拒绝，不看旧行的状态。
func TestCopyProjectEncryptedDepDisabledByLatest(t *testing.T) {
	// 建厂和操作人。
	e := newLatestEdgeEnv(t)
	// 第一修订可用。
	v1 := e.deliverProc(t, id.New(), 1, `{"current":221}`, factory.AssetAvailable, false)
	// 工程声明第一修订。
	proj, err := e.fac.CreateFactoryProject(e.ctx, e.tok, factory.WorkContext{}, "源工程", encWeldBody(v1.ID), nil, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 把建工程的错误打出来并停住。
		t.Fatal(err)
	}
	// 云端停用，修订升高。
	e.deliverProc(t, v1.ID, 2, `{"current":221}`, factory.AssetDisabled, false)
	// 新工程不得新依赖停用工艺。
	if _, err := e.fac.CopyAsset(e.ctx, e.tok, proj.ID, "副本"); !errors.Is(err, domain.ErrAssetNotAvailable) {
		// 放行或错因不对就停。
		t.Fatalf("copy with disabled: %v", err)
	}
}

// 工程已声明的工艺后来停用：改依赖时留着它照常成，只拒新加停用工艺；工艺的已声明修订不重写。
func TestSetProjectDepsKeepsDeclaredDisabled(t *testing.T) {
	// 建厂和操作人。
	e := newLatestEdgeEnv(t)
	// 第一修订可用。
	v1 := e.deliverProc(t, id.New(), 1, `{"current":231}`, factory.AssetAvailable, true)
	// 另一条可用工艺，准备新加。
	other := e.deliverProc(t, id.New(), 1, `{"current":232}`, factory.AssetAvailable, true)
	// 还有一条没声明过的停用工艺。
	off := e.deliverProc(t, id.New(), 1, `{"current":233}`, factory.AssetDisabled, true)
	// 工程声明第一修订。
	proj, err := e.fac.CreateFactoryProject(e.ctx, e.tok, factory.WorkContext{}, "工程", encWeldBody(v1.ID), nil, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 把建工程的错误打出来并停住。
		t.Fatal(err)
	}
	// 云端停用已声明的工艺。
	e.deliverProc(t, v1.ID, 2, `{"current":231}`, factory.AssetDisabled, true)
	// 网页只传身份加旧依赖，再加一条新工艺。
	next, err := e.fac.SetProjectDeps(e.ctx, e.tok, proj.ID, proj.Revision, []factory.AssetDep{
		// 已声明的那条原样带回。
		{ID: v1.ID, Revision: v1.Revision, Digest: v1.Digest},
		// 新加可用工艺。
		{ID: other.ID, Revision: other.Revision, Digest: other.Digest},
	})
	// 已声明的停用工艺不该挡住改依赖。
	if err != nil {
		// 把改依赖的错误打出来并停住。
		t.Fatalf("set deps: %v", err)
	}
	// 两条都在，已声明的那条不重写。
	if len(next.Deps) != 2 || next.Deps[0].ID != v1.ID || next.Deps[0].Revision != v1.Revision || next.Deps[1].ID != other.ID {
		// 把实际依赖打出来并停住。
		t.Fatalf("deps %+v", next.Deps)
	}
	// 新加停用工艺仍拒绝。
	if _, err := e.fac.SetProjectDeps(e.ctx, e.tok, next.ID, next.Revision, []factory.AssetDep{
		// 已声明的留着。
		next.Deps[0], next.Deps[1],
		// 没声明过的停用工艺。
		{ID: off.ID, Revision: off.Revision, Digest: off.Digest},
	}); !errors.Is(err, domain.ErrAssetNotAvailable) {
		// 放行或错因不对就停。
		t.Fatalf("add disabled: %v", err)
	}
}

// 云端删除的工艺，厂级工程已声明过：改依赖、改作业类型都不因它拒绝；没声明过的新加仍拒绝。
func TestSetProjectDepsKeepsDeclaredRetracted(t *testing.T) {
	// 建厂和操作人。
	e := newLatestEdgeEnv(t)
	// 已送达的工艺。
	gone := e.deliverProc(t, id.New(), 1, `{"current":241}`, factory.AssetAvailable, true)
	// 准备新加的工艺。
	other := e.deliverProc(t, id.New(), 1, `{"current":242}`, factory.AssetAvailable, true)
	// 准备被删、又没声明过的工艺。
	never := e.deliverProc(t, id.New(), 1, `{"current":243}`, factory.AssetAvailable, true)
	// 工程声明这条工艺。
	proj, err := e.fac.CreateFactoryProject(e.ctx, e.tok, factory.WorkContext{}, "工程", encWeldBody(gone.ID), nil, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 把建工程的错误打出来并停住。
		t.Fatal(err)
	}
	// 云端删除两条。
	for _, m := range []factory.ClosureMember{gone, never} {
		// 撤回失败就停。
		if err := e.fac.RetractPlatformDelivery(e.ctx, m.ID); err != nil {
			// 把撤回的错误打出来并停住。
			t.Fatal(err)
		}
	}
	// 留着已声明的，再加一条可用的。
	next, err := e.fac.SetProjectDeps(e.ctx, e.tok, proj.ID, proj.Revision, []factory.AssetDep{
		// 已声明的那条原样带回。
		{ID: gone.ID, Revision: gone.Revision, Digest: gone.Digest},
		// 新加可用工艺。
		{ID: other.ID, Revision: other.Revision, Digest: other.Digest},
	})
	// 已声明的工艺被云端删除不该挡住改依赖。
	if err != nil || len(next.Deps) != 2 {
		// 把结果打出来并停住。
		t.Fatalf("set deps: %+v %v", next.Deps, err)
	}
	// 新加已撤回的工艺仍拒绝。
	if _, err := e.fac.SetProjectDeps(e.ctx, e.tok, next.ID, next.Revision, []factory.AssetDep{
		// 已声明的留着。
		next.Deps[0], next.Deps[1],
		// 没声明过的已撤回工艺。
		{ID: never.ID, Revision: never.Revision, Digest: never.Digest},
	}); !errors.Is(err, domain.ErrAssetDependency) {
		// 放行或错因不对就停。
		t.Fatalf("add retracted: %v", err)
	}
	// 改作业类型按身份筛依赖，已撤回的工艺也读得到类型，不会整次失败。
	if _, err := e.fac.SetAssetWeldKind(e.ctx, e.tok, next.ID, next.Revision, store.WeldKindTBar); err != nil {
		// 把改类型的错误打出来并停住。
		t.Fatalf("set weld kind: %v", err)
	}
}

// 个人工程依赖加密平台工艺，当前版已停用：升厂级按当前版拒绝，不看声明的旧修订行。
func TestPromotePersonalDepDisabledByLatest(t *testing.T) {
	// 建厂和操作人。
	e := newLatestEdgeEnv(t)
	// 第一修订可用。
	v1 := e.deliverProc(t, id.New(), 1, `{"current":251}`, factory.AssetAvailable, false)
	// 个人工程声明第一修订。
	proj, err := e.fac.CreatePersonalProject(e.ctx, e.tok, factory.WorkContext{}, "个人工程", encWeldBody(v1.ID), nil, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 把建工程的错误打出来并停住。
		t.Fatal(err)
	}
	// 工艺升到第二修订，仍可用。
	v2 := e.deliverProc(t, v1.ID, 2, `{"current":252}`, factory.AssetAvailable, false)
	// 工艺升版不挡升厂级。
	up, err := e.fac.PromoteToFactory(e.ctx, e.tok, proj.ID, uuid.Nil)
	// 挡住了就停。
	if err != nil {
		// 把升档的错误打出来并停住。
		t.Fatalf("promote after upgrade: %v", err)
	}
	// 新厂级工程身份不同，源不动。
	if up.ID == proj.ID {
		// 把实际身份打出来并停住。
		t.Fatalf("promoted %+v", up)
	}
	// 云端停用，修订升高。
	e.deliverProc(t, v1.ID, v2.Revision+1, `{"current":252}`, factory.AssetDisabled, false)
	// 再升一次：当前版已停用就拒绝。
	if _, err := e.fac.PromoteToFactory(e.ctx, e.tok, proj.ID, uuid.Nil); !errors.Is(err, domain.ErrAssetNotAvailable) {
		// 放行或错因不对就停。
		t.Fatalf("promote disabled: %v", err)
	}
}
