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

	"wmesh/global/internal/platform/migrate"
	"wmesh/global/migrations"
)

// 环境给了维护库就用它，否则连本机测试库。
func testDSN() string {
	// 方便本机改端口时覆盖连接串。
	if v := os.Getenv("WMESH_TEST_PG"); v != "" {
		return v
	}
	return "postgres://wmesh:wmesh@127.0.0.1:55432/postgres?sslmode=disable"
}

// 建一个本例专用的空库，结束就拆掉。
func openTemp(t *testing.T) *gorm.DB {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 连维护库，日志关掉免得淹没用例。
	admin, err := gorm.Open(postgres.Open(testDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 连不上就没有库可测。
	if err != nil {
		// 连不上库就中止本用例。
		t.Fatal(err)
	}
	// 库名带到纳秒，避免并行用例撞名。
	name := "wmesh_migrate_" + strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
	// 建库失败则本例没有独立库。
	if err := admin.Exec(`CREATE DATABASE "` + name + `"`).Error; err != nil {
		// 建库失败就中止本用例。
		t.Fatal(err)
	}
	// 用例结束就拆掉临时库。
	t.Cleanup(func() {
		// 强制删掉，连上的会话一并断开。
		_ = admin.Exec(`DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`).Error
	})
	// 拆开连接串，只准备换库名。
	u, err := url.Parse(testDSN())
	// 拆不开就没法安全换库。
	if err != nil {
		// 连接串拆不开就中止。
		t.Fatal(err)
	}
	// 路径换成新库名，账号口令不动。
	u.Path = "/" + name
	// 打开这块临时库，日志同样关掉。
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 打不开就测不了迁移。
	if err != nil {
		// 临时库打不开就中止本用例。
		t.Fatal(err)
	}
	return db
}

// 同一套脚本套两次，成功历史仍应是两行。
func TestUpIdempotent(t *testing.T) {
	// 本例用一块全新的空库。
	db := openTemp(t)
	// 两支脚本：建表，再加一列。
	fsys := fstest.MapFS{
		"0001_init.up.sql":    {Data: []byte("CREATE TABLE t (id INT);")},
		"0002_comment.up.sql": {Data: []byte("ALTER TABLE t ADD COLUMN n INT;")},
	}
	// 第一次套用必须成功。
	if err := migrate.Up(db, fsys, "."); err != nil {
		// 套用失败就中止本用例。
		t.Fatal(err)
	}
	// 准备去数成功的历史行。
	var n int64
	// 第一次应记下两行成功。
	if err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history WHERE success").Scan(&n).Error; err != nil || n != 2 {
		// 历史行数不对就中止。
		t.Fatalf("history %d %v", n, err)
	}
	// 再套一次，已成功的不该重跑。
	if err := migrate.Up(db, fsys, "."); err != nil {
		// 第二次失败就中止。
		t.Fatal(err)
	}
	// 成功行仍应是两行。
	if err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history WHERE success").Scan(&n).Error; err != nil || n != 2 {
		// 多出来或少了就中止。
		t.Fatalf("idempotent %d %v", n, err)
	}
}

// 旧表已记过的脚本不重跑，后面的新脚本仍要套上。
func TestBaselineLegacy(t *testing.T) {
	// 本例用一块全新的空库。
	db := openTemp(t)
	// 先摆出旧的已套用表。
	if err := db.Exec(`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`).Error; err != nil {
		// 旧表建失败就中止。
		t.Fatal(err)
	}
	// 第一支脚本的效果先手工放上。
	if err := db.Exec(`CREATE TABLE t (id INT)`).Error; err != nil {
		// 旧表建不起来就中止。
		t.Fatal(err)
	}
	// 旧表声称第一支已经套过。
	if err := db.Exec(`INSERT INTO schema_migrations (name) VALUES ('0001_init.up.sql')`).Error; err != nil {
		// 旧表登记失败就中止。
		t.Fatal(err)
	}
	// 第一支若重跑会因表已在而失败，第二支才是增量。
	fsys := fstest.MapFS{
		"0001_init.up.sql": {Data: []byte("CREATE TABLE t (id INT);")},
		"0002_next.up.sql": {Data: []byte("ALTER TABLE t ADD COLUMN n INT;")},
	}
	// 套用应跳过第一支并执行第二支。
	if err := migrate.Up(db, fsys, "."); err != nil {
		// 套用失败就中止本用例。
		t.Fatal(err)
	}
	// 准备去数成功的历史行。
	var n int64
	// 基线加新脚本应是两行。
	if err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history WHERE success").Scan(&n).Error; err != nil || n != 2 {
		// 历史行数不对就中止。
		t.Fatalf("history %d %v", n, err)
	}
	// 新列必须真的加上，说明第二支执行了。
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 't' AND column_name = 'n'").Scan(&n).Error; err != nil || n != 1 {
		// 新列没加上就中止本用例。
		t.Fatalf("applied next %d %v", n, err)
	}
}

// 给旧历史表补注释的脚本应能套上。
func TestCommentOnLegacyHistory(t *testing.T) {
	// 本例用一块全新的空库。
	db := openTemp(t)
	// 第二支只给旧表写库内注释。
	fsys := fstest.MapFS{
		"0001_init.up.sql": {Data: []byte("CREATE TABLE t (id INT);")},
		"0002_comments.up.sql": {Data: []byte(`
COMMENT ON TABLE schema_migrations IS '已套用的向前迁移文件名，禁止改已落库数据';
COMMENT ON COLUMN schema_migrations.name IS '已执行的 SQL 文件名';
COMMENT ON COLUMN schema_migrations.applied_at IS '套用成功时间';
`)},
	}
	// 注释脚本必须套成功。
	if err := migrate.Up(db, fsys, "."); err != nil {
		// 套用失败就中止本用例。
		t.Fatal(err)
	}
}

// 仓库里的向前脚本应能套上，再套一次仍成功。
func TestAppliesEmbeddedSQL(t *testing.T) {
	// 本例用一块全新的空库。
	db := openTemp(t)
	// 套上本侧全部向前脚本。
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		// 第一次失败就中止。
		t.Fatal(err)
	}
	// 再套一次，已成功的不该重跑失败。
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		// 第二次失败就中止。
		t.Fatal(err)
	}
}
