// 示教器拉 APK 走流式：查包和下载都不整包读，摘要每个对象只核一次，续传不重记允许。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/blob"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// noGetBlobs 包住内存存根：整包读一律报错，打开次数和读出字节都计数。
type noGetBlobs struct {
	*blob.Memory               // 真正存字节的内存存根
	gets         atomic.Int64  // 整包读的次数，必须一直是零
	opens        atomic.Int64  // 打开流的次数，看有几路在核
	reads        atomic.Int64  // 从流里读出的字节合计，看整包核了几遍
	gate         chan struct{} // 非空时读流要等它关上，用来压住首次核对
}

// Get 整包读一律报错，谁调了就记一笔。
func (b *noGetBlobs) Get(context.Context, string) ([]byte, error) {
	// 记下有人整包读，断言时必须是零。
	b.gets.Add(1)
	// 交回错误，整包读的路径走不通。
	return nil, errors.New("whole read forbidden")
}

// Open 打开内存流并包上计数。
func (b *noGetBlobs) Open(ctx context.Context, key string) (blob.Object, error) {
	// 记下打开一次，单飞失效时会多出来。
	b.opens.Add(1)
	// 交给内存存根真正打开。
	obj, err := b.Memory.Open(ctx, key)
	// 没有该键就原样交回找不到。
	if err != nil {
		return nil, err
	}
	// 包上计数，读多少都记下。
	return &countObject{Object: obj, owner: b}, nil
}

// countObject 读的时候按门等待并累计读出的字节。
type countObject struct {
	blob.Object             // 真正的内存流，定位和关闭都交给它
	owner       *noGetBlobs // 记数和门都在存根上
}

// Read 先等门开，再读一段并累计字节。
func (o *countObject) Read(p []byte) (int, error) {
	// 设了门就等它关上，压住首次核对。
	if o.owner.gate != nil {
		// 等门关上才往下读。
		<-o.owner.gate
	}
	// 从真正的内存流读一段。
	n, err := o.Object.Read(p)
	// 读出的字节记进合计。
	o.owner.reads.Add(int64(n))
	return n, err
}

