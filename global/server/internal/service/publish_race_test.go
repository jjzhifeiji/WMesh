// 验收同版本并发上传不同包：只成一份，其余按完整性拒绝，对象互不覆盖。
package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/blob"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	global "wmesh/global/internal/service"
)

// 闸门存根：凑齐几路写入才一起放行，各路都过了查重才去入库。
type gateBlobs struct {
	*blob.Memory               // 字节照常存在内存里
	want         int32         // 凑齐这么多路才开闸
	seen         atomic.Int32  // 已到闸门的写入路数
	open         chan struct{} // 凑齐后关上，等着的写入一起走
}

// Put 等凑齐几路再写；凑不齐也只等一会儿，免得测试挂死。
func (g *gateBlobs) Put(ctx context.Context, key string, body []byte) error {
	// 最后一路到了就开闸。
	if g.seen.Add(1) == g.want {
		// 关上通道，等着的各路一起去写。
		close(g.open)
	}
	// 等开闸、请求断或超时，谁先到走谁。
	select {
	// 开闸了就往下写。
	case <-g.open:
	// 请求断了就不写了。
	case <-ctx.Done():
		// 交回本请求的取消原因。
		return ctx.Err()
	// 凑不齐也不一直等，照常写下去。
	case <-time.After(5 * time.Second):
	}
	// 照常写进内存存根。
	return g.Memory.Put(ctx, key, body)
}

// 同版本几路同时传不同字节：一路成、其余完整性错，库与对象一致，落败方对象已删。
func TestPublishSoftwareSameVersionRace(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 同时上传的路数，各路都过查重后才写对象。
	const ways = 4
	// 内存存根留在手里，事后直接查对象。
	mem := blob.NewMemory()
	// 套上闸门，凑齐各路写入再放行，查重和入库才撞得上。
	h.WAN.SetBlobs(&gateBlobs{Memory: mem, want: ways, open: make(chan struct{})})
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票发布不了。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票发布不了。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 各路的包字节，彼此都不同。
	bodies := make([][]byte, ways)
	// 逐路备一份不同的包字节。
	for i := range ways {
		// 带上路号，摘要各不相同。
		bodies[i] = []byte(fmt.Sprintf("svc-5-way-%d", i))
	}
	// 各路发布的结果，按路号收。
	errs := make([]error, ways)
	// 等各路发布都结束再断言。
	var wg sync.WaitGroup
	// 起各路并发发布同一版本。
	for i := range ways {
		// 登记一路，等全部结束再断言。
		wg.Add(1)
		// 这一路发布自己那份字节。
		go func() {
			// 这一路结束就销账。
			defer wg.Done()
			// 同种类同版本发布，只字节不同。
			_, errs[i] = h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.5.0", 5, bodies[i])
		}()
	}
	// 等各路都结束。
	wg.Wait()
	// 记下成功那一路，还没找到就是负数。
	winner := -1
	// 逐路看结果：只许一路成，其余都按完整性拒绝。
	for i, err := range errs {
		// 按成败分开数。
		switch {
		// 成了的一路记下，第二路也成就是换了包。
		case err == nil:
			// 已有一路成了，同版本出了两份。
			if winner >= 0 {
				// 不符即停：同版本两路都发布成功。
				t.Fatalf("two winners %d %d", winner, i)
			}
			// 记下这一路是赢家。
			winner = i
		// 完整性拒绝是预期的落败。
		case errors.Is(err, domain.ErrIntegrity):
		// 别的错都不该出现。
		default:
			// 不符即停：落败方报错类别不对。
			t.Fatalf("way %d: %v", i, err)
		}
	}
	// 一路都没成就是全被误拒。
	if winner < 0 {
		// 不符即停：没有一路发布成功。
		t.Fatal("no winner")
	}
	// 读库里这一类的发布行。
	rows, err := h.WAN.ListSoftwareReleases(ctx, tok, global.SoftwareFactoryService)
	// 读不到发布行就停，没法核对。
	if err != nil {
		// 不符即停：读发布行失败。
		t.Fatal(err)
	}
	// 库里只该一行，且摘要是赢家那份。
	if len(rows) != 1 || !digest.Match(bodies[winner], rows[0].Digest) {
		// 不符即停：库里发布行不对。
		t.Fatalf("rows %+v", rows)
	}
	// 按库里的键读对象，厂拉的就是它。
	got, err := mem.Get(ctx, rows[0].ObjectKey)
	// 读不到或和库里摘要对不上，厂都拉不下来。
	if err != nil || !digest.Match(got, rows[0].Digest) {
		// 不符即停：对象与库里摘要不一致。
		t.Fatalf("object %q %v", got, err)
	}
	// 数存根里剩几份对象。
	used, objects, err := mem.Usage(ctx)
	// 只该剩赢家一份，落败方写的都删掉了。
	if err != nil || objects != 1 || used != int64(len(bodies[winner])) {
		// 不符即停：落败方对象没删干净。
		t.Fatalf("usage %d %d %v", used, objects, err)
	}
	// 允许审计只该一条，就是赢家那次。
	if n := len(softwareAudit(t, ctx, h.WAN, "publish_software", audit.Allow)); n != 1 {
		// 不符即停：允许审计条数不对。
		t.Fatalf("allow %d", n)
	}
	// 每个落败方都该留一条拒绝审计。
	if n := len(softwareAudit(t, ctx, h.WAN, "publish_software", audit.Deny)); n != ways-1 {
		// 不符即停：拒绝审计条数不对。
		t.Fatalf("deny %d", n)
	}
	// 赢家原样再发仍成功，库和对象对得上才行。
	if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.5.0", 5, bodies[winner]); err != nil {
		// 不符即停：同包再发失败。
		t.Fatal(err)
	}
}
