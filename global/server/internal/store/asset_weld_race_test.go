// 改工艺作业类型与绑依赖并发：改类型锁行复查引用，绑依赖共享锁后核类型，不留类型对不上的工程。
package store_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/store"
)

// weldTo 按当前行拼一笔只改作业类型的写入。
func weldTo(a store.Asset, weld string) store.AssetWrite {
	// 名称、正文、摘要、状态照旧，只换作业类型。
	return store.AssetWrite{Name: a.Name, Content: a.Content, Digest: a.Digest, Copyable: a.Copyable, Status: a.Status, WeldKind: weld}
}

// 验绑依赖的事务先锁住工艺，并发改作业类型要等它提交，之后复查引用被拒。
func TestWeldKindChangeWaitsForBindThenInUse(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 建库并备好单层工艺和单层工程。
	f := newRefFixture(t)
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
		_, err := f.s.UpdateAsset(context.Background(), f.project.ID, f.project.Revision, store.AssetWrite{
			Name: f.project.Name, Content: f.project.Content, Digest: f.project.Digest,
			Status: f.project.Status, Deps: []store.AssetDep{pin(f.process)},
		})
		// 把这一路的结果交回去。
		bindDone <- err
	}()
	// 绑依赖卡在占锁事务上，说明已共享锁住工艺。
	waitLockWaiters(t, f.db, 1)
	// 改作业类型那一路的结果。
	weldDone := make(chan error, 1)
	// 这时把工艺改成 T 排，应等绑依赖提交。
	go func() {
		// 按当前修订改类型，错误交回主协程。
		_, err := f.s.UpdateAsset(context.Background(), f.process.ID, f.process.Revision, weldTo(f.process, store.WeldKindTBar))
		// 把这一路的结果交回去。
		weldDone <- err
	}()
	// 改类型也卡住，说明锁行和共享锁互斥。
	waitLockWaiters(t, f.db, 2)
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
	p, err := f.s.AssetByID(ctx, f.process.ID)
	// 读失败、类型变了或修订升了都不对。
	if err != nil || p.WeldKind != store.WeldKindSingle || p.Revision != f.process.Revision {
		// 带上当前行和原因停测。
		t.Fatalf("process after refused weld: %+v %v", p, err)
	}
}

