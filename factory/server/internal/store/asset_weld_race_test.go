// 改工艺作业类型与绑依赖并发：改类型锁行复查引用，绑依赖共享锁后核类型，不留类型对不上的工程。
package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/store"
)

// weldFixture 是一份干净厂库里的一条可用单层工艺和一条空依赖单层工程。
type weldFixture struct {
	db      *gorm.DB     // 底层连接，用来开占锁事务和查锁等待
	s       *store.Store // 被测的库接口
	creator uuid.UUID    // 创建人
	process store.Asset  // 被钉、被改类型的厂级工艺
	project store.Asset  // 起点没有依赖的厂级工程
}

// newWeldFixture 建厂库并写入一条可用单层工艺和一条空依赖单层工程。
func newWeldFixture(t *testing.T) weldFixture {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	db, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(db, facID)
	// 夹具自签租约，才能把正文封进厂库。
	if err := s.GrantLocalLease(ctx); err != nil {
		// 签租约失败就停测。
		t.Fatal(err)
	}
	// 写本厂短码，没有短码就发不了号。
	if err := s.PutFactoryShortCode(ctx, "F01"); err != nil {
		// 写短码失败就停测。
		t.Fatal(err)
	}
	// 建一名创建人，资产都记在他名下。
	p, err := s.CreatePerson(ctx, "pe", "工艺员", "", false)
	// 建账号失败就停测。
	if err != nil {
		// 建账号失败就停测。
		t.Fatal(err)
	}
	// 工艺正文随便一小段，摘要现算。
	pBody := []byte(`{"current":180}`)
	// 写入一条可用单层工艺，后面拿它绑和改类型。
	proc, err := s.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProcess, Level: store.AssetLevelFactory, Name: "被钉工艺", Status: store.AssetAvailable,
		Copyable: true, Content: pBody, Digest: digest.Sum(pBody), CreatorID: p.ID,
	})
	// 工艺没写进去就停。
	if err != nil {
		// 带上原因停测。
		t.Fatalf("process: %v", err)
	}
	// 工程正文也只要一小段，摘要现算。
	jBody := []byte(`[]`)
	// 写入一条没有依赖的单层工程，后面给它改绑。
	proj, err := s.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProject, Level: store.AssetLevelFactory, Name: "起点工程", Status: store.AssetDraft,
		Copyable: true, Content: jBody, Digest: digest.Sum(jBody), CreatorID: p.ID,
	})
	// 工程没写进去就停。
	if err != nil {
		// 带上原因停测。
		t.Fatalf("project: %v", err)
	}
	return weldFixture{db: db, s: s, creator: p.ID, process: proc, project: proj}
}

// weldPin 把工艺收成工程要钉的一条依赖。
func weldPin(a store.Asset) store.AssetDep {
	// 身份、修订、摘要照当前行钉死。
	return store.AssetDep{ID: a.ID, Revision: a.Revision, Digest: a.Digest}
}

// weldTo 按当前行拼一笔只改作业类型的写入，正文不动。
func weldTo(a store.Asset, weld string) store.AssetWrite {
	// 名称、摘要、状态照旧，只换作业类型。
	return store.AssetWrite{Name: a.Name, Digest: a.Digest, Copyable: a.Copyable, Status: a.Status, WeldKind: weld, KeepContent: true}
}

// bindTo 按当前行拼一笔只改依赖的写入，正文不动。
func bindTo(a store.Asset, name string, deps ...store.AssetDep) store.AssetWrite {
	// 名称按给的写，摘要、状态照旧，依赖换成给的。
	return store.AssetWrite{Name: name, Digest: a.Digest, Copyable: a.Copyable, Status: a.Status, Deps: deps, KeepContent: true}
}