// 读完整份包流并关上，读不完就停掉本例。
func readStream(t *testing.T, pack factory.SoftwareStream) []byte {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 读完就关上流
	defer pack.Body.Close()
	// 把流里的字节全部读完
	body, err := io.ReadAll(pack.Body)
	// 读不完就停，避免拿半截正文去比
	if err != nil {
		// 读失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	return body
}

// 数示教器拉包留下的允许审计，收包那几条没有操作者不算。
func padPullAllows(t *testing.T, ctx context.Context, fac *factory.Service) int {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 读出本厂审计，读不到就没法数
	rows, err := fac.ListAudit(ctx)
	// 读审计失败就停，避免带着错误继续验
	if err != nil {
		// 读审计失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 从零开始数
	n := 0
	// 逐行挑出有操作者的拉包允许
	for _, r := range rows {
		// 动作、结果和操作者都对上才算
		if r.Action == "pull_software" && r.Result == audit.Allow && r.ActorID != nil {
			// 对上一条就加一
			n++
		}
	}
	return n
}

// 查元数据和下载都不整包读；续传不重记允许；重启后并发首查只核一次；换了存储判不完整
func TestPadClientSoftwareStream(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seed, fac, err := h.Provision(ctx, "sa-apk", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := fac.Activate(ctx, "sa-apk", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	tok := mustLogin(t, ctx, fac, "sa-apk", "sa-pass")
	// 换上整包读就报错的存根，证明后面都不整包读
	first := &noGetBlobs{Memory: blob.NewMemory()}
	// 挂上存根，之后收包和查包都走它
	fac.Updates.SetBlobs(first)
	// 准备一份客户端包，长度够区分整包核了几遍
	apk := bytes.Repeat([]byte("apk-stream-"), 64)
	// 收下这份客户端包，失败则后面没有包可拉
	if err := fac.IngestSoftware(ctx, offerOf(factory.SoftwareClientAPK, 51, "6.1.0", apk)); err != nil {
		// 收包失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 查平板可装软件，收包时已记下摘要，不该再整包读
	row, err := fac.PadClientSoftware(ctx, tok)
	// 查不到或版本不对就失败
	if err != nil || row == nil || row.Version != 51 {
		// 元数据不对就停掉本例
		t.Fatalf("meta %+v %v", row, err)
	}
	// 查元数据既不能整包读，也不能流式重核
	if first.gets.Load() != 0 || first.reads.Load() != 0 {
		// 查元数据还在读正文就失败
		t.Fatalf("meta read gets=%d reads=%d", first.gets.Load(), first.reads.Load())
	}
	// 从头下载一份，算一次允许
	pack, err := fac.PullPadClientSoftware(ctx, tok, 51, false)
	// 打不开就失败
	if err != nil {
		// 打开失败就把原因打出并停掉本例
		t.Fatalf("pull %v", err)
	}
	// 读完整份流，正文必须对上
	if got := readStream(t, pack); !bytes.Equal(got, apk) {
		// 正文不对就停掉本例
		t.Fatalf("pull body %d bytes", len(got))
	}
	// 下载只读一遍正文，不整包读也不额外核
	if first.gets.Load() != 0 || first.reads.Load() != int64(len(apk)) {
		// 读多了就是还在整包读或重核
		t.Fatalf("pull read gets=%d reads=%d", first.gets.Load(), first.reads.Load())
	}
	// 从头下载必须记一条允许
	if n := padPullAllows(t, ctx, fac); n != 1 {
		// 允许审计条数不对就失败
		t.Fatalf("allow after pull %d", n)
	}
	// 断线后续传，不该再记允许
	pack, err = fac.PullPadClientSoftware(ctx, tok, 51, true)
	// 打不开就失败
	if err != nil {
		// 打开失败就把原因打出并停掉本例
		t.Fatalf("resume %v", err)
	}
	// 跳到续传起点，模拟带区间头的请求
	if _, err := pack.Body.Seek(4, io.SeekStart); err != nil {
		// 定位失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 读出剩下的部分，必须是包的后半
	if got := readStream(t, pack); !bytes.Equal(got, apk[4:]) {
		// 续传正文不对就停掉本例
		t.Fatalf("resume body %d bytes", len(got))
	}
	// 续传后允许审计还是一条
	if n := padPullAllows(t, ctx, fac); n != 1 {
		// 续传还在记允许就失败
		t.Fatalf("allow after resume %d", n)
	}

	// 对象键和收包时一致，模拟重启后同一个桶
	key := store.SoftwareObjectKey(factory.SoftwareClientAPK, 51)
	// 新存根带门，首次核对会被压住
	second := &noGetBlobs{Memory: blob.NewMemory(), gate: make(chan struct{})}
	// 同一份字节放进新存根，核对应当通过
	if err := second.Put(ctx, key, apk); err != nil {
		// 写入失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 换上新存根，旧的核对记录随之作废
	fac.Updates.SetBlobs(second)
	// 同时来查的示教器台数
	const pads = 8
	// 等所有查包结束
	var wg sync.WaitGroup
	// 每台的查包结果，按下标放
	results := make([]*factory.SoftwareReplica, pads)
	// 每台的查包错误，按下标放
	errs := make([]error, pads)
	// 同时起几台一起查元数据
	for i := 0; i < pads; i++ {
		// 记下多一台在查
		wg.Add(1)
		// 每台各自去查，模拟整厂重启后一起来
		go func(i int) {
			// 查完就报一声
			defer wg.Done()
			// 查本厂可装的客户端包
			results[i], errs[i] = fac.PadClientSoftware(ctx, tok)
		}(i)
	}
	// 最晚等五秒，各台打开对象加一路核对
	deadline := time.Now().Add(5 * time.Second)
	// 等每台都打开过对象，且核对那一路也已打开
	for second.opens.Load() < pads+1 && time.Now().Before(deadline) {
		// 稍等再看，不空转
		time.Sleep(5 * time.Millisecond)
	}
	// 再留一点时间，让各台都排进同一路核对
	time.Sleep(50 * time.Millisecond)
	// 放开门，让核对那一路读下去
	close(second.gate)
	// 等所有台查完
	wg.Wait()
	// 每台都要拿到完整副本
	for i := 0; i < pads; i++ {
		// 有一台没拿到就失败
		if errs[i] != nil || results[i] == nil || results[i].Version != 51 {
			// 并发首查结果不对就停掉本例
			t.Fatalf("pad %d %+v %v", i, results[i], errs[i])
		}
	}
	// 只许整包核一遍，各台只各打开一次问长度
	if second.reads.Load() != int64(len(apk)) || second.opens.Load() != pads+1 || second.gets.Load() != 0 {
		// 核了多遍就是没有单飞
		t.Fatalf("verify reads=%d opens=%d gets=%d", second.reads.Load(), second.opens.Load(), second.gets.Load())
	}

	// 同长度但内容不同的坏对象，长度比不出来，必须重核
	bad := append([]byte(nil), apk...)
	// 改掉第一个字节，摘要就对不上
	bad[0] ^= 0xff
	// 坏对象放在同一个键下
	tampered := blob.NewMemory()
	// 写入坏对象失败就停
	if err := tampered.Put(ctx, key, bad); err != nil {
		// 写入失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 换上带坏对象的存储
	fac.Updates.SetBlobs(tampered)
	// 换存储后必须判为不完整，不能沿用旧核对
	if row, err := fac.PadClientSoftware(ctx, tok); err != nil || row != nil {
		// 坏对象还被当完整就失败
		t.Fatalf("tampered meta %+v %v", row, err)
	}
	// 坏对象不能下载出去
	if _, err := fac.PullPadClientSoftware(ctx, tok, 51, false); !errors.Is(err, domain.ErrIntegrity) {
		// 坏对象还能下就失败
		t.Fatalf("tampered pull %v", err)
	}
	// 再换成空存储，对象丢了
	fac.Updates.SetBlobs(blob.NewMemory())
	// 对象丢了也判为不完整
	if row, err := fac.PadClientSoftware(ctx, tok); err != nil || row != nil {
		// 空存储还被当完整就失败
		t.Fatalf("empty meta %+v %v", row, err)
	}
	// 对象丢了下载报完整性失败
	if _, err := fac.PullPadClientSoftware(ctx, tok, 51, false); !errors.Is(err, domain.ErrIntegrity) {
		// 丢了对象还能下就失败
		t.Fatalf("empty pull %v", err)
	}
}
