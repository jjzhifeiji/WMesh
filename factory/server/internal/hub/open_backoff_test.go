package hub

import (
	"context"
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/migrate"
	"wmesh/factory/internal/platform/provision"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/service"
	"wmesh/factory/migrations"
)

// migrateLockKey 与 migrate 包的迁移锁同键，测试另开连接占住它来卡住迁移。
const migrateLockKey int64 = 0x574d657368

// connectFactoryDB 建出这家厂的空库并直连上去，测完关掉直连；不套迁移。
func connectFactoryDB(t *testing.T, h *Hub, fid uuid.UUID) *gorm.DB {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 按工厂身份算出库名。
	name := provision.DBName(fid)
	// 建不出厂库就停测。
	if err := provision.Ensure(h.admin, name); err != nil {
		// 把建库错误打出来并停住。
		t.Fatalf("create db: %v", err)
	}
	// 拼出这家厂库的连接串。
	dsn, err := provision.SwapDB(testpg.AdminDSN(), name)
	// 拼不出连接串就停测。
	if err != nil {
		// 把拼串错误打出来并停住。
		t.Fatalf("dsn: %v", err)
	}
	// 直连这家厂库，日志关掉免得刷屏。
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 连不上就停测。
	if err != nil {
		// 把连库错误打出来并停住。
		t.Fatalf("open: %v", err)
	}
	// 测完关掉这条直连，删库前不占着。
	t.Cleanup(func() {
		// 取出底层连接池，能取到才关。
		if sqlDB, err := db.DB(); err == nil {
			// 关掉直连池。
			_ = sqlDB.Close()
		}
	})
	return db
}

// breakFactoryDB 建出这家厂的库并先占掉 people 表，第一支迁移必撞表已存在；交回连这库的句柄好修。
func breakFactoryDB(t *testing.T, h *Hub, fid uuid.UUID) *gorm.DB {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 建库并直连上去。
	db := connectFactoryDB(t, h, fid)
	// 先占掉 people 表，0001 建表时必失败。
	if err := db.Exec("CREATE TABLE people (x INT)").Error; err != nil {
		// 占不了表就停测，前提不成立。
		t.Fatalf("break db: %v", err)
	}
	return db
}

// latestMigration 算出嵌进程序的最大迁移版本，迁完厂库应到这一版。
func latestMigration(t *testing.T) int64 {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 列出全部迁移脚本。
	names, err := fs.Glob(migrations.FS, "*.sql")
	// 列不出就停测。
	if err != nil {
		// 把列脚本的错误打出来并停住。
		t.Fatal(err)
	}
	// 准备记最大版本。
	var top int64
	// 逐个取文件名开头数字。
	for _, name := range names {
		// 取下划线前的数字当版本。
		v, err := strconv.ParseInt(name[:strings.IndexByte(name, '_')], 10, 64)
		// 文件名开头不是数字就停测。
		if err != nil {
			// 把坏文件名打出来并停住。
			t.Fatalf("name %s: %v", name, err)
		}
		// 更大就换上。
		if v > top {
			// 记下更大的版本。
			top = v
		}
	}
	return top
}