// waitWeldLockWaiters 等本库至少 n 个会话卡在锁上，超时就停测。
func waitWeldLockWaiters(t *testing.T, db *gorm.DB, n int64) {
	// 失败行指到调用用例，不指到帮手。
	t.Helper()
	// 最多等十秒，免得用例挂死。
	deadline := time.Now().Add(10 * time.Second)
	// 反复数锁等待，够数就交回。
	for {
		// 准备接住正在等锁的会话数。
		var got int64
		// 只数本测试库里卡在锁上的会话。
		if err := db.Raw("SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'").Scan(&got).Error; err != nil {
			// 查不出来就停，没法判断有没有在等。
			t.Fatalf("lock waiters: %v", err)
		}
		// 等锁的够数了就交回。
		if got >= n {
			return
		}
		// 超时还不够数，说明该等的没在等。
		if time.Now().After(deadline) {
			// 带上实际和期望停测。
			t.Fatalf("lock waiters: got %d want %d", got, n)
		}
		// 稍等一会再数，不空转。
		time.Sleep(20 * time.Millisecond)
	}
}

// 验绑依赖的事务先锁住工艺，并发改作业类型要等它提交，之后复查引用被拒。
func TestFactoryWeldKindChangeWaitsForBindThenInUse(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 建厂库并备好单层工艺和单层工程。
	f := newWeldFixture(t)
	// 开一笔占锁事务，让绑依赖停在锁住工艺之后。
	hold := f.db.Begin()
	// 开事务失败就停，后面没法卡住绑依赖。
	if hold.Error != nil {
		// 带上原因停测。
		t.Fatalf("begin hold: %v", hold.Error)
	}
	// 用例中途失败也放开占锁，免得别的协程挂死。
	defer hold.Rollback()
	// 占住起点工程行，改依赖会在写行时停下。
	if err := hold.Exec("SELECT id FROM assets WHERE id = ? FOR UPDATE", f.project.ID).Error; err != nil {
		// 带上原因停测。
		t.Fatalf("hold project: %v", err)
	}
	// 改依赖那一路的结果。
	bindDone := make(chan error, 1)
	// 给起点工程绑这条工艺，共享锁住工艺后卡在写行上。
	go func() {
		// 按当前修订把工艺绑上，错误交回主协程。
		_, err := f.s.UpdateGovernedAsset(context.Background(), f.project.ID, f.project.Revision, bindTo(f.project, f.project.Name, weldPin(f.process)))
		// 把这一路的结果交回去。
		bindDone <- err
	}()
	// 绑依赖卡在占锁事务上，说明已共享锁住工艺。
	waitWeldLockWaiters(t, f.db, 1)
	// 改作业类型那一路的结果。
	weldDone := make(chan error, 1)
	// 这时把工艺改成 T 排，应等绑依赖提交。
	go func() {
		// 按当前修订改类型，错误交回主协程。
		_, err := f.s.UpdateGovernedAsset(context.Background(), f.process.ID, f.process.Revision, weldTo(f.process, store.WeldKindTBar))
		// 把这一路的结果交回去。
		weldDone <- err
	}()
	// 改类型也卡住，说明锁行和共享锁互斥。
	waitWeldLockWaiters(t, f.db, 2)
	// 放开占锁，让绑依赖提交。
	if err := hold.Commit().Error; err != nil {
		// 带上原因停测。
		t.Fatalf("commit hold: %v", err)
	}
	// 绑依赖那一路应成功。
	if err := <-bindDone; err != nil {
		// 带上原因停测。
		t.Fatalf("bind: %v", err)
	}
	// 改类型复查时已看到工程钉着它，应按在用被拒。
	if err := <-weldDone; !errors.Is(err, domain.ErrWeldKindInUse) {
		// 带上实际错误停测。
		t.Fatalf("weld after bind: %v", err)
	}
	// 再读工艺，类型和修订都不该变。
	p, err := f.s.GovernedAssetMetaByID(ctx, f.process.ID)
	// 读失败、类型变了或修订升了都不对。
	if err != nil || p.WeldKind != store.WeldKindSingle || p.Revision != f.process.Revision {
		// 带上当前行和原因停测。
		t.Fatalf("process after refused weld: %+v %v", p, err)
	}
}

