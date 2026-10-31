// Package hub 按工厂身份打开对应厂库并组装应用服务。
// 不认 WAN 名录，不把本厂密码送到 WAN。
package hub

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/factory/internal/platform/blob"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/migrate"
	"wmesh/factory/internal/platform/nodekey"
	"wmesh/factory/internal/platform/provision"
	"wmesh/factory/internal/platform/release"
	"wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
	"wmesh/factory/internal/wanchannel"
)

// 一家厂已打开的库和应用服务。
type tenant struct {
	db       *gorm.DB           // 这家厂已经打开的库连接。
	svc      *service.Service   // 这家厂已经组装好的应用服务。
	stopPush context.CancelFunc // 停掉这家厂的焊汇总上送循环。
	pushDone chan struct{}      // 上送循环退出时关闭，关库前要等它。
}

// close 先停焊汇总上送循环并等它退出，再关库，免得循环拿着已关的库。
// 调用方不要占着 h.mu：循环上送时要取这把锁。
func (t *tenant) close() {
	// 叫停上送循环，正在推的请求随之取消。
	t.stopPush()
	// 等循环真正退出，之后不会再碰这家厂的库。
	<-t.pushDone
	// 取出底层连接，能关才关。
	if sqlDB, err := t.db.DB(); err == nil {
		// 关掉这家厂的库连接。
		_ = sqlDB.Close()
	}
}

// 厂库打开失败后的退避：30 秒起，每再失败一次翻倍，封顶 10 分钟。
const (
	openBackoffMin = 30 * time.Second // 首次失败后等多久再试
	openBackoffMax = 10 * time.Minute // 连续失败时最多等这么久
)

// openFailure 一家厂最近一次打开失败；退避期内直接交回这次的错，到点再试。
type openFailure struct {
	err   error         // 上次建库、连库或迁移失败的原错误
	at    time.Time     // 上次失败的时间
	wait  time.Duration // 本轮退避时长，再失败就翻倍
	retry time.Time     // 到这个时间之后才再试
}

// Hub 按工厂稳定身份选库；没有库就当作工厂还不存在。
type Hub struct {
	mu          sync.Mutex                                // 护着已打开的厂库表；只在查表和登记时短占，建库迁移不占
	opening     singleflight.Group                        // 同一家厂的并发打开合并成一路，别的厂不等它
	failed      map[uuid.UUID]*openFailure                // 打开失败、正在退避的厂；打开成功即清掉，同受 mu 保护
	now         func() time.Time                          // 取当前时间算退避；测试可换成假钟
	presenceMu  sync.Mutex                                // 护着出站通道表，避免两路同时重连。
	adminDSN    string                                    // 维护库连接串，用来派生各厂的库。
	admin       *gorm.DB                                  // 维护库连接，只负责建厂和列厂。
	tenants     map[uuid.UUID]*tenant                     // 已经打开的厂，按身份直接复用。
	presence    map[uuid.UUID]context.CancelFunc          // 每厂一条出站通道
	syncReq     map[uuid.UUID]chan wanchannel.SyncRequest // 进页补拉，Hold 在线才有
	wanURL      string                                    // WAN 根地址，空则不连
	mqttURL     string                                    // WAN MQTT 地址
	run         context.Context                           // 进程生命周期，给通道重连用
	pendingSink service.PendingSink                       // 确认后落盘；空则只走夹具
	blobs       blob.Store                                // 软件包字节；空则各厂用内存
	clientMu    sync.Mutex                                // 护着本厂 Client Broker
	clientBus   clientBroker                              // 厂→Client MQTT；未起则为空
	demoData    atomic.Bool                               // 只开发版打开：放开写入演示焊次；默认关
	wan         wanStates                                 // 各厂到 WAN 通道的现状，给管理页横幅看
}

