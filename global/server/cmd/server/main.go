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

	"github.com/google/uuid"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/applog"
	"wmesh/global/internal/platform/appupdate"
	"wmesh/global/internal/platform/config"
	"wmesh/global/internal/platform/migrate"
	"wmesh/global/internal/platform/mqttbroker"
	"wmesh/global/internal/platform/oss"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
	"wmesh/global/internal/web"
	"wmesh/global/migrations"
)

// version 由构建时 -ldflags 注入，随探活 build 返回。
var version = "dev"

// 启动进程，失败则退出。
func main() {
	// 收拢启动日志，失败时才能写明退出原因。
	log := applog.New(os.Stdout, "global", version)
	// 跑起来失败就记日志并退出。
	if err := run(context.Background(), log); err != nil {
		// 记下退出原因，方便对照是配置还是库。
		log.Error("global server exit", "err", err)
		// 以失败码退出，避免被当成正常停机。
		os.Exit(1)
	}
}

// 读配置、迁移、引导管理员后对外服务。
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

	// 打开平台库，打不开就不要对外服务。
	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	// 平台库打不开就停机，名录不能对外。
	if err != nil {
		// 把开库失败交回启动方，进程就此退出。
		return fmt.Errorf("open wan db: %w", err)
	}
	// 取出底层连接，后面才能在退出时关掉。
	sqlDB, err := db.DB()
	// 拿不到连接就停机，避免留下半开的库。
	if err != nil {
		return err
	}
	// 进程退出时关掉平台库连接。
	defer sqlDB.Close()
	// 迁移只向前；失败直接不启动，避免半新半旧的库对外服务。
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		// 迁移失败就停机，避免半新半旧的库对外。
		return fmt.Errorf("migrate: %w", err)
	}

	// 按平台库组装服务，账号和名录都走这里。
	svc := service.NewService(store.Open(db))
	// 确认只把 tar 落到更新目录；真正换容器由本机 updater 做。
	updateDir := appupdate.Dir(cfg.UpdateDir)
	// 把落盘位置交给更新服务，确认后才写目录。
	svc.Updates.SetPendingSink(updateDir)
	// 换完容器后由这个目录回报是否已装上。
	svc.Updates.SetApplyReporter(updateDir)
	// 旧镜像由这个目录清理，避免磁盘被占满。
	svc.Updates.SetImageJanitor(updateDir)
	// 更新器报告已换成功，才把库里的包记成已装。
	if kind, version, ok, present, err := updateDir.Report(); err == nil && present && ok {
		// 记下已装，失败只警告，不拦住启动。
		if err := svc.Updates.MarkInstalled(ctx, kind, version); err != nil {
			// 没能标成已装就记警告，下次仍可再认。
			log.Warn("mark wan software installed", "err", err)
		}
	}
	// 启动时清掉在线标记，避免上次崩溃留下假在线。
	if err := svc.ResetChannelPresence(ctx); err != nil {
		// 在线状态清不掉就停机，避免名录显示假在线。
		return fmt.Errorf("reset channel presence: %w", err)
	}
	// 拉起平台侧通道，厂连上来才能收指令。
	bus, err := mqttbroker.Listen(cfg.MQTTAddr, mqttbroker.Hooks{
		// 校对厂钥签名，对不上就拒绝这条连接。
		Auth: func(factoryID uuid.UUID, unix int64, sig []byte) error {
			// 签名对不上就拒绝连接，避免假厂接入。
			return svc.Channel.VerifyFactoryProof(context.Background(), factoryID, unix, sig)
		},
		// 厂连上就记在线，名录才显示得到。
		Online: func(factoryID uuid.UUID) {
			// 记下在线，失败也不拆已经建立的连接。
			_ = svc.MarkChannelOnline(context.Background(), factoryID)
		},
		// 厂断开就记离线，避免名录一直显示在线。
		Offline: func(factoryID uuid.UUID) {
			// 记下离线，失败也不影响别的厂。
			_ = svc.MarkChannelOffline(context.Background(), factoryID)
		},
		// 厂的上行交给通道处理，正文不在这条消息里。
		Up: func(factoryID uuid.UUID, payload []byte) {
			// 处理上行回执，失败由通道内部消化。
			svc.Channel.HandleFactoryUp(context.Background(), factoryID, payload)
		},
	})
	// 通道起不来就停机，厂无法把指令送达。
	if err != nil {
		// 把通道失败交回，进程就此退出。
		return fmt.Errorf("mqtt: %w", err)
	}
	// 进程退出时关掉平台通道。
	defer bus.Close()
	// 把通道交给服务，后面才能向厂下发。
	svc.SetBus(bus)
	// 按配置引导管理员，失败就不要对外登录。
	if err := bootstrapAdmin(ctx, log, svc, cfg); err != nil {
		return err
	}

	// 装配账号接口，探活带上构建号。
	api := httpapi.New(svc, version)
	// 对象存储打开才接桶，关掉则软件包不走外存。
	if cfg.OSS.Enabled() {
		// 按配置接上对象存储，包字节不进进程内存。
		ossStore := oss.New(cfg.OSS.Endpoint, cfg.OSS.Bucket, cfg.OSS.AccessKey, cfg.OSS.SecretKey)
		// 软件包字节进对象存储，不进进程内存。
		svc.SetBlobs(ossStore)
		// 探活顺带保证桶在：对象存储晚起或被清空后能自愈，不用重启应用。
		api.OSSProbe = ossStore.EnsureBucket
		// 启动时只提醒不拦截：OSS 掉线不该让名录管理起不来。
		if err := ossStore.EnsureBucketRetry(ctx, 5, 2*time.Second); err != nil {
			// 桶未就绪只记警告，名录管理仍要起来。
			log.Warn("oss bucket not ready at startup", "endpoint", cfg.OSS.Endpoint, "bucket", cfg.OSS.Bucket, "err", err)
		} else { // 桶已就绪就记下，便于和告警对照。
			// 记下桶已可用，方便确认启动正常。
			log.Info("oss bucket ready", "endpoint", cfg.OSS.Endpoint, "bucket", cfg.OSS.Bucket)
		}
	}

	// 接口和页面分开挂，避免路径互相吞掉。
	mux := http.NewServeMux()
	// 取出已经装配好的接口路由。
	routes := api.Router()
	// 账号接口挂在版本前缀下。
	mux.Handle("/v1/", routes)
	// 探活走同一套路由，不另开监听。
	mux.Handle("/healthz", routes)
	// 有前端目录才托管页面，否则只留接口。
	if cfg.WebDir != "" {
		// 页面接在根路径，接口前缀不会被它吞掉。
		mux.Handle("/", web.Handler(cfg.WebDir))
	}
	// 写上监听和超时，大包上传不被读超时掐断。
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.Wrap(log, mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Minute, // 软件包上传会到百兆，30 秒会掐
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}
	// 接住监听失败，主循环才能决定退出。
	errCh := make(chan error, 1)
	// 后台监听，主流程才能等信号停机。
	go func() {
		// 记下已经在听，方便确认地址和构建号。
		log.Info("global http listening", "addr", cfg.HTTPAddr, "mqtt", cfg.MQTTAddr, "version", version, "web", cfg.WebDir != "", "oss", cfg.OSS.Enabled())
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
	log.Info("global http shutting down")
	// 停掉监听并等在途请求结束。
	return srv.Shutdown(shutdownCtx)
}

