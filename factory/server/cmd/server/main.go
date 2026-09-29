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
	// 收拢启动日志，失败时才能写明退出原因。
	log := applog.New(os.Stdout, "factory", version)
	// 跑起来失败就记日志并退出。
	if err := run(context.Background(), log); err != nil {
		// 记下退出原因，方便对照是配置还是库。
		log.Error("factory server exit", "err", err)
		// 以失败码退出，避免被当成正常停机。
		os.Exit(1)
	}
}

// 读配置、开维护库、按身份开厂库后对外服务。
func run(ctx context.Context, log *slog.Logger) error {
	// 读取进程配置，读不到就不能对外服务。
	cfg, err := config.Load()
	// 配置读不到就停机，避免空参数对外服务。
	if err != nil {
		// 把读配置失败交回启动方，进程就此退出。
		return fmt.Errorf("config: %w", err)
	}
	// 把中断收成取消，整进程才能一起停。
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	// 退出时解除信号登记，避免通知泄漏。
	defer stop()

	// 维护库只用来建/开厂库；各厂库在第一次被访问时套用迁移。
	h, err := hub.New(cfg.DSN)
	// 维护库打不开就停机，没有厂库可开。
	if err != nil {
		// 把开库失败交回启动方，进程就此退出。
		return fmt.Errorf("open admin db: %w", err)
	}
	// 退出时关掉已打开的厂库和维护库。
	defer h.Close()
	// 确认只把 tar 落到更新目录；真正换容器由本机 updater 做。
	h.SetPendingSink(appupdate.Dir(cfg.DockerUpdateDir))
	// 先留空对象存储，启用时再接上。
	var ossStore *oss.Client
	// 对象存储打开才接桶，关掉则软件包不走外存。
	if cfg.OSS.Enabled() {
		// 按配置接上对象存储，软件包字节不进内存。
		ossStore = oss.New(cfg.OSS.Endpoint, cfg.OSS.Bucket, cfg.OSS.AccessKey, cfg.OSS.SecretKey)
		// 软件包字节进对象存储；须在打开厂库和通道之前挂上。
		h.SetBlobs(ossStore)
		// 启动时确认桶在，未就绪只警告，不拦住开服。
		if err := ossStore.EnsureBucketRetry(ctx, 5, 2*time.Second); err != nil {
			// 桶未就绪只记警告，厂服务仍要起来。
			log.Warn("oss bucket not ready at startup", "endpoint", cfg.OSS.Endpoint, "bucket", cfg.OSS.Bucket, "err", err)
		} else { // 桶已就绪就记下，便于和告警对照。
			// 记下桶已可用，方便确认启动正常。
			log.Info("oss bucket ready", "endpoint", cfg.OSS.Endpoint, "bucket", cfg.OSS.Bucket)
		}
	}

	// 配了平台地址才保持出站，否则可以离线开工。
	if cfg.WANURL != "" {
		// 已认领的厂保持出站，断了会自己重连。
		h.StartChannel(ctx, cfg.WANURL, cfg.WANMQTT)
	}
	// 拉起本厂设备通道，失败则平板连不上。
	if err := h.StartClientBroker(cfg.ClientMQTTAddr); err != nil {
		// 设备通道失败就停机，避免页面以为能连。
		return fmt.Errorf("client mqtt: %w", err)
	}

	// 装配账号接口，引导口令只在这里核对。
	api := httpapi.New(h, cfg.BootstrapToken, cfg.WANURL)
	// 探活带回构建号，便于对上发布包。
	api.Version = version
	// 登录时把设备通道地址回给平板。
	api.ClientMQTTURL = cfg.ClientMQTTURL
	// 登录时把设备通道端口回给平板。
	api.ClientMQTTPort = cfg.ClientMQTTPort
	// 有对象存储才把探活交给桶检查。
	if ossStore != nil {
		// 探活顺带确认桶在，晚起也能自愈。
		api.OSSProbe = ossStore.EnsureBucket
	}

	// 接口和页面分开挂，避免路径互相吞掉。
	mux := http.NewServeMux()
	// 取出已经装配好的接口路由。
	routes := api.Router()
	// 账号接口挂在版本前缀下。
	mux.Handle("/v1/", routes)
	// 引导口单独挂内部前缀，不和页面混在一起。
	mux.Handle("/internal/", routes)
	// 探活走同一套路由，不另开监听。
	mux.Handle("/healthz", routes)
	// 有前端目录才托管页面，否则只留接口。
	if cfg.WebDir != "" {
		// 页面接在根路径，接口前缀不会被它吞掉。
		mux.Handle("/", web.Handler(cfg.WebDir))
	}
	// 写上监听和超时，慢拉包不被写超时掐断。
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.Wrap(log, mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // 平板拉 APK 会超过一分钟，60 秒写超时会掐成 unexpected end of stream
		IdleTimeout:       120 * time.Second,
	}
	// 接住监听失败，主循环才能决定退出。
	errCh := make(chan error, 1)
	// 后台监听，主流程才能等信号停机。
	go func() {
		// 记下已经在听，方便确认地址和构建号。
		log.Info("factory http listening", "addr", cfg.HTTPAddr, "version", version, "web", cfg.WebDir != "", "oss", cfg.OSS.Enabled(), "clientMqtt", h.ClientMQTTAddr())
		// 把监听结果送回主循环。
		errCh <- srv.ListenAndServe()
	}()
	// 先等监听自己退出或停机信号。
	select {
	// 监听自己退出时，先辨是不是正常关闭。
	case err := <-errCh:
		// 非正常关闭才交回，正常停机不当成故障。
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	// 收到停机信号后进入收尾。
	case <-ctx.Done():
	}
	// 限定收尾时间，避免停机一直卡住。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	// 收尾结束就释放这次超时。
	defer cancel()
	// 记下开始收尾，便于对照退出日志。
	log.Info("factory http shutting down")
	// 停掉监听并等在途请求结束。
	return srv.Shutdown(shutdownCtx)
}
