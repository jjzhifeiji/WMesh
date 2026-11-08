// D-102：工程 deps 只记声明时的版本；厂端收包和组包都按身份跟工艺最新版。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// 造一份平台工艺成员，正文按修订区分。
func latestProc(pid uuid.UUID, rev int64, body string, copyable bool) factory.ClosureMember {
	// 摘要跟正文走，组包和验收都核它。
	return factory.ClosureMember{
		ID: pid, Kind: factory.KindProcess, Level: factory.AssetLevelPlatform, Name: "跟最新工艺",
		Status: factory.AssetAvailable, Copyable: copyable, Revision: rev, Content: []byte(body), Digest: digest.Sum([]byte(body)),
	}
}

// 造一份平台工程根成员，依赖写成给定工艺那一版。
func latestProj(pid uuid.UUID, dep factory.ClosureMember) factory.ClosureMember {
	// 工程修订固定为一，工艺升版不动工程。
	body := []byte("latest-proj")
	// 依赖只写身份、修订和摘要。
	return factory.ClosureMember{
		ID: pid, Kind: factory.KindProject, Level: factory.AssetLevelPlatform, Name: "跟最新工程",
		Status: factory.AssetAvailable, Copyable: true, Revision: 1, Content: body, Digest: digest.Sum(body),
		Deps: []factory.AssetDep{{ID: dep.ID, Revision: dep.Revision, Digest: dep.Digest}},
	}
}

// 新厂收到云端按当前版改写过 deps 的工程包：验收通过，判重为已有，篡改的 deps 仍拒绝。
func TestAcceptProjectWithLatestProcessNewFactory(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 记下本厂标识，下发的快照要指到这家厂。
	fid := seed.ID
	// 云端工艺当前已到第二修订。
	cur := latestProc(id.New(), 2, `{"current":233}`, true)
	// 云端把快照根 deps 改写成当前版。
	proj := latestProj(id.New(), cur)
	// 收下这份工程包，新厂不该因工程声明的是旧版而拒绝。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProject, proj, []factory.ClosureMember{cur}, &fid, nil)); err != nil {
		// 把验收的错误打出来并停住。
		t.Fatal(err)
	}
	// 判重应为已有，通道不会再拉。
	if ok, err := fac.HasPlatformDelivery(ctx, proj.ID, 1); err != nil || !ok {
		// 收过却说没有就会循环重拉。
		t.Fatalf("has %v %v", ok, err)
	}
	// 另一份工程：根 deps 和成员修订对不上，仍是坏包。
	bad := latestProj(id.New(), latestProc(cur.ID, 1, `{"current":211}`, true))
	// 收这份坏包应拒绝。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProject, bad, []factory.ClosureMember{cur}, &fid, nil)); !errors.Is(err, domain.ErrClosureMismatch) {
		// 坏包被放行或错因不对就停。
		t.Fatalf("bad snap %v", err)
	}
}

// 已收旧版的厂：工艺升版单独到达，工程判重仍为已有；再收同修订工程不报错，已存 deps 不变，工艺是新版。
func TestAcceptProjectWithLatestProcessOldFactory(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 记下本厂标识，下发的快照要指到这家厂。
	fid := seed.ID
	// 工艺第一修订，工程声明时就是它。
	v1 := latestProc(id.New(), 1, `{"current":211}`, true)
	// 工程声明依赖第一修订。
	proj := latestProj(id.New(), v1)
	// 先收旧版工程包。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProject, proj, []factory.ClosureMember{v1}, &fid, nil)); err != nil {
		// 把验收的错误打出来并停住。
		t.Fatal(err)
	}
	// 工艺第二修订。
	v2 := latestProc(v1.ID, 2, `{"current":233}`, true)
	// 工艺升版单独送达。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProcess, v2, nil, &fid, nil)); err != nil {
		// 把验收的错误打出来并停住。
		t.Fatal(err)
	}
	// 工程修订没变，判重仍为已有，不会被重推。
	if ok, err := fac.HasPlatformDelivery(ctx, proj.ID, 1); err != nil || !ok {
		// 说没有就会循环重拉。
		t.Fatalf("has after upgrade %v %v", ok, err)
	}
	// 云端再组同修订工程，deps 已改写成第二修订，总摘要也变了。
	again := latestProj(proj.ID, v2)
	// 再收不该报错，比如进页补拉碰巧又送一次。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProject, again, []factory.ClosureMember{v2}, &fid, nil)); err != nil {
		// 把验收的错误打出来并停住。
		t.Fatal(err)
	}
	// 已存工程副本原样留着，声明的依赖不被改。
	row, err := fac.Store().ReplicaMetaByIDRev(ctx, proj.ID, 1)
	// 读不到或 deps 变了都算失败。
	if err != nil || row.Deps[0].Revision != 1 || !bytes.Equal(row.Deps[0].Digest, v1.Digest) {
		// 把实际值打出来并停住。
		t.Fatalf("replica deps %+v %v", row.Deps, err)
	}
	// 工艺取到的是第二修订。
	top, err := fac.Store().LatestReplica(ctx, v1.ID)
	// 取不到或不是新版都算失败。
	if err != nil || top.Revision != 2 || !bytes.Equal(top.Content, v2.Content) {
		// 把实际值打出来并停住。
		t.Fatalf("latest %d %s %v", top.Revision, top.Content, err)
	}
}