// New 连维护库（用来建厂库），不预先打开任何厂库。
func New(adminDSN string) (*Hub, error) {
	// 打开维护库，打不开就没有厂库可建。
	admin, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 维护库打不开就交回，调用方不要继续启动。
	if err != nil {
		return nil, err
	}
	// 取出底层连接，后面才能探活和关闭。
	sqlDB, err := admin.DB()
	// 拿不到连接就交回，避免留下半开的库。
	if err != nil {
		return nil, err
	}
	// 探活失败就拒绝启动，维护库现在不可用。
	if err := sqlDB.Ping(); err != nil {
		return nil, err
	}
	return &Hub{adminDSN: adminDSN, admin: admin, tenants: map[uuid.UUID]*tenant{}, failed: map[uuid.UUID]*openFailure{}, now: time.Now, presence: map[uuid.UUID]context.CancelFunc{}, syncReq: map[uuid.UUID]chan wanchannel.SyncRequest{}}, nil
}

// SetPendingSink 生产写入更新目录；须在打开厂库之前调用。
func (h *Hub) SetPendingSink(sink service.PendingSink) {
	// 先占住厂库表，避免并发改落盘位置。
	h.mu.Lock()
	// 离开时放开厂库锁。
	defer h.mu.Unlock()
	// 记下落盘位置，打开厂库之前必须已经挂上。
	h.pendingSink = sink
}

// SetBlobs 生产改用对象存储；须在打开厂库之前调用。
func (h *Hub) SetBlobs(store blob.Store) {
	// 先占住厂库表，避免并发改对象存储。
	h.mu.Lock()
	// 离开时放开厂库锁。
	defer h.mu.Unlock()
	// 记下对象存储，打开厂库之前必须已经挂上。
	h.blobs = store
}

// SetDemoData 开发版放开写入演示焊次；须在打开厂库之前调用。
func (h *Hub) SetDemoData(on bool) {
	// 记下开关，之后打开的厂按它放开或拒绝演示焊次。
	h.demoData.Store(on)
}

// DemoData 交回演示焊次开关，探活据此告诉页面要不要露入口。
func (h *Hub) DemoData() bool {
	// 原子读开关，探活不必去抢厂库锁。
	return h.demoData.Load()
}

// Ping 只确认维护库连接可用，给探活用；不打开任何厂库。
func (h *Hub) Ping(ctx context.Context) error {
	// 读维护库前先占住，避免读到换到一半的连接。
	h.mu.Lock()
	// 取出维护库连接，马上就可以放开锁。
	admin := h.admin
	// 读完就放开，探活不必一直占着锁。
	h.mu.Unlock()
	// 维护库还没连上就当厂不存在。
	if admin == nil {
		return domain.ErrNotFound
	}
	// 做一次探活查询，失败就告诉探活方。
	return admin.WithContext(ctx).Exec("SELECT 1").Error
}

// Enroll 为目标厂建库并按约定身份写入初始超管和自设密码；认领和测试夹具都走这里。
func (h *Hub) Enroll(ctx context.Context, factoryID, personID uuid.UUID, saLogin, saDisplay, password string) (*service.Service, error) {
	// 没有厂库就建并组装，失败则不能写初始超管。
	svc, err := h.ensure(factoryID)
	// 厂库打不开就交回，不写入初始超管。
	if err != nil {
		return nil, err
	}
	// 把初始超管落库，当场有效。
	if _, err := svc.Auth.AcceptEnrollment(ctx, personID, saLogin, saDisplay, password); err != nil {
		return nil, err
	}
	return svc, nil
}

// Service 打开已存在的厂库；库还不在就当作工厂未初始化。
func (h *Hub) Service(ctx context.Context, factoryID uuid.UUID) (*service.Service, error) {
	// 先看这家厂是否已经打开，避免重复开库。
	h.mu.Lock()
	// 已经打开就直接复用，不再迁移一次。
	if t, ok := h.tenants[factoryID]; ok {
		// 复用前先放开锁，调用方不必继续占着。
		h.mu.Unlock()
		return t.svc, nil
	}
	// 内存里没有就放开，再去看库是否已建。
	h.mu.Unlock()
	// 按工厂身份算出厂库名字。
	name := provision.DBName(factoryID)
	// 确认厂库是否已建，查失败就不要当成没有。
	ok, err := provision.Exists(h.admin, name)
	// 查库失败就交回，避免把故障当成厂不存在。
	if err != nil {
		return nil, err
	}
	// 库还不在就当工厂未初始化，不去新建。
	if !ok {
		return nil, domain.ErrNotFound
	}
	// 库已在就打开并组装服务，失败则没有服务可用。
	return h.ensure(factoryID)
}

