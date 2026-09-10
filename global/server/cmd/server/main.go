// 进程入口：连 WAN 库、跑迁移、组装应用服务。本阶段不对外提供 HTTP。
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/global/internal/platform/migrate"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
	"wmesh/global/migrations"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dsn := os.Getenv("WMESH_DSN")
	if dsn == "" {
		dsn = "postgres://wmesh:wmesh@127.0.0.1:55432/wmesh?sslmode=disable"
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return err
	}
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		return err
	}
	svc := service.NewService(store.Open(db), nil)
	if login := os.Getenv("WMESH_ADMIN_LOGIN"); login != "" {
		pass := os.Getenv("WMESH_ADMIN_PASSWORD")
		if pass == "" {
			return fmt.Errorf("WMESH_ADMIN_PASSWORD required with WMESH_ADMIN_LOGIN")
		}
		if err := svc.BootstrapAdmin(context.Background(), login, pass); err != nil {
			return err
		}
	}
	log.Println("global server ready; HTTP is not served in this phase")
	wait()
	return nil
}

func wait() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}
