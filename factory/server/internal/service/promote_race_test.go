// 验收同一个人级源并发升厂级：只留一条厂级，各路都成功并指向同一条。
package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 同一个人级几路同时升厂级，撞唯一索引的那路回读已有行按摘要跳过，库里只一条带源厂级。
func TestPromoteToFactoryConcurrentSameSource(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 连上维护库，用来另开一座厂库。
	admin := testpg.Open(t)
	// 另开一座空库，自己拿着连接池好调空闲数。
	_, dsn := testpg.CreateDB(t, admin, "wmesh_fac")
	// 迁好库，服务和用例共用这一池。
	db := testpg.OpenMigrated(t, dsn)
	// 取出底层连接池，才能调空闲连接数。
	sqlDB, err := db.DB()
	// 拿不到连接池就停，没法调空闲数。
	if err != nil {
		// 把取连接池的错误打出来并停住。
		t.Fatal(err)
	}
	// 同时起跑的路数和轮数，够撞出先查后插的窗口即可。
	const ways, rounds = 6, 8
	// 空闲连接留够，各路不用现拨号，查和插才撞得上。
	sqlDB.SetMaxIdleConns(ways * 2)
	// 按一家新厂装上厂服务。
	fac := factory.NewService(store.Open(db, id.New()))
	// 夹具不连 WAN，自签一把租约才能写厂库正文。
	if err := fac.Store().GrantLocalLease(ctx); err != nil {
		// 把签租约的错误打出来并停住。
		t.Fatal(err)
	}
	// 写本厂短码，没有短码就发不了号。
	if err := fac.Store().PutFactoryShortCode(ctx, "F01"); err != nil {
		// 把写短码的错误打出来并停住。
		t.Fatal(err)
	}
	// 写入首个超管并拿到激活码。
	_, act, err := fac.BootstrapInitial(ctx, "sa-a", "超管A")
	// 写首个超管失败就停，后面没有人能登录。
	if err != nil {
		// 把建超管的错误打出来并停住。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa-a", act, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	sa := mustLogin(t, ctx, fac, "sa-a", "sa-pass")
	// 建人并授好角色，升厂级要有制作权。
	pe := mustCreateRole(t, ctx, fac, sa, "pe-a", "pe-pass", factory.RoleOperator, factory.ScopeFactory, nil)
	// 跑几轮，首轮顺带拨好连接，后几轮真撞在一起。
	for round := range rounds {
		// 每轮新建一份个人级工艺，各路都升它。
		proc, err := fac.CreatePersonalProcess(ctx, pe.tok, factory.WorkContext{Direct: true}, fmt.Sprintf("并发升档工艺%d", round), []byte(`{"name":"race"}`))
		// 建工艺失败就停，否则后面没有源可升。
		if err != nil {
			// 把建工艺的错误打出来并停住，避免继续往下验。
			t.Fatal(err)
		}
		// 发令枪，各路就位后一起开跑，尽量叠在一起。
		gate := make(chan struct{})
		// 等各路升档都回来。
		var wg sync.WaitGroup
		// 每路拿到的厂级身份各占一格，不用加锁。
		ids := make([]uuid.UUID, ways)
		// 每路错误各占一格，不用加锁。
		errs := make([]error, ways)
		// 每路一个协程，同时升同一个源。
		for w := range ways {
			// 登记一路升档。
			wg.Add(1)
			// 并发升档，模拟连点或两个窗口同时升。
			go func() {
				// 升档回来就记一路完成。
				defer wg.Done()
				// 等发令枪，让各路尽量同时查同时插。
				<-gate
				// 升同一份个人级，撞索引的那路应回读已有行。
				got, err := fac.PromoteToFactory(ctx, pe.tok, proc.ID)
				// 记下这一路的身份和错误。
				ids[w], errs[w] = got.ID, err
			}()
		}
		// 各路就位后一起放行。
		close(gate)
		// 等全部升档回来。
		wg.Wait()
		// 逐路核对，各路都要成功并指向同一条厂级。
		for w := range ways {
			// 有一路失败说明撞索引后没回读已有行；身份不同说明同源升出两条。
			if errs[w] != nil || ids[w] != ids[0] {
				// 带上轮号和这一路的结果停住。
				t.Fatalf("round %d way %d: id %s want %s err %v", round, w, ids[w], ids[0], errs[w])
			}
		}
		// 准备承接数出来的条数。
		var n int64
		// 数库里带这个源的厂级，必须只有一条。
		if err := db.Raw("SELECT COUNT(*) FROM assets WHERE source_id = ?", proc.ID).Scan(&n).Error; err != nil || n != 1 {
			// 同一源留下了多条厂级就停。
			t.Fatalf("round %d factory rows for source %d %v", round, n, err)
		}
	}
}
