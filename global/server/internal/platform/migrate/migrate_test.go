// goose 套用、老库过渡、并发排队与失败重试；不 import testpg，避免与 migrate 循环引用。
package migrate_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pressly/goose/v3/database"
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

// gooseSQL 给假脚本补上 goose 的 Up 标记，和仓里的迁移文件同一写法。
func gooseSQL(body string) *fstest.MapFile {
	// 交回带标记的脚本。
	return &fstest.MapFile{Data: []byte("-- 测试脚本\n-- +goose Up\n" + body + "\n")}
}

// mustUp 迁库，失败就停住本用例。
func mustUp(t *testing.T, db *gorm.DB, fsys fs.FS) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 迁不上就停住本用例。
	if err := migrate.Up(db, fsys, "."); err != nil {
		// 把迁移错误打出来并停住。
		t.Fatalf("migrate: %v", err)
	}
}

// mustExec 执行一句准备数据的 SQL，失败就停住本用例。
func mustExec(t *testing.T, db *gorm.DB, sql string, args ...any) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 执行失败就停，后面的断言没有底数。
	if err := db.Exec(sql, args...).Error; err != nil {
		// 把失败的语句和错误打出来。
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// wantVersion 核对库里已套到的版本。
func wantVersion(t *testing.T, db *gorm.DB, want int64) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 读出当前版本。
	got, err := migrate.Version(db)
	// 读不出或对不上就停。
	if err != nil || got != want {
		// 把读到的版本和错误打出来。
		t.Fatalf("version %d %v, want %d", got, err, want)
	}
}

// countRows 数一张表的行数，数不出就停。
func countRows(t *testing.T, db *gorm.DB, query string) int64 {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 准备承接数出来的条数。
	var n int64
	// 数不出就停，后面没法比。
	if err := db.Raw(query).Scan(&n).Error; err != nil {
		// 把查询错误打出来并停住。
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// hasColumn 看这张表上有没有这一列。
func hasColumn(t *testing.T, db *gorm.DB, table, column string) bool {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 准备承接数出来的条数。
	var n int64
	// 查不了就停，后面没法判断。
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_name = ? AND column_name = ?", table, column).Scan(&n).Error; err != nil {
		// 把查询错误打出来并停住。
		t.Fatal(err)
	}
	return n == 1
}

// tableComment 读一张表的库内注释，没有时回空串。
func tableComment(t *testing.T, db *gorm.DB, table string) string {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 准备接表注释。
	var comment string
	// 读不出就停，后面没法判断。
	if err := db.Raw("SELECT COALESCE(obj_description(to_regclass(?), 'pg_class'), '')", table).Scan(&comment).Error; err != nil {
		// 把查询错误打出来并停住。
		t.Fatalf("comment %s: %v", table, err)
	}
	return comment
}

// createLegacyTables 按旧执行器的格式建出 Flyway 历史表和文件名表。
func createLegacyTables(t *testing.T, db *gorm.DB) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 按旧执行器的格式建 Flyway 历史表。
	mustExec(t, db, `CREATE TABLE flyway_schema_history (
		installed_rank INT NOT NULL,
		version VARCHAR(50),
		description VARCHAR(200) NOT NULL,
		type VARCHAR(20) NOT NULL,
		script VARCHAR(1000) NOT NULL,
		checksum INTEGER,
		installed_by VARCHAR(100) NOT NULL,
		installed_on TIMESTAMP NOT NULL DEFAULT NOW(),
		execution_time INTEGER NOT NULL,
		success BOOLEAN NOT NULL,
		CONSTRAINT flyway_schema_history_pk PRIMARY KEY (installed_rank)
	)`)
	// 旧执行器同时记的文件名表。
	mustExec(t, db, "CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())")
}

// threeSteps 三支假脚本：建表，再各加一列；重跑第一支会撞表已存在。
func threeSteps() fstest.MapFS {
	// 交回三支脚本。
	return fstest.MapFS{
		"0001_init.up.sql": gooseSQL("CREATE TABLE t (id INT);"),
		"0002_next.up.sql": gooseSQL("ALTER TABLE t ADD COLUMN n INT;"),
		"0003_more.up.sql": gooseSQL("ALTER TABLE t ADD COLUMN m INT;"),
	}
}

