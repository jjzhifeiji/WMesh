// 改绑和启停在行锁下排队后，作废通知和踢会话都按锁内读到的原值来定。
package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

// 并发安全的记录总线，几路同时推送也不丢条。
type syncBus struct {
	// 守住下面几格，几路协程会同时写。
	mu sync.Mutex
	// 收到的指令，按投递顺序记。
	sent []busSent
	// 被踢掉会话的工厂，按踢的顺序记。
	drops []uuid.UUID
}

// 一条投递：投给哪一厂、什么指令。
type busSent struct {
	// 收到这条指令的工厂。
	factory uuid.UUID
	// 解开后的指令，用来核对种类和修订。
	cmd global.Cmd
}

// Publish 解开报文记下投给哪一厂，解不开就忽略。
func (b *syncBus) Publish(factoryID uuid.UUID, payload []byte) error {
	// 先留空，解开成功再记。
	var cmd global.Cmd
	// 解不开的报文不记，断言只看认得的指令。
	if json.Unmarshal(payload, &cmd) != nil {
		return nil
	}
	// 上锁再记，几路同时推送不丢条。
	b.mu.Lock()
	// 记完就放锁，别挡住别的协程。
	defer b.mu.Unlock()
	// 按投递顺序记下这一条。
	b.sent = append(b.sent, busSent{factory: factoryID, cmd: cmd})
	return nil
}

// Call 测试不走问询应答，一律当成厂通道离线。
func (b *syncBus) Call(context.Context, uuid.UUID, string, []byte) ([]byte, error) {
	return nil, domain.ErrFactoryOffline
}

// Drop 不拆通道，只记下踢了哪一厂供断言。
func (b *syncBus) Drop(factoryID uuid.UUID) {
	// 上锁再记，几路同时踢会话不丢条。
	b.mu.Lock()
	// 记完就放锁，别挡住别的协程。
	defer b.mu.Unlock()
	// 按顺序记下被踢的厂。
	b.drops = append(b.drops, factoryID)
}

// byType 按投递顺序取出某一类指令的副本。
func (b *syncBus) byType(typ string) []busSent {
	// 上锁再读，免得和推送撞上。
	b.mu.Lock()
	// 读完就放锁。
	defer b.mu.Unlock()
	// 准备接住这一类指令。
	var out []busSent
	// 逐条筛出指定种类。
	for _, s := range b.sent {
		// 种类对得上才收下。
		if s.cmd.Typ == typ {
			// 收下这一条。
			out = append(out, s)
		}
	}
	return out
}

// dropsCopy 取出被踢会话的工厂副本，读时不和写撞上。
func (b *syncBus) dropsCopy() []uuid.UUID {
	// 上锁再读，免得和踢会话撞上。
	b.mu.Lock()
	// 读完就放锁。
	defer b.mu.Unlock()
	// 拷一份交出去，调用方随便用。
	return append([]uuid.UUID(nil), b.drops...)
}

// holdRowLock 另开一笔事务行锁住某表一行，被测操作读锁时就得排队；交回放锁函数。
func holdRowLock(t *testing.T, h *Harness, table string, rowID uuid.UUID) func() {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 单开一笔事务，占住一条连接一直不放。
	tx := h.DB.Begin()
	// 事务开不出来就停，后面没法挡住被测操作。
	if tx.Error != nil {
		// 不符即停：开事务失败。
		t.Fatal(tx.Error)
	}
	// 行锁住这一行，表名只来自用例常量。
	if err := tx.Exec("SELECT 1 FROM "+table+" WHERE id = ? FOR UPDATE", rowID).Error; err != nil {
		// 锁不上就回滚，免得连接挂着。
		tx.Rollback()
		// 不符即停：行锁失败。
		t.Fatal(err)
	}
	// 放锁只做一次，用例和收尾都可能调。
	var once sync.Once
	// 放锁函数：回滚这笔事务，排队的操作接着跑。
	release := func() {
		// 只回滚一次，重复调用不报错。
		once.Do(func() {
			// 什么都没改，回滚就放开行锁。
			tx.Rollback()
		})
	}
	// 用例中途失败也要放锁，免得被测协程一直挂着。
	t.Cleanup(release)
	return release
}

