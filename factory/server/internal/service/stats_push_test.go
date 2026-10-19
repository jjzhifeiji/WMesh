// 焊汇总后台上送：汇聚不等 WAN、同一时刻只推一批、可恢复的失败才重试。
package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// 并发汇聚不等上送；推送中来的合并成下一批，同一时刻只有一批在推，最后一批是全量。
func TestWeldPushSerialAndCoalesced(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 建厂并登录超管，焊次都记在超管名下。
	fac, saTok, saID := pushFactory(t, ctx)
	// 上送口先卡住，看汇聚会不会跟着等。
	g := &gateWAN{hold: make(chan struct{}), began: make(chan struct{}, 16)}
	// 挂上会卡住的假上送口，须在起循环前挂。
	fac.SetWeldWANPoster(g)
	// 起后台上送循环，用例结束时停掉。
	startWANPush(t, fac)
	// 先汇聚一条，让后台开始推第一批。
	out, err := fac.FlushWeldFacts(ctx, saTok, []factory.WeldFact{pushWeld(saID, 100)})
	// 汇聚失败或没收下就停。
	if err != nil || len(out.Accepted) != 1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("first flush: %+v %v", out, err)
	}
	// 等第一批真的开始推，后面的汇聚才落在推送期间。
	select {
	// 第一批已经卡在上送口里。
	case <-g.began:
	// 迟迟不推说明信号没发到后台。
	case <-time.After(10 * time.Second):
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatal("first push not started")
	}
	// 记下全部焊长，最后一批应等于它。
	total := int64(100)
	// 等并发汇聚全部回来。
	var wg sync.WaitGroup
	// 收各路汇聚的错误，协程里不能直接停测。
	errs := make(chan error, 5)
	// 推送卡着时再并发汇聚五批。
	for i := 0; i < 5; i++ {
		// 每批焊长不同，合计才能看出有没有漏。
		length := int64(200 + i)
		// 累进全部焊长。
		total += length
		// 造这一批的焊次。
		item := pushWeld(saID, length)
		// 登记一路汇聚。
		wg.Add(1)
		// 并发汇聚，模拟多台平板同时回连。
		go func() {
			// 汇聚回来就记一路完成。
			defer wg.Done()
			// 汇聚这一条，不该等上送口放行。
			out, err := fac.FlushWeldFacts(ctx, saTok, []factory.WeldFact{item})
			// 没报错却没收下也算失败。
			if err == nil && len(out.Accepted) != 1 {
				// 把收下结果写进错误，交给主协程报。
				err = fmt.Errorf("accepted %+v", out)
			}
			// 交回这一路的结果。
			errs <- err
		}()
	}
	// 全部汇聚回来时关掉，主协程据此不再等。
	back := make(chan struct{})
	// 另起协程等齐，主协程好设超时。
	go func() {
		// 等五路汇聚都回来。
		wg.Wait()
		// 通知主协程汇聚已全部回来。
		close(back)
	}()
	// 上送口还卡着，汇聚也必须先回来。
	select {
	// 汇聚都回来了，没有等 WAN。
	case <-back:
	// 卡住说明汇聚还在等上送。
	case <-time.After(10 * time.Second):
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatal("flush waited for wan push")
	}
	// 全部写完再收口，读完就不会再有新错误。
	close(errs)
	// 逐路核对汇聚结果。
	for err := range errs {
		// 有一路失败就停。
		if err != nil {
			// 把汇聚的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
	}
	// 第一批还卡着，后来的信号只能攒着，不能另起一批。
	if calls, peak, _ := g.stats(); calls != 1 || peak != 1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("while held: calls %d peak %d", calls, peak)
	}
	// 放行上送口，攒下的信号应合并成一批。
	close(g.hold)
	// 等最后一批带上全部焊长。
	last := g.waitFor(t, func(_ int, rows []factory.WeldWANSummary) bool { return sumLength(rows) == total })
	// 取出推送次数和同时在推的峰值。
	calls, peak, _ := g.stats()
	// 只该推两次：第一批和合并后的一批；峰值只能是一。
	if calls != 2 || peak != 1 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("calls %d peak %d", calls, peak)
	}
	// 次数合计要等于全部焊次。
	if n := sumRuns(last); n != 6 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("runs %d: %+v", n, last)
	}
}