// firstTwo 只取三支里的前两支，模拟升级前的旧库。
func firstTwo() fstest.MapFS {
	// 先拿全套。
	fsys := threeSteps()
	// 去掉第三支。
	delete(fsys, "0003_more.up.sql")
	return fsys
}

// embeddedNames 列出嵌进程序的全部迁移脚本，按文件名排好。
func embeddedNames(t *testing.T) []string {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 列出全部脚本，Glob 交回时已按名字排好。
	names, err := fs.Glob(migrations.FS, "*.sql")
	// 列不出或一支都没有就停。
	if err != nil || len(names) == 0 {
		// 把列脚本的结果打出来并停住。
		t.Fatalf("glob %v %v", names, err)
	}
	return names
}

// versionOf 取文件名开头数字当版本，与 goose 的取法一致。
func versionOf(t *testing.T, name string) int64 {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 取下划线前的数字当版本。
	v, err := strconv.ParseInt(name[:strings.IndexByte(name, '_')], 10, 64)
	// 文件名开头不是数字就停。
	if err != nil {
		// 把坏文件名打出来并停住。
		t.Fatalf("name %s: %v", name, err)
	}
	return v
}

// latestEmbedded 算出嵌进程序的最大迁移版本。
func latestEmbedded(t *testing.T) int64 {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 列出全部脚本。
	names := embeddedNames(t)
	// 文件名已排好，最后一支就是最大版本。
	return versionOf(t, names[len(names)-1])
}

// embeddedFirst 只取嵌进程序的前 n 支脚本，模拟换执行器前的旧库。
func embeddedFirst(t *testing.T, n int) fstest.MapFS {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 准备只含前 n 支的假文件系统。
	out := fstest.MapFS{}
	// 逐支读出原文，原样放进假文件系统。
	for _, name := range embeddedNames(t)[:n] {
		// 读出脚本原文。
		body, err := fs.ReadFile(migrations.FS, name)
		// 读不出就停，旧库搭不全。
		if err != nil {
			// 把读脚本的错误打出来并停住。
			t.Fatal(err)
		}
		// 按原文件名登记，版本号不变。
		out[name] = &fstest.MapFile{Data: body}
	}
	return out
}

// 从空库迁到最新，同一套再跑一遍也必须成功、不多记版本。
func TestUpIdempotent(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 首次迁到最新。
	mustUp(t, db, threeSteps())
	// 三支都该记上版本。
	wantVersion(t, db, 3)
	// 第二遍什么都不该做。
	mustUp(t, db, threeSteps())
	// 除起点 0 外每版只记一次。
	if n := countRows(t, db, "SELECT COUNT(*) FROM goose_db_version WHERE version_id > 0"); n != 3 {
		// 多记或少记都算错。
		t.Fatalf("version rows %d", n)
	}
}

// 新库只建 goose 版本表，表和各列都有库内注释，旧的两张历史表不再出现。
func TestNewDBVersionTableComments(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 迁到最新。
	mustUp(t, db, threeSteps())
	// 版本表没注释说明建表时漏了。
	if c := tableComment(t, db, "goose_db_version"); c == "" {
		// 报出缺表注释。
		t.Fatal("goose_db_version without table comment")
	}
	// 数出没有注释的列。
	n := countRows(t, db, `SELECT COUNT(*) FROM information_schema.columns c
		WHERE c.table_name = 'goose_db_version'
		AND COALESCE(col_description('goose_db_version'::regclass, c.ordinal_position), '') = ''`)
	// 每列都要有注释。
	if n != 0 {
		// 报出缺注释的列数。
		t.Fatalf("columns without comment %d", n)
	}
	// 新库不该再建旧的历史表。
	if n := countRows(t, db, "SELECT COUNT(*) FROM pg_tables WHERE tablename IN ('schema_migrations', 'flyway_schema_history')"); n != 0 {
		// 报出多出的旧表数。
		t.Fatalf("legacy tables %d", n)
	}
}