// 工程副本已在、声明的旧修订却没收到（上回收到一半），又已有更高修订：补收后判重为已有，不循环重拉。
func TestHasPlatformDeliveryAcceptsHigherDep(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 记下本厂标识，下发的快照要指到这家厂。
	fid := seed.ID
	// 工程声明第一修订，这一版本厂没收到。
	v1 := latestProc(id.New(), 1, `{"current":211}`, true)
	// 工程根副本先落了库，模拟收到一半。
	proj := latestProj(id.New(), v1)
	// 直接写工程副本，依赖那一版不写。
	if _, err := fac.Store().InsertReplica(ctx, factory.AssetReplica{
		// 字段照成员抄。
		ID: proj.ID, Revision: proj.Revision, Kind: proj.Kind, Level: proj.Level, Name: proj.Name,
		Status: proj.Status, Copyable: proj.Copyable, Content: proj.Content, Digest: proj.Digest, Deps: proj.Deps,
	}); err != nil {
		// 把写副本的错误打出来并停住。
		t.Fatal(err)
	}
	// 依赖一份都没有，应判为没有，通道才会补。
	if ok, err := fac.HasPlatformDelivery(ctx, proj.ID, 1); err != nil || ok {
		// 缺依赖却说已有就补不上了。
		t.Fatalf("missing dep %v %v", ok, err)
	}
	// 云端当前已到第二修订，补收的包带的是它。
	v2 := latestProc(v1.ID, 2, `{"current":233}`, true)
	// 补收这份改写过 deps 的工程包。
	if err := fac.AcceptPlatformDelivery(ctx, sealSnap(factory.KindProject, latestProj(proj.ID, v2), []factory.ClosureMember{v2}, &fid, nil)); err != nil {
		// 把验收的错误打出来并停住。
		t.Fatal(err)
	}
	// 已存 deps 仍声明第一修订，但收到了更高的，要算收齐。
	if ok, err := fac.HasPlatformDelivery(ctx, proj.ID, 1); err != nil || !ok {
		// 说没有就会每轮重拉。
		t.Fatalf("higher dep %v %v", ok, err)
	}
}

