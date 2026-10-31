// 建树根并发验收：多路同时给同一人建个人根，全部成功、拿到同一根，库里只有一行。
package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/store"
)

// rootRaceRounds 重放的轮数，每轮换一个新人，多跑几轮才压得出交错。
const rootRaceRounds = 10

// rootRaceWorkers 每轮同时建根的路数，模拟示教器和网页一起打开。
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

// 同一人的个人树多路同时建根：全部成功、拿到同一根，库里只有一行。
func TestEnsureFSRootConcurrentPersonal(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 每轮换一个新人，个人根一开始都不存在。
	for r := 0; r < rootRaceRounds; r++ {
		// 建这一轮的账号，个人根的主人要真实存在。
		p, err := s.CreatePerson(ctx, fmt.Sprintf("root-race-%02d", r), "建根并发", "", false)
		// 建账号失败就停测，避免后面没有前提。
		if err != nil {
			// 建账号失败就停测。
			t.Fatalf("round %d person: %v", r, err)
		}
		// 多路同时给这个人建工艺个人根。
		nodes, errs := ensureRootAllAtOnce(rootRaceWorkers, func() (store.FSNode, error) {
			// 建这个人的工艺个人根，并发时只能落一行。
			return s.EnsureFSRoot(ctx, store.KindProcess, store.FSTreePersonal, &p.ID)
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
		// 准备承接库里这个人的个人根行数。
		var n int64
		// 直接数库里的根，确认没有多建。
		if err := facDB.Raw("SELECT count(*) FROM asset_fs_nodes WHERE parent_id IS NULL AND asset_kind = ? AND tree_level = ? AND owner_id = ?",
			store.KindProcess, store.FSTreePersonal, p.ID).Scan(&n).Error; err != nil {
			// 数不出来就停测。
			t.Fatalf("round %d count: %v", r, err)
		}
		// 个人根必须恰好一行。
		if n != 1 {
			// 根行数不对就停测。
			t.Fatalf("round %d roots %d, want 1", r, n)
		}
	}
}