// 旧执行器迁过的库：按 flyway_schema_history 的成功行登记，不重跑，旧表原样留着。
func TestAdoptFlywayHistory(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 先迁前两支，搭出旧库的表结构。
	mustUp(t, db, firstTwo())
	// 去掉 goose 版本表，当作还没换过执行器。
	mustExec(t, db, "DROP TABLE goose_db_version")
	// 按旧执行器的格式建两张历史表。
	createLegacyTables(t, db)
	// 前两支按旧格式记成功；第三支记一条失败行，过渡时不能当已套。
	mustExec(t, db, `INSERT INTO flyway_schema_history
		(installed_rank, version, description, type, script, checksum, installed_by, execution_time, success) VALUES
		(1, '1', 'init', 'SQL', '0001_init.up.sql', 11, 'wmesh', 1, TRUE),
		(2, '2', 'next', 'SQL', '0002_next.up.sql', 22, 'wmesh', 1, TRUE),
		(3, '3', 'more', 'SQL', '0003_more.up.sql', 33, 'wmesh', 1, FALSE)`)
	// 文件名表只记成功的两支。
	mustExec(t, db, "INSERT INTO schema_migrations (name) VALUES ('0001_init.up.sql'), ('0002_next.up.sql')")
	// 换成 goose 迁全套；重跑第一支会撞表已存在而失败。
	mustUp(t, db, threeSteps())
	// 第三支这回真的套上。
	if !hasColumn(t, db, "t", "m") {
		// 第三支没套就停。
		t.Fatal("0003 not applied")
	}
	// 版本到 3。
	wantVersion(t, db, 3)
	// 前两支只登记一次，第三支由 goose 自己记。
	if n := countRows(t, db, "SELECT COUNT(*) FROM goose_db_version WHERE version_id > 0"); n != 3 {
		// 多记或少记都算错。
		t.Fatalf("version rows %d", n)
	}
	// 旧 Flyway 历史原样，不删不改。
	if n := countRows(t, db, "SELECT COUNT(*) FROM flyway_schema_history"); n != 3 {
		// 旧表被动过就停。
		t.Fatalf("flyway rows %d", n)
	}
	// 旧文件名表原样，不删不改。
	if n := countRows(t, db, "SELECT COUNT(*) FROM schema_migrations"); n != 2 {
		// 旧表被动过就停。
		t.Fatalf("schema_migrations rows %d", n)
	}
}

// 更老的库只有 schema_migrations：按文件名开头数字登记，不重跑。
func TestAdoptSchemaMigrations(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 先迁前两支，搭出旧库的表结构。
	mustUp(t, db, firstTwo())
	// 去掉 goose 版本表，当作还没换过执行器。
	mustExec(t, db, "DROP TABLE goose_db_version")
	// 按最早的执行器格式建文件名表。
	mustExec(t, db, "CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())")
	// 记下已套过的两支。
	mustExec(t, db, "INSERT INTO schema_migrations (name) VALUES ('0001_init.up.sql'), ('0002_next.up.sql')")
	// 换成 goose 迁全套；重跑第一支会撞表已存在而失败。
	mustUp(t, db, threeSteps())
	// 版本到 3。
	wantVersion(t, db, 3)
	// 第三支这回真的套上。
	if !hasColumn(t, db, "t", "m") {
		// 第三支没套就停。
		t.Fatal("0003 not applied")
	}
	// 旧文件名表原样，不删不改。
	if n := countRows(t, db, "SELECT COUNT(*) FROM schema_migrations"); n != 2 {
		// 旧表被动过就停。
		t.Fatalf("schema_migrations rows %d", n)
	}
}

