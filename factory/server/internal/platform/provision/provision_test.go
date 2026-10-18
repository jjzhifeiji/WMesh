// 厂库打开与迁移；迁移失败时不能把连接池留在外面，重试会越积越多。
package provision_test

import (
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/factory/internal/platform/provision"
	"wmesh/factory/internal/platform/testpg"
)

// 迁移失败重试几次后，这个厂库上不能再留着连接。
func TestOpenMigratedClosesPoolOnMigrateError(t *testing.T) {
	// 连上维护库，后面建库和数连接都用它。
	admin := testpg.Open(t)
	// 建一个测完就删的临时库。
	name, dsn := testpg.CreateDB(t, admin, "wmesh_prov")
	// 预先建好同名表，让初始迁移必定失败。
	seedConflict(t, dsn)
	// 连着重试几次，模拟通道和请求反复打开同一个坏库。
	for i := 0; i < 3; i++ {
		// 打开并迁移，这里应当失败，失败时不该交出库。
		db, err := provision.OpenMigrated(dsn)
		// 迁移居然成功说明夹具没起作用，用例不成立。
		if err == nil || db != nil {
			// 报出第几次和拿到的结果，方便定位。
			t.Fatalf("open migrated #%d want error, got db=%v err=%v", i+1, db, err)
		}
	}
	// 等这个库上的连接全部退掉，数不到零就算漏池。
	waitNoConn(t, admin, name)
}

// seedConflict 在临时库里先建 people 表，让 0001_init 撞表失败。
func seedConflict(t *testing.T, dsn string) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 把库日志关掉，免得刷屏。
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 连不上临时库就停住本用例。
	if err != nil {
		// 连不上临时库就停住本用例。
		t.Fatalf("open: %v", err)
	}
	// 取出底层连接池，建完表要关掉，免得算进连接数。
	sqlDB, err := db.DB()
	// 取不出底层连接池就停住本用例。
	if err != nil {
		// 取不出底层连接池就停住本用例。
		t.Fatalf("sql db: %v", err)
	}
	// 建完表就关池，后面数连接只算被测函数留下的。
	defer sqlDB.Close()
	// 建一张同名表占住位置，初始迁移再建就会报错。
	if err := db.Exec("CREATE TABLE people (id INT)").Error; err != nil {
		// 夹具表没建成就停住本用例。
		t.Fatalf("seed people: %v", err)
	}
}

// waitNoConn 轮询几秒等库上连接数归零；超时仍有连接就判漏池。
func waitNoConn(t *testing.T, admin *gorm.DB, name string) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 关池后后端退出要一点时间，最多等五秒。
	deadline := time.Now().Add(5 * time.Second)
	// 准备承接数出来的连接数。
	var n int64
	// 反复数，直到归零或超时。
	for {
		// 数这个库上还活着的连接，数不出就停住本用例。
		if err := admin.Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE datname = ?", name).Scan(&n).Error; err != nil {
			// 数不出连接数就停住本用例。
			t.Fatalf("count conn: %v", err)
		}
		// 连接已经全部退掉，说明没有漏池。
		if n == 0 {
			return
		}
		// 超时还有连接，就是失败时没关池。
		if time.Now().After(deadline) {
			// 报出剩下的连接数，方便看漏了多少。
			t.Fatalf("db %s still has %d conns after failed migrations", name, n)
		}
		// 稍等一下再数，给后端退出留时间。
		time.Sleep(100 * time.Millisecond)
	}
}
