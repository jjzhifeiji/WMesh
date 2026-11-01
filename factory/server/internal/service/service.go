// Package service 是厂内应用入口：认证、允许/拒绝和审计。
// 不管 WAN 管理员，也不直连 SQL，不把本厂密码送到 WAN。
// 按域分类型：Auth / Org / Attr / Node / Assets / Templates / Closure / Sync / Updates / Stats；本文件只组装。
package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/blob"
)

// kernel 是各域共用的本厂库与审计；不对外当业务入口。
type kernel struct {
	store          *Store                   // 本厂库连接，业务不在这里拼语句
	blobs          blob.Store               // 上传正文与软件包；不进库、不进审计
	applyOutcome   ApplyOutcome             // 确认后是否立刻记已装；测试可改
	softwareSource SoftwareSource           // 厂→WAN 拉包；空则只能夹具 Ingest
	pendingSink    PendingSink              // 确认后落盘；空则只走夹具
	applyReporter  ApplyReporter            // 读 updater 进度；测试可空
	applyMu        sync.Mutex               // 串起落地失败的查重和写入，同一次结果只记一条
	runningCode    int64                    // 本进程厂服务版本号；已装基线不低于它，测试可零
	runningName    string                   // 本进程厂服务版本名；库里没这版副本时给人看
	imageJanitor   ImageJanitor             // 请 updater 清无用镜像；测试可空
	clientDown     ClientDown               // 厂→Client MQTT；空则只留下发记录
	wanWeld        WeldWANPoster            // 把无人员组织的焊汇总交给 WAN；空则不上送
	weldDirty      chan struct{}            // 焊汇总已变的信号，容量 1，多次变化合并成一次上送
	wanRetryMin    time.Duration            // 后台上送退避起点，连续失败才加倍
	wanRetryMax    time.Duration            // 后台上送退避封顶，WAN 久断也不拖太久
	mqttMu         sync.Mutex               // 护着示教器在线计数，避免并发写乱
	mqttOnline     map[uuid.UUID]int        // 本厂示教器 MQTT 连接数；>0 才算在线
	softwareMu     sync.Mutex               // 护着同版本拉包单飞
	softwareIn     map[string]*softwareWait // 正在拉的（种类/版本）
	upRejects      atomic.Uint64            // 被拒或解不开的本机上行条数，只计数不写审计

	blobMu      sync.Mutex           // 护着已核摘要表，收包、查包和下载会并发改
	blobChecked map[string]blobCheck // 对象键到已核摘要和长度，每个对象只整包核一次
	blobFly     singleflight.Group   // 同一对象首次核摘要只跑一路，别的等它

	logins loginLimiter // 登录失败限速，网页和示教器登录共用
}

// softwareWait 等同版本那一次拉完，避免并发各下一份。
type softwareWait struct {
	done chan struct{} // 同版本拉完就关闭，等待方被叫醒
	err  error         // 这一次拉包的失败原因，成功则为空
}

// PendingSink 把已确认的 app 包落到更新目录，供 updater 来换。
type PendingSink interface {
	// 把已确认的包落到更新目录，失败则换不了版本
	Stage(kind string, version int64, digest, body []byte) error
}

// ClientDown 向指定本机投控制面小信封，不含正文。
type ClientDown interface {
	// 向这台本机投控制面信封，不含正文
	PublishDown(factoryID, clientID uuid.UUID, payload []byte)
}

// Auth 管登录、激活、会话、停用和密码重置。
type Auth struct {
	*kernel // 共用本厂库、审计和在线计数
}

// Org 管人员、组织、角色和名册。
type Org struct {
	*kernel // 共用本厂库、审计和在线计数
}

// Node 管本厂已分配的设备和运行凭证。
type Node struct {
	*kernel // 共用本厂库、审计和在线计数
}

// Assets 管本厂工艺/工程治理。
type Assets struct {
	*kernel // 共用本厂库、审计和在线计数
}

// Templates 管已收的工艺/工程字段模版。
type Templates struct {
	*kernel // 共用本厂库、审计和在线计数
}

// Closure 管组包、下发、缓存和平台级副本。
type Closure struct {
	*kernel // 共用本厂库、审计和在线计数
}

// Sync 管上传入队与回连汇聚。
type Sync struct {
	*kernel // 共用本厂库、审计和在线计数
}

// Service 组装各域；HTTP 调 svc.Auth / svc.Org，验收仍可走提升方法。
type Service struct {
	*kernel    // 共用本厂库、审计和在线计数
	*Auth      // 登录、激活、会话和口令
	*Org       // 人员、组织、角色和名册
	*Node      // 设备绑定、登录和运行凭证
	*Assets    // 本厂工艺和工程治理
	*Templates // 已收到的工艺工程模版
	*Closure   // 组包、下发、缓存和副本
	*Sync      // 上传入队和回连汇聚
	*Updates   // 厂服务包和客户端包更新
	*Stats     // 焊长时长汇聚和厂端报表
}

// ApplyReporter 读本机待切换与 updater 结果，不换容器。
type ApplyReporter interface {
	// 读待切换阶段和报错，业务进程不换容器
	Progress() (phase, kind string, version int64, errMsg string, err error)
}