// 厂库迁移失败只挡这一家：退避期内不重开，30 秒起翻倍封顶 10 分钟，到点再试，修好后打开并清掉记录。
func TestOpenBackoff(t *testing.T) {
	// 连上测试库并建枢纽，连不上就停测。
	h, err := New(testpg.AdminDSN())
	// 测试库连不上则停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("hub: %v", err)
	}
	// 坏的那家和好的那家各一个身份。
	bad, good := id.New(), id.New()
	// 测完删掉两家厂库并关枢纽。
	t.Cleanup(func() {
		// 删掉坏的那家。
		_ = h.Drop(bad)
		// 删掉好的那家。
		_ = h.Drop(good)
		// 关掉枢纽，释放测试占用的连接。
		h.Close()
	})
	// 假钟，退避全靠它推进，不真等。
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 枢纽改看假钟。
	h.now = func() time.Time {
		// 交回当前假钟。
		return clock
	}
	// 建出坏库，第一支迁移必失败。
	broken := breakFactoryDB(t, h, bad)
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 连续失败时每轮应等的时长：30 秒起翻倍，封顶 10 分钟。
	waits := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute}
	// 逐轮到点重试，每轮都还失败。
	for i, want := range waits {
		// 到点后访问这家，会真去重开并再失败。
		_, err := h.Service(ctx, bad)
		// 准备接迁移错误，里面带着失败的文件名。
		var me *migrate.Error
		// 不是第一支迁移失败就停测。
		if !errors.As(err, &me) || me.File != "0001_init.up.sql" {
			// 把这一轮拿到的错误打出来。
			t.Fatalf("round %d: want 0001 failure, got %v", i, err)
		}
		// 探活该列出这家和下次重试时间。
		st := h.SchemaStatus(ctx)
		// 只该有这一家在失败，原因、文件和重试点都要对。
		if len(st.Failing) != 1 || st.Failing[0].FactoryID != bad || st.Failing[0].Reason != "migrate" || st.Failing[0].File != "0001_init.up.sql" || !st.Failing[0].RetryAt.Equal(clock.Add(want)) || !st.Failing[0].FailedAt.Equal(clock) {
			// 把这一轮的探活结果打出来。
			t.Fatalf("round %d: failing %+v, want retry in %v", i, st.Failing, want)
		}
		// 第一轮顺带看别的厂照常能打开。
		if i == 0 {
			// 好的那家建库迁移都该成功。
			if _, err := h.ensure(good); err != nil {
				// 别的厂被拖累就停测。
				t.Fatalf("other factory blocked: %v", err)
			}
		}
		// 退到重试点前一秒。
		clock = clock.Add(want - time.Second)
		// 退避期内再访问，应直接交回上次那个错，不去重开。
		if _, again := h.Service(ctx, bad); again != err {
			// 换了个错说明退避期内又重开了一次。
			t.Fatalf("round %d: reopened inside backoff: %v", i, again)
		}
		// 走到重试点，下一轮访问就会真去重开。
		clock = clock.Add(time.Second)
	}
	// 修好坏库：拿掉占位表。
	if err := broken.Exec("DROP TABLE people").Error; err != nil {
		// 修不了就停测。
		t.Fatalf("fix db: %v", err)
	}
	// 再把钟往回拨到退避期内，修好了也要等到点。
	clock = clock.Add(-time.Second)
	// 退避期内仍交回上次的错。
	if _, err := h.Service(ctx, bad); err == nil {
		// 退避期内就重开了，退避没生效。
		t.Fatal("reopened inside backoff after fix")
	}
	// 走到重试点。
	clock = clock.Add(time.Second)
	// 到点后重开，这回迁移该成功。
	if _, err := h.Service(ctx, bad); err != nil {
		// 修好了还打不开就停测。
		t.Fatalf("reopen after fix: %v", err)
	}
	// 打开成功后探活不再列失败，两家都报到最新版本。
	st := h.SchemaStatus(ctx)
	// 失败记录该清空。
	if len(st.Failing) != 0 {
		// 失败记录没清就停测。
		t.Fatalf("failing after success %+v", st.Failing)
	}
	// 算出最新版本好对照。
	latest := latestMigration(t)
	// 准备按身份查版本。
	versions := map[uuid.UUID]int64{}
	// 逐家收下版本。
	for _, f := range st.Factories {
		// 记下这家的版本。
		versions[f.FactoryID] = f.Version
	}
	// 两家都该迁到最新。
	if versions[bad] != latest || versions[good] != latest {
		// 把各家版本打出来便于对照。
		t.Fatalf("versions %+v, want %d", st.Factories, latest)
	}
}