// 上送口回不可达时退避重推，直到推上。
func TestWeldPushRetriesUnreachable(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 建厂并登录超管，焊次都记在超管名下。
	fac, saTok, saID := pushFactory(t, ctx)
	// 第一次回不可达，之后都成功。
	g := &gateWAN{errs: []error{domain.ErrWANUnreachable}, began: make(chan struct{}, 16)}
	// 挂上假上送口，须在起循环前挂。
	fac.SetWeldWANPoster(g)
	// 退避调小，用例不必等五秒。
	fac.SetWANPushBackoff(10*time.Millisecond, 40*time.Millisecond)
	// 起后台上送循环，用例结束时停掉。
	startWANPush(t, fac)
	// 汇聚一条，触发后台上送。
	if _, err := fac.FlushWeldFacts(ctx, saTok, []factory.WeldFact{pushWeld(saID, 300)}); err != nil {
		// 把汇聚的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 等重推成功，收到的就是这条焊长。
	g.waitFor(t, func(_ int, rows []factory.WeldWANSummary) bool { return sumLength(rows) == 300 })
	// 失败一次、重推一次，不该多推。
	if calls, _, _ := g.stats(); calls != 2 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("calls %d", calls)
	}
}

// 上送口回业务拒绝时不重试，等下一个信号才再推。
func TestWeldPushNoRetryOnReject(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 建厂并登录超管，焊次都记在超管名下。
	fac, saTok, saID := pushFactory(t, ctx)
	// 第一次回无权，之后都成功。
	g := &gateWAN{errs: []error{domain.ErrForbidden}, began: make(chan struct{}, 16)}
	// 挂上假上送口，须在起循环前挂。
	fac.SetWeldWANPoster(g)
	// 退避调小，若误重试很快就能看出来。
	fac.SetWANPushBackoff(10*time.Millisecond, 40*time.Millisecond)
	// 起后台上送循环，用例结束时停掉。
	startWANPush(t, fac)
	// 汇聚一条，触发后台上送。
	if _, err := fac.FlushWeldFacts(ctx, saTok, []factory.WeldFact{pushWeld(saID, 400)}); err != nil {
		// 把汇聚的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 等第一次上送开始。
	g.waitFor(t, func(calls int, _ []factory.WeldWANSummary) bool { return calls >= 1 })
	// 留足几轮退避的时间，误重试就会多推。
	time.Sleep(300 * time.Millisecond)
	// 被拒后不该重推，也没有推成功的行。
	if calls, _, last := g.stats(); calls != 1 || len(last) != 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("rejected retried: calls %d last %+v", calls, last)
	}
	// 再发一个信号，这次应推上。
	fac.RequestWANPush()
	// 等新信号推上，收到的就是这条焊长。
	g.waitFor(t, func(_ int, rows []factory.WeldWANSummary) bool { return sumLength(rows) == 400 })
}

// 刚起进程时钟没校正，签名被 WAN 拒；回包已校正时差，退避后重推应推上。
func TestWeldPushRetriesSignatureRejected(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 建厂并登录超管，焊次都记在超管名下。
	fac, saTok, saID := pushFactory(t, ctx)
	// 第一次回签名被拒，之后都成功。
	g := &gateWAN{errs: []error{domain.ErrWANSignatureRejected}, began: make(chan struct{}, 16)}
	// 挂上假上送口，须在起循环前挂。
	fac.SetWeldWANPoster(g)
	// 退避调小，用例不必等五秒。
	fac.SetWANPushBackoff(10*time.Millisecond, 40*time.Millisecond)
	// 起后台上送循环，用例结束时停掉。
	startWANPush(t, fac)
	// 汇聚一条，触发后台上送。
	if _, err := fac.FlushWeldFacts(ctx, saTok, []factory.WeldFact{pushWeld(saID, 500)}); err != nil {
		// 把汇聚的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 等重推成功，收到的就是这条焊长。
	g.waitFor(t, func(_ int, rows []factory.WeldWANSummary) bool { return sumLength(rows) == 500 })
	// 被拒一次、重推一次，不该多推。
	if calls, _, _ := g.stats(); calls != 2 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("calls %d", calls)
	}
}

