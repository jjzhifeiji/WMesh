// 建树根并发验收：根被删后多路同时建同一种类的根，全部成功、拿到同一根，库里只有一行。
package store_test

import (
	"context"
	"sync"
	"testing"

	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/store"
)

// rootRaceRounds 重放的轮数，每轮先删根再抢建，多跑几轮才压得出交错。
const rootRaceRounds = 10

// rootRaceWorkers 每轮同时建根的路数，模拟几处一起打开目录。
const rootRaceWorkers = 8

// ensureRootAllAtOnce 多路等同一声起跑后一起建根，按下标交回各自的结果。
func ensureRootAllAtOnce(n int, fn func() (store.FSNode, error)) ([]store.FSNode, []error) {
	// 准备承接每一路拿到的根，各写各的下标不打架。
	nodes := make([]store.FSNode, n)
	// 准备承接每一路的错误，各写各的下标不打架。
	errs := make([]error, n)
	// 起跑信号，全部就位后一起放行。
	start := make(chan struct{})
	// 等所有路都跑完再交回结果。
	var wg sync.WaitGroup
	// 先登记要等的路数，免得提前收尾。
	wg.Add(n)
	// 逐路起协程，都先卡在起跑信号上。
	for i := 0; i < n; i++ {
		// 每路等起跑后建根，结果写进自己的下标。
		go func(i int) {
			// 这一路跑完就报到，主协程好收尾。
			defer wg.Done()
			// 等起跑信号，让多路尽量同时进库。
			<-start
			// 建根并把结果放进自己的下标。
			nodes[i], errs[i] = fn()
		}(i)
	}
	// 一起放行，制造同时建根的交错。
	close(start)
	// 等全部路跑完再看结果。
	wg.Wait()
	return nodes, errs
}

// 同一种类多路同时建根：全部成功、拿到同一根，库里只有一行。
func TestEnsureFSRootConcurrentKind(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 每次打开干净库，避免别的用例留下的行。
	db := testpg.Fresh(t)
	// 用这份库打开存储层，后面直接数行要用同一份库。
	s := store.Open(db)
	// 每轮先删掉根再抢建，让根一开始都不存在。
	for r := 0; r < rootRaceRounds; r++ {
		// 新库只有迁移种下的空根，删掉它好让各路去抢建。
		if err := db.Exec("DELETE FROM asset_fs_nodes WHERE parent_id IS NULL AND asset_kind = ?", store.KindProcess).Error; err != nil {
			// 删根失败就停测，后面没法抢建。
			t.Fatalf("round %d delete root: %v", r, err)
		}
		// 多路同时建工艺平台树的根。
		nodes, errs := ensureRootAllAtOnce(rootRaceWorkers, func() (store.FSNode, error) {
			// 建工艺平台树的根，并发时只能落一行。
			return s.EnsureFSRoot(ctx, store.KindProcess)
		})
		// 逐路核对，每一路都必须成功且拿到同一根。
		for i := range nodes {
			// 有一路报错就说明撞约束后没兜住。
			if errs[i] != nil {
				// 并发建根不能报库错误。
				t.Fatalf("round %d worker %d: %v", r, i, errs[i])
			}
			// 拿到的根必须和第一路是同一个。
			if nodes[i].ID != nodes[0].ID {
				// 各路拿到不同根就说明建出了两棵。
				t.Fatalf("round %d worker %d root %s, want %s", r, i, nodes[i].ID, nodes[0].ID)
			}
		}
		// 准备承接库里这一种类的根行数。
		var n int64
		// 直接数库里的根，确认没有多建。
		if err := db.Raw("SELECT count(*) FROM asset_fs_nodes WHERE parent_id IS NULL AND asset_kind = ?", store.KindProcess).Scan(&n).Error; err != nil {
			// 数不出来就停测。
			t.Fatalf("round %d count: %v", r, err)
		}
		// 这一种类的根必须恰好一行。
		if n != 1 {
			// 根行数不对就停测。
			t.Fatalf("round %d roots %d, want 1", r, n)
		}
	}
}
