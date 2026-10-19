package service_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wmesh/factory/internal/platform/blob"
	"wmesh/factory/internal/platform/digest"
	factory "wmesh/factory/internal/service"
)

// 卡住的软件源：拉包卡到放行才交回，用来看领头请求被取消后的走向。
type gateSource struct {
	body     []byte        // 放行后交回的包字节
	entered  chan struct{} // 开始拉包就通知用例，留一格不堵
	release  chan struct{} // 用例关上它，卡住的拉包才交回
	pulls    atomic.Int32  // 实际拉了几次，多一次说明单飞没起作用
	mu       sync.Mutex    // 护住收下记录，后台拉包和用例会并发读写
	received [][]byte      // 每次收下通知带的摘要，按先后记
}

// Received 记下收下通知带的摘要，用例据此看暂存有没有清。
func (g *gateSource) Received(_ string, _ int64, sum []byte) {
	// 占住记录，避免和用例读交错。
	g.mu.Lock()
	// 记完就放开。
	defer g.mu.Unlock()
	// 拷一份摘要记下，调用方事后改不到。
	g.received = append(g.received, append([]byte(nil), sum...))
}

// 读出收下通知带过的摘要。
func (g *gateSource) receivedSums() [][]byte {
	// 占住记录，避免和后台写交错。
	g.mu.Lock()
	// 读完就放开。
	defer g.mu.Unlock()
	// 拷一份切片交回，读的时候不怕再追加。
	return append([][]byte(nil), g.received...)
}

// 写不进的对象存储，用来让收下这一步失败。
type failPutBlobs struct {
	*blob.Memory // 读删照常走内存存根
}

// Put 一律报错，模拟对象存储暂时不可用。
func (failPutBlobs) Put(context.Context, string, []byte) error {
	// 交回固定错误，收下应当失败。
	return errors.New("oss unavailable")
}

// Latest 交回客户端包 v3 和它的摘要。
func (g *gateSource) Latest(_ context.Context, kind string) (factory.SoftwareMeta, error) {
	// 交回 v3 和按包字节算出的摘要。
	return factory.SoftwareMeta{Kind: kind, Version: 3, VersionName: "6.3.0", Digest: digest.Sum(g.body)}, nil
}

// Pull 记一次拉包，通知用例后卡到放行。
func (g *gateSource) Pull(ctx context.Context, _ string, _ int64, _ []byte) ([]byte, error) {
	// 记一次拉包。
	g.pulls.Add(1)
	// 通知用例已经开始拉，重复的丢掉。
	select {
	// 送出一次开始信号。
	case g.entered <- struct{}{}:
	// 已经送过就不再塞。
	default:
	}
	// 卡到放行，或下载自己的上下文被取消。
	select {
	// 放行了就交回包字节。
	case <-g.release:
		return g.body, nil
	// 下载被取消就交回原因，说明没和领头请求脱开。
	case <-ctx.Done():
		// 交回取消原因，用例会因此失败。
		return nil, ctx.Err()
	}
}

