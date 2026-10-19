// 查本厂副本是否完整时，存储瞬时故障要交回错误，只有对象缺失或摘要不符才重拉。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/blob"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// errOSSFlaky 是模拟对象存储一时连不上的错误，不是找不到。
var errOSSFlaky = errors.New("oss flaky")

// flakyBlobs 包住内存存根：按开关让打开或读流报瞬时故障。
type flakyBlobs struct {
	*blob.Memory      // 真正存字节的内存存根
	openFail     bool // 为真时打开对象一律报瞬时故障
	readFail     bool // 为真时打开照常，读流报瞬时故障
}

// Open 按开关报瞬时故障，或交回读流会断的对象。
func (b *flakyBlobs) Open(ctx context.Context, key string) (blob.Object, error) {
	// 开了打开故障就直接报错，模拟存储连不上。
	if b.openFail {
		return nil, errOSSFlaky
	}
	// 交给内存存根真正打开，没有该键照常报找不到。
	obj, err := b.Memory.Open(ctx, key)
	// 打不开就原样交回。
	if err != nil {
		return nil, err
	}
	// 开了读流故障就包一层，读的时候报错。
	if b.readFail {
		return flakyObject{Object: obj}, nil
	}
	return obj, nil
}

// flakyObject 是读流必断的对象，长度和关闭照常。
type flakyObject struct {
	blob.Object // 长度、定位和关闭交给内存流
}

// Read 一律报瞬时故障，模拟读到一半连接断了。
func (flakyObject) Read([]byte) (int, error) {
	// 交回瞬时故障，核摘要时会读到它。
	return 0, errOSSFlaky
}

// 存储打开或读流瞬时出错时交回错误不重拉；对象缺失或摘要不符才重拉补回。
func TestEnsureSoftwareReplicaBlobFaults(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	_, fac, err := h.Provision(ctx, "sa-flaky", "超管")
	// 建厂失败就停，避免带着错误继续验
	if err != nil {
		// 建厂失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 放行的源：拉包立刻交回包字节，并记拉包次数。
	src := &gateSource{body: []byte("client-apk-3"), entered: make(chan struct{}, 1), release: make(chan struct{})}
	// 先放行，拉包不卡。
	close(src.release)
	// 换上软件源，拉包都走它
	fac.Updates.SetSoftwareSource(src)
	// 自备内存存根，后面包上故障层还用同一份字节。
	mem := blob.NewMemory()
	// 换上自备的存根，收下的包落在这里。
	fac.Updates.SetBlobs(mem)
	// 第一次要客户端包 v3，应拉回并收下。
	if err := fac.Updates.EnsureSoftware(ctx, factory.SoftwareClientAPK, 3); err != nil {
		// 第一次收下失败就停掉本例
		t.Fatal(err)
	}
	// 收下这一份只该拉过一次。
	if n := src.pulls.Load(); n != 1 {
		// 拉包次数不对就停掉本例
		t.Fatalf("first pulls %d", n)
	}
	// 打开对象报瞬时故障，同一份字节还在。
	fac.Updates.SetBlobs(&flakyBlobs{Memory: mem, openFail: true})
	// 打开失败应交回原因，不当缺失去重下。
	if err := fac.Updates.EnsureSoftware(ctx, factory.SoftwareClientAPK, 3); !errors.Is(err, errOSSFlaky) {
		// 没交回瞬时故障就停掉本例
		t.Fatalf("open fault %v", err)
	}
	// 读流报瞬时故障，打开能问到长度。
	fac.Updates.SetBlobs(&flakyBlobs{Memory: mem, readFail: true})
	// 核摘要读断应交回原因，不当摘要不符去重下。
	if err := fac.Updates.EnsureSoftware(ctx, factory.SoftwareClientAPK, 3); !errors.Is(err, errOSSFlaky) {
		// 没交回读流故障就停掉本例
		t.Fatalf("read fault %v", err)
	}
	// 两次瞬时故障都不该再去 WAN 拉包。
	if n := src.pulls.Load(); n != 1 {
		// 瞬时故障触发了重下就停掉本例
		t.Fatalf("pulls after faults %d", n)
	}
	// 换成空存根，对象确实丢了。
	fac.Updates.SetBlobs(blob.NewMemory())
	// 对象缺失应重拉补回，交回成功。
	if err := fac.Updates.EnsureSoftware(ctx, factory.SoftwareClientAPK, 3); err != nil {
		// 缺失没能补回就停掉本例
		t.Fatalf("missing %v", err)
	}
	// 对象缺失该多拉一次。
	if n := src.pulls.Load(); n != 2 {
		// 缺失没去重拉就停掉本例
		t.Fatalf("pulls after missing %d", n)
	}
	// 对象键和收包时一致，坏对象放在同一处。
	key := store.SoftwareObjectKey(factory.SoftwareClientAPK, 3)
	// 拷一份包字节再改坏，长度不变摘要对不上。
	bad := append([]byte(nil), src.body...)
	// 改掉第一个字节，摘要就对不上。
	bad[0] ^= 0xff
	// 新存根里放坏对象。
	tampered := blob.NewMemory()
	// 写入坏对象失败就停
	if err := tampered.Put(ctx, key, bad); err != nil {
		// 写入失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 换上带坏对象的存根，旧核对记录随之作废。
	fac.Updates.SetBlobs(tampered)
	// 摘要不符应重拉补回，交回成功。
	if err := fac.Updates.EnsureSoftware(ctx, factory.SoftwareClientAPK, 3); err != nil {
		// 坏对象没能补回就停掉本例
		t.Fatalf("tampered %v", err)
	}
	// 摘要不符该再多拉一次。
	if n := src.pulls.Load(); n != 3 {
		// 摘要不符没去重拉就停掉本例
		t.Fatalf("pulls after tampered %d", n)
	}
	// 读出补回后的对象，应是原包字节。
	got, err := tampered.Get(ctx, key)
	// 补回的对象不对就停
	if err != nil || !bytes.Equal(got, src.body) {
		// 坏对象没被原包覆盖就停掉本例
		t.Fatalf("restored %q %v", got, err)
	}
}