// ensure 没有厂库就建并组装服务；已打开则复用。
// 同一家厂的并发打开合并成一路；建库、迁移不占 h.mu，慢迁移只挡这一家。
func (h *Hub) ensure(factoryID uuid.UUID) (*service.Service, error) {
	// 按厂身份合并并发打开，同一家只跑一路，别的厂各走各的。
	v, err, _ := h.opening.Do(factoryID.String(), func() (any, error) {
		// 真正去查表、建库迁移并登记这家厂。
		return h.open(factoryID)
	})
	// 打开失败就原样交回，退避记录已在 open 里记下。
	if err != nil {
		return nil, err
	}
	// 合并的各路拿到同一份服务。
	return v.(*service.Service), nil
}

// open 打开一家厂：只在查表和登记两处占 h.mu，建库、迁移和组装都在锁外做。
// 只由 ensure 经合并调用，同一家厂不会有两路同时进来。
func (h *Hub) open(factoryID uuid.UUID) (*service.Service, error) {
	// 占住厂库表，只看已打开和退避记录，并抄出组装要用的配置。
	h.mu.Lock()
	// 已经打开就复用，不再建第二份。
	if t, ok := h.tenants[factoryID]; ok {
		// 复用前先放开锁。
		h.mu.Unlock()
		return t.svc, nil
	}
	// 退避期内不再重开，直接交回上次的错，免得每个请求都重跑失败的迁移。
	if f, ok := h.failed[factoryID]; ok && h.now().Before(f.retry) {
		// 取出上次的错再放锁。
		err := f.err
		// 交回前先放开锁。
		h.mu.Unlock()
		return nil, err
	}
	// 抄出维护库、对象存储和更新目录，锁外组装时不再读共享字段。
	admin, blobs, sink := h.admin, h.blobs, h.pendingSink
	// 查完就放开，建库迁移期间别的厂和探活照常取锁。
	h.mu.Unlock()
	// 枢纽已关就当厂不存在，不再建库。
	if admin == nil {
		return nil, domain.ErrNotFound
	}
	// 按工厂身份算出这家厂的库名。
	name := provision.DBName(factoryID)
	// 没有厂库就建出来，建失败就不要打开半套。
	if err := provision.Ensure(admin, name); err != nil {
		// 记下失败并排好下次重试，原错误照旧交回。
		return nil, h.recordFailure(factoryID, err)
	}
	// 拼出这家厂的连接串，拼不出就不要去连。
	dsn, err := provision.SwapDB(h.adminDSN, name)
	// 连接串拼不出就交回，不要拿空地址去连。
	if err != nil {
		// 记下失败并排好下次重试，原错误照旧交回。
		return nil, h.recordFailure(factoryID, err)
	}
	// 打开并套用迁移，失败就不要对外服务；等锁、等表锁都在锁外。
	db, err := provision.OpenMigrated(dsn)
	// 迁移或打开失败就交回，不要带着半套库继续。
	if err != nil {
		// 只影响这一家：记下失败按退避重试，别的厂照常打开。
		return nil, h.recordFailure(factoryID, err)
	}
	// 按工厂身份打开本厂库并组装应用服务。
	svc := service.NewService(store.Open(db, factoryID))
	// 正在跑的版本算已装基线，新装厂不被提示装回同版或旧版。
	svc.Updates.SetRunningSoftware(release.Code, release.Name)
	// 演示焊次开关跟进程配置走，生产关着就一条都写不进。
	svc.Stats.SetDemoData(h.demoData.Load())
	// 已经挂了对象存储才交给这家厂。
	if blobs != nil {
		// 软件包字节走对象存储，不进进程内存。
		svc.SetBlobs(blobs)
	}
	// 已经挂了更新目录才交给这家厂。
	if sink != nil {
		// 确认后的安装包落到更新目录。
		svc.Updates.SetPendingSink(sink)
		// 能回报换装结果的才记上，供启动时补记。
		if r, ok := sink.(service.ApplyReporter); ok {
			// 能回报换装结果的才记上，供启动时补记。
			svc.Updates.SetApplyReporter(r)
		}
		// 能清理旧镜像的才记上，避免磁盘被占满。
		if j, ok := sink.(service.ImageJanitor); ok {
			// 能清理旧镜像的才记上，避免磁盘被占满。
			svc.Updates.SetImageJanitor(j)
		}
		// 更新器若已经换完，把库里的包记成已装。
		h.completeStaged(svc)
	}
	// 读平台地址前先占住通道表。
	h.presenceMu.Lock()
	// 取出平台地址，空的就不挂拉包源。
	wanURL := h.wanURL
	// 读完地址就放开通道锁。
	h.presenceMu.Unlock()
	// 有签发钥并且配了平台，才挂上拉包源。
	if k, err := svc.Store().SigningKey(context.Background()); err == nil && wanURL != "" {
		// 用厂钥把拉包源挂到这家厂。
		h.attachSoftwareSource(factoryID, svc, wanURL, k.PrivateKey)
	}
	// 让这家厂能向本机平板下发。
	svc.SetClientDown(h)
	// 让这家厂能把焊汇总送到平台。
	svc.SetWeldWANPoster(h)
	// 组装完再占住厂库表登记。
	h.mu.Lock()
	// 离开时放开，登记只占这一小段。
	defer h.mu.Unlock()
	// 拿回锁再查一次：已有登记就用它，这份库关掉，免得登记两份。
	if t, ok := h.tenants[factoryID]; ok {
		// 关掉多开的这份连接池。
		closeDB(db)
		return t.svc, nil
	}
	// 组装期间枢纽已关就不登记，连接池关掉免得泄漏。
	if h.admin == nil {
		// 关掉刚开的连接池。
		closeDB(db)
		return nil, domain.ErrNotFound
	}
	// 打开成功就清掉这家的失败记录，探活不再列它。
	delete(h.failed, factoryID)
	// 上送循环跟着这家厂的库走，不跟 h.run：没配 WAN 时 h.run 一直是空。
	pushCtx, stopPush := context.WithCancel(context.Background())
	// 循环退出时关闭，关库方据此等它。
	pushDone := make(chan struct{})
	// 后台串行上送焊汇总，汇聚接口不必等 WAN。
	go func() {
		// 循环退出就通知关库方。
		defer close(pushDone)
		// 收到「已变」信号才推，关库前被取消。
		svc.Stats.RunWANPush(pushCtx)
	}()
	// 记下来，下次按身份直接复用这份服务。
	h.tenants[factoryID] = &tenant{db: db, svc: svc, stopPush: stopPush, pushDone: pushDone}
	return svc, nil
}