// 领头调用的 ctx 被取消后，后台下载照常跑完，等着的那一路拿到收下的结果。
func TestEnsureSoftwareSurvivesLeaderCancel(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	_, fac, err := h.Provision(ctx, "sa-lead", "超管")
	// 建厂失败就停，避免带着错误继续验
	if err != nil {
		// 建厂失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 用卡住的源代替云端，拉包卡到放行。
	src := &gateSource{body: []byte("client-apk-3"), entered: make(chan struct{}, 1), release: make(chan struct{})}
	// 换上软件源，拉包都走它
	fac.Updates.SetSoftwareSource(src)
	// 领头调用的上下文，后面主动取消。
	lctx, lcancel := context.WithCancel(ctx)
	// 离开时取消，含提前失败。
	defer lcancel()
	// 接住领头调用的结果。
	leader := make(chan error, 1)
	// 领头调用在后台跑，卡在拉包里。
	go func() {
		// 领头去拉客户端包 v3。
		leader <- fac.Updates.EnsureSoftware(lctx, factory.SoftwareClientAPK, 3)
	}()
	// 等拉包真正开始。
	select {
	// 拉包已开始，往下走。
	case <-src.entered:
	// 两秒还没开始就停测。
	case <-time.After(2 * time.Second):
		// 拉包没开始就停掉本例
		t.Fatal("pull not started")
	}
	// 接住等着的那一路的结果。
	waiter := make(chan error, 1)
	// 第二路同版本调用，应等领头那次下载。
	go func() {
		// 同版本再要一次，不该另起下载。
		waiter <- fac.Updates.EnsureSoftware(ctx, factory.SoftwareClientAPK, 3)
	}()
	// 稍等让第二路排进等待。
	time.Sleep(100 * time.Millisecond)
	// 取消领头调用，它应立刻返回。
	lcancel()
	// 领头调用返回或超时，谁先到走谁。
	select {
	// 领头调用应按取消返回。
	case err := <-leader:
		// 不是取消就停测。
		if !errors.Is(err, context.Canceled) {
			// 领头返回原因不对就停掉本例
			t.Fatalf("leader %v", err)
		}
	// 一秒还没返回说明被下载拖住了。
	case <-time.After(time.Second):
		// 领头被下载拖住就停掉本例
		t.Fatal("leader blocked after cancel")
	}
	// 放行卡住的下载，后台应照常收下。
	close(src.release)
	// 等着的那一路返回或超时，谁先到走谁。
	select {
	// 等着的那一路应拿到收下的结果。
	case err := <-waiter:
		// 没收下就停测。
		if err != nil {
			// 等着的一路失败就停掉本例
			t.Fatalf("waiter %v", err)
		}
	// 两秒还没返回就停测。
	case <-time.After(2 * time.Second):
		// 等着的一路挂住就停掉本例
		t.Fatal("waiter hung")
	}
	// 已收下完整副本，再要一次不该再拉。
	if err := fac.Updates.EnsureSoftware(ctx, factory.SoftwareClientAPK, 3); err != nil {
		// 再要一次失败就停掉本例
		t.Fatal(err)
	}
	// 两路加一次重查，只该真正拉过一次。
	if n := src.pulls.Load(); n != 1 {
		// 拉包次数不对就停掉本例
		t.Fatalf("pulls %d", n)
	}
}

// 收下失败不通知源清暂存；换回能写的存储重试，收下后才通知一次，摘要对得上。
func TestEnsureSoftwareClearsPartAfterIngest(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	_, fac, err := h.Provision(ctx, "sa-part", "超管")
	// 建厂失败就停，避免带着错误继续验
	if err != nil {
		// 建厂失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 放行的源：拉包立刻交回包字节。
	src := &gateSource{body: []byte("client-apk-3"), entered: make(chan struct{}, 1), release: make(chan struct{})}
	// 先放行，拉包不卡。
	close(src.release)
	// 换上软件源，拉包都走它
	fac.Updates.SetSoftwareSource(src)
	// 换上写不进的存储，收下必然失败。
	fac.SetBlobs(failPutBlobs{Memory: blob.NewMemory()})
	// 拉回来了但收不下，应交回错误。
	if err := fac.Updates.EnsureSoftware(ctx, factory.SoftwareClientAPK, 3); err == nil {
		// 收不下却报成功就停掉本例
		t.Fatal("ingest should fail")
	}
	// 没收下不该通知清暂存，否则重试要从零重下。
	if n := len(src.receivedSums()); n != 0 {
		// 收下失败也清了暂存就停掉本例
		t.Fatalf("received after failed ingest %d", n)
	}
	// 换回能写的存储，重试应能收下。
	fac.SetBlobs(blob.NewMemory())
	// 重试收下，这次应成功。
	if err := fac.Updates.EnsureSoftware(ctx, factory.SoftwareClientAPK, 3); err != nil {
		// 重试仍失败就停掉本例
		t.Fatal(err)
	}
	// 读出收下通知带过的摘要。
	sums := src.receivedSums()
	// 只该通知一次，摘要是这份包的。
	if len(sums) != 1 || !bytes.Equal(sums[0], digest.Sum(src.body)) {
		// 通知次数或摘要不对就停掉本例
		t.Fatalf("received %x", sums)
	}
}