// bootstrapAdmin 库里没有管理员时写入；已有则跳过。显式打开覆盖开关时按环境变量重写密码。
func bootstrapAdmin(ctx context.Context, log *slog.Logger, svc *service.Service, cfg config.Config) error {
	// 没配管理员就跳过引导，库保持原样。
	if cfg.AdminLogin == "" {
		return nil
	}
	// 先看库里有没有管理员，查失败就不要乱写。
	exists, err := svc.AdminExists(ctx)
	// 查不到就停机，避免空库对外却没人能登录。
	if err != nil {
		// 把查询失败交回启动方，进程就此退出。
		return fmt.Errorf("check wan admin: %w", err)
	}
	// 已经有管理员就不要再造一个。
	if exists {
		// 没打开覆盖开关就保留原密码。
		if !cfg.AdminReset {
			return nil
		}
		// 按配置重写密码，失败就不要假装已经改成。
		if err := svc.ResetAdminPassword(ctx, cfg.AdminLogin, cfg.AdminPassword); err != nil {
			// 改密码失败就停机，避免配置和库不一致。
			return fmt.Errorf("reset wan admin: %w", err)
		}
		// 记下密码已按配置重写，便于对照启动。
		log.Info("wan admin password reset from config", "login", cfg.AdminLogin)
		return nil
	}
	// 库里没有管理员就写入第一个，失败则无法登录。
	if err := svc.BootstrapAdmin(ctx, cfg.AdminLogin, cfg.AdminPassword); err != nil {
		// 第一个管理员写不进去就停机。
		return fmt.Errorf("bootstrap wan admin: %w", err)
	}
	// 记下第一个管理员已写入。
	log.Info("wan admin bootstrapped", "login", cfg.AdminLogin)
	return nil
}