// 验改作业类型的事务先锁住工艺，并发绑依赖要等它提交，之后读到新类型被拒。
func TestFactoryBindWaitsForWeldKindChangeThenMismatch(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 建厂库并备好单层工艺和单层工程。
	f := newWeldFixture(t)
	// 开一笔占锁事务，让改类型停在锁住工艺之后。
	hold := f.db.Begin()
	// 开事务失败就停，后面没法卡住改类型。
	if hold.Error != nil {
		// 带上原因停测。
		t.Fatalf("begin hold: %v", hold.Error)
	}
	// 用例中途失败也放开占锁，免得别的协程挂死。
	defer hold.Rollback()
	// 占住工艺的文件节点，改类型会在同步文件名时停下。
	if err := hold.Exec("SELECT id FROM asset_fs_nodes WHERE asset_id = ? FOR UPDATE", f.process.ID).Error; err != nil {
		// 带上原因停测。
		t.Fatalf("hold fs node: %v", err)
	}
	// 改作业类型那一路的结果。
	weldDone := make(chan error, 1)
	// 把工艺改成 T 排，锁住工艺并复查后卡在同步文件名上。
	go func() {
		// 按当前修订改类型，错误交回主协程。
		_, err := f.s.UpdateGovernedAsset(context.Background(), f.process.ID, f.process.Revision, weldTo(f.process, store.WeldKindTBar))
		// 把这一路的结果交回去。
		weldDone <- err
	}()
	// 改类型卡在占锁事务上，说明已锁住工艺。
	waitWeldLockWaiters(t, f.db, 1)
	// 改依赖那一路的结果。
	bindDone := make(chan error, 1)
	// 给起点工程绑这条工艺，应等改类型提交。
	go func() {
		// 按当前修订把工艺绑上，错误交回主协程。
		_, err := f.s.UpdateGovernedAsset(context.Background(), f.project.ID, f.project.Revision, bindTo(f.project, f.project.Name, weldPin(f.process)))
		// 把这一路的结果交回去。
		bindDone <- err
	}()
	// 新建工程那一路的结果。
	insertDone := make(chan error, 1)
	// 新建一条钉这条工艺的单层工程，也应等改类型提交。
	go func() {
		// 新工程正文随便一小段。
		body := []byte(`[{}]`)
		// 写入新工程并钉住工艺，错误交回主协程。
		_, err := f.s.InsertGovernedAsset(context.Background(), store.Asset{
			Kind: store.KindProject, Level: store.AssetLevelFactory, Name: "迟到单层工程", Status: store.AssetDraft,
			Copyable: true, Content: body, Digest: digest.Sum(body), CreatorID: f.creator, Deps: []store.AssetDep{weldPin(f.process)},
		})
		// 把这一路的结果交回去。
		insertDone <- err
	}()
	// 两路绑依赖也卡住，说明共享锁在等改类型。
	waitWeldLockWaiters(t, f.db, 3)
	// 撤掉占锁，让改类型同步文件名并提交。
	if err := hold.Rollback().Error; err != nil {
		// 带上原因停测。
		t.Fatalf("rollback hold: %v", err)
	}
	// 改类型那一路应成功。
	if err := <-weldDone; err != nil {
		// 带上原因停测。
		t.Fatalf("weld: %v", err)
	}
	// 改依赖那一路锁后读到 T 排，应按类型对不上被拒。
	if err := <-bindDone; !errors.Is(err, domain.ErrWeldKindMismatch) {
		// 带上实际错误停测。
		t.Fatalf("bind after weld: %v", err)
	}
	// 新建工程那一路同样应被拒。
	if err := <-insertDone; !errors.Is(err, domain.ErrWeldKindMismatch) {
		// 带上实际错误停测。
		t.Fatalf("insert after weld: %v", err)
	}
	// 再读起点工程，依赖和修订都不该变。
	j, err := f.s.GovernedAssetMetaByID(ctx, f.project.ID)
	// 读失败、修订变了或带上了依赖都不对。
	if err != nil || j.Revision != f.project.Revision || len(j.Deps) != 0 {
		// 带上当前行和原因停测。
		t.Fatalf("project after refused bind: %+v %v", j, err)
	}
	// 库里不该有工程钉着已改成 T 排的工艺。
	used, err := f.s.AssetIsReferenced(ctx, f.process.ID)
	// 查不出或仍被引用都不对。
	if err != nil || used {
		// 带上结果和原因停测。
		t.Fatalf("mismatched ref: %v %v", used, err)
	}
}

