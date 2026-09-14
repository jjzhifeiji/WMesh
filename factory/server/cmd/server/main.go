// 进程入口：读配置、连维护库、按工厂身份开各厂库，对外提供账号 HTTP 与管理端静态页。
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

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/config"
	"wmesh/factory/internal/platform/oss"
	"wmesh/factory/internal/web"
)

// version 由构建时 -ldflags "-X main.version=..." 注入，随探活返回。
var version = "dev"

// 启动进程，失败则退出。
func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	if err := run(context.Background(), log); err != nil {
		log.Error("factory server exit", "err", err)
		os.Exit(1)
	}
}

// 读配置、开维护库、按身份开厂库后对外服务。
func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 维护库只用来建/开厂库；各厂库在第一次被访问时套用迁移。
	h, err := hub.New(cfg.DSN)
	if err != nil {
		return fmt.Errorf("open admin db: %w", err)
	}
	defer h.Close()

	if cfg.WANURL != "" {
		h.StartChannel(ctx, cfg.WANURL)
	}

	api := httpapi.New(h, cfg.BootstrapToken, cfg.WANURL)
	api.Version = version
	if cfg.OSS.Enabled() {
		store := oss.New(cfg.OSS.Endpoint, cfg.OSS.Bucket, cfg.OSS.AccessKey, cfg.OSS.SecretKey)
		// 探活顺带保证桶在：对象存储晚起或被清空后能自愈，不用重启应用。
		api.OSSProbe = store.EnsureBucket
		// 启动时只提醒不拦截：OSS 掉线不该让账号管理起不来。
		if err := store.EnsureBucketRetry(ctx, 5, 2*time.Second); err != nil {
			log.Warn("oss bucket not ready at startup", "endpoint", cfg.OSS.Endpoint, "bucket", cfg.OSS.Bucket, "err", err)
		} else {
			log.Info("oss bucket ready", "endpoint", cfg.OSS.Endpoint, "bucket", cfg.OSS.Bucket)
		}
	}

	mux := http.NewServeMux()
	routes := api.Router()
	mux.Handle("/v1/", routes)
	mux.Handle("/internal/", routes)
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
		log.Info("factory http listening", "addr", cfg.HTTPAddr, "version", version, "web", cfg.WebDir != "", "oss", cfg.OSS.Enabled())
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
	log.Info("factory http shutting down")
	return srv.Shutdown(shutdownCtx)
}