// recordFailure 锁外打开失败后占住厂库表记下退避，原错误照旧交回。
func (h *Hub) recordFailure(factoryID uuid.UUID, err error) error {
	// 记退避要改失败表，先占住厂库表。
	h.mu.Lock()
	// 记完就放开。
	defer h.mu.Unlock()
	// 记下失败并排好下次重试。
	return h.openFailed(factoryID, err)
}

// closeDB 关掉一份没登记上的厂库连接池，关不掉也不盖过主流程。
func closeDB(db *gorm.DB) {
	// 取出底层连接池，能取到才关。
	if sqlDB, err := db.DB(); err == nil {
		// 关掉这份连接池。
		_ = sqlDB.Close()
	}
}

// openFailed 记下这家厂打开失败并排好下次重试；调用方须占着 h.mu。原错误照旧交回。
func (h *Hub) openFailed(factoryID uuid.UUID, err error) error {
	// 取当前时间，失败时间和下次重试都从这里算。
	now := h.now()
	// 首次失败先等最短的一档。
	wait := openBackoffMin
	// 上次也失败过就翻倍，封顶不再加。
	if f, ok := h.failed[factoryID]; ok {
		// 翻倍后与封顶取小。
		wait = min(f.wait*2, openBackoffMax)
	}
	// 记下这次失败，退避期内直接交回它。
	h.failed[factoryID] = &openFailure{err: err, at: now, wait: wait, retry: now.Add(wait)}
	// 原错误只进日志，探活只报文件名或大类。
	slog.Warn("open factory db failed, will retry", "factory", factoryID, "wait", wait, "err", err)
	return err
}

