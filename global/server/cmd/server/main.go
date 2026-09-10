// 进程入口：连 WAN 库、跑迁移、对外提供账号 HTTP。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/global/internal/factoryboot"
	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/domain"
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
	bootURL := os.Getenv("WMESH_FACTORY_BOOTSTRAP_URL")
	if bootURL == "" {
		bootURL = "http://127.0.0.1:8081"
	}
	bootToken := os.Getenv("WMESH_BOOTSTRAP_TOKEN")
	if bootToken == "" {
		return fmt.Errorf("WMESH_BOOTSTRAP_TOKEN required")
	}
	svc := service.NewService(store.Open(db), &factoryboot.Client{BaseURL: bootURL, Token: bootToken})
	if login := os.Getenv("WMESH_ADMIN_LOGIN"); login != "" {
		pass := os.Getenv("WMESH_ADMIN_PASSWORD")
		if pass == "" {
			return fmt.Errorf("WMESH_ADMIN_PASSWORD required with WMESH_ADMIN_LOGIN")
		}
		if err := svc.BootstrapAdmin(context.Background(), login, pass); err != nil && !errors.Is(err, domain.ErrWANAdminExists) {
			return err
		}
	}
	addr := os.Getenv("WMESH_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: httpapi.New(svc).Router()}
	go func() {
		log.Printf("global http %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	wait()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func wait() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}
