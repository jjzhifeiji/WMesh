// 验收 WAN 焊汇总并发上送：同一家厂按厂排队，不撞唯一索引，也不两批混写。
package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"wmesh/global/internal/service"
)

// 同一家厂并发整表上送，全部成功，最后库里是其中完整的一批。
func TestWeldSummariesConcurrentPut(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面查不了汇总。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面查不了汇总。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 登记一家厂，并发上送都指它。
	created, err := h.WAN.CreateFactory(ctx, tok, "厂并发", "sa-c", "超管C")
	// 登记工厂失败就停，没有厂可上送。
	if err != nil {
		// 不符即停：登记工厂失败。
		t.Fatal(err)
	}
	// 记下这家厂，并发上送和查询都指它。
	fid := created.Factory.ID
	// 并发几批，每批同样的键、不同的数。
	const batches, days = 8, 20
	// 等各路上送都回来。
	var wg sync.WaitGroup
	// 收各路上送的错误，协程里不能直接停测。
	errs := make(chan error, batches)
	// 每批一路，同时上送整表。
	for b := 0; b < batches; b++ {
		// 按天数预留这一批的行。
		rows := make([]service.WeldSummaryIn, 0, days)
		// 每批都覆盖同样的二十天，键完全相同。
		for d := 1; d <= days; d++ {
			// 次数和焊长带上批号，混写就能看出来。
			rows = append(rows, service.WeldSummaryIn{
				Day: fmt.Sprintf("2026-09-%02d", d), ProjectName: "工程A", WeldKind: "single",
				RunCount: int64(b + 1), LengthMM: int64(b+1) * 100, DurationSec: int64(b + 1),
			})
		}
		// 登记一路上送。
		wg.Add(1)
		// 并发上送，模拟厂端几路同时推。
		go func() {
			// 上送回来就记一路完成。
			defer wg.Done()
			// 交回这一路的结果，撞唯一索引会在这里报错。
			errs <- h.WAN.Stats.PutFromFactory(ctx, fid, rows)
		}()
	}
	// 等全部上送回来。
	wg.Wait()
	// 全部写完再收口，读完就不会再有新错误。
	close(errs)
	// 逐路核对，有一路失败就停。
	for err := range errs {
		// 并发上送报错说明没有按厂排队。
		if err != nil {
			// 不符即停：并发上送报错。
			t.Fatalf("concurrent put: %v", err)
		}
	}
	// 查这家厂的汇总，看最后留下的是哪一批。
	got, err := h.WAN.Stats.ListWeldReports(ctx, tok, fid.String(), nil, nil)
	// 查询失败或天数不对就停。
	if err != nil || len(got) != days {
		// 不符即停：查询失败或行数不对。
		t.Fatalf("list %d %v", len(got), err)
	}
	// 以第一行的批号为准，其余行都该是同一批。
	batch := got[0].RunCount
	// 逐行核对，批号不一说明两批混写。
	for _, r := range got {
		// 次数或焊长和第一行不是同一批就停。
		if r.RunCount != batch || r.LengthMM != batch*100 || r.DurationSec != batch {
			// 不符即停：两批混在一起。
			t.Fatalf("mixed batches: %+v", got)
		}
	}
}