// SetApplyReporter 生产读更新目录；测试可空。
func (s *Updates) SetApplyReporter(r ApplyReporter) { s.applyReporter = r }

// ImageJanitor 把清镜像请求交给 updater，并读本机占用；不在业务进程里碰 docker。
type ImageJanitor interface {
	// 请更新器清掉这份无用镜像
	RequestPrune(ref string) error
	// 读上次清理收回多少，以及结果是否已到
	PruneResult() (reclaimed string, ok bool, present bool, err error)
	// 读本机镜像清单，业务进程不碰容器
	ImagesJSON() ([]byte, error)
}

// SetImageJanitor 生产写清镜像请求；测试可空。
func (s *Updates) SetImageJanitor(j ImageJanitor) { s.imageJanitor = j }

// SetBlobs 生产改用对象存储；测试保持内存。
func (k *kernel) SetBlobs(store blob.Store) {
	// 没换存储则保持原来的内存实现
	if store == nil {
		return
	}
	// 换成对象存储，之后上传正文不再进内存
	k.blobs = store
	// 换了存储，旧的核对记录不再算数
	k.forgetBlobs()
}

// NewService 组装厂内应用服务；调用方先打开这一家厂库。
func NewService(st *Store) *Service {
	// 正文先放内存，生产再换对象存储；焊汇总信号留一格，后台上送按默认退避
	k := &kernel{
		store: st, blobs: blob.NewMemory(), weldDirty: make(chan struct{}, 1),
		wanRetryMin: wanPushRetryMin, wanRetryMax: wanPushRetryMax,
	}
	return &Service{
		kernel:    k,
		Auth:      &Auth{kernel: k},
		Org:       &Org{k},
		Node:      &Node{k},
		Assets:    &Assets{k},
		Templates: &Templates{k},
		Closure:   &Closure{k},
		Sync:      &Sync{k},
		Updates:   &Updates{k},
		Stats:     &Stats{kernel: k},
	}
}

// Store 取出本厂库连接，给验收夹具用。
func (k *kernel) Store() *Store { return k.store }

// SetClientDown 注入厂→Client 下行；测试可假实现。
func (s *Service) SetClientDown(p ClientDown) {
	// 接上厂到本机的下行，空则只留下发记录
	s.kernel.clientDown = p
}

// SetWeldWANPoster 注入厂→WAN 焊汇总上送；测试可假实现。
func (s *Service) SetWeldWANPoster(p WeldWANPoster) {
	// 接上焊汇总上送，空则厂内仍记账但不到广域
	s.kernel.wanWeld = p
}

// Account 是对外可见的账号视图，不含密码或激活码。
type Account struct {
	ID              uuid.UUID  `json:"id"`                      // 稳定身份，改名也不变
	LoginName       string     `json:"loginName"`               // 厂内登录名，一厂唯一
	DisplayName     string     `json:"displayName"`             // 显示名，可改
	Status          string     `json:"status"`                  // 人员状态：active / disabled
	OrgUnitID       *uuid.UUID `json:"orgUnitId"`               // 所属组织；空表示厂直属
	AppOnline       bool       `json:"appOnline"`               // 示教器 MQTT 连着才算在线；管理端登录不算
	AppLastSeenAt   *time.Time `json:"appLastSeenAt,omitempty"` // 最近一次示教器登录或 MQTT 见到
	AppVersion      int64      `json:"appVersion"`              // 示教器自报 versionCode；离线仍留上次
	AppVersionName  string     `json:"appVersionName"`          // 示教器自报 versionName
	AppClientName   string     `json:"appClientName"`           // 最近一次登录用过的设备名；没有则为空
	AppDeviceSerial string     `json:"appDeviceSerial"`         // 最近一次登录用过的识别号；没有则为空
	KeepPouch       bool       `json:"keepPouch"`               // 退出后是否保留示教器库文件；默认留
}

// 对外账号视图，不含密码；在线标记由名册另填。
func accountOf(p Person) Account {
	return Account{
		ID: p.ID, LoginName: p.LoginName, DisplayName: p.DisplayName, Status: p.Status, OrgUnitID: p.OrgUnitID,
		AppLastSeenAt: p.AppLastSeenAt, AppVersion: p.AppVersion, AppVersionName: p.AppVersionName,
		KeepPouch: p.KeepPouch,
	}
}

// ListAudit 列出本厂审计行，给验收用。
func (k *kernel) ListAudit(ctx context.Context) ([]audit.Row, error) {
	// 列出本厂审计行给验收核对，再交回调用方
	return k.store.ListAudit(ctx)
}

// audit 记下允许或拒绝；令牌和密码不进原文。
func (k *kernel) audit(ctx context.Context, actor *uuid.UUID, claimed *string, action, target, result string) error {
	// 取出本厂稳定身份，封包和审计都要用
	fid := k.store.FactoryID()
	// 把允许或拒绝写入审计，不含口令
	return k.store.AppendAudit(ctx, audit.Event{
		ActorID:      actor,
		ClaimedLogin: claimed,
		FactoryID:    &fid,
		Action:       action,
		Target:       target,
		Result:       result,
		TimeSource:   audit.Server,
	})
}
