// Package service 是 WAN 侧入口：唯一管理员、工厂名录、现场设备分配和初始化交付。
// 不代建厂内普通账号、组织或角色，也不直连 SQL。
// 按域分类型：Auth / Factories / Channel / Clients / Assets / Templates / Closure / Updates / Stats；本文件只组装。
package service

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/blob"
)

// 唯一管理员的登录失败总闸：每三秒补一次，最多连错二十次。
const (
	loginFailEvery = 3 * time.Second // 失败名额每三秒补一个
	loginFailBurst = 20              // 最多连着失败二十次
)

// kernel 是各域共用的 WAN 库、对象存根和审计。
type kernel struct {
	store         *Store        // 本侧库连接，不在这里拼 SQL。
	blobs         blob.Store    // 软件包字节；不进库、不进审计
	bus           CmdBus        // 厂端 MQTT 指令；测试可不挂
	applyOutcome  ApplyOutcome  // 确认后是否立刻记已装；测试可改
	pendingSink   PendingSink   // 确认后落盘；空则只走夹具
	applyReporter ApplyReporter // 读 updater 进度；测试可空
	applyMu       sync.Mutex    // 串起落地失败的查重和写入，同一次结果只记一条
	runningCode   int64         // 本进程云端服务版本号；已装基线不低于它，测试可零
	runningName   string        // 本进程云端服务版本名；库里没这版发布行时给人看
	imageJanitor  ImageJanitor  // 请 updater 清无用镜像；测试可空

	blobMu      sync.Mutex           // 护着已核摘要表，拉包和发布会并发改
	blobChecked map[string]blobCheck // 对象键到已核摘要和长度，每个对象只整包核一次
	blobFly     singleflight.Group   // 同一对象首次核摘要只跑一路，别的等它
}

// ImageJanitor 把清镜像请求交给 updater，并读本机占用；不在业务进程里碰 docker。
type ImageJanitor interface {
	// 请更新器清掉这个镜像引用。
	RequestPrune(ref string) error
	// 读取清镜像结果，还没有就不算完成。
	PruneResult() (reclaimed string, ok bool, present bool, err error)
	// 读取本机镜像清单，这里不碰 docker。
	ImagesJSON() ([]byte, error)
}

// PendingSink 把已确认的 app 包落到更新目录，供 updater 来换。
type PendingSink interface {
	// 把已确认的包落到更新目录待切换。
	Stage(kind string, version int64, digest, body []byte) error
}

// SetPendingSink 生产写入更新目录；测试可空。
func (s *Updates) SetPendingSink(sink PendingSink) { s.pendingSink = sink }

// ApplyReporter 读本机待切换与 updater 结果，不换容器。
type ApplyReporter interface {
	// 读取待切换阶段和失败原因。
	Progress() (phase, kind string, version int64, errMsg string, err error)
}

// SetApplyReporter 生产读更新目录；测试可空。
func (s *Updates) SetApplyReporter(r ApplyReporter) { s.applyReporter = r }

// SetImageJanitor 生产写清镜像请求；测试可空。
func (s *Updates) SetImageJanitor(j ImageJanitor) { s.imageJanitor = j }

// SetBlobs 生产改用对象存储；测试保持内存。
func (k *kernel) SetBlobs(store blob.Store) {
	// 没传入存储就保持原样，测试内存库不被清掉。
	if store == nil {
		return
	}
	// 换上对象存储，软件包字节不再进内存夹具。
	k.blobs = store
	// 换了存储，旧的核对记录不再算数。
	k.forgetBlobs()
}

// Auth 管 WAN 管理员登录、会话和改密码。
type Auth struct {
	*kernel                  // 登录和会话共用的库与审计。
	loginFails *rate.Limiter // 唯一管理员的登录失败总闸，只在失败时扣
}

// Factories 管工厂名录、建厂和明确拒绝的代管入口。
type Factories struct {
	*kernel // 名录和建厂共用的库与审计。
}

// Channel 在 channel.go。

// Clients 管现场设备名录与分厂。
type Clients struct {
	*kernel // 设备名录共用的库与审计。
}

// Assets 管平台级工艺/工程。
type Assets struct {
	*kernel // 工艺工程共用的库与审计。
}

// Templates 管工艺/工程字段模版。
type Templates struct {
	*kernel // 字段模版共用的库与审计。
}

// Closure 管平台级组包与下发授权。
type Closure struct {
	*kernel // 组包下发共用的库与审计。
}

// Stats 在 stats.go。

// Service 组装各域；HTTP 调 svc.Auth / svc.Factories，验收仍可走提升方法。
type Service struct {
	*kernel    // 各域共用的库、对象存根和审计。
	*Auth      // 管理员登录、会话和改密。
	*Factories // 工厂名录、建厂和拒绝代管。
	*Channel   // 认领、在线、心跳和租约。
	*Clients   // 现场设备名录和分厂。
	*Assets    // 平台级工艺和工程。
	*Templates // 工艺和工程的字段模版。
	*Closure   // 组包、授权和下发。
	*Updates   // 软件包发布、拉取和确认。
	*Stats     // 各厂上送的焊汇总。
}

// NewService 组装 WAN 应用服务。建厂不再反打厂内网。
func NewService(st *Store) *Service {
	// 测试用内存对象库，字节不进数据库。
	k := &kernel{store: st, blobs: blob.NewMemory()}
	return &Service{
		kernel:    k,
		Auth:      &Auth{kernel: k, loginFails: rate.NewLimiter(rate.Every(loginFailEvery), loginFailBurst)},
		Factories: &Factories{k},
		Channel:   &Channel{k},
		Clients:   &Clients{k},
		Assets:    &Assets{k},
		Templates: &Templates{k},
		Closure:   &Closure{k},
		Updates:   &Updates{k},
		Stats:     &Stats{k},
	}
}

// Store 取出本侧库连接，给验收夹具用。
func (k *kernel) Store() *Store { return k.store }

// Ping 只确认 WAN 库可达，给探活用。
func (k *kernel) Ping(ctx context.Context) error {
	// 探一下库是否可达。
	return k.store.Ping(ctx)
}

// AdminExists 给进程启动判断是否还需引导，避免每次重启都留一条拒绝审计。
func (k *kernel) AdminExists(ctx context.Context) (bool, error) {
	// 按管理员处理，失败就不能继续。
	n, err := k.store.AdminCount(ctx)
	return n > 0, err
}

// ListAudit 读 WAN 审计行，不含秘密原文。
func (k *kernel) ListAudit(ctx context.Context) ([]audit.Row, error) {
	// 列出这一批供后面筛选。
	return k.store.ListAudit(ctx)
}

// HasTable 查迁移是否已建该表，给夹具用。
func (k *kernel) HasTable(ctx context.Context, name string) (bool, error) {
	// 看是否已经具备。
	return k.store.HasTable(ctx, name)
}

// audit 写审计行，不含秘密原文。
func (k *kernel) audit(ctx context.Context, actor *uuid.UUID, claimed *string, factoryID *uuid.UUID, action, target, result string) error {
	// 追加审计，失败就不能继续。
	return k.store.AppendAudit(ctx, audit.Event{
		ActorID:      actor,
		ClaimedLogin: claimed,
		FactoryID:    factoryID,
		Action:       action,
		Target:       target,
		Result:       result,
		TimeSource:   audit.Server,
	})
}