// waitLockWaiters 等本库里排队等锁的会话凑够 n 个，超时就停测。
func waitLockWaiters(t *testing.T, h *Harness, n int) {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 最多等这么久，凑不够说明没撞上锁。
	deadline := time.Now().Add(10 * time.Second)
	// 反复数，直到凑够或超时。
	for {
		// 准备接住等锁的会话数。
		var got int64
		// 只数本测试库里在等锁的会话，别的库的测试不算。
		if err := h.DB.Raw("SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'").Scan(&got).Error; err != nil {
			// 不符即停：数等锁会话失败。
			t.Fatal(err)
		}
		// 凑够了就交回，后面可以放锁或起下一路。
		if got >= int64(n) {
			return
		}
		// 超时还没凑够就停，免得测试挂死。
		if time.Now().After(deadline) {
			// 不符即停：等锁会话不够。
			t.Fatalf("lock waiters %d, want %d", got, n)
		}
		// 歇一下再数，别把库问爆。
		time.Sleep(5 * time.Millisecond)
	}
}

// 验同一设备两路并发改到不同厂：各自的作废通知发给锁内读到的原厂，没有哪家漏收。
func TestRebindConcurrentVoidsLockedOldFactory(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立超管并登录，后面都用这张票。
	tok := retiredClientLogin(t, h)
	// 三家厂：起点厂一，两路分别改到厂二、厂三。
	facs := make([]uuid.UUID, 3)
	// 逐家登记，登录名带序号免撞。
	for i := range facs {
		// 登记一家厂，作为起点或改绑目标。
		f, err := h.WAN.CreateFactory(ctx, tok, fmt.Sprintf("厂%d", i+1), fmt.Sprintf("sa-%d", i+1), "超管")
		// 登记失败就停，后面没有目标厂。
		if err != nil {
			// 不符即停：登记工厂失败。
			t.Fatal(err)
		}
		// 记下这家厂的身份。
		facs[i] = f.Factory.ID
	}
	// 在厂一登记一台，绑定修订从一起。
	c, err := h.WAN.RegisterClient(ctx, tok, "焊机", id.New(), facs[0], nil, "ARM-RACE")
	// 登记失败或没分到厂一就停，没有改绑起点。
	if err != nil || c.FactoryID == nil || *c.FactoryID != facs[0] || c.BindingRevision != 1 {
		// 不符即停：在厂一登记设备失败。
		t.Fatalf("register: %+v %v", c, err)
	}
	// 准备并发安全的记录总线，核对作废发给了谁。
	bus := &syncBus{}
	// 换上记录总线，登记那一笔不算在内。
	h.WAN.SetBus(bus)
	// 先行锁住这台设备，两路改绑都得在锁上排队。
	release := holdRowLock(t, h, "clients", c.ID)
	// 两路改绑的结果，按路号收。
	outs := make([]global.Client, 2)
	// 两路改绑的错误，按路号收。
	errs := make([]error, 2)
	// 等两路都跑完再断言。
	var wg sync.WaitGroup
	// 依次起两路，第一路改到厂二，第二路改到厂三。
	for i, to := range facs[1:] {
		// 这一路改到自己的目标厂。
		wg.Go(func() {
			// 改绑并记下结果和错误。
			outs[i], errs[i] = h.WAN.RebindClient(ctx, tok, c.ID, to)
		})
		// 等这一路排上锁再起下一路，先后就定了。
		waitLockWaiters(t, h, i+1)
	}
	// 两路都在排队了，放锁让它们依次改。
	release()
	// 等两路都改完。
	wg.Wait()
	// 逐路核对：都要成，第一路到厂二修订二，第二路到厂三修订三。
	for i, to := range facs[1:] {
		// 没成、归属不对或修订不是排队先后都不对。
		if errs[i] != nil || outs[i].FactoryID == nil || *outs[i].FactoryID != to || outs[i].BindingRevision != int64(i)+2 {
			// 不符即停：改绑结果不符排队先后。
			t.Fatalf("rebind %d: %+v %v", i, outs[i], errs[i])
		}
	}
	// 每个修订该作废的厂：修订二作废厂一，修订三作废厂二。
	wantVoid := map[int64]uuid.UUID{2: facs[0], 3: facs[1]}
	// 取出全部作废通知，逐条核对。
	voids := bus.byType(global.CmdClientVoid)
	// 条数不对就停，多发少发都会让厂端分叉。
	if len(voids) != len(wantVoid) {
		// 不符即停：作废通知条数不对。
		t.Fatalf("voids %+v", voids)
	}
	// 逐条核对作废发给锁内读到的原厂。
	for _, v := range voids {
		// 查这一修订该作废的厂。
		want, ok := wantVoid[v.cmd.BindingRevision]
		// 设备不对、修订意外或投错厂都不对。
		if !ok || v.cmd.ClientID != c.ID.String() || v.factory != want {
			// 不符即停：作废通知投错厂。
			t.Fatalf("void rev %d to %s, want %s (all %+v)", v.cmd.BindingRevision, v.factory, want, voids)
		}
		// 用过就划掉，同一修订不能作废两次。
		delete(wantVoid, v.cmd.BindingRevision)
	}
	// 每个修订该收到绑定的厂：修订二厂二，修订三厂三。
	wantBind := map[int64]uuid.UUID{2: facs[1], 3: facs[2]}
	// 取出全部绑定通知，逐条核对。
	binds := bus.byType(global.CmdClientBind)
	// 条数不对就停，新厂漏收就用不了设备。
	if len(binds) != len(wantBind) {
		// 不符即停：绑定通知条数不对。
		t.Fatalf("binds %+v", binds)
	}
	// 逐条核对绑定发给改到的新厂。
	for _, b := range binds {
		// 查这一修订该收到绑定的厂。
		want, ok := wantBind[b.cmd.BindingRevision]
		// 设备不对、修订意外或投错厂都不对。
		if !ok || b.cmd.ClientID != c.ID.String() || b.factory != want {
			// 不符即停：绑定通知投错厂。
			t.Fatalf("bind rev %d to %s, want %s (all %+v)", b.cmd.BindingRevision, b.factory, want, binds)
		}
		// 用过就划掉，同一修订不能绑两次。
		delete(wantBind, b.cmd.BindingRevision)
	}
}