// 真实的 WAN 老库：旧执行器迁过前一半，换成 goose 后只套后一半，到最新且旧历史原样。
func TestAdoptEmbeddedLegacy(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 列出嵌进程序的全部脚本。
	names := embeddedNames(t)
	// 旧库套过前一半，后一半留给换执行器之后。
	half := len(names) / 2
	// 先迁前一半，搭出旧库的真实表结构。
	mustUp(t, db, embeddedFirst(t, half))
	// 去掉 goose 版本表，当作还没换过执行器。
	mustExec(t, db, "DROP TABLE goose_db_version")
	// 按旧执行器的格式建两张历史表。
	createLegacyTables(t, db)
	// 前一半逐支按旧执行器的写法记成功。
	for i, name := range names[:half] {
		// 旧执行器记的版本是去掉前导零的数字。
		v := strconv.FormatInt(versionOf(t, name), 10)
		// 按旧格式补一行成功历史。
		mustExec(t, db, `INSERT INTO flyway_schema_history
			(installed_rank, version, description, type, script, checksum, installed_by, execution_time, success)
			VALUES (?, ?, ?, 'SQL', ?, 0, 'wmesh', 0, TRUE)`, i+1, v, name, name)
		// 文件名表也记一笔，与旧执行器一致。
		mustExec(t, db, "INSERT INTO schema_migrations (name) VALUES (?)", name)
	}
	// 换成 goose 迁全套；任何一支已套的若被重跑都会撞已存在而失败。
	mustUp(t, db, migrations.FS)
	// 版本到最新。
	wantVersion(t, db, latestEmbedded(t))
	// 每支脚本恰好登记一次。
	if n := countRows(t, db, "SELECT COUNT(DISTINCT version_id) FROM goose_db_version WHERE version_id > 0"); n != int64(len(names)) {
		// 多记或少记都算错。
		t.Fatalf("versions %d, scripts %d", n, len(names))
	}
	// 旧 Flyway 历史原样，不删不改。
	if n := countRows(t, db, "SELECT COUNT(*) FROM flyway_schema_history"); n != int64(half) {
		// 旧表被动过就停。
		t.Fatalf("flyway rows %d, want %d", n, half)
	}
	// 旧文件名表原样，不删不改。
	if n := countRows(t, db, "SELECT COUNT(*) FROM schema_migrations"); n != int64(half) {
		// 旧表被动过就停。
		t.Fatalf("schema_migrations rows %d, want %d", n, half)
	}
}

// 0002 给旧文件名表写注释：表在才写，新库没有这张表也能从头迁过去。
func TestCommentsLegacyTableWhenPresent(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 先摆一张空的旧文件名表，当作旧执行器建好还没套任何脚本。
	mustExec(t, db, "CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())")
	// 迁全套，0002 应给这张表补上注释。
	mustUp(t, db, migrations.FS)
	// 表在却没写上注释就停。
	if c := tableComment(t, db, "schema_migrations"); c == "" {
		// 报出缺表注释。
		t.Fatal("schema_migrations without table comment")
	}
	// 版本到最新，说明空的旧表没让过渡误登记。
	wantVersion(t, db, latestEmbedded(t))
}

// 坏迁移失败后整支回滚、版本不动、后面的也不套；修好再迁自动接着套。
func TestFailedMigrationRetries(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 第二支先建表再插不存在的表，第二句必失败。
	fsys := fstest.MapFS{
		"0001_init.up.sql": gooseSQL("CREATE TABLE t (id INT);"),
		"0002_bad.up.sql":  gooseSQL("CREATE TABLE u (id INT);\nINSERT INTO nope VALUES (1);"),
		"0003_more.up.sql": gooseSQL("ALTER TABLE t ADD COLUMN m INT;"),
	}
	// 迁移应失败。
	err := migrate.Up(db, fsys, ".")
	// 准备接带文件名的迁移错误。
	var me *migrate.Error
	// 没报错或没指出坏的那支都算错。
	if !errors.As(err, &me) || me.File != "0002_bad.up.sql" || !strings.Contains(err.Error(), "0002_bad.up.sql") {
		// 把实际拿到的错误打出来。
		t.Fatalf("want error on 0002_bad.up.sql, got %v", err)
	}
	// 版本停在第一支。
	wantVersion(t, db, 1)
	// 坏的那支整支回滚，先建的表也不留。
	if n := countRows(t, db, "SELECT COUNT(*) FROM pg_tables WHERE tablename = 'u'"); n != 0 {
		// 留下半套就停。
		t.Fatalf("table u left behind %d", n)
	}
	// 后面的第三支不能越过坏的那支先套。
	if hasColumn(t, db, "t", "m") {
		// 越过去套了就停。
		t.Fatal("0003 applied after failure")
	}
	// 修好第二支。
	fsys["0002_bad.up.sql"] = gooseSQL("CREATE TABLE u (id INT);")
	// 再迁应接着套完。
	mustUp(t, db, fsys)
	// 版本到 3。
	wantVersion(t, db, 3)
}

// 嵌进程序的脚本要真正执行到库里，跑两遍也成功，版本到最新。
func TestAppliesEmbeddedSQL(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 首次迁到最新。
	mustUp(t, db, migrations.FS)
	// 第二遍什么都不该做。
	mustUp(t, db, migrations.FS)
	// 版本等于最大文件号。
	wantVersion(t, db, latestEmbedded(t))
}

