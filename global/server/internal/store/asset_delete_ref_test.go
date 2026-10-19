// 删资产与绑依赖并发：删除锁行复查引用，绑依赖共享锁依赖行，不留悬空依赖。
package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/store"
)

// refFixture 是一份干净库里的一条可用工艺和一条空依赖工程。
type refFixture struct {
	db      *gorm.DB     // 底层连接，用来开占锁事务和查锁等待
	s       *store.Store // 被测的库接口
	admin   uuid.UUID    // 创建人
	process store.Asset  // 被依赖、被删的平台工艺
	project store.Asset  // 起点没有依赖的平台工程
}

// newRefFixture 建库并写入一条可用工艺和一条空依赖工程。
func newRefFixture(t *testing.T) refFixture {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 每次打开干净库，避免别的用例留下的行。
	db := testpg.Fresh(t)
	// 用刚才那份库，资产都写进这里。
	s := store.Open(db)
	// 先写入管理员，当作资产的创建人。
	admin, err := s.CreateAdmin(ctx, "wan", "hash")
	// 管理员没建成就停，后面没有创建人。
	if err != nil {
		// 带上原因停测。
		t.Fatalf("admin: %v", err)
	}
	// 工艺正文随便一小段，摘要现算。
	pBody := []byte(`{"voltage":40}`)
	// 写入一条可用工艺，后面拿它绑和删。
	p, err := s.InsertAsset(ctx, store.Asset{
		Kind: store.KindProcess, Name: "被依赖工艺", Status: store.AssetAvailable,
		Content: pBody, Digest: digest.Sum(pBody), CreatorID: admin.ID,
	})
	// 工艺没写进去就停，后面没有被依赖的对象。
	if err != nil {
		// 带上原因停测。
		t.Fatalf("process: %v", err)
	}
	// 工程正文也只要一小段，摘要现算。
	jBody := []byte(`{"items":[]}`)
	// 写入一条没有依赖的工程，后面给它改绑。
	j, err := s.InsertAsset(ctx, store.Asset{
		Kind: store.KindProject, Name: "起点工程", Status: store.AssetDraft,
		Content: jBody, Digest: digest.Sum(jBody), CreatorID: admin.ID,
	})
	// 工程没写进去就停，后面没有改绑的对象。
	if err != nil {
		// 带上原因停测。
		t.Fatalf("project: %v", err)
	}
	return refFixture{db: db, s: s, admin: admin.ID, process: p, project: j}
}

// pin 把工艺收成工程要钉的一条依赖。
func pin(a store.Asset) store.AssetDep {
	// 身份、修订、摘要照当前行钉死。
	return store.AssetDep{ID: a.ID, Revision: a.Revision, Digest: a.Digest}
}