// 验不并发时：在用工艺改类型被拒、同类型不拦、钉类型不同的工艺被拒，已有旧依赖不挡改名。
func TestFactoryWeldKindGuardSequential(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 建厂库并备好单层工艺和单层工程。
	f := newWeldFixture(t)
	// 工程绑上这条工艺，之后它就在用。
	j, err := f.s.UpdateGovernedAsset(ctx, f.project.ID, f.project.Revision, bindTo(f.project, f.project.Name, weldPin(f.process)))
	// 同类型绑不上就停。
	if err != nil {
		// 带上原因停测。
		t.Fatalf("bind same kind: %v", err)
	}
	// 在用工艺改成 T 排应被拒。
	if _, err := f.s.UpdateGovernedAsset(ctx, f.process.ID, f.process.Revision, weldTo(f.process, store.WeldKindTBar)); !errors.Is(err, domain.ErrWeldKindInUse) {
		// 带上实际错误停测。
		t.Fatalf("in use: %v", err)
	}
	// 写回同一类型，类型没变不该被拦。
	same, err := f.s.UpdateGovernedAsset(ctx, f.process.ID, f.process.Revision, weldTo(f.process, store.WeldKindSingle))
	// 被拒或类型变了都不对。
	if err != nil || same.WeldKind != store.WeldKindSingle {
		// 带上结果和原因停测。
		t.Fatalf("same kind: %+v %v", same, err)
	}
	// 另写一条没人钉的多层工艺。
	mBody := []byte(`{"current":181}`)
	// 写入多层工艺，后面拿它钉进单层工程。
	multi, err := f.s.InsertGovernedAsset(ctx, store.Asset{
		Kind: store.KindProcess, Level: store.AssetLevelFactory, Name: "多层工艺", Status: store.AssetAvailable,
		Copyable: true, WeldKind: store.WeldKindMultilayer, Content: mBody, Digest: digest.Sum(mBody), CreatorID: f.creator,
	})
	// 工艺没写进去就停。
	if err != nil {
		// 带上原因停测。
		t.Fatalf("multi process: %v", err)
	}
	// 单层工程新钉多层工艺应按类型对不上被拒。
	if _, err := f.s.UpdateGovernedAsset(ctx, j.ID, j.Revision, bindTo(j, j.Name, weldPin(same), weldPin(multi))); !errors.Is(err, domain.ErrWeldKindMismatch) {
		// 带上实际错误停测。
		t.Fatalf("bind mismatch: %v", err)
	}
	// 新建单层工程直接钉多层工艺同样被拒。
	body := []byte(`[{}]`)
	// 拼一条钉多层工艺的单层工程。
	bad := store.Asset{
		Kind: store.KindProject, Level: store.AssetLevelFactory, Name: "错配工程", Status: store.AssetDraft,
		Copyable: true, Content: body, Digest: digest.Sum(body), CreatorID: f.creator, Deps: []store.AssetDep{weldPin(multi)},
	}
	// 写入时就核类型，不能先落库。
	if _, err := f.s.InsertGovernedAsset(ctx, bad); !errors.Is(err, domain.ErrWeldKindMismatch) {
		// 带上实际错误停测。
		t.Fatalf("insert mismatch: %v", err)
	}
	// 没人钉的多层工艺改成单层照常可以。
	if _, err := f.s.UpdateGovernedAsset(ctx, multi.ID, multi.Revision, weldTo(multi, store.WeldKindSingle)); err != nil {
		// 带上原因停测。
		t.Fatalf("unreferenced weld: %v", err)
	}
	// 绕过守卫把在用工艺改成多层，摆出旧数据里类型对不上的情形。
	if err := f.s.TamperAssetWeldKind(ctx, f.process.ID, store.WeldKindMultilayer); err != nil {
		// 带上原因停测。
		t.Fatalf("force weld: %v", err)
	}
	// 只改名、依赖照旧，旧依赖不在锁里核，应能保存。
	if _, err := f.s.UpdateGovernedAsset(ctx, j.ID, j.Revision, bindTo(j, "改名工程", j.Deps...)); err != nil {
		// 带上原因停测。
		t.Fatalf("rename with old dep: %v", err)
	}
}
