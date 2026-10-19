// 验收同一厂级源并发升平台级：只留一条平台级，各路都成功并指向同一条。
package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/contentcrypt"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

// 同一快照几路同时升档，撞唯一索引的那路回读已有行按摘要跳过，库里只一条带源平台级。
func TestPromoteFromSnapshotConcurrentSameSource(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 取出底层连接池，才能调空闲连接数。
	sqlDB, err := h.DB.DB()
	// 拿不到连接池就停，没法调空闲数。
	if err != nil {
		// 不符即停：取连接池失败。
		t.Fatal(err)
	}
	// 同时起跑的路数，够撞出先查后插的窗口即可。
	const ways, rounds = 6, 8
	// 空闲连接留够，各路不用现拨号，查和插才撞得上。
	sqlDB.SetMaxIdleConns(ways * 2)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面升不了档。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面升不了档。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 登记源厂，快照要带上这家的身份。
	fac, err := h.WAN.CreateFactory(ctx, tok, "厂并发升档", "sa-p", "超管P")
	// 登记工厂失败就停，没有源厂可升档。
	if err != nil {
		// 不符即停：登记工厂失败。
		t.Fatal(err)
	}
	// 签发正文租约，快照正文要用它封过站。
	lease, err := h.WAN.IssueContentLease(ctx, fac.Factory.ID)
	// 签不出租约就停，封不了过站信封。
	if err != nil {
		// 不符即停：签发正文租约失败。
		t.Fatal(err)
	}
	// 跑几轮，首轮顺带拨好连接，后几轮真撞在一起。
	for round := range rounds {
		// 每轮换一个厂级源身份，各路都升它。
		srcID := id.New()
		// 准备正文样本，带上轮号免得各轮摘要相同。
		plain := []byte(fmt.Sprintf("race-promote-plain-%d", round))
		// 封成过站信封，同一轮各路共用同一份快照。
		env, err := contentcrypt.Seal(lease.Key, plain, contentcrypt.TransitAAD(fac.Factory.ID, srcID, 1))
		// 封不上就停，没有快照可升。
		if err != nil {
			// 不符即停：封过站信封失败。
			t.Fatal(err)
		}
		// 组好同一份快照，各路原样送进升档。
		snap := global.AssetSnapshot{
			SourceID: srcID, SourceRevision: 1, SourceFactoryID: fac.Factory.ID,
			Kind: global.KindProcess, Name: fmt.Sprintf("并发升档焊%d", round), Content: env, Digest: digest.Sum(plain),
			Copyable: true, Status: global.AssetAvailable,
		}
		// 发令枪，各路就位后一起开跑，尽量叠在一起。
		gate := make(chan struct{})
		// 等各路升档都回来。
		var wg sync.WaitGroup
		// 每路拿到的平台级身份各占一格，不用加锁。
		ids := make([]uuid.UUID, ways)
		// 每路错误各占一格，不用加锁。
		errs := make([]error, ways)
		// 每路一个协程，同时升同一个源。
		for w := range ways {
			// 登记一路升档。
			wg.Add(1)
			// 并发升档，模拟管理员连点或两处同时拉升档。
			go func() {
				// 升档回来就记一路完成。
				defer wg.Done()
				// 等发令枪，让各路尽量同时查同时插。
				<-gate
				// 升同一份快照，撞索引的那路应回读已有行。
				got, err := h.WAN.PromoteFromSnapshot(ctx, tok, snap)
				// 记下这一路的身份和错误。
				ids[w], errs[w] = got.ID, err
			}()
		}
		// 各路就位后一起放行。
		close(gate)
		// 等全部升档回来。
		wg.Wait()
		// 逐路核对，各路都要成功并指向同一条平台级。
		for w := range ways {
			// 有一路失败说明撞索引后没回读已有行；身份不同说明同源升出两条。
			if errs[w] != nil || ids[w] != ids[0] {
				// 不符即停：带上轮号和这一路的结果。
				t.Fatalf("round %d way %d: id %s want %s err %v", round, w, ids[w], ids[0], errs[w])
			}
		}
		// 准备承接数出来的条数。
		var n int64
		// 数库里带这个源的平台级，必须只有一条。
		if err := h.DB.Raw("SELECT COUNT(*) FROM assets WHERE source_id = ?", srcID).Scan(&n).Error; err != nil || n != 1 {
			// 不符即停：同一源留下了多条平台级。
			t.Fatalf("round %d platform rows for source %d %v", round, n, err)
		}
	}
}