// 验改作业类型的事务先锁住工艺，并发绑依赖要等它提交，之后读到新类型被拒。
func TestBindWaitsForWeldKindChangeThenMismatch(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 建库并备好单层工艺和单层工程。
	f := newRefFixture(t)
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
		_, err := f.s.UpdateAsset(context.Background(), f.process.ID, f.process.Revision, weldTo(f.process, store.WeldKindTBar))
		// 把这一路的结果交回去。
		weldDone <- err
	}()
	// 改类型卡在占锁事务上，说明已锁住工艺。
	waitLockWaiters(t, f.db, 1)
	// 改依赖那一路的结果。
	bindDone := make(chan error, 1)
	// 给起点工程绑这条工艺，应等改类型提交。
	go func() {
		// 按当前修订把工艺绑上，错误交回主协程。
		_, err := f.s.UpdateAsset(context.Background(), f.project.ID, f.project.Revision, store.AssetWrite{
			Name: f.project.Name, Content: f.project.Content, Digest: f.project.Digest,
			Status: f.project.Status, Deps: []store.AssetDep{pin(f.process)},
		})
		// 把这一路的结果交回去。
		bindDone <- err
	}()
	// 新建工程那一路的结果。
	insertDone := make(chan error, 1)
	// 新建一条钉这条工艺的单层工程，也应等改类型提交。
	go func() {
		// 新工程正文随便一小段。
		body := []byte(`{"items":[4]}`)
		// 写入新工程并钉住工艺，错误交回主协程。
		_, err := f.s.InsertAsset(context.Background(), store.Asset{
			Kind: store.KindProject, Name: "迟到单层工程", Status: store.AssetDraft,
			Content: body, Digest: digest.Sum(body), CreatorID: f.admin, Deps: []store.AssetDep{pin(f.process)},
		})
		// 把这一路的结果交回去。
		insertDone <- err
	}()
	// 两路绑依赖也卡住，说明共享锁在等改类型。
	waitLockWaiters(t, f.db, 3)
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
	j, err := f.s.AssetByID(ctx, f.project.ID)
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
func TestWeldKindGuardSequential(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 建库并备好单层工艺和单层工程。
	f := newRefFixture(t)
	// 工程绑上这条工艺，之后它就在用。
	j, err := f.s.UpdateAsset(ctx, f.project.ID, f.project.Revision, store.AssetWrite{
		Name: f.project.Name, Content: f.project.Content, Digest: f.project.Digest,
		Status: f.project.Status, Deps: []store.AssetDep{pin(f.process)},
	})
	// 同类型绑不上就停。
	if err != nil {
		// 带上原因停测。
		t.Fatalf("bind same kind: %v", err)
	}
	// 在用工艺改成 T 排应被拒。
	if _, err := f.s.UpdateAsset(ctx, f.process.ID, f.process.Revision, weldTo(f.process, store.WeldKindTBar)); !errors.Is(err, domain.ErrWeldKindInUse) {
		// 带上实际错误停测。
		t.Fatalf("in use: %v", err)
	}
	// 写回同一类型，类型没变不该被拦。
	same, err := f.s.UpdateAsset(ctx, f.process.ID, f.process.Revision, weldTo(f.process, store.WeldKindSingle))
	// 被拒或类型变了都不对。
	if err != nil || same.WeldKind != store.WeldKindSingle {
		// 带上结果和原因停测。
		t.Fatalf("same kind: %+v %v", same, err)
	}
	// 另写一条没人钉的多层工艺。
	mBody := []byte(`{"voltage":41}`)
	// 写入多层工艺，后面拿它钉进单层工程。
	multi, err := f.s.InsertAsset(ctx, store.Asset{
		Kind: store.KindProcess, Name: "多层工艺", Status: store.AssetAvailable, WeldKind: store.WeldKindMultilayer,
		Content: mBody, Digest: digest.Sum(mBody), CreatorID: f.admin,
	})
	// 工艺没写进去就停。
	if err != nil {
		// 带上原因停测。
		t.Fatalf("multi process: %v", err)
	}
	// 拼一笔给单层工程新钉多层工艺的写入。
	mismatch := store.AssetWrite{
		Name: j.Name, Content: j.Content, Digest: j.Digest, Status: j.Status, Deps: []store.AssetDep{pin(same), pin(multi)},
	}
	// 单层工程新钉多层工艺应按类型对不上被拒。
	if _, err := f.s.UpdateAsset(ctx, j.ID, j.Revision, mismatch); !errors.Is(err, domain.ErrWeldKindMismatch) {
		// 带上实际错误停测。
		t.Fatalf("bind mismatch: %v", err)
	}
	// 没人钉的多层工艺改成单层照常可以。
	if _, err := f.s.UpdateAsset(ctx, multi.ID, multi.Revision, weldTo(multi, store.WeldKindSingle)); err != nil {
		// 带上原因停测。
		t.Fatalf("unreferenced weld: %v", err)
	}
	// 绕过守卫把在用工艺改成多层，摆出旧数据里类型对不上的情形。
	if err := f.db.Exec("UPDATE assets SET weld_kind = ? WHERE id = ?", store.WeldKindMultilayer, f.process.ID).Error; err != nil {
		// 带上原因停测。
		t.Fatalf("force weld: %v", err)
	}
	// 拼一笔只改名、依赖照旧的写入。
	rename := store.AssetWrite{
		Name: "改名工程", Content: j.Content, Digest: j.Digest, Status: j.Status, Deps: j.Deps,
	}
	// 旧依赖不在锁里核，只改名应能保存。
	if _, err := f.s.UpdateAsset(ctx, j.ID, j.Revision, rename); err != nil {
		// 带上原因停测。
		t.Fatalf("rename with old dep: %v", err)
	}
}