// SchemaState 探活看的各厂库结构：已打开的报迁移版本，打开失败的报退避。
type SchemaState struct {
	Factories []FactorySchema  `json:"factories"` // 已打开的厂库及其迁移版本，按厂身份排序
	Failing   []FactoryFailure `json:"failing"`   // 最近一次打开失败、尚未重开成功的厂
}

// FactorySchema 一家已打开厂库的迁移版本。
type FactorySchema struct {
	FactoryID uuid.UUID `json:"factoryId"` // 工厂稳定身份
	Version   int64     `json:"version"`   // 已套到的迁移版本；读不出时为 -1
}

// FactoryFailure 一家打开失败的厂；只给失败的迁移文件或大类原因，不带库原文。
type FactoryFailure struct {
	FactoryID uuid.UUID `json:"factoryId"`      // 工厂稳定身份
	Reason    string    `json:"reason"`         // migrate=迁移没做完；open=建库或连库失败
	File      string    `json:"file,omitempty"` // 没套上的迁移文件；说不清是哪支时为空
	FailedAt  time.Time `json:"failedAt"`       // 最近一次失败时间
	RetryAt   time.Time `json:"retryAt"`        // 到这个时间之后，下次有人访问才再试
}

// SchemaStatus 交回各厂库迁移版本和打开失败的厂，给探活用；不打开任何新厂库。
func (h *Hub) SchemaStatus(ctx context.Context) SchemaState {
	// 先占住厂库表，抄出要看的库和失败记录。
	h.mu.Lock()
	// 按家数预留已打开的库，放锁后再去读版本。
	open := make(map[uuid.UUID]*gorm.DB, len(h.tenants))
	// 逐家抄下库连接，读版本不必占着锁。
	for id, t := range h.tenants {
		// 记下这家的库连接。
		open[id] = t.db
	}
	// 按家数预留失败记录，空的也回空数组。
	failing := make([]FactoryFailure, 0, len(h.failed))
	// 逐家抄下失败记录；过了重试点但还没人来重开的也照列。
	for id, f := range h.failed {
		// 先按建库或连库失败算，认出迁移错误再改。
		row := FactoryFailure{FactoryID: id, Reason: "open", FailedAt: f.at, RetryAt: f.retry}
		// 准备接迁移错误，里面带着失败的文件名。
		var me *migrate.Error
		// 是迁移没做完就报文件名，库原文不外露。
		if errors.As(f.err, &me) {
			// 标成迁移失败并带上文件名。
			row.Reason, row.File = "migrate", me.File
		}
		// 收下这一家。
		failing = append(failing, row)
	}
	// 抄完就放开，读版本时别的厂照常打开。
	h.mu.Unlock()
	// 按家数预留版本结果，空的也回空数组。
	factories := make([]FactorySchema, 0, len(open))
	// 逐家读迁移版本。
	for id, db := range open {
		// 带上探活的限时去读。
		v, err := migrate.Version(db.WithContext(ctx))
		// 读不出就标 -1，原因只进日志。
		if err != nil {
			// 记下读不出的原因，探活里不写库原文。
			slog.Warn("read factory schema version", "factory", id, "err", err)
			// 标成读不出。
			v = -1
		}
		// 收下这一家的版本。
		factories = append(factories, FactorySchema{FactoryID: id, Version: v})
	}
	// 按厂身份排序，探活输出稳定好比对。
	sort.Slice(factories, func(i, j int) bool {
		// 按身份文本从小到大。
		return factories[i].FactoryID.String() < factories[j].FactoryID.String()
	})
	// 失败的厂也按身份排序。
	sort.Slice(failing, func(i, j int) bool {
		// 按身份文本从小到大。
		return failing[i].FactoryID.String() < failing[j].FactoryID.String()
	})
	return SchemaState{Factories: factories, Failing: failing}
}

// SiteFactory 是本机已认领的一家工厂，给登录页免填 UUID。
type SiteFactory struct {
	ID        uuid.UUID `json:"id"`                  // 工厂稳定身份
	Name      string    `json:"name,omitempty"`      // 本厂显示名
	ShortCode string    `json:"shortCode,omitempty"` // 本厂短码，给人认厂
	SALogin   string    `json:"saLogin"`             // 初始超管登录名，方便认领后登录
	Status    string    `json:"status"`              // 本厂治理状态：active / disabled / retired
}

