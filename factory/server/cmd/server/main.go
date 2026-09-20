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
	"wmesh/factory/internal/platform/applog"
	"wmesh/factory/internal/platform/appupdate"
	"wmesh/factory/internal/platform/config"
	"wmesh/factory/internal/platform/oss"
	"wmesh/factory/internal/web"
)

// version 由构建时 -ldflags 注入，随探活 build 返回。
var version = "dev"

// 启动进程，失败则退出。
func main() {
	log := applog.New(os.Stdout, "wmesh-factory", "factory", version)
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
	// 确认只把 tar 落到更新目录；真正换容器由本机 updater 做。
	h.SetPendingSink(appupdate.Dir(cfg.DockerUpdateDir))
	var ossStore *oss.Client
	if cfg.OSS.Enabled() {
		ossStore = oss.New(cfg.OSS.Endpoint, cfg.OSS.Bucket, cfg.OSS.AccessKey, cfg.OSS.SecretKey)
		// 软件包字节进对象存储；须在打开厂库和通道之前挂上。
		h.SetBlobs(ossStore)
		if err := ossStore.EnsureBucketRetry(ctx, 5, 2*time.Second); err != nil {
			log.Warn("oss bucket not ready at startup", "endpoint", cfg.OSS.Endpoint, "bucket", cfg.OSS.Bucket, "err", err)
		} else {
			log.Info("oss bucket ready", "endpoint", cfg.OSS.Endpoint, "bucket", cfg.OSS.Bucket)
		}
	}

	if cfg.WANURL != "" {
		h.StartChannel(ctx, cfg.WANURL, cfg.WANMQTT)
	}
	if err := h.StartClientBroker(cfg.ClientMQTTAddr); err != nil {
		return fmt.Errorf("client mqtt: %w", err)
	}

	api := httpapi.New(h, cfg.BootstrapToken, cfg.WANURL)
	api.Version = version
	api.ClientMQTTURL = cfg.ClientMQTTURL
	api.ClientMQTTPort = cfg.ClientMQTTPort
	if ossStore != nil {
		api.OSSProbe = ossStore.EnsureBucket
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
		log.Info("factory http listening", "addr", cfg.HTTPAddr, "version", version, "web", cfg.WebDir != "", "oss", cfg.OSS.Enabled(), "clientMqtt", h.ClientMQTTAddr())
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
