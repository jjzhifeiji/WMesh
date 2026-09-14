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
	if v := os.Getenv("WMESH_TEST_PG"); v != "" {
		return v
	}
	return "postgres://wmesh:wmesh@127.0.0.1:55433/postgres?sslmode=disable"
}

// Open 连上维护库，通不了就失败。
func Open(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(AdminDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v (start: docker compose up -d postgres)", err)
	}
	return db
}

// CreateDB 建临时库，测试结束强制删掉。
func CreateDB(t *testing.T, admin *gorm.DB, prefix string) (name, dsn string) {
	t.Helper()
	name = prefix + "_" + strings.ReplaceAll(id.New().String(), "-", "")
	if err := admin.Exec("CREATE DATABASE " + quoteIdent(name)).Error; err != nil {
		t.Fatalf("create db %s: %v", name, err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP DATABASE IF EXISTS " + quoteIdent(name) + " WITH (FORCE)").Error
	})
	dsn = swapDB(t, AdminDSN(), name)
	return name, dsn
}

// OpenMigrated 打开目标库并套向前迁移。
func OpenMigrated(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// Fresh 建全新已迁移厂库，并给出测试用工厂身份。
func Fresh(t *testing.T) (db *gorm.DB, factoryID uuid.UUID) {
	t.Helper()
	admin := Open(t)
	_, dsn := CreateDB(t, admin, "wmesh_fac")
	return OpenMigrated(t, dsn), id.New()
}

// quoteIdent 库名当标识符引用，避免被拆开。
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// swapDB 只换 DSN 里的库名，账号密码和参数照旧。
func swapDB(t *testing.T, dsn, name string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}