// 签名一直被拒（厂钥真不对）时每批只补推有限几次，之后等下一个信号。
func TestWeldPushSignatureRejectedBounded(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 建厂并登录超管，焊次都记在超管名下。
	fac, saTok, saID := pushFactory(t, ctx)
	// 前三次都回签名被拒，之后才成功。
	rej := domain.ErrWANSignatureRejected
	// 首推加两次补推都被拒，这一批就该停下。
	g := &gateWAN{errs: []error{rej, rej, rej}, began: make(chan struct{}, 16)}
	// 挂上假上送口，须在起循环前挂。
	fac.SetWeldWANPoster(g)
	// 退避调小，若误多推很快就能看出来。
	fac.SetWANPushBackoff(10*time.Millisecond, 40*time.Millisecond)
	// 起后台上送循环，用例结束时停掉。
	startWANPush(t, fac)
	// 汇聚一条，触发后台上送。
	if _, err := fac.FlushWeldFacts(ctx, saTok, []factory.WeldFact{pushWeld(saID, 600)}); err != nil {
		// 把汇聚的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 等首推和两次补推都推过。
	g.waitFor(t, func(calls int, _ []factory.WeldWANSummary) bool { return calls >= 3 })
	// 留足几轮退避的时间，误多推就会超过三次。
	time.Sleep(300 * time.Millisecond)
	// 名额用完后不该再推，也没有推成功的行。
	if calls, _, last := g.stats(); calls != 3 || len(last) != 0 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("bounded: calls %d last %+v", calls, last)
	}
	// 再发一个信号，这次应推上。
	fac.RequestWANPush()
	// 等新信号推上，收到的就是这条焊长。
	g.waitFor(t, func(_ int, rows []factory.WeldWANSummary) bool { return sumLength(rows) == 600 })
}

// 会卡住的假上送口，记下同时在推几批和每次推上的行。
type gateWAN struct {
	mu     sync.Mutex               // 护住计数和收到的行，上送和断言不在同一协程
	active int                      // 此刻正在推的批数
	peak   int                      // 同时在推的最多批数
	calls  int                      // 一共推了几次，失败的也算
	last   []factory.WeldWANSummary // 最近一次推成功的行
	errs   []error                  // 按次序交回的错误，用完后都回成功
	hold   chan struct{}            // 不空时每次上送都等它关闭才返回
	began  chan struct{}            // 每次开始上送就投一下，满了就丢
}

// 记下并发数，按需卡住，再按次序交回错误。
func (g *gateWAN) PostWeldSummaries(ctx context.Context, _ uuid.UUID, rows []factory.WeldWANSummary) error {
	// 加锁记下这一批开始推。
	g.mu.Lock()
	// 正在推的批数加一。
	g.active++
	// 刷新同时在推的峰值。
	g.peak = max(g.peak, g.active)
	// 记下这是第几次，按次序取错误。
	n := g.calls
	// 推送次数加一。
	g.calls++
	// 记完就放锁，卡住时不挡断言读。
	g.mu.Unlock()
	// 通知用例上送已开始，满了就不等。
	select {
	// 投一下开始信号。
	case g.began <- struct{}{}:
	// 用例没在收就丢掉。
	default:
	}
	// 设了闸门就等放行或取消。
	if g.hold != nil {
		// 闸门关闭或循环被停才往下走。
		select {
		// 用例放行了。
		case <-g.hold:
		// 循环被停，不再卡着。
		case <-ctx.Done():
		}
	}
	// 加锁记下这一批推完。
	g.mu.Lock()
	// 放锁后再交回结果。
	defer g.mu.Unlock()
	// 正在推的批数减一。
	g.active--
	// 循环被停就交回取消，不算推成功。
	if err := ctx.Err(); err != nil {
		return err
	}
	// 按次序取这一次该交回的错误，用完就成功。
	var err error
	// 还有排好的错误就用它。
	if n < len(g.errs) {
		// 取出第 n 次的错误。
		err = g.errs[n]
	}
	// 只有推成功才记下收到的行。
	if err == nil {
		// 拷一份留着，调用方改原切片不影响断言。
		g.last = append([]factory.WeldWANSummary(nil), rows...)
	}
	return err
}

// 加锁取出推送次数、峰值和最近推成功的行。
func (g *gateWAN) stats() (int, int, []factory.WeldWANSummary) {
	// 加锁再读，免得和上送并发读写。
	g.mu.Lock()
	// 读完就放锁。
	defer g.mu.Unlock()
	// 交回计数和行的拷贝。
	return g.calls, g.peak, append([]factory.WeldWANSummary(nil), g.last...)
}

