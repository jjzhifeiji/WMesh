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

func testDSN() string {
	if v := os.Getenv("WMESH_TEST_PG"); v != "" {
		return v
	}
	return "postgres://wmesh:wmesh@127.0.0.1:55432/postgres?sslmode=disable"
}

func openTemp(t *testing.T) *gorm.DB {
	t.Helper()
	admin, err := gorm.Open(postgres.Open(testDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	name := "wmesh_migrate_" + strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
	if err := admin.Exec(`CREATE DATABASE "` + name + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = admin.Exec(`DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`).Error
	})
	u, err := url.Parse(testDSN())
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestUpIdempotent(t *testing.T) {
	db := openTemp(t)
	fsys := fstest.MapFS{
		"0001_init.up.sql":    {Data: []byte("CREATE TABLE t (id INT);")},
		"0002_comment.up.sql": {Data: []byte("ALTER TABLE t ADD COLUMN n INT;")},
	}
	if err := migrate.Up(db, fsys, "."); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history WHERE success").Scan(&n).Error; err != nil || n != 2 {
		t.Fatalf("history %d %v", n, err)
	}
	if err := migrate.Up(db, fsys, "."); err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history WHERE success").Scan(&n).Error; err != nil || n != 2 {
		t.Fatalf("idempotent %d %v", n, err)
	}
}

func TestBaselineLegacy(t *testing.T) {
	db := openTemp(t)
	if err := db.Exec(`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE t (id INT)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO schema_migrations (name) VALUES ('0001_init.up.sql')`).Error; err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{
		"0001_init.up.sql": {Data: []byte("CREATE TABLE t (id INT);")},
		"0002_next.up.sql": {Data: []byte("ALTER TABLE t ADD COLUMN n INT;")},
	}
	if err := migrate.Up(db, fsys, "."); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM flyway_schema_history WHERE success").Scan(&n).Error; err != nil || n != 2 {
		t.Fatalf("history %d %v", n, err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 't' AND column_name = 'n'").Scan(&n).Error; err != nil || n != 1 {
		t.Fatalf("applied next %d %v", n, err)
	}
}

func TestCommentOnLegacyHistory(t *testing.T) {
	db := openTemp(t)
	fsys := fstest.MapFS{
		"0001_init.up.sql": {Data: []byte("CREATE TABLE t (id INT);")},
		"0002_comments.up.sql": {Data: []byte(`
COMMENT ON TABLE schema_migrations IS '已套用的向前迁移文件名，禁止改已落库数据';
COMMENT ON COLUMN schema_migrations.name IS '已执行的 SQL 文件名';
COMMENT ON COLUMN schema_migrations.applied_at IS '套用成功时间';
`)},
	}
	if err := migrate.Up(db, fsys, "."); err != nil {
		t.Fatal(err)
	}
}

func TestAppliesEmbeddedSQL(t *testing.T) {
	db := openTemp(t)
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		t.Fatal(err)
	}
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		t.Fatal(err)
	}
}
