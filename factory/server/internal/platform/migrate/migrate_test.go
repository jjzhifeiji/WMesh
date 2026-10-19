// Flyway 套用与旧表基线；不 import testpg，避免与 migrate 循环引用。
package migrate_test

import (
	"io/fs"
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

// 多路同时迁同一个新库（如两个进程同时首次打开厂库），要排队套用，不能撞历史主键。
func TestConcurrentUpSameDB(t *testing.T) {
	// 打开一个测完就删的临时库，各路共用这一池，每路各占一条连接。
	db := openTemp(t)
	// 数出嵌进程序的脚本，迁完历史应正好这么多条。
	scripts, err := fs.Glob(migrations.FS, "*.sql")
	// 列不出脚本就停，后面没有底数。
	if err != nil {
		// 把列脚本的错误打出来并停住。
		t.Fatal(err)
	}
	// 同时起跑的路数，够撞出排号竞争即可。
	const ways = 4
	// 各路把结果送回来，收齐再判。
	errs := make(chan error, ways)
	// 发令枪，各路就位后一起开跑，尽量叠在一起。
	gate := make(chan struct{})
	// 逐路起协程，都等发令再迁。
	for i := 0; i < ways; i++ {
		// 这一路等发令后迁库，结果送回。
		go func() {
			// 等发令，别先跑完一路。
			<-gate
			// 迁完把结果送回，成败都要送。
			errs <- migrate.Up(db, migrations.FS, ".")
		}()
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
	// 准备承接历史条数、去重后的脚本数和最大排号。
	var rows, distinct, maxRank int64
	// 一次读出三个数，读不出就停。
	if err := db.Raw("SELECT COUNT(*), COUNT(DISTINCT script), COALESCE(MAX(installed_rank), 0) FROM flyway_schema_history").Row().Scan(&rows, &distinct, &maxRank); err != nil {
		// 把读历史的错误打出来并停住。
		t.Fatal(err)
	}
	// 每个脚本只记一次、排号连续，才说明没有重复套用。
	if rows != int64(len(scripts)) || distinct != rows || maxRank != rows {
		// 把三个数和脚本数打出来便于对照。
		t.Fatalf("history rows=%d distinct=%d maxRank=%d scripts=%d", rows, distinct, maxRank, len(scripts))
	}
}

// 登录名去空白的迁移：只去能去的，去后为空或撞名的原样留着，重跑也不报错。
func TestTrimLoginNameMigration(t *testing.T) {
	// 打开一个测完就删的临时库。
	db := openTemp(t)
	// 没能把库结构迁到当前版本就停住本用例。
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		// 没能把库结构迁到当前版本就停住本用例。
		t.Fatal(err)
	}
	// 迁好之后再塞进带空白的旧账号，模拟升级前的存量。
	names := []string{" zhang3", "zhang3 ", "li4\u3000", "wang5\t", "\u00a0qian7\u2028", " sun8 ", "\u3000 ", "zhao6", "zhao6\u00a0"}
	// 逐个写进人员表，状态随便给一个合法值。
	for _, n := range names {
		// 写不进去就停，后面的断言没有底数。
		if err := db.Exec(`INSERT INTO people (id, login_name, display_name, status) VALUES (gen_random_uuid(), ?, '旧账号', 'active')`, n).Error; err != nil {
			// 把写旧账号的错误打出来并停住。
			t.Fatalf("insert %q: %v", n, err)
		}
	}
	// 取出嵌进程序的那份迁移原文，和升级时跑的是同一份。
	body, err := migrations.FS.ReadFile("0041_trim_login_name.up.sql")
	// 读不出迁移原文就停，说明文件名或嵌入不对。
	if err != nil {
		// 把读迁移原文的错误打出来并停住。
		t.Fatal(err)
	}
	// 连跑两遍，撞名的不改也不能撞唯一约束，重跑不报错。
	for i := 0; i < 2; i++ {
		// 执行失败就停，说明撞了唯一约束或语法不对。
		if err := db.Exec(string(body)).Error; err != nil {
			// 把第几遍失败打出来并停住。
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	// 准备承接库里现在的全部登录名。
	var got []string
	// 读不出登录名就停，后面没法比对。
	if err := db.Raw(`SELECT login_name FROM people`).Scan(&got).Error; err != nil {
		// 把读登录名的错误打出来并停住。
		t.Fatal(err)
	}
	// 记下每个登录名出现了几次，方便按集合比对。
	seen := map[string]int{}
	// 逐个计数，顺序不要紧。
	for _, n := range got {
		// 这个名字再记一次。
		seen[n]++
	}
	// 能去的已去掉，互撞、和干净名撞、全空白的都原样。
	want := []string{" zhang3", "zhang3 ", "li4", "wang5", "qian7", "sun8", "\u3000 ", "zhao6", "zhao6\u00a0"}
	// 条数对不上说明多改或少改了行。
	if len(got) != len(want) {
		// 把实际读到的名字打出来便于对照。
		t.Fatalf("rows %q", got)
	}
	// 逐个核对期望的名字都在。
	for _, n := range want {
		// 缺了这个名字说明去空白规则不对。
		if seen[n] != 1 {
			// 把缺的名字和实际结果打出来。
			t.Fatalf("want %q in %q", n, got)
		}
	}
}
