// Package testpg 为每例测试单独建本侧库，用完销毁。仅测试夹具，生产路径禁依赖。
package testpg

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/global/internal/platform/id"
	"wmesh/global/internal/platform/migrate"
	"wmesh/global/migrations"
)

// AdminDSN 测试库维护连接串，可被环境变量覆盖。
func AdminDSN() string {
	// 环境给了维护库就用它，方便本机改端口。
	if v := os.Getenv("WMESH_TEST_PG"); v != "" {
		return v
	}
	return "postgres://wmesh:wmesh@127.0.0.1:55432/postgres?sslmode=disable"
}

// Open 连上维护库，通不了就失败。
func Open(t *testing.T) *gorm.DB {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 连维护库，日志关掉免得淹没用例。
	db, err := gorm.Open(postgres.Open(AdminDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 连不上就没有库可测。
	if err != nil {
		// 连失败就中止并带上原因。
		t.Fatalf("postgres: %v", err)
	}
	// 取出底层连接，才能探活。
	sqlDB, err := db.DB()
	// 拿不到连接池就测不了。
	if err != nil {
		// 拿失败就中止并带上原因。
		t.Fatalf("sql db: %v", err)
	}
	// 探活最多等五秒，避免用例挂死。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// 探完就取消，放开超时计时。
	defer cancel()
	// 维护库没起来就没法建临时库。
	if err := sqlDB.PingContext(ctx); err != nil {
		// 探活失败就中止，并提示把库拉起来。
		t.Fatalf("ping postgres: %v (start: docker compose up -d postgres)", err)
	}
	return db
}

// CreateDB 建临时库，测试结束强制删掉。
func CreateDB(t *testing.T, admin *gorm.DB, prefix string) (name, dsn string) {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 库名带上新身份，避免并行用例撞名。
	name = prefix + "_" + strings.ReplaceAll(id.New().String(), "-", "")
	// 建库失败则本例没有独立库。
	if err := admin.Exec("CREATE DATABASE " + quoteIdent(name)).Error; err != nil {
		// 建失败就中止并带上库名。
		t.Fatalf("create db %s: %v", name, err)
	}
	// 用例结束就拆掉临时库，避免留下垃圾库。
	t.Cleanup(func() {
		// 强制删掉，连上的会话一并断开。
		_ = admin.Exec("DROP DATABASE IF EXISTS " + quoteIdent(name) + " WITH (FORCE)").Error
	})
	// 连接串只换库名，账号口令照旧。
	dsn = swapDB(t, AdminDSN(), name)
	return name, dsn
}

// OpenMigrated 打开目标库并套向前迁移。
func OpenMigrated(t *testing.T, dsn string) *gorm.DB {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 打开目标库，日志关掉免得淹没用例。
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 打不开就没有可迁移的库。
	if err != nil {
		// 打开失败就中止并带上原因。
		t.Fatalf("open: %v", err)
	}
	// 套上向前迁移，表结构才跟本侧一致。
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		// 迁移失败就中止，避免在半套表上测。
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// Fresh 建全新已迁移 WAN 库给本例用。
func Fresh(t *testing.T) *gorm.DB {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 先连上维护库才能建临时库。
	admin := Open(t)
	// 建一个本例专用的空库。
	_, dsn := CreateDB(t, admin, "wmesh_wan")
	// 迁移完成再交给用例。
	return OpenMigrated(t, dsn)
}

// quoteIdent 库名当标识符引用，避免被拆开。
func quoteIdent(name string) string {
	// 标识符加引号，内部引号加倍，避免被拆开。
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// swapDB 只换 DSN 里的库名，账号密码和参数照旧。
func swapDB(t *testing.T, dsn, name string) string {
	// 失败行指到调用用例，不指到夹具。
	t.Helper()
	// 拆开连接串，只准备换库名。
	u, err := url.Parse(dsn)
	// 拆不开就没法安全换库。
	if err != nil {
		// 拆失败就中止并带上原因。
		t.Fatalf("parse dsn: %v", err)
	}
	// 路径换成新库名，账号口令不动。
	u.Path = "/" + name
	// 收成换过库名的连接串。
	return u.String()
}