// waitLockWaiters 等到这家厂库上有人排队等迁移锁，交回排队的路数；限时内等不到就停测。
func waitLockWaiters(t *testing.T, db *gorm.DB) int64 {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 迁移刚起步，给足时间走到等锁那一步。
	deadline := time.Now().Add(30 * time.Second)
	// 到点前反复查。
	for time.Now().Before(deadline) {
		// 准备接排队的路数。
		var n int64
		// 只数本库上没拿到的咨询锁，别的库不算。
		err := db.Raw(`SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted
			AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&n).Error
		// 查不了就停测。
		if err != nil {
			// 把查锁错误打出来并停住。
			t.Fatalf("pg_locks: %v", err)
		}
		// 已有人在等就交回。
		if n > 0 {
			return n
		}
		// 还没走到等锁，稍等再查。
		time.Sleep(20 * time.Millisecond)
	}
	// 限时内没人等锁，前提不成立。
	t.Fatal("no migration waiting on advisory lock")
	return 0
}

// 一家厂迁移卡在迁移锁上只挡它自己：别的厂照常打开，探活及时返回；同一家的并发打开合并成一路。
func TestSlowOpenOnlyBlocksItself(t *testing.T) {
	// 连上测试库并建枢纽，连不上就停测。
	h, err := New(testpg.AdminDSN())
	// 测试库连不上则停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("hub: %v", err)
	}
	// 卡住的那家和照常打开的那家各一个身份。
	slow, good := id.New(), id.New()
	// 测完删掉两家厂库并关枢纽。
	t.Cleanup(func() {
		// 删掉卡住的那家。
		_ = h.Drop(slow)
		// 删掉好的那家。
		_ = h.Drop(good)
		// 关掉枢纽，释放测试占用的连接。
		h.Close()
	})
	// 建出卡住那家的空库并直连上去。
	locked := connectFactoryDB(t, h, slow)
	// 取出底层连接池，要钉一条连接拿会话锁。
	sqlDB, err := locked.DB()
	// 取不出连接池就停测。
	if err != nil {
		// 把取池错误打出来并停住。
		t.Fatalf("pool: %v", err)
	}
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 钉住一条连接，会话锁跟着这条连接走。
	conn, err := sqlDB.Conn(ctx)
	// 钉不住就停测。
	if err != nil {
		// 把取连接错误打出来并停住。
		t.Fatalf("conn: %v", err)
	}
	// 占住迁移锁，模拟别的进程正迁这家库。
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrateLockKey); err != nil {
		// 占不到锁就停测，前提不成立。
		t.Fatalf("lock: %v", err)
	}
	// 记下锁是否已放，免得清理时重复放。
	released := false
	// 放锁并还回连接；连接还回池前必须先放锁。
	release := func() {
		// 已放过就不再放。
		if released {
			return
		}
		// 记下已放。
		released = true
		// 放掉会话锁，卡住的迁移随之继续。
		_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrateLockKey)
		// 还回钉住的连接。
		_ = conn.Close()
	}
	// 中途停测也要放锁，免得卡住的协程等到删库。
	t.Cleanup(release)
	// 一路打开的结果。
	type opened struct {
		svc *service.Service // 打开得到的服务
		err error            // 打开失败的原错误
	}
	// 两路结果都收进来。
	results := make(chan opened, 2)
	// 两路同时打开卡住的那家。
	for range 2 {
		// 这一路会卡在迁移锁上，结果送回。
		go func() {
			// 库已建好，走打开已有厂库的路。
			svc, err := h.Service(ctx, slow)
			// 成败都送回。
			results <- opened{svc: svc, err: err}
		}()
	}
	// 等到有人卡在迁移锁上，此时这家正在打开。
	waitLockWaiters(t, locked)
	// 好的那家另起一路打开，结果送回。
	goodDone := make(chan error, 1)
	// 打开好的那家，不该被卡住的那家挡住。
	go func() {
		// 建库迁移都走一遍。
		_, err := h.ensure(good)
		// 结果送回。
		goodDone <- err
	}()
	// 等好的那家打开，限时内打不开就是被挡住了。
	select {
	// 打开有结果了。
	case err := <-goodDone:
		// 打开失败就停测。
		if err != nil {
			// 把好的那家的错误打出来。
			t.Fatalf("open other factory: %v", err)
		}
	// 一分钟还没好，说明被卡住的那家拖住了。
	case <-time.After(time.Minute):
		// 报出被挡住。
		t.Fatal("other factory blocked by slow open")
	}
	// 同一家只该有一路在等锁，另一路合并等它。
	if n := waitLockWaiters(t, locked); n != 1 {
		// 两路各自去迁说明没合并。
		t.Fatalf("lock waiters %d, want 1", n)
	}
	// 探活的结果送回。
	statusDone := make(chan SchemaState, 1)
	// 照探活的顺序先探维护库，再看各厂结构。
	go func() {
		// 探活限时三秒，与容器健康检查一致。
		sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		// 用完就释放限时。
		defer cancel()
		// 维护库探活，失败也照样往下看结构。
		_ = h.Ping(sctx)
		// 看各厂结构并送回。
		statusDone <- h.SchemaStatus(sctx)
	}()
	// 等探活结果，限时内回不来就是被挡住了。
	select {
	// 探活回来了。
	case st := <-statusDone:
		// 只该列出好的那家，卡住的还没打开也没失败。
		if len(st.Factories) != 1 || st.Factories[0].FactoryID != good || st.Factories[0].Version != latestMigration(t) || len(st.Failing) != 0 {
			// 把探活结果打出来便于对照。
			t.Fatalf("schema status %+v", st)
		}
	// 三秒没回来，探活会判失败。
	case <-time.After(3 * time.Second):
		// 报出探活被挡住。
		t.Fatal("schema status blocked by slow open")
	}
	// 放锁，卡住的那家接着迁完。
	release()
	// 第一路拿到的服务，第二路应是同一份。
	var first *service.Service
	// 收齐两路结果。
	for range 2 {
		// 等这一路打开。
		select {
		// 这一路有结果了。
		case r := <-results:
			// 放锁后还打不开就停测。
			if r.err != nil {
				// 把卡住那家的错误打出来。
				t.Fatalf("open slow factory: %v", r.err)
			}
			// 第一路记下，交给第二路比对。
			if first == nil {
				// 记下第一路的服务。
				first = r.svc
				continue
			}
			// 第二路必须拿到同一份服务。
			if r.svc != first {
				// 同一家开出两份服务就停测。
				t.Fatal("two services for one factory")
			}
		// 一分钟还没迁完就停测。
		case <-time.After(time.Minute):
			// 报出放锁后仍没打开。
			t.Fatal("slow factory never opened after unlock")
		}
	}
}