// 多路同时迁同一个新库（如两个 WAN 实例同时首次启动），要排队套用，每版只记一次。
func TestConcurrentUpSameDB(t *testing.T) {
	// 打开一个测完就删的临时库，各路共用这一池，每路各占一条连接。
	db := openTemp(t)
	// 数出嵌进程序的脚本，迁完版本应正好这么多条。
	scripts := embeddedNames(t)
	// 同时起跑的路数，够撞出排队竞争即可。
	const ways = 4
	// 各路把结果送回来，收齐再判。
	errs := make(chan error, ways)
	// 发令枪，各路就位后一起开跑，尽量叠在一起。
	gate := make(chan struct{})
	// 逐路起协程，都等发令再迁。
	for i := 0; i < ways; i++ {
		// 这一路等发令后迁库，结果送回。
		go func(i int) {
			// 等发令，别先跑完一路。
			<-gate
			// 迁库失败时带上是第几路，成败都要送回。
			if err := migrate.Up(db, migrations.FS, "."); err != nil {
				// 送回带路号的错误。
				errs <- fmt.Errorf("way %d: %w", i, err)
				return
			}
			// 成功也送回，收齐才算完。
			errs <- nil
		}(i)
	}
	// 发令，各路同时开迁。
	close(gate)
	// 逐路收结果，有一路失败就停。
	for i := 0; i < ways; i++ {
		// 某一路迁库失败就停，说明并发没排上队。
		if err := <-errs; err != nil {
			// 把失败那一路的错误打出来并停住。
			t.Fatalf("concurrent up: %v", err)
		}
	}
	// 准备承接版本行数、去重后的版本数和起点行数。
	var rows, distinct, zero int64
	// 一次读出三个数，读不出就停。
	if err := db.Raw("SELECT COUNT(*) FILTER (WHERE version_id > 0), COUNT(DISTINCT version_id) FILTER (WHERE version_id > 0), COUNT(*) FILTER (WHERE version_id = 0) FROM goose_db_version").Row().Scan(&rows, &distinct, &zero); err != nil {
		// 把读版本表的错误打出来并停住。
		t.Fatal(err)
	}
	// 每个脚本只记一次、起点只有一行，才说明没有重复套用或重复建表。
	if rows != int64(len(scripts)) || distinct != rows || zero != 1 {
		// 把三个数和脚本数打出来便于对照。
		t.Fatalf("version rows=%d distinct=%d zero=%d scripts=%d", rows, distinct, zero, len(scripts))
	}
}

