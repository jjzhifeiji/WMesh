// Package testpg 为每例测试单独建本厂库，用完销毁。仅测试夹具，生产路径禁依赖。
package testpg

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/migrate"
	"wmesh/factory/migrations"
)

// AdminDSN 测试库维护连接串，可被环境变量覆盖。
func AdminDSN() string {
	// 环境里指定了测试库就用那一条。
	if v := os.Getenv("WMESH_TEST_PG"); v != "" {
		return v
	}
	return "postgres://wmesh:wmesh@127.0.0.1:55433/postgres?sslmode=disable"
}

// Open 连上维护库，通不了就失败。
func Open(t *testing.T) *gorm.DB {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 把库日志关掉，免得刷屏。
	db, err := gorm.Open(postgres.Open(AdminDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 没能把库日志关掉，免得刷屏就停，避免带着残缺继续。
	if err != nil {
		// 库没起来或结果不对。
		t.Fatalf("postgres: %v", err)
	}
	// 取出底层连接好去建库或删库。
	sqlDB, err := db.DB()
	// 没能取出底层连接好去建库或删库就停，避免带着残缺继续。
	if err != nil {
		// 查库失败就停住，再继续处理。
		t.Fatalf("sql db: %v", err)
	}
	// 拿出不会被取消的上下文。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// 用完就取消限时，避免上下文一直占着。
	defer cancel()
	// 没能上下文，再交给后面就停，避免带着残缺继续。
	if err := sqlDB.PingContext(ctx); err != nil {
		// 库没起来或结果不对。
		t.Fatalf("ping postgres: %v (start: docker compose up -d postgres)", err)
	}
	return db
}

// CreateDB 建临时库，测试结束强制删掉。
func CreateDB(t *testing.T, admin *gorm.DB, prefix string) (name, dsn string) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 收成普通文本再拿去比较或拼接。
	name = prefix + "_" + strings.ReplaceAll(id.New().String(), "-", "")
	// 没能库名当标识符引用，避免被拆开就停，避免带着残缺继续。
	if err := admin.Exec("CREATE DATABASE " + quoteIdent(name)).Error; err != nil {
		// 临时库没建成就停住。
		t.Fatalf("create db %s: %v", name, err)
	}
	// 用例结束强制删掉临时库，避免留下垃圾。
	t.Cleanup(func() {
		// 库名当标识符引用，避免被拆开。
		_ = admin.Exec("DROP DATABASE IF EXISTS " + quoteIdent(name) + " WITH (FORCE)").Error
	})
	// 取出测试库的维护连接串。
	dsn = swapDB(t, AdminDSN(), name)
	return name, dsn
}

// OpenMigrated 打开目标库并套向前迁移。
func OpenMigrated(t *testing.T, dsn string) *gorm.DB {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 把库日志关掉，免得刷屏。
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 没能把库日志关掉，免得刷屏就停，避免带着残缺继续。
	if err != nil {
		// 打开失败就停住本用例。
		t.Fatalf("open: %v", err)
	}
	// 没能把库结构迁到当前版本就停，避免带着残缺继续。
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		// 迁移结果不对就停住。
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// Fresh 建全新已迁移厂库，并给出测试用工厂身份。
func Fresh(t *testing.T) (db *gorm.DB, factoryID uuid.UUID) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 打开这一份资源，再交给后面。
	admin := Open(t)
	// 建一个测完就删的临时库。
	_, dsn := CreateDB(t, admin, "wmesh_fac")
	// 交回新建这一份后面要用的对象的结果。
	return OpenMigrated(t, dsn), id.New()
}

// quoteIdent 库名当标识符引用，避免被拆开。
func quoteIdent(name string) string {
	// 交回把不该留下的字符换掉的结果。
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// swapDB 只换 DSN 里的库名，账号密码和参数照旧。
func swapDB(t *testing.T, dsn, name string) string {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 把连接串拆成可替换的各段。
	u, err := url.Parse(dsn)
	// 没能把连接串拆成可替换的各段就停，避免带着残缺继续。
	if err != nil {
		// 把输入读成后面能用的结构和预期不符就停住。
		t.Fatalf("parse dsn: %v", err)
	}
	// 只换连接串里的库名，账号和参数不动。
	u.Path = "/" + name
	// 交回收成普通文本再拿去比较或拼接的结果。
	return u.String()
}
