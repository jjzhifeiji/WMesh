// Package hub 按工厂身份打开对应厂库并组装应用服务。
// 不认 WAN 名录，不把本厂密码送到 WAN。
package hub

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/factory/internal/platform/blob"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
	"wmesh/factory/internal/platform/provision"
	"wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
	"wmesh/factory/internal/wanchannel"
)

// 一家厂已打开的库和应用服务。
type tenant struct {
	db  *gorm.DB         // 这家厂已经打开的库连接。
	svc *service.Service // 这家厂已经组装好的应用服务。
}

// Hub 按工厂稳定身份选库；没有库就当作工厂还不存在。
type Hub struct {
	mu          sync.Mutex                                // 护着已打开的厂库表，避免并发开出两份。
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
	return &Hub{adminDSN: adminDSN, admin: admin, tenants: map[uuid.UUID]*tenant{}, presence: map[uuid.UUID]context.CancelFunc{}, syncReq: map[uuid.UUID]chan wanchannel.SyncRequest{}}, nil
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

// Bootstrap 为目标厂建库并写入待启用初始超管；激活码只返回给调用方。
func (h *Hub) Bootstrap(ctx context.Context, factoryID uuid.UUID, saLogin, saDisplay string) (uuid.UUID, string, error) {
	// 没有厂库就建并组装，失败则不能写初始超管。
	svc, err := h.ensure(factoryID)
	// 厂库打不开就交回，不写入初始超管。
	if err != nil {
		return uuid.Nil, "", err
	}
	// 写入待启用初始超管，激活码只交回调用方。
	acc, token, err := svc.BootstrapInitial(ctx, saLogin, saDisplay)
	// 初始超管写不进去就交回，不把空账号当成功。
	if err != nil {
		return uuid.Nil, "", err
	}
	return acc.ID, token, nil
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

// 没有厂库就建并组装服务；已打开则复用。
func (h *Hub) ensure(factoryID uuid.UUID) (*service.Service, error) {
	// 占住厂库表，避免两路同时建同一家。
	h.mu.Lock()
	// 离开时放开，别的厂才能继续打开。
	defer h.mu.Unlock()
	// 已经打开就复用，不再建第二份。
	if t, ok := h.tenants[factoryID]; ok {
		return t.svc, nil
	}
	// 按工厂身份算出这家厂的库名。
	name := provision.DBName(factoryID)
	// 没有厂库就建出来，建失败就不要打开半套。
	if err := provision.Ensure(h.admin, name); err != nil {
		return nil, err
	}
	// 拼出这家厂的连接串，拼不出就不要去连。
	dsn, err := provision.SwapDB(h.adminDSN, name)
	// 连接串拼不出就交回，不要拿空地址去连。
	if err != nil {
		return nil, err
	}
	// 打开并套用迁移，失败就不要对外服务。
	db, err := provision.OpenMigrated(dsn)
	// 迁移或打开失败就交回，不要带着半套库继续。
	if err != nil {
		return nil, err
	}
	// 按工厂身份打开本厂库并组装应用服务。
	svc := service.NewService(store.Open(db, factoryID))
	// 已经挂了对象存储才交给这家厂。
	if h.blobs != nil {
		// 软件包字节走对象存储，不进进程内存。
		svc.SetBlobs(h.blobs)
	}
	// 已经挂了更新目录才交给这家厂。
	if h.pendingSink != nil {
		// 确认后的安装包落到更新目录。
		svc.Updates.SetPendingSink(h.pendingSink)
		// 能回报换装结果的才记上，供启动时补记。
		if r, ok := h.pendingSink.(service.ApplyReporter); ok {
			// 能回报换装结果的才记上，供启动时补记。
			svc.Updates.SetApplyReporter(r)
		}
		// 能清理旧镜像的才记上，避免磁盘被占满。
		if j, ok := h.pendingSink.(service.ImageJanitor); ok {
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
	// 记下来，下次按身份直接复用这份服务。
	h.tenants[factoryID] = &tenant{db: db, svc: svc}
	return svc, nil
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

	// 按换到的身份建厂库，失败则先不落超管。
	svc, err := h.ensure(offer.FactoryID)
	// 厂库建不开就交回，不继续落超管。
	if err != nil {
		return SiteFactory{}, err
	}
	// 把初始超管落库并激活，失败则认领未完成。
	if _, err := svc.Auth.AcceptEnrollment(ctx, offer.SAPersonID, offer.SALogin, offer.SADisplay, password); err != nil {
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
	// 把公钥交给平台并作废建厂码，失败则未认领完。
	if err := wanchannel.Confirm(ctx, wanURL, enrollmentCode, pub); err != nil {
		return SiteFactory{}, err
	}
	// 确认成功后接上出站，断了会自己重连。
	h.ensureChannel(offer.FactoryID)
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
	// 占住厂库表，再关连接和登记。
	h.mu.Lock()
	// 内存里开过就先关掉这家的连接。
	if t, ok := h.tenants[factoryID]; ok {
		// 取出底层连接，能关才关。
		if sqlDB, err := t.db.DB(); err == nil {
			// 关掉这家厂的库连接。
			_ = sqlDB.Close()
		}
		// 从内存拿掉，避免还能访问已删的库。
		delete(h.tenants, factoryID)
	}
	// 登记清完就放开厂库锁。
	h.mu.Unlock()
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
	// 占住厂库表，逐家关掉数据库连接。
	h.mu.Lock()
	// 全部关完再放开厂库锁。
	defer h.mu.Unlock()
	// 关掉每家已经打开的厂库。
	for id, t := range h.tenants {
		// 取出这家厂的底层连接。
		if sqlDB, err := t.db.DB(); err == nil {
			// 关掉这家厂的库连接。
			_ = sqlDB.Close()
		}
		// 从内存拿掉，进程里不再留这家厂。
		delete(h.tenants, id)
	}
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