// TestAdoptRechecksAfterLockWait 别人持锁建好版本表后，排队方复查要看得到，不能再建一遍撞表。
func TestAdoptRechecksAfterLockWait(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 取出底层连接池，好单独占一条连接持锁。
	sqlDB, err := db.DB()
	// 取不到连接池就没法布置时序。
	if err != nil {
		// 取连接池失败就停。
		t.Fatal(err)
	}
	// 只留两条连接：持锁占一条，迁移的预查和事务只能共用另一条。
	sqlDB.SetMaxOpenConns(2)
	// 本例统一用的上下文。
	ctx := context.Background()
	// 占一条连接，扮演先到的进程。
	holder, err := sqlDB.Conn(ctx)
	// 占不到连接就停。
	if err != nil {
		// 占连接失败就停。
		t.Fatal(err)
	}
	// 中途失败时也把连接还回池子。
	defer holder.Close()
	// 先到的进程在事务里持迁移锁。
	tx, err := holder.BeginTx(ctx, nil)
	// 开不了事务就停。
	if err != nil {
		// 开事务失败就停。
		t.Fatal(err)
	}
	// 中途失败时回滚，放掉锁。
	defer tx.Rollback()
	// 拿迁移用的同一把咨询锁。
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", int64(0x574d657368)); err != nil {
		// 拿锁失败就停。
		t.Fatal(err)
	}
	// 后到的进程把迁移结果从这里送回。
	done := make(chan error, 1)
	// 后到的进程开迁：预查说没表，随后排到锁后面。
	go func() {
		// 把迁移结果送回。
		done <- migrate.Up(db, migrations.FS, ".")
	}()
	// 最多等十秒，等不到说明没排上队。
	deadline := time.Now().Add(10 * time.Second)
	// 准备接本库里排队中的咨询锁数。
	var waiting int64
	// 等到有人卡在本库的咨询锁上，才说明后到的已在排队。
	for waiting == 0 {
		// 超时就停，后到的没排到锁后面，时序不成立。
		if time.Now().After(deadline) {
			// 打出超时并停住。
			t.Fatal("up never waited on the migration lock")
		}
		// 稍等再看，别空转。
		time.Sleep(20 * time.Millisecond)
		// 只数本库没拿到的咨询锁，别的用例的库不算。
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted AND database = (SELECT oid FROM pg_database WHERE datname = current_database())").Scan(&waiting); err != nil {
			// 查锁表失败就停。
			t.Fatal(err)
		}
	}
	// 用 goose 的标准建法建版本表，和真实进程建的一样。
	store, err := database.NewStore(database.DialectPostgres, migrate.VersionTable)
	// 建不出存取件就停。
	if err != nil {
		// 建存取件失败就停。
		t.Fatal(err)
	}
	// 先到的进程建好版本表。
	if err := store.CreateVersionTable(ctx, tx); err != nil {
		// 建表失败就停。
		t.Fatal(err)
	}
	// 记上起点行，与正常建表一致。
	if err := store.Insert(ctx, tx, database.InsertRequest{Version: 0}); err != nil {
		// 记起点失败就停。
		t.Fatal(err)
	}
	// 提交即放锁，后到的接着跑。
	if err := tx.Commit(); err != nil {
		// 提交失败就停。
		t.Fatal(err)
	}
	// 把连接还回池子，goose 套脚本要用。
	_ = holder.Close()
	// 后到的复查应看到表，直接交给 goose 套完。
	if err := <-done; err != nil {
		// 复查没看到表会再建一遍，这里报撞表。
		t.Fatalf("queued up: %v", err)
	}
	// 套到最新一支才算没被卡住。
	wantVersion(t, db, latestEmbedded(t))
}

// TestAdoptLockWaitHasLimit 迁移锁被别人一直占着时，过渡也按上限报错交回，不无限挂着。
func TestAdoptLockWaitHasLimit(t *testing.T) {
	// 上限缩到一秒，测完恢复。
	defer migrate.SetLockWaitSeconds(1)()
	// 打开一个测完就删的临时库，还没有版本表，会走过渡。
	db := openTemp(t)
	// 取出底层连接池，好单独占一条连接持锁。
	sqlDB, err := db.DB()
	// 取不到连接池就没法布置。
	if err != nil {
		// 取连接池失败就停。
		t.Fatal(err)
	}
	// 本例统一用的上下文。
	ctx := context.Background()
	// 占一条连接，扮演卡住不放锁的旧进程。
	holder, err := sqlDB.Conn(ctx)
	// 占不到连接就停。
	if err != nil {
		// 占连接失败就停。
		t.Fatal(err)
	}
	// 测完放掉会话锁再还连接，免得锁跟着连接留在池里。
	defer func() {
		// 放掉本会话持有的全部咨询锁。
		_, _ = holder.ExecContext(ctx, "SELECT pg_advisory_unlock_all()")
		// 把连接还回池子。
		_ = holder.Close()
	}()
	// 用会话锁占住迁移锁，一直不放。
	if _, err := holder.ExecContext(ctx, "SELECT pg_advisory_lock($1)", int64(0x574d657368)); err != nil {
		// 拿锁失败就停。
		t.Fatal(err)
	}
	// 迁移结果从这里送回。
	done := make(chan error, 1)
	// 另起一路迁移，它会排在锁后面。
	go func() {
		// 把迁移结果送回。
		done <- migrate.Up(db, migrations.FS, ".")
	}()
	// 准备承接迁移交回的错误。
	var me *migrate.Error
	// 上限一秒，十秒内必须交回锁超时。
	select {
	// 交回了，要是带前缀的迁移错误，且点明锁超时。
	case err := <-done:
		// 不是迁移错误或不是锁超时都算错。
		if !errors.As(err, &me) || !strings.Contains(err.Error(), "lock timeout") {
			// 打出实际交回的错误。
			t.Fatalf("want lock timeout, got %v", err)
		}
	// 十秒还没交回，说明等锁没有上限。
	case <-time.After(10 * time.Second):
		// 打出超时并停住。
		t.Fatal("up still waiting on the migration lock")
	}
}