// ListSite 列出本机已有厂库；没有 WAN 名录。
func (h *Hub) ListSite(ctx context.Context) ([]SiteFactory, error) {
	// 列出本机厂库，列不出登录页就没有厂可选。
	ids, err := provision.ListFactoryIDs(h.admin)
	// 列厂失败就交回，不要把故障显示成没有厂。
	if err != nil {
		return nil, err
	}
	// 按厂数预留登录页结果，避免中途丢厂。
	out := make([]SiteFactory, 0, len(ids))
	// 逐个厂库补上名称、短码和治理状态。
	for _, id := range ids {
		// 打开这家厂，打不开就跳过，不挡别的厂。
		svc, err := h.Service(ctx, id)
		// 这家打不开就跳过，登录页仍显示其他厂。
		if err != nil {
			continue
		}
		// 读初始账号，读不到就先留空登录名。
		p, err := svc.Store().InitialPerson(ctx)
		// 先留空登录名，读到初始账号再填。
		login := ""
		if err == nil {
			// 把初始超管登录名留给登录页。
			login = p.LoginName
		}
		// 先当启用，读到治理状态再改。
		status := store.FactoryActive
		// 先留空厂名，读到治理记录再填。
		name := ""
		shortCode := ""
		// 读本厂已落地的治理状态，给登录页提示停用/注销。
		if lc, err := svc.Store().Lifecycle(ctx); err == nil {
			// 用已落地的治理状态，登录页才能提示停用。
			status = lc.Status
			// 用已落地的厂名给登录页显示。
			name = lc.Name
			// 用已落地的短码，方便人认厂。
			shortCode = lc.ShortCode
		}
		// 把这家厂放进登录页列表。
		out = append(out, SiteFactory{ID: id, Name: name, ShortCode: shortCode, SALogin: login, Status: status})
	}
	return out, nil
}

// Claim 用建厂码向 WAN 认领本厂：落库初始超管并当场激活，再登记签发公钥。
func (h *Hub) Claim(ctx context.Context, wanURL, enrollmentCode, password string) (SiteFactory, error) {
	// 没有平台地址就拒绝认领，避免去拨空主机。
	if wanURL == "" {
		return SiteFactory{}, domain.ErrWANUnreachable
	}
	// 用建厂码换身份，换不到就不要建库。
	offer, err := wanchannel.Enroll(ctx, wanURL, enrollmentCode)
	// 建厂码无效或平台拒绝就交回，不建厂库。
	if err != nil {
		return SiteFactory{}, err
	}

	// 按换到的身份建厂库并落初始超管，失败则认领未完成。
	svc, err := h.Enroll(ctx, offer.FactoryID, offer.SAPersonID, offer.SALogin, offer.SADisplay, password)
	// 厂库建不开或超管落不下就交回。
	if err != nil {
		return SiteFactory{}, err
	}
	// 平台给了短码才落库，方便认厂。
	if offer.ShortCode != "" {
		// 短码写不进厂库就拒绝这次认领。
		if err := svc.Store().PutFactoryShortCode(ctx, offer.ShortCode); err != nil {
			return SiteFactory{}, err
		}
	}
	// 厂名写不进厂库就拒绝这次认领。
	if err := svc.Store().PutFactoryName(ctx, offer.Name); err != nil {
		return SiteFactory{}, err
	}
	// 取出或生成签发公钥，没有钥不能确认。
	pub, _, err := h.signingMaterial(ctx, svc)
	// 签发钥准备失败就先不确认，建厂码留着。
	if err != nil {
		return SiteFactory{}, err
	}
	// 把公钥交给平台并作废建厂码，结果等接上出站后再交回。
	confirmErr := wanchannel.Confirm(ctx, wanURL, enrollmentCode, pub)
	// 签发钥已落本厂就接上出站，回包丢了平台已登记也能连上；未登记则按未授权长退避空转。
	h.ensureChannel(offer.FactoryID)
	// 确认失败照旧交回原错误，页面据此提示重试。
	if confirmErr != nil {
		return SiteFactory{}, confirmErr
	}
	return SiteFactory{ID: offer.FactoryID, Name: offer.Name, ShortCode: offer.ShortCode, SALogin: offer.SALogin, Status: store.FactoryActive}, nil
}

