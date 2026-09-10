// 进程入口：读配置、连 WAN 库、跑迁移、按需引导唯一管理员，再对外提供账号 HTTP 与管理端静态页。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	"wmesh/global/internal/platform/config"
	"wmesh/global/internal/platform/migrate"
	"wmesh/global/internal/platform/oss"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
	"wmesh/global/internal/web"
	"wmesh/global/migrations"
)

// version 由构建时 -ldflags "-X main.version=..." 注入，随探活返回。
var version = "dev"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	if err := run(context.Background(), log); err != nil {
		log.Error("global server exit", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return fmt.Errorf("open wan db: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	// 迁移只向前；失败直接不启动，避免半新半旧的库对外服务。
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	svc := service.NewService(store.Open(db), &factoryboot.Client{BaseURL: cfg.FactoryBootstrapURL, Token: cfg.BootstrapToken})
	if err := bootstrapAdmin(ctx, log, svc, cfg); err != nil {
		return err
	}

	api := httpapi.New(svc, version)
	if cfg.OSS.Enabled() {
		store := oss.New(cfg.OSS.Endpoint, cfg.OSS.Bucket, cfg.OSS.AccessKey, cfg.OSS.SecretKey)
		// 探活顺带保证桶在：对象存储晚起或被清空后能自愈，不用重启应用。
		api.OSSProbe = store.EnsureBucket
		// 启动时只提醒不拦截：OSS 掉线不该让名录管理起不来。
		if err := store.EnsureBucketRetry(ctx, 5, 2*time.Second); err != nil {
			log.Warn("oss bucket not ready at startup", "endpoint", cfg.OSS.Endpoint, "bucket", cfg.OSS.Bucket, "err", err)
		} else {
			log.Info("oss bucket ready", "endpoint", cfg.OSS.Endpoint, "bucket", cfg.OSS.Bucket)
		}
	}

	mux := http.NewServeMux()
	routes := api.Router()
	mux.Handle("/v1/", routes)
	mux.Handle("/healthz", routes)
	if cfg.WebDir != "" {
		mux.Handle("/", web.Handler(cfg.WebDir))
	}
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.Wrap(log, mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("global http listening", "addr", cfg.HTTPAddr, "version", version, "web", cfg.WebDir != "", "oss", cfg.OSS.Enabled())
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	log.Info("global http shutting down")
	return srv.Shutdown(shutdownCtx)
}

// bootstrapAdmin 只在库里还没有管理员时写入；已有则跳过，不给每次重启都留拒绝审计。
func bootstrapAdmin(ctx context.Context, log *slog.Logger, svc *service.Service, cfg config.Config) error {
	if cfg.AdminLogin == "" {
		return nil
	}
	exists, err := svc.AdminExists(ctx)
	if err != nil {
		return fmt.Errorf("check wan admin: %w", err)
	}
	if exists {
		return nil
	}
	if err := svc.BootstrapAdmin(ctx, cfg.AdminLogin, cfg.AdminPassword); err != nil {
		return fmt.Errorf("bootstrap wan admin: %w", err)
	}
	log.Info("wan admin bootstrapped", "login", cfg.AdminLogin)
	return nil
}
