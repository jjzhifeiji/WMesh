// 下发授权并发写入：同一资产和厂只落一行，收回后能重新激活。
package store_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/store"
)

// 每轮同时授权的路数，连接池空闲数也按它留。
const grantWorkers = 16

// 验多路同时授权同一资产到同一厂全部成功、只有一行，收回后能重新打开。
func TestUpsertFactoryGrantConcurrent(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 自己拿库连接，后面要直接数表里的行。
	db := testpg.Fresh(t)
	// 用刚才那份库，授权都写进这里。
	s := store.Open(db)
	// 取出底层连接池，才能调空闲连接数。
	sqlDB, err := db.DB()
	// 拿不到连接池就停，没法调空闲数。
	if err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	// 空闲连接留够，后几轮各路不用现拨号，写入才撞得上。
	sqlDB.SetMaxIdleConns(grantWorkers)
	// 先写入管理员，当作资产的创建人。
	admin, err := s.CreateAdmin(ctx, "wan", "hash")
	// 管理员没写成就停，后面的资产没有创建人。
	if err != nil {
		// 管理员没建成就停，后面的步骤没有操作者。
		t.Fatal(err)
	}
	// 登记一家厂，作为授权的目标。
	fac, err := s.RegisterFactory(ctx, id.New(), "厂A", id.New(), "sa-a", "超管A", "enroll-a")
	// 厂没登记上就停，授权没有目标厂。
	if err != nil {
		// 厂没建成就停，后面没有名录可挂。
		t.Fatal(err)
	}
	// 用一小段正文，摘要现算。
	body := []byte(`{"voltage":40}`)
	// 每轮换一份新资产，最后一轮的留给后面验收回。
	var a store.Asset
	// 每轮各路交回的授权，最后一轮的留给后面比对。
	var first store.DistributionGrant
	// 跑几轮，第一轮顺带把连接拨好，后几轮真并发。
	for round := range 5 {
		// 写入一条平台工艺，作为这一轮被授权的资产。
		a, err = s.InsertAsset(ctx, store.Asset{
			Kind: store.KindProcess, Name: "平台工艺", Status: store.AssetAvailable,
			Content: body, Digest: digest.Sum(body), CreatorID: admin.ID,
		})
		// 资产没写进去就停，授权没有对象。
		if err != nil {
			// 工艺没写进去就停，后面没有对象可授。
			t.Fatalf("round %d insert asset: %v", round, err)
		}
		// 这一轮并发授权，各路都该成功并指向同一行。
		first = upsertGrantsConcurrently(t, s, a.ID, fac.ID)
		// 准备接住行数，不能事先把零当成对。
		var n int64
		// 直接数库里这对资产和厂的授权行。
		if err := db.Raw("SELECT count(*) FROM distribution_grants WHERE asset_id = ? AND factory_id = ?", a.ID, fac.ID).Scan(&n).Error; err != nil {
			// 数不出来就停，不能把故障当成一行。
			t.Fatal(err)
		}
		// 不是恰好一行就停，并发授权多插或没插。
		if n != 1 {
			// 带上轮次和行数停测。
			t.Fatalf("round %d grant rows = %d, want 1", round, n)
		}
	}

	// 收回授权，后面验能重新打开。
	if err := s.RevokeFactoryGrant(ctx, a.ID, fac.ID); err != nil {
		// 收回失败就停，重新激活没有前提。
		t.Fatalf("revoke: %v", err)
	}
	// 读回收回后的授权，确认已失效。
	revoked, err := s.FactoryGrant(ctx, a.ID, fac.ID)
	// 读失败或仍有效就停，收回没生效。
	if err != nil || revoked.Active {
		// 带上结果停测。
		t.Fatalf("revoked: %+v %v", revoked, err)
	}
	// 再授权一次，应就地重新激活原来那行。
	again, err := s.UpsertFactoryGrant(ctx, a.ID, fac.ID)
	// 报错、没生效、换了身份或授权时间被改都不对。
	if err != nil || !again.Active || again.ID != first.ID || !again.CreatedAt.Equal(first.CreatedAt) {
		// 带上前后两份结果停测。
		t.Fatalf("reactivate: %+v first %+v err %v", again, first, err)
	}
	// 变更时间不能早于收回，说明这次写真落了库。
	if again.UpdatedAt.Before(revoked.UpdatedAt) {
		// 时间倒退就停，带上两份时间。
		t.Fatalf("updatedAt %v before revoke %v", again.UpdatedAt, revoked.UpdatedAt)
	}
	// 授权给不存在的资产，外键不过应按不存在拒绝。
	if _, err := s.UpsertFactoryGrant(ctx, id.New(), fac.ID); err != domain.ErrNotFound {
		// 不是不存在就停，外键错误没译对。
		t.Fatalf("missing asset: %v", err)
	}
}

// 多路同时授权同一资产到同一厂，核对全部成功且指向同一行，交回第一路结果。
func upsertGrantsConcurrently(t *testing.T, s *store.Store, assetID, factoryID uuid.UUID) store.DistributionGrant {
	// 失败行指到调用用例，不指到这个帮手。
	t.Helper()
	// 等所有协程跑完再核对。
	var wg sync.WaitGroup
	// 发令前全部挡住，放开后一起写，尽量撞在一起。
	start := make(chan struct{})
	// 每路的结果各占一格，不用加锁。
	grants := make([]store.DistributionGrant, grantWorkers)
	// 每路的错误各占一格，不用加锁。
	errs := make([]error, grantWorkers)
	// 起多路，同时授权同一资产到同一厂。
	for i := range grants {
		// 每路等发令后再写，记下各自的结果。
		wg.Go(func() {
			// 等发令，免得先起的协程先写完。
			<-start
			// 这一路授权，结果和错误各记一格。
			grants[i], errs[i] = s.UpsertFactoryGrant(context.Background(), assetID, factoryID)
		})
	}
	// 一起放开所有协程。
	close(start)
	// 等所有协程跑完。
	wg.Wait()
	// 逐路核对，全部成功且指向同一行。
	for i := range grants {
		// 有一路报错就停，并发授权不该撞约束。
		if errs[i] != nil {
			// 带上路号停测，便于看是哪一路撞上。
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		// 身份不同或没生效就停，说明交回的不是库里那一行。
		if grants[i].ID != grants[0].ID || !grants[i].Active {
			// 带上两路结果停测，便于比对。
			t.Fatalf("worker %d grant %+v, first %+v", i, grants[i], grants[0])
		}
	}
	return grants[0]
}