// 已有签发钥就用；没有则生成并写入本厂库。
func (h *Hub) signingMaterial(ctx context.Context, svc *service.Service) (pub, priv []byte, err error) {
	// 已有签发钥就用，没有再生成新的。
	k, err := svc.Store().SigningKey(ctx)
	// 已经有钥就直接用，不再生成第二把。
	if err == nil {
		return k.PublicKey, k.PrivateKey, nil
	}
	// 不是没有钥的失败就交回，避免把读失败当成该新建。
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, nil, err
	}
	// 本厂还没有签发钥时现生成一对。
	pub, priv, err = nodekey.Generate()
	// 钥生不出来就交回，不能去确认认领。
	if err != nil {
		return nil, nil, err
	}
	// 本厂还没有签发钥时生成并写入。
	if err := svc.Node.InstallSigningKey(ctx, pub, priv); err != nil {
		return nil, nil, err
	}
	return pub, priv, nil
}

// Drop 关掉该厂连接并删库；仅测试回收。
func (h *Hub) Drop(factoryID uuid.UUID) error {
	// 先停出站，避免删库时还在重连。
	h.stopChannel(factoryID)
	// 占住厂库表，取出并摘掉这家的登记。
	h.mu.Lock()
	// 取出这家厂，内存里没开过就不用关。
	t, ok := h.tenants[factoryID]
	// 从内存拿掉，避免还能访问已删的库。
	delete(h.tenants, factoryID)
	// 失败记录一并清掉，探活不再列已删的厂。
	delete(h.failed, factoryID)
	// 登记清完就放开厂库锁，上送循环退出时还要取它。
	h.mu.Unlock()
	// 内存里开过就先停上送循环，再关这家的连接。
	if ok {
		// 停循环、等它退出，再关库。
		t.close()
	}
	// 删掉厂库，只在测试回收时使用。
	return provision.Drop(h.admin, provision.DBName(factoryID))
}

// Close 关掉已打开的厂库和维护库连接。
func (h *Hub) Close() {
	// 先占住通道表，再逐家取消重连。
	h.presenceMu.Lock()
	// 取消每家出站，避免进程退出后还在拨。
	for id, cancel := range h.presence {
		// 取消这一家的重连。
		cancel()
		// 清掉登记，避免退出过程中又被拉起。
		delete(h.presence, id)
	}
	// 通道都取消后放开这把锁。
	h.presenceMu.Unlock()
	// 占住设备通道，退出时关掉监听。
	h.clientMu.Lock()
	// 拉起过才关，没拉起就不用动。
	if h.clientBus != nil {
		// 关掉设备通道，平板不应再连上这个进程。
		_ = h.clientBus.Close()
		// 清掉句柄，避免退出后还被拿去下发。
		h.clientBus = nil
	}
	// 关完就放开设备通道锁。
	h.clientMu.Unlock()
	// 占住厂库表，先把各家摘下来。
	h.mu.Lock()
	// 按家数预留，摘下来的放锁外再关。
	open := make([]*tenant, 0, len(h.tenants))
	// 逐家摘掉登记，进程里不再留这家厂。
	for id, t := range h.tenants {
		// 收下这家，放锁后再停循环关库。
		open = append(open, t)
		// 从内存拿掉，进程里不再留这家厂。
		delete(h.tenants, id)
	}
	// 放开厂库锁，上送循环退出时还要取它。
	h.mu.Unlock()
	// 逐家停上送循环再关库。
	for _, t := range open {
		// 停循环、等它退出，再关这家厂的库连接。
		t.close()
	}
	// 再占住厂库表，关维护库。
	h.mu.Lock()
	// 关完维护库再放开厂库锁。
	defer h.mu.Unlock()
	// 维护库还在就关掉，避免连接泄漏。
	if h.admin != nil {
		// 取出维护库的底层连接。
		if sqlDB, err := h.admin.DB(); err == nil {
			// 关掉维护库连接，避免退出后还占着。
			_ = sqlDB.Close()
		}
		// 清掉维护库句柄，避免退出后还被使用。
		h.admin = nil
	}
}
