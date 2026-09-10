// Package testpg 为每例测试单独建本厂库，用完销毁。仅测试夹具，生产路径禁依赖。
package testpg

import (
	"context"
	"fmt"
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

func AdminDSN() string {
	if v := os.Getenv("WMESH_TEST_PG"); v != "" {
		return v
	}
	return "postgres://wmesh:wmesh@127.0.0.1:55433/postgres?sslmode=disable"
}

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

func CreateDB(t *testing.T, admin *gorm.DB, prefix string) (name, dsn string) {
	t.Helper()
	name = prefix + "_" + strings.ReplaceAll(id.New().String(), "-", "")
	if err := admin.Exec("CREATE DATABASE " + quoteIdent(name)).Error; err != nil {
		t.Fatalf("create db %s: %v", name, err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP DATABASE IF EXISTS " + quoteIdent(name) + " WITH (FORCE)").Error
	})
	dsn = swapDB(AdminDSN(), name)
	return name, dsn
}

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

func Fresh(t *testing.T) (db *gorm.DB, factoryID uuid.UUID) {
	t.Helper()
	admin := Open(t)
	_, dsn := CreateDB(t, admin, "wmesh_fac")
	return OpenMigrated(t, dsn), id.New()
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func swapDB(dsn, name string) string {
	const key = "/postgres"
	i := strings.LastIndex(dsn, key)
	if i < 0 {
		return fmt.Sprintf("postgres://wmesh:wmesh@127.0.0.1:55433/%s?sslmode=disable", name)
	}
	return dsn[:i] + "/" + name + dsn[i+len(key):]
}
