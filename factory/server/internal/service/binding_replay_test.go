// WAN 回连重放绑定：换臂和改名不被冲回，已作废的不被同修订复活，迟到的旧作废不作废新绑定。
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/nodekey"
	factory "wmesh/factory/internal/service"
)

// 换臂场景：随绑定补上 ARM-1，换臂注册成 ARM-2 后重放补号，号仍是 ARM-2 且能登录
func TestWANReplayKeepsSwappedArm(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建一个操作员，换臂后用它在本机登录
	mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 生成焊机密钥，失败则绑定无法开始
	pub, _, err := nodekey.Generate()
	// 生成焊机密钥失败就停，避免带着错误继续验
	if err != nil {
		// 生成焊机密钥失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 新开一个身份，避免和别的绑定撞号
	cid := id.New()
	// WAN 首次推来的分配，带着名录里的旧臂号
	bind := factory.WANBinding{ClientID: cid, Name: "焊机", PublicKey: pub, Revision: 1, DeviceSerial: "ARM-1"}
	// 首次随绑定补上 ARM-1，失败就停
	if err := fac.ApplyWANBinding(ctx, bind); err != nil {
		// 首次落绑定失败就把原因打出并停掉本例
		t.Fatalf("first bind: %v", err)
	}
	// 本厂入口尚不支持改已钉的号，借库层覆盖模拟现场换臂重新注册成 ARM-2
	if _, err := fac.Store().PinDeviceSerial(ctx, cid, "ARM-2", true); err != nil {
		// 换臂登记失败就把原因打出并停掉本例
		t.Fatalf("swap arm: %v", err)
	}
	// 超管顺手改了名，验重放也不冲回名字
	if _, err := fac.RenameClient(ctx, saTok, cid, "一线焊机"); err != nil {
		// 改名失败就把原因打出并停掉本例
		t.Fatalf("rename: %v", err)
	}
	// WAN 回连以同修订重放同一条分配，不能报错
	if err := fac.ApplyWANBinding(ctx, bind); err != nil {
		// 重放报错就把原因打出并停掉本例
		t.Fatalf("replay: %v", err)
	}
	// 读回这台，核对臂号和名字
	row, err := fac.Store().ClientByID(ctx, cid)
	// 读不到、号被冲回 ARM-1 或名字被冲回都算错
	if err != nil || row.DeviceSerial != "ARM-2" || row.Name != "一线焊机" || row.Status != factory.ClientStatusBound {
		// 打出读回结果，便于看是哪一项被冲回
		t.Fatalf("after replay %+v %v", row, err)
	}
	// 用换臂后的 ARM-2 在本机登录，应当成功
	if _, err := fac.LoginOnClient(ctx, cid, "ARM-2", "op", "op-pass"); err != nil {
		// 换臂那台登不上就失败，说明号被冲回了
		t.Fatalf("login arm-2: %v", err)
	}
	// 旧臂号 ARM-1 不该再认这台
	if _, err := fac.LoginOnClient(ctx, cid, "ARM-1", "op", "op-pass"); !errors.Is(err, domain.ErrDeviceSerialMismatch) {
		// 旧号还能登录就失败
		t.Fatalf("login arm-1: %v", err)
	}
}

// 本厂作废的设备不被同修订重推复活；作废指令按修订判定，迟到的旧作废忽略
func TestWANVoidAndReplayRespectRevision(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 读回设备的小工具，读不到就停
	read := func(cid uuid.UUID) factory.Client {
		// 按身份读回这台设备
		c, err := fac.Store().ClientByID(ctx, cid)
		// 读不到说明落库出了错
		if err != nil {
			// 读设备失败就把原因打出并停掉本例
			t.Fatalf("read %s: %v", cid, err)
		}
		return c
	}

	// 本厂超管手工作废的这台，验同修订重推不复活
	local := id.New()
	// WAN 推来的分配，带着臂号
	bind := factory.WANBinding{ClientID: local, Name: "焊机L", Revision: 1, DeviceSerial: "ARM-L"}
	// 首次落绑定失败就停
	if err := fac.ApplyWANBinding(ctx, bind); err != nil {
		// 首次落绑定失败就把原因打出并停掉本例
		t.Fatalf("bind local: %v", err)
	}
	// 本厂超管作废这台，失败就停
	if err := fac.VoidClientBinding(ctx, saTok, local); err != nil {
		// 作废失败就把原因打出并停掉本例
		t.Fatalf("void local: %v", err)
	}
	// WAN 回连以同修订重推，不报错也不复活
	if err := fac.ApplyWANBinding(ctx, bind); err != nil {
		// 重推报错就把原因打出并停掉本例
		t.Fatalf("replay local: %v", err)
	}
	// 读回仍应是作废
	if c := read(local); c.Status != factory.ClientStatusVoid || c.BindingRevision != 1 {
		// 被同修订复活就失败
		t.Fatalf("local revived %+v", c)
	}
	// 作废的这台不能在本机登录
	if _, err := fac.LoginOnClient(ctx, local, "ARM-L", "sa", "sa-pass"); !errors.Is(err, domain.ErrBindingVoid) {
		// 作废设备还能登录就失败
		t.Fatalf("login void: %v", err)
	}
	// WAN 改分带来更高修订，才重新绑定
	bind.Revision = 2
	// 更高修订落绑定失败就停
	if err := fac.ApplyWANBinding(ctx, bind); err != nil {
		// 更高修订落不上就把原因打出并停掉本例
		t.Fatalf("rebind local: %v", err)
	}
	// 读回应恢复有效，修订升到二
	if c := read(local); c.Status != factory.ClientStatusBound || c.BindingRevision != 2 {
		// 更高修订没恢复就失败
		t.Fatalf("local not rebound %+v", c)
	}

	// 改走又改回的这台，验先落新绑定再来迟到的旧作废
	moved := id.New()
	// 起初分到本厂，修订一
	if err := fac.ApplyWANBinding(ctx, factory.WANBinding{ClientID: moved, Name: "焊机M", Revision: 1}); err != nil {
		// 落绑定失败就把原因打出并停掉本例
		t.Fatalf("bind moved: %v", err)
	}
	// 回连索引里已是改回本厂的修订三
	if err := fac.ApplyWANBinding(ctx, factory.WANBinding{ClientID: moved, Name: "焊机M", Revision: 3}); err != nil {
		// 新绑定落不上就把原因打出并停掉本例
		t.Fatalf("bind moved rev3: %v", err)
	}
	// 队列里迟到的修订二作废，应只记日志忽略
	if err := fac.ApplyWANVoid(ctx, moved, 2); err != nil {
		// 迟到作废报错就把原因打出并停掉本例
		t.Fatalf("late void: %v", err)
	}
	// 读回仍应是修订三的有效绑定
	if c := read(moved); c.Status != factory.ClientStatusBound || c.BindingRevision != 3 {
		// 新绑定被迟到的旧作废作废就失败
		t.Fatalf("moved voided by late frame %+v", c)
	}
	// WAN 再次改走，修订四的作废才生效
	if err := fac.ApplyWANVoid(ctx, moved, 4); err != nil {
		// 作废失败就把原因打出并停掉本例
		t.Fatalf("void rev4: %v", err)
	}
	// 读回应已作废
	if c := read(moved); c.Status != factory.ClientStatusVoid {
		// 更高修订的作废没生效就失败
		t.Fatalf("moved not voided %+v", c)
	}
	// 没有这台的作废交回不存在，通道据此当已完成
	if err := fac.ApplyWANVoid(ctx, id.New(), 2); !errors.Is(err, domain.ErrNotFound) {
		// 不存在却没交回不存在就失败
		t.Fatalf("void missing: %v", err)
	}
}