// waitLockWaiters 等本库至少 n 个会话卡在锁上，超时就停测。
func waitLockWaiters(t *testing.T, db *gorm.DB, n int64) {
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

// 验绑依赖的事务先锁住工艺，并发删除要等它提交，之后复查引用被拒。
func TestDeleteAssetWaitsForBindThenReferenced(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 建库并备好工艺和工程。
	f := newRefFixture(t)
	// 开一笔占锁事务，让两路绑依赖都停在锁依赖之后。
	hold := f.db.Begin()
	// 开事务失败就停，后面没法卡住绑依赖。
	if hold.Error != nil {
		// 带上原因停测。
		t.Fatalf("begin hold: %v", hold.Error)
	}
	// 用例中途失败也放开占锁，免得别的协程挂死。
	defer hold.Rollback()
	// 占住工程编号计数行，新建工程会在发号时停下。
	if err := hold.Exec("SELECT next_n FROM asset_code_seq WHERE kind = ? FOR UPDATE", store.KindProject).Error; err != nil {
		// 带上原因停测。
		t.Fatalf("hold code seq: %v", err)
	}
	// 占住起点工程行，改依赖会在写行时停下。
	if err := hold.Exec("SELECT id FROM assets WHERE id = ? FOR UPDATE", f.project.ID).Error; err != nil {
		// 带上原因停测。
		t.Fatalf("hold project: %v", err)
	}
	// 这条工艺的依赖，两路绑依赖都钉它。
	dep := pin(f.process)
	// 新建工程那一路的结果。
	insertDone := make(chan error, 1)
	// 新建一条带依赖的工程，锁住工艺后卡在发号上。
	go func() {
		// 新工程正文随便一小段。
		body := []byte(`{"items":[1]}`)
		// 写入新工程并钉住工艺，错误交回主协程。
		_, err := f.s.InsertAsset(context.Background(), store.Asset{
			Kind: store.KindProject, Name: "并发新工程", Status: store.AssetDraft,
			Content: body, Digest: digest.Sum(body), CreatorID: f.admin, Deps: []store.AssetDep{dep},
		})
		// 把这一路的结果交回去。
		insertDone <- err
	}()
	// 改依赖那一路的结果。
	updateDone := make(chan error, 1)
	// 给起点工程改绑这条工艺，锁住工艺后卡在写行上。
	go func() {
		// 按当前修订把工艺绑上，错误交回主协程。
		_, err := f.s.UpdateAsset(context.Background(), f.project.ID, f.project.Revision, store.AssetWrite{
			Name: f.project.Name, Content: f.project.Content, Digest: f.project.Digest,
			Status: f.project.Status, Deps: []store.AssetDep{dep},
		})
		// 把这一路的结果交回去。
		updateDone <- err
	}()
	// 两路都卡在占锁事务上，说明已锁住工艺。
	waitLockWaiters(t, f.db, 2)
	// 删除那一路的结果。
	deleteDone := make(chan error, 1)
	// 这时去删工艺，应该等绑依赖的两笔提交。
	go func() {
		// 删工艺并记撤回，错误交回主协程。
		deleteDone <- f.s.DeleteAssetAndRetract(context.Background(), f.process.ID)
	}()
	// 删除也卡住，说明锁行和共享锁互斥。
	waitLockWaiters(t, f.db, 3)
	// 放开占锁，让两路绑依赖提交。
	if err := hold.Commit().Error; err != nil {
		// 带上原因停测。
		t.Fatalf("commit hold: %v", err)
	}
	// 新建工程那一路应成功。
	if err := <-insertDone; err != nil {
		// 带上原因停测。
		t.Fatalf("insert with dep: %v", err)
	}
	// 改依赖那一路应成功。
	if err := <-updateDone; err != nil {
		// 带上原因停测。
		t.Fatalf("update with dep: %v", err)
	}
	// 删除复查时已看到两条工程钉着它，应被拒。
	if err := <-deleteDone; !errors.Is(err, domain.ErrReferenced) {
		// 带上实际错误停测。
		t.Fatalf("delete after bind: %v", err)
	}
	// 工艺必须还在，工程的依赖不悬空。
	if _, err := f.s.AssetByID(ctx, f.process.ID); err != nil {
		// 带上原因停测。
		t.Fatalf("process gone: %v", err)
	}
	// 读出撤回清单，被拒的删除不该留撤回。
	gone, err := f.s.ListRetractions(ctx)
	// 撤回清单读不出或含这条工艺都不对。
	if err != nil || slices.Contains(gone, f.process.ID) {
		// 带上清单和原因停测。
		t.Fatalf("retractions: %v %v", gone, err)
	}
}

// 验删除事务先锁住工艺，并发绑依赖要等它提交，之后因依赖已删被拒。
func TestBindWaitsForDeleteThenDependencyMissing(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 建库并备好工艺和工程。
	f := newRefFixture(t)
	// 开一笔占锁事务，让删除停在锁住工艺之后。
	hold := f.db.Begin()
	// 开事务失败就停，后面没法卡住删除。
	if hold.Error != nil {
		// 带上原因停测。
		t.Fatalf("begin hold: %v", hold.Error)
	}
	// 用例中途失败也放开占锁，免得别的协程挂死。
	defer hold.Rollback()
	// 先占一条同身份撤回，删除记撤回时会等它。
	if err := hold.Exec("INSERT INTO asset_retractions (asset_id, created_at) VALUES (?, now())", f.process.ID).Error; err != nil {
		// 带上原因停测。
		t.Fatalf("hold retraction: %v", err)
	}
	// 删除那一路的结果。
	deleteDone := make(chan error, 1)
	// 删工艺，锁行复查后卡在记撤回上。
	go func() {
		// 删工艺并记撤回，错误交回主协程。
		deleteDone <- f.s.DeleteAssetAndRetract(context.Background(), f.process.ID)
	}()
	// 删除卡在占锁事务上，说明已锁住工艺。
	waitLockWaiters(t, f.db, 1)
	// 这条工艺的依赖，两路绑依赖都钉它。
	dep := pin(f.process)
	// 新建工程那一路的结果。
	insertDone := make(chan error, 1)
	// 新建一条钉这条工艺的工程，应等删除提交。
	go func() {
		// 新工程正文随便一小段。
		body := []byte(`{"items":[2]}`)
		// 写入新工程并钉住工艺，错误交回主协程。
		_, err := f.s.InsertAsset(context.Background(), store.Asset{
			Kind: store.KindProject, Name: "迟到新工程", Status: store.AssetDraft,
			Content: body, Digest: digest.Sum(body), CreatorID: f.admin, Deps: []store.AssetDep{dep},
		})
		// 把这一路的结果交回去。
		insertDone <- err
	}()
	// 改依赖那一路的结果。
	updateDone := make(chan error, 1)
	// 给起点工程改绑这条工艺，应等删除提交。
	go func() {
		// 按当前修订把工艺绑上，错误交回主协程。
		_, err := f.s.UpdateAsset(context.Background(), f.project.ID, f.project.Revision, store.AssetWrite{
			Name: f.project.Name, Content: f.project.Content, Digest: f.project.Digest,
			Status: f.project.Status, Deps: []store.AssetDep{dep},
		})
		// 把这一路的结果交回去。
		updateDone <- err
	}()
	// 两路绑依赖也卡住，说明共享锁在等删除。
	waitLockWaiters(t, f.db, 3)
	// 撤掉占的撤回，让删除自己记撤回并提交。
	if err := hold.Rollback().Error; err != nil {
		// 带上原因停测。
		t.Fatalf("rollback hold: %v", err)
	}
	// 删除那一路应成功。
	if err := <-deleteDone; err != nil {
		// 带上原因停测。
		t.Fatalf("delete: %v", err)
	}
	// 新建工程那一路应因依赖已删被拒。
	if err := <-insertDone; !errors.Is(err, domain.ErrAssetDependency) {
		// 带上实际错误停测。
		t.Fatalf("insert after delete: %v", err)
	}
	// 改依赖那一路也应因依赖已删被拒。
	if err := <-updateDone; !errors.Is(err, domain.ErrAssetDependency) {
		// 带上实际错误停测。
		t.Fatalf("update after delete: %v", err)
	}
	// 再读起点工程，依赖和修订都不该变。
	j, err := f.s.AssetByID(ctx, f.project.ID)
	// 读失败、修订变了或带上了依赖都不对。
	if err != nil || j.Revision != f.project.Revision || len(j.Deps) != 0 {
		// 带上当前行和原因停测。
		t.Fatalf("project after refused bind: %+v %v", j, err)
	}
	// 库里不该有工程还钉着已删的工艺。
	used, err := f.s.AssetIsReferenced(ctx, f.process.ID)
	// 查不出或仍被引用都不对。
	if err != nil || used {
		// 带上结果和原因停测。
		t.Fatalf("dangling ref: %v %v", used, err)
	}
	// 读出撤回清单，删掉的工艺要记在里面。
	gone, err := f.s.ListRetractions(ctx)
	// 撤回清单读不出或没有这条都不对。
	if err != nil || !slices.Contains(gone, f.process.ID) {
		// 带上清单和原因停测。
		t.Fatalf("retractions: %v %v", gone, err)
	}
}

// 验不并发时：删仍被引用的拒绝，钉不存在的拒绝，已有的旧依赖不挡改名。
func TestAssetDepsLockSequential(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 建库并备好工艺和工程。
	f := newRefFixture(t)
	// 不存在的工艺依赖，用来验缺失被拒。
	ghost := store.AssetDep{ID: id.New(), Revision: 1, Digest: f.process.Digest}
	// 新工程正文随便一小段。
	body := []byte(`{"items":[3]}`)
	// 新建工程钉不存在的工艺，必须被拒。
	if _, err := f.s.InsertAsset(ctx, store.Asset{
		// 工程只钉一条不存在的工艺。
		Kind: store.KindProject, Name: "钉空工程", Status: store.AssetDraft,
		Content: body, Digest: digest.Sum(body), CreatorID: f.admin, Deps: []store.AssetDep{ghost},
	}); !errors.Is(err, domain.ErrAssetDependency) {
		// 带上实际错误停测。
		t.Fatalf("insert ghost dep: %v", err)
	}
	// 绕过接口给工程塞一条早年留下的悬空依赖。
	legacy, err := json.Marshal([]store.AssetDep{ghost})
	// 依赖编不成正文就停。
	if err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	// 直接改表，造出旧数据里的悬空依赖。
	if err := f.db.Exec("UPDATE assets SET deps = ?::jsonb WHERE id = ?", string(legacy), f.project.ID).Error; err != nil {
		// 带上原因停测。
		t.Fatalf("seed legacy dep: %v", err)
	}
	// 只改名、依赖照旧，已有的悬空依赖不该挡住。
	renamed, err := f.s.UpdateAsset(ctx, f.project.ID, f.project.Revision, store.AssetWrite{
		Name: "改名工程", Content: f.project.Content, Digest: f.project.Digest,
		Status: f.project.Status, Deps: []store.AssetDep{ghost},
	})
	// 改名失败就停，已有依赖不该再锁。
	if err != nil {
		// 带上原因停测。
		t.Fatalf("rename with legacy dep: %v", err)
	}
	// 再加一条不存在的依赖，必须被拒。
	if _, err := f.s.UpdateAsset(ctx, f.project.ID, renamed.Revision, store.AssetWrite{
		// 旧悬空依赖照留，另加一条新的不存在依赖。
		Name: renamed.Name, Content: f.project.Content, Digest: f.project.Digest,
		Status: renamed.Status, Deps: []store.AssetDep{ghost, {ID: id.New(), Revision: 1, Digest: f.process.Digest}},
	}); !errors.Is(err, domain.ErrAssetDependency) {
		// 带上实际错误停测。
		t.Fatalf("update new ghost dep: %v", err)
	}
	// 再加那条在库的工艺，应该成功。
	if _, err := f.s.UpdateAsset(ctx, f.project.ID, renamed.Revision, store.AssetWrite{
		// 旧悬空依赖照留，另加那条在库的工艺。
		Name: renamed.Name, Content: f.project.Content, Digest: f.project.Digest,
		Status: renamed.Status, Deps: []store.AssetDep{ghost, pin(f.process)},
	}); err != nil {
		// 带上原因停测。
		t.Fatalf("update real dep: %v", err)
	}
	// 工艺已被钉着，删除事务里复查必须拒绝。
	if err := f.s.DeleteAssetAndRetract(ctx, f.process.ID); !errors.Is(err, domain.ErrReferenced) {
		// 带上实际错误停测。
		t.Fatalf("delete referenced: %v", err)
	}
	// 读出撤回清单，被拒的删除不该留撤回。
	gone, err := f.s.ListRetractions(ctx)
	// 撤回清单读不出或不空都不对。
	if err != nil || len(gone) != 0 {
		// 带上清单和原因停测。
		t.Fatalf("retractions: %v %v", gone, err)
	}
	// 删不存在的身份，按不存在拒绝。
	if err := f.s.DeleteAssetAndRetract(ctx, id.New()); !errors.Is(err, domain.ErrNotFound) {
		// 带上实际错误停测。
		t.Fatalf("delete missing: %v", err)
	}
}
