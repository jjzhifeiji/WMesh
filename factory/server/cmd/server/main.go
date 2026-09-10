// 进程入口：连本厂库、跑迁移、组装应用服务。本阶段不对外提供 HTTP。
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/factory/internal/platform/migrate"
	"wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
	"wmesh/factory/migrations"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	idStr := os.Getenv("WMESH_FACTORY_ID")
	if idStr == "" {
		return fmt.Errorf("WMESH_FACTORY_ID required")
	}
	facID, err := uuid.Parse(idStr)
	if err != nil {
		return fmt.Errorf("WMESH_FACTORY_ID: %w", err)
	}
	dsn := os.Getenv("WMESH_DSN")
	if dsn == "" {
		dsn = "postgres://wmesh:wmesh@127.0.0.1:55433/wmesh?sslmode=disable"
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return err
	}
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		return err
	}
	_ = service.NewService(store.Open(db, facID))
	log.Println("factory server ready; HTTP is not served in this phase")
	wait()
	return nil
}

func wait() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}