// 轮询到条件成立，交回此时最近推成功的行；超时就停测。
func (g *gateWAN) waitFor(t *testing.T, cond func(calls int, last []factory.WeldWANSummary) bool) []factory.WeldWANSummary {
	// 失败算在调用方行号上。
	t.Helper()
	// 最多等十秒，后台推不上就算失败。
	deadline := time.Now().Add(10 * time.Second)
	// 到点前反复看。
	for time.Now().Before(deadline) {
		// 取出此刻的次数和行。
		calls, _, last := g.stats()
		// 条件成立就交回。
		if cond(calls, last) {
			return last
		}
		// 稍等再看，别空转。
		time.Sleep(10 * time.Millisecond)
	}
	// 取出最后一次的状态，便于看是哪里没到。
	calls, peak, last := g.stats()
	// 超时就停测，并带上当时的状态。
	t.Fatalf("wan push timeout: calls %d peak %d last %+v", calls, peak, last)
	return nil
}

// 轮询到收下非空摘要为止，交回拷贝；超时就停测。
func (c *captureWAN) waitRows(t *testing.T) []factory.WeldWANSummary {
	// 失败算在调用方行号上。
	t.Helper()
	// 最多等十秒，后台推不上就算失败。
	deadline := time.Now().Add(10 * time.Second)
	// 到点前反复看。
	for time.Now().Before(deadline) {
		// 加锁取出收到的摘要。
		rows := c.snapshot()
		// 收到了就交回。
		if len(rows) > 0 {
			return rows
		}
		// 稍等再看，别空转。
		time.Sleep(10 * time.Millisecond)
	}
	// 超时就停测，后台没把汇总推上来。
	t.Fatal("wan summaries not pushed")
	return nil
}

// 起后台上送循环，用例结束时停掉并等它退出，免得删库后还在读。
func startWANPush(t *testing.T, fac *factory.Service) {
	// 失败算在调用方行号上。
	t.Helper()
	// 循环跟着这条用例，结束时取消。
	ctx, cancel := context.WithCancel(context.Background())
	// 循环退出时关闭，收尾要等它。
	done := make(chan struct{})
	// 后台跑上送循环。
	go func() {
		// 循环退出就通知收尾。
		defer close(done)
		// 收到信号才推，取消就退出。
		fac.RunWANPush(ctx)
	}()
	// 收尾先停循环再等退出，库要等这之后才删。
	t.Cleanup(func() {
		// 叫停循环。
		cancel()
		// 等循环真的退出。
		<-done
	})
}

// 建厂、开通并登录超管，交回服务、令牌和超管身份。
func pushFactory(t *testing.T, ctx context.Context) (*factory.Service, string, uuid.UUID) {
	// 失败算在调用方行号上。
	t.Helper()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	created, fac, err := h.Provision(ctx, "sa-push", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa-push", created.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录换取会话令牌，失败说明口令或状态不对。
	tok, err := fac.Login(ctx, "sa-push", "sa-pass")
	// 登录失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把登录的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	return fac, tok, created.SuperAdminID
}

// 造一条超管名下的焊事实，只有焊长不同。
func pushWeld(creator uuid.UUID, length int64) factory.WeldFact {
	// 交回带新身份的焊事实，同一天同工程同模式。
	return factory.WeldFact{
		ID: id.New(), CreatorID: creator, OrgPath: []factory.PathNode{},
		ProjectName: "工程A", WeldKind: "single", LengthMM: length, DurationSec: 1,
		OccurredAt: time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC),
	}
}

// 把上送行的焊长加起来。
func sumLength(rows []factory.WeldWANSummary) int64 {
	// 从零累加。
	var n int64
	// 逐行加上焊长。
	for _, r := range rows {
		// 累进这一行的焊长。
		n += r.LengthMM
	}
	return n
}

// 把上送行的次数加起来。
func sumRuns(rows []factory.WeldWANSummary) int64 {
	// 从零累加。
	var n int64
	// 逐行加上次数。
	for _, r := range rows {
		// 累进这一行的次数。
		n += r.RunCount
	}
	return n
}
