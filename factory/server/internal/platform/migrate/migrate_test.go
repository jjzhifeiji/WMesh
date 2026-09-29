// Flyway 套用与旧表基线；不 import testpg，避免与 migrate 循环引用。
package migrate_test

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/factory/internal/platform/migrate"
	"wmesh/factory/migrations"
)

// 维护库连接串，环境变量可以改掉。
func testDSN() string {
	// 环境里指定了测试库就用那一条。
	if v := os.Getenv("WMESH_TEST_PG"); v != "" {
		return v
	}
	return "postgres://wmesh:wmesh@127.0.0.1:55433/postgres?sslmode=disable"
}

// 开一个临时库，测完就删掉。
func openTemp(t *testing.T) *gorm.DB {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 把库日志关掉，免得刷屏。
	admin, err := gorm.Open(postgres.Open(testDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 没能把库日志关掉，免得刷屏就停住本用例。
	if err != nil {
		// 没能把库日志关掉，免得刷屏就停住本用例。
		t.Fatal(err)
	}
	// 按种类和短码把编号拼出来。
	name := "wmesh_migrate_" + strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
	// 没能把这一句交给库去执行就停住本用例。
	if err := admin.Exec(`CREATE DATABASE "` + name + `"`).Error; err != nil {
		// 没能把这一句交给库去执行就停住本用例。
		t.Fatal(err)
	}
	// 用例结束强制删掉临时库，避免留下垃圾。
	t.Cleanup(func() {
		// 把这一句交给库去执行。
		_ = admin.Exec(`DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`).Error
	})
	// 取出测试库的连接串。
	u, err := url.Parse(testDSN())
	// 没能取出测试库的连接串就停住本用例。
	if err != nil {
		// 没能取出测试库的连接串就停住本用例。
		t.Fatal(err)
	}
	// 只换连接串里的库名，账号和参数不动。
	u.Path = "/" + name
	// 把库日志关掉，免得刷屏。
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 没能把库日志关掉，免得刷屏就停住本用例。
	if err != nil {
		// 没能把库日志关掉，免得刷屏就停住本用例。
		t.Fatal(err)
	}
	return db
}

// 同一套迁移跑两遍也必须成功。
func TestUpIdempotent(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 准备一份只含这几个脚本的假文件系统。
	fsys := fstest.MapFS{
		"0001_init.up.sql":    {Data: []byte("CREATE TABLE t (id INT);")},
		"0002_comment.up.sql": {Data: []byte("ALTER TABLE t ADD COLUMN n INT;")},
	}
	// 没能把库结构迁到当前版本就停住本用例。
	if err := migrate.Up(db, fsys, "."); err != nil {
		// 没能把库结构迁到当前版本就停住本用例。
		t.Fatal(err)
	}
	// 准备承接数出来的条数。
	var n int64
	// 出错或结果对不上就停住本用例。
	if err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history WHERE success").Scan(&n).Error; err != nil || n != 2 {
		// 迁移历史的条数不对。
		t.Fatalf("history %d %v", n, err)
	}
	// 没能把库结构迁到当前版本就停住本用例。
	if err := migrate.Up(db, fsys, "."); err != nil {
		// 没能把库结构迁到当前版本就停住本用例。
		t.Fatal(err)
	}
	// 出错或结果对不上就停住本用例。
	if err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history WHERE success").Scan(&n).Error; err != nil || n != 2 {
		// 第二遍迁移的结果不对。
		t.Fatalf("idempotent %d %v", n, err)
	}
}

// 旧库先垫基线再往前迁，不能重做已有的。
func TestBaselineLegacy(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 没能把这一句交给库去执行就停住本用例。
	if err := db.Exec(`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`).Error; err != nil {
		// 没能把这一句交给库去执行就停住本用例。
		t.Fatal(err)
	}
	// 没能把这一句交给库去执行就停住本用例。
	if err := db.Exec(`CREATE TABLE t (id INT)`).Error; err != nil {
		// 没能把这一句交给库去执行就停住本用例。
		t.Fatal(err)
	}
	// 没能把这一句交给库去执行就停住本用例。
	if err := db.Exec(`INSERT INTO schema_migrations (name) VALUES ('0001_init.up.sql')`).Error; err != nil {
		// 没能把这一句交给库去执行就停住本用例。
		t.Fatal(err)
	}
	// 准备一份只含这几个脚本的假文件系统。
	fsys := fstest.MapFS{
		"0001_init.up.sql": {Data: []byte("CREATE TABLE t (id INT);")},
		"0002_next.up.sql": {Data: []byte("ALTER TABLE t ADD COLUMN n INT;")},
	}
	// 没能把库结构迁到当前版本就停住本用例。
	if err := migrate.Up(db, fsys, "."); err != nil {
		// 没能把库结构迁到当前版本就停住本用例。
		t.Fatal(err)
	}
	// 准备承接数出来的条数。
	var n int64
	// 出错或结果对不上就停住本用例。
	if err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history WHERE success").Scan(&n).Error; err != nil || n != 2 {
		// 迁移历史的条数不对。
		t.Fatalf("history %d %v", n, err)
	}
	// 出错或结果对不上就停住本用例。
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 't' AND column_name = 'n'").Scan(&n).Error; err != nil || n != 1 {
		// 换代之后的版本不对。
		t.Fatalf("applied next %d %v", n, err)
	}
}

// 旧历史表也要补上列注释。
func TestCommentOnLegacyHistory(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 准备一份只含这几个脚本的假文件系统。
	fsys := fstest.MapFS{
		"0001_init.up.sql": {Data: []byte("CREATE TABLE t (id INT);")},
		"0002_comments.up.sql": {Data: []byte(`
COMMENT ON TABLE schema_migrations IS '已套用的向前迁移文件名，禁止改已落库数据';
COMMENT ON COLUMN schema_migrations.name IS '已执行的 SQL 文件名';
COMMENT ON COLUMN schema_migrations.applied_at IS '套用成功时间';
`)},
	}
	// 没能把库结构迁到当前版本就停住本用例。
	if err := migrate.Up(db, fsys, "."); err != nil {
		// 没能把库结构迁到当前版本就停住本用例。
		t.Fatal(err)
	}
}

// 嵌进程序的脚本要真正执行到库里。
func TestAppliesEmbeddedSQL(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 没能把库结构迁到当前版本就停住本用例。
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		// 没能把库结构迁到当前版本就停住本用例。
		t.Fatal(err)
	}
	// 没能把库结构迁到当前版本就停住本用例。
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		// 没能把库结构迁到当前版本就停住本用例。
		t.Fatal(err)
	}
}