// 验停用和启用并发、停用先提交时，启用按锁内读到的停用判定，照常踢会话逼厂端重拉。
func TestEnableAfterConcurrentDisableDropsSession(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立超管并登录，后面都用这张票。
	tok := retiredClientLogin(t, h)
	// 登记一家厂，起点是有效。
	f, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记失败或起点不是有效就停。
	if err != nil || f.Factory.Status != global.FactoryActive {
		// 不符即停：登记工厂失败。
		t.Fatalf("create: %+v %v", f, err)
	}
	// 准备并发安全的记录总线，核对踢了哪一厂。
	bus := &syncBus{}
	// 换上记录总线，后面才能核对踢会话。
	h.WAN.SetBus(bus)
	// 先行锁住这家厂，停用和启用都得在锁上排队。
	release := holdRowLock(t, h, "factories", f.Factory.ID)
	// 两路操作：先排停用，再排启用。
	ops := []func() (global.Factory, error){
		// 第一路停用这家厂。
		func() (global.Factory, error) {
			// 停用，读到的原状态是有效。
			return h.WAN.DisableFactory(ctx, tok, f.Factory.ID)
		},
		// 第二路重新启用这家厂。
		func() (global.Factory, error) {
			// 启用，锁内读到的原状态应是停用。
			return h.WAN.EnableFactory(ctx, tok, f.Factory.ID)
		},
	}
	// 两路的结果，按路号收。
	outs := make([]global.Factory, len(ops))
	// 两路的错误，按路号收。
	errs := make([]error, len(ops))
	// 等两路都跑完再断言。
	var wg sync.WaitGroup
	// 依次起两路，排上锁再起下一路。
	for i, op := range ops {
		// 跑这一路并记下结果。
		wg.Go(func() {
			// 改状态并记下结果和错误。
			outs[i], errs[i] = op()
		})
		// 等这一路排上锁再起下一路，先后就定了。
		waitLockWaiters(t, h, i+1)
	}
	// 两路都在排队了，放锁让它们依次改。
	release()
	// 等两路都改完。
	wg.Wait()
	// 停用要先成，修订升到一。
	if errs[0] != nil || outs[0].Status != global.FactoryDisabled || outs[0].LifecycleRevision != 1 {
		// 不符即停：停用结果不符排队先后。
		t.Fatalf("disable: %+v %v", outs[0], errs[0])
	}
	// 启用随后成，修订升到二。
	if errs[1] != nil || outs[1].Status != global.FactoryActive || outs[1].LifecycleRevision != 2 {
		// 不符即停：启用结果不符排队先后。
		t.Fatalf("enable: %+v %v", outs[1], errs[1])
	}
	// 停用转有效必须恰好踢这家厂一次，否则停用期间的撤回补不回来。
	if drops := bus.dropsCopy(); len(drops) != 1 || drops[0] != f.Factory.ID {
		// 不符即停：启用没按锁内原状态踢会话。
		t.Fatalf("drops %+v", drops)
	}
}