// 加密平台工艺走过站加封时，工程包按当前修订绑定信封，到站解开、验收通过。
func TestAcceptProjectWithLatestEncryptedProcess(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 记下本厂标识，下发的快照要指到这家厂。
	fid := seed.ID
	// 取本厂在途钥，夹具用它当云端发的租约。
	key, err := fac.Store().TransitKey()
	// 取不到就停。
	if err != nil {
		// 把取钥的错误打出来并停住。
		t.Fatal(err)
	}
	// 装上租约，到站解封和落库都要用。
	if err := fac.Store().ApplyContentLease(ctx, key, time.Now().Add(time.Hour)); err != nil {
		// 把装租约的错误打出来并停住。
		t.Fatal(err)
	}
	// 加密工艺当前已到第三修订。
	cur := latestProc(id.New(), 3, `{"current":266}`, false)
	// 工程根 deps 已改写成第三修订。
	proj := latestProj(id.New(), cur)
	// 先按明文算好整包摘要，再换信封，和云端加封同样的次序。
	snap := sealSnap(factory.KindProject, proj, []factory.ClosureMember{cur}, &fid, nil)
	// 工程根封成过站信封。
	rootEnv, err := contentcrypt.Seal(key, proj.Content, contentcrypt.TransitAAD(fid, proj.ID, proj.Revision))
	// 封不上就停。
	if err != nil {
		// 把加封的错误打出来并停住。
		t.Fatal(err)
	}
	// 工艺按当前修订封成过站信封。
	procEnv, err := contentcrypt.Seal(key, cur.Content, contentcrypt.TransitAAD(fid, cur.ID, cur.Revision))
	// 封不上就停。
	if err != nil {
		// 把加封的错误打出来并停住。
		t.Fatal(err)
	}
	// 根换成信封。
	snap.Members[0].Content = rootEnv
	// 工艺换成信封。
	snap.Members[1].Content = procEnv
	// 到站解封验收，不该报错。
	if err := fac.AcceptPlatformDelivery(ctx, snap); err != nil {
		// 把验收的错误打出来并停住。
		t.Fatal(err)
	}
	// 读回工艺，正文是当前版，加密标记不变。
	got, err := fac.Store().LatestReplica(ctx, cur.ID)
	// 读不到、版本不对或被当成可复制都算失败。
	if err != nil || got.Revision != 3 || got.Copyable || !bytes.Equal(got.Content, cur.Content) {
		// 把实际值打出来并停住。
		t.Fatalf("replica %+v %v", got, err)
	}
}

// 厂级工程依赖平台工艺：组包取已收最高修订；云端撤回后仍可读；工程行不动。
func TestAssembleProjectFollowsPlatformReplica(t *testing.T) {
	// 准备闭包夹具，失败则本段用例无法开始。
	e := newEnv04(t)
	// 记下本厂标识，下发的快照要指到这家厂。
	fid := e.fac.Store().FactoryID()
	// 平台工艺第一修订。
	v1 := latestProc(id.New(), 1, `{"current":211}`, true)
	// 收下第一修订。
	if err := e.fac.AcceptPlatformDelivery(e.ctx, sealSnap(factory.KindProcess, v1, nil, &fid, nil)); err != nil {
		// 把验收的错误打出来并停住。
		t.Fatal(err)
	}
	// 建厂级工程，声明依赖第一修订。
	proj := e.pubProj(t, "依赖平台工艺", []byte("fp"), []factory.AssetDep{{ID: v1.ID, Revision: 1, Digest: v1.Digest}})
	// 平台工艺第二修订到厂。
	v2 := latestProc(v1.ID, 2, `{"current":233}`, true)
	// 收下第二修订。
	if err := e.fac.AcceptPlatformDelivery(e.ctx, sealSnap(factory.KindProcess, v2, nil, &fid, nil)); err != nil {
		// 把验收的错误打出来并停住。
		t.Fatal(err)
	}
	// 组包应带第二修订。
	got, err := e.fac.AssembleProject(e.ctx, e.pe.tok, proj.ID)
	// 拒绝或仍带第一修订都算失败。
	if err != nil || got.Members[1].Revision != 2 || got.Members[0].Deps[0].Revision != 2 {
		// 把实际值打出来并停住。
		t.Fatalf("assemble %+v %v", got, err)
	}
	// 云端删除，厂端撤回展示。
	if err := e.fac.RetractPlatformDelivery(e.ctx, v1.ID); err != nil {
		// 把撤回的错误打出来并停住。
		t.Fatal(err)
	}
	// 撤回后组包仍可读，还是最高修订。
	got, err = e.fac.AssembleProject(e.ctx, e.pe.tok, proj.ID)
	// 拒绝或退回旧修订都算失败。
	if err != nil || got.Members[1].Revision != 2 || !bytes.Equal(got.Members[1].Content, v2.Content) {
		// 把实际值打出来并停住。
		t.Fatalf("assemble after retract %+v %v", got, err)
	}
	// 读工程行，声明的依赖不被组包改掉。
	row, err := e.fac.Store().GovernedAssetByID(e.ctx, proj.ID)
	// 读不到或被改都算失败。
	if err != nil || row.Revision != proj.Revision || row.Deps[0].Revision != 1 {
		// 把实际值打出来并停住。
		t.Fatalf("project row %+v %v", row.Deps, err)
	}
}
