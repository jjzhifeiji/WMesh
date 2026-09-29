package wanchannel

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
	"wmesh/factory/internal/platform/release"
)

// 关掉 Paho 默认日志，避免噪声进 stdout。
func init() {
	// 把库自带日志丢进空设备，避免噪声打进标准输出。
	mqtt.ERROR, mqtt.CRITICAL, mqtt.WARN, mqtt.DEBUG = log.New(io.Discard, "", 0), log.New(io.Discard, "", 0), log.New(io.Discard, "", 0), log.New(io.Discard, "", 0)
}

var leaseEvery = time.Hour           // 内容租约续期间隔
var aliveEvery = 20 * time.Second    // Broker 死后必须退回外层重拨，不能干等到租约点
var softwareEvery = 15 * time.Minute // 通道一直连着也要问有没有新包

// ClientSyncHandler 按索引里仍有效的绑定，把漏掉的本厂绑定作废。
type ClientSyncHandler func(keep []uuid.UUID) error

// Hold 用厂钥连 WAN MQTT，订下行、拉索引和正文，并回答升档问询。
func Hold(ctx context.Context, mqttURL, wanHTTP string, factoryID uuid.UUID, privateKey []byte, apply func(State) error, applyClient func(ClientIntent) error, syncClients ClientSyncHandler, applyClosure ClosureHandler, applyTemplate ClosureHandler, applySoftware ClosureHandler, applyRetract RetractHandler, applyFS ClosureHandler, applyLease LeaseHandler, onRequest RequestHandler, outbound <-chan SyncRequest) error {
	// 没有签发钥就拒绝连接，避免匿名拨上平台。
	if len(privateKey) == 0 {
		return domain.ErrNotFound
	}
	// 解析代理地址，没有主机就不要拨。
	broker, err := resolveMQTT(mqttURL, wanHTTP)
	// 代理地址解析失败就退出，外层会再拨。
	if err != nil {
		return err
	}
	// 没有平台地址就当不可达，避免去拉空主机。
	if strings.TrimSpace(wanHTTP) == "" {
		return domain.ErrWANUnreachable
	}
	// 准备连接参数，后面补上身份、签名和超时。
	opts := mqtt.NewClientOptions()
	// 写上代理地址，拨错主机就连不上。
	opts.AddBroker(broker)
	// 用工厂身份当会话名，平台据此认厂。
	opts.SetClientID(factoryID.String())
	// 用户名同样用工厂身份，和签名配套。
	opts.SetUsername(factoryID.String())
	// 用当前时间窗签名当密码，过期会被拒绝。
	opts.SetPassword(nodekey.SignMQTTPassword(privateKey, factoryID, time.Now().Unix()))
	// 保留会话，短断线不丢还没处理的下行。
	opts.SetCleanSession(false)
	// HMAC 有时间窗，禁止 Paho 自动重连；断线必须退回外层用新签名再拨。
	opts.SetAutoReconnect(false)
	// 十五秒探活，死连接不能一直占着。
	opts.SetKeepAlive(15 * time.Second)
	// 探活应答超过十秒就当断开。
	opts.SetPingTimeout(10 * time.Second)
	// 十秒拨不上就放弃，交给外层再试。
	opts.SetConnectTimeout(10 * time.Second)
	// 准备断线通知，外层才能用新签名重拨。
	lost := make(chan error, 1)
	// 断线时把原因送出，外层重新签名再拨。
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		// 断线时只送出一次，已经有人在等就不再塞。
		select {
		// 把断线送出去，外层用新签名重拨。
		case lost <- err:
		// 已经有断线在等就不再塞，避免堵住回调。
		default:
		}
	})
	// 按这些参数建立会话，此时尚未拨号。
	cli := mqtt.NewClient(opts)
	// 发起拨号，超时或拒绝就不要订下行。
	tok := cli.Connect()
	// 拨号超时或被拒绝就收成未授权或不可达。
	if !tok.WaitTimeout(15*time.Second) || tok.Error() != nil {
		// 把拒绝收成未授权或不可达，促使外层重拨。
		return mapMQTTConnect(tok.Error())
	}
	// 离开时断开，避免会话占着代理。
	defer cli.Disconnect(250)

	// 拼下行主题，只订这一家厂。
	down := "wan/" + factoryID.String() + "/down"
	up := "wan/" + factoryID.String() + "/up"
	msgs := make(chan []byte, 64)
	// 收到下行就拷进队列，取消则丢掉。
	sub := cli.Subscribe(down, 1, func(_ mqtt.Client, m mqtt.Message) {
		// 拷一份载荷，避免底层缓冲被下一条覆盖。
		payload := append([]byte(nil), m.Payload()...)
		// 队列有空位才收，取消则丢掉这条。
		select {
		// 把下行送进处理队列。
		case msgs <- payload:
		// 取消了就丢掉这条，不再往队列塞。
		case <-ctx.Done():
		}
	})
	// 订不上下行就当平台不可达，调用方会重试。
	if !sub.WaitTimeout(10*time.Second) || sub.Error() != nil {
		return domain.ErrWANUnreachable
	}
	// 连上后把本进程前端/服务版本报给 WAN，名录只在在线时展示。
	if err := publishUp(cli, up, presenceCmd()); err != nil {
		return err
	}

	// 用厂钥签后面的拉取，没有签名会被拒绝。
	pull := newPuller(wanHTTP, factoryID, privateKey)
	// 落地串行，避免两条下行交错改本厂。
	var mu sync.Mutex
	// 串行落地一条指令，失败就退出重拨。
	handle := func(cmd Cmd) error {
		// 占住落地，避免并发把同一条写乱。
		mu.Lock()
		// 离开时放开，下一条才能落地。
		defer mu.Unlock()
		// 按类型落地，失败交给外层重拨。
		return applyCmd(ctx, cli, up, pull, cmd, apply, applyClient, applyClosure, applyTemplate, applySoftware, applyRetract, applyFS, applyLease, onRequest)
	}

	// 先同步一轮，失败就不要进入长循环。
	if err := pullAndApply(ctx, pull, "", handle, syncClients, applySoftware); err != nil {
		return err
	}

	// 按点续租，到期前换新解包钥。
	renew := time.NewTicker(leaseEvery)
	// 离开时停掉续租节拍。
	defer renew.Stop()
	// 短周期探活，死代理不能干等到续租。
	alive := time.NewTicker(aliveEvery)
	// 离开时停掉探活节拍。
	defer alive.Stop()
	// 连着也定时问有没有新包。
	soft := time.NewTicker(softwareEvery)
	// 离开时停掉问包节拍。
	defer soft.Stop()
	// 谁先到就处理谁，取消才离开。
	for {
		// 下行、续租、探活和补拉谁先到就处理谁。
		select {
		// 取消就结束本次连接，把原因交回外层。
		case <-ctx.Done():
			// 把取消原因交回，外层不再重拨。
			return ctx.Err()
		// 代理断线就交回，外层用新签名重拨。
		case err := <-lost:
			// 断线一律当不可达，促使外层重拨。
			return mapMQTTDisconnect(err)
		// 到点就探活，死连接要立刻退回。
		case <-alive.C:
			// 已经断开就当不可达，不要空报在线。
			if !cli.IsConnected() {
				return domain.ErrWANUnreachable
			}
			// 半开 TCP 上探活会失败，立刻退回外层重拨。
			if err := publishUp(cli, up, presenceCmd()); err != nil {
				return err
			}
		// 取出一条下行再落地。
		case raw := <-msgs:
			// 预备装这一条指令。
			var cmd Cmd
			// 解不开就丢掉，不断开通道。
			if json.Unmarshal(raw, &cmd) != nil {
				continue
			}
			// 落地失败就退出循环，外层重拨。
			if err := handle(cmd); err != nil {
				return err
			}
		// 到点就问有没有新包，一直连着也不漏。
		case <-soft.C:
			// 通道一直连着也去问最高版，有新包就静默拉。
			syncSoftware(ctx, pull, applySoftware)
		// 到点就续租，没人接收则跳过。
		case <-renew.C:
			// 没人接收租约就跳过，不空打平台。
			if applyLease == nil {
				continue
			}
			// 预备装新的解包钥。
			var lr leaseResp
			// 续租失败只记日志，不断开通道。
			if err := pull.post(ctx, "/v1/channel/lease", &lr); err != nil {
				// 记下续租失败，下一拍再试。
				slog.Warn("renew content lease", "err", err)
				continue
			}
			// 新钥落不上只记日志，不断开。
			if err := applyLeaseCmd(applyLease, Cmd{Typ: CmdLease, Lease: lr.Lease, NotAfter: lr.NotAfter}); err != nil {
				slog.Warn("apply content lease", "err", err)
			}
		// 页面要补拉时按种类再同步一轮。
		case req := <-outbound:
			// 取出要补的种类，空则该类型全量。
			kind := req.Kind
			// 模版未指定种类就保持全量补拉。
			if req.Typ == "sync_templates" && kind == "" {
				// 保持全量，不去收窄索引。
				kind = ""
			}
			// 补拉失败只记日志，通道继续留着。
			if err := pullAndApply(ctx, pull, kind, handle, syncClients, applySoftware); err != nil {
				// 记下补拉失败，页面可以再试。
				slog.Warn("wan index sync", "err", err)
			}
		}
	}
}

// 先领租约再按索引逐条落地；有效厂再对账绑定。
func pullAndApply(ctx context.Context, pull *puller, kind string, handle func(Cmd) error, syncClients ClientSyncHandler, applySoftware ClosureHandler) error {
	// 预备装当前解包钥，没有它不解正文。
	var lr leaseResp
	// 先拿租约，拿不到就整批不做。
	if err := pull.get(ctx, "/v1/channel/lease", &lr); err != nil {
		return err
	}
	// 租约落不上就整批停止，避免无钥解正文。
	if err := handle(Cmd{Typ: CmdLease, Lease: lr.Lease, NotAfter: lr.NotAfter}); err != nil {
		return err
	}
	// 没指定种类时先拉全量索引。
	path := "/v1/channel/index"
	if kind != "" {
		// 把种类放进查询，平台只回这一类。
		path += "?kind=" + url.QueryEscape(kind)
	}
	// 预备装平台返回的指令清单。
	var idx indexResp
	// 索引拿不到就整批停止。
	if err := pull.get(ctx, path, &idx); err != nil {
		return err
	}
	// 先当厂仍有效，遇到停用再改。
	active := true
	// 收集仍有效的绑定，供后面的对账。
	var keep []uuid.UUID
	// 逐条落地，一条失败就整批停。
	for _, cmd := range idx.Cmds {
		// 非启用就不对账，避免误作废绑定。
		if cmd.Typ == CmdFactoryState && cmd.Status != "" && cmd.Status != "active" {
			// 记下厂已停用或注销。
			active = false
		}
		// 这一条落不上就整批停止。
		if err := handle(cmd); err != nil {
			return err
		}
		// 绑定才进入对账名单。
		if cmd.Typ == CmdClientBind {
			// 设备身份不合法就略过，不让坏编号进对账。
			id, err := uuid.Parse(cmd.ClientID)
			// 编号合法才留在对账名单里。
			if err == nil {
				// 把仍有效的设备留在对账名单。
				keep = append(keep, id)
			}
		}
	}
	// 厂仍有效才补拉软件，停用厂不去问包。
	if active {
		// 回连补拉厂服务包和漏掉的 APK。
		syncSoftware(ctx, pull, applySoftware)
	}
	// 没人对账或厂已停用就到此为止。
	if syncClients == nil || !active {
		return nil
	}
	// 把仍有效的绑定交给本厂对账，漏掉的应作废。
	return syncClients(keep)
}

// 拉当前最高厂服务包和客户端包；没有或失败只记日志。
func syncSoftware(ctx context.Context, pull *puller, applySoftware ClosureHandler) {
	// 没人接收就不问包，避免空拉。
	if applySoftware == nil {
		return
	}
	// 厂包和客户端包都问最高版。
	for _, kind := range []string{"factory_service", "client_apk"} {
		// 只收种类和版本，字节不走这条。
		var meta struct {
			Kind    string `json:"kind"`    // factory_service / client_apk
			Version int64  `json:"version"` // 当前最高
		}
		if err := pull.get(ctx, "/v1/software/latest?kind="+url.QueryEscape(kind), &meta); err != nil {
			continue
		}
		acceptSoftware(applySoftware, kind, meta.Version, "")
	}
}

// 只把种类和版本交给本厂去拉；正文不进 MQTT，同版本由本厂跳过重下。
func acceptSoftware(applySoftware ClosureHandler, kind string, version int64, versionName string) {
	// 没人接收或版本无效就不通知。
	if applySoftware == nil || version < 1 {
		return
	}
	// 编出软件通知，编不出就放弃这一条。
	snap, err := json.Marshal(Cmd{Typ: CmdSoftware, Kind: kind, Version: version, VersionName: versionName})
	// 编不出就放弃这一条通知，不断开通道。
	if err != nil {
		return
	}
	// 本厂拒收只记日志，不断开通道。
	if err := applySoftware(snap); err != nil {
		// 记下拒收原因，下一轮再问。
		slog.Warn("accept software", "kind", kind, "err", err)
	}
}

// 按指令类型拉正文或回答升档问询。
func applyCmd(ctx context.Context, cli mqtt.Client, up string, pull *puller, cmd Cmd, apply func(State) error, applyClient func(ClientIntent) error, applyClosure ClosureHandler, applyTemplate ClosureHandler, applySoftware ClosureHandler, applyRetract RetractHandler, applyFS ClosureHandler, applyLease LeaseHandler, onRequest RequestHandler) error {
	// 按指令类型拉正文或回答问询。
	switch cmd.Typ {
	// 有解包钥才交给本厂，空的直接忽略。
	case CmdLease:
		// 没有接收方或钥是空的就跳过。
		if applyLease == nil || len(cmd.Lease) == 0 {
			return nil
		}
		// 租约落不上只记日志，不断开。
		if err := applyLeaseCmd(applyLease, cmd); err != nil {
			// 记下租约失败，连接继续留着。
			slog.Warn("apply content lease", "err", err)
		}
		return nil
	// 治理状态交给本厂落地，没人接收就跳过。
	case CmdFactoryState:
		// 没人接收治理状态就跳过。
		if apply == nil {
			return nil
		}
		// 把启用、停用或注销交给本厂落地。
		return apply(State{Status: cmd.Status, Revision: cmd.Revision, ShortCode: cmd.ShortCode, Name: cmd.FactoryName})
	// 绑定或作废收成设备意图，解析失败只记日志。
	case CmdClientBind, CmdClientVoid:
		// 没人接收设备意图就跳过。
		if applyClient == nil {
			return nil
		}
		// 收成设备意图，身份不合法则放弃这一条。
		in, err := parseCmdClient(cmd)
		// 解析失败只记日志，不断开通道。
		if err != nil {
			// 记下坏的设备指令，继续处理后面的。
			slog.Warn("client cmd", "err", err)
			return nil
		}
		// 把绑定或作废交给本厂，失败则退出重拨。
		return applyClient(in)
	// 按身份去拉闭包正文，拉不到不断开。
	case CmdClosure:
		// 没人接收或缺少身份就跳过，不断开。
		if applyClosure == nil || cmd.AssetID == "" {
			return nil
		}
		// 预备装拉回来的闭包正文。
		var snap json.RawMessage
		// 拉不到闭包只记日志，不断开。
		if err := pull.get(ctx, "/v1/channel/pull/closure/"+cmd.AssetID, &snap); err != nil {
			// 记下拉闭包失败，连接继续留着。
			slog.Warn("pull closure", "err", err)
			return nil
		}
		// 把闭包正文交给本厂落地。
		return applyClosure(snap)
	// 按身份去拉模版正文，拉不到不断开。
	case CmdTemplate:
		// 没人接收或缺少模版身份就跳过。
		if applyTemplate == nil || cmd.TemplateID == "" {
			return nil
		}
		// 预备装拉回来的模版正文。
		var snap json.RawMessage
		// 拉不到模版只记日志，不断开。
		if err := pull.get(ctx, "/v1/channel/pull/template/"+cmd.TemplateID, &snap); err != nil {
			// 记下拉模版失败，连接继续留着。
			slog.Warn("pull template", "err", err)
			return nil
		}
		// 把模版正文交给本厂落地。
		return applyTemplate(snap)
	// 身份合法才撤回，坏编号直接忽略。
	case CmdRetract:
		// 没人接收或缺少资产身份就跳过。
		if applyRetract == nil || cmd.AssetID == "" {
			return nil
		}
		// 撤回身份不合法就跳过，不断开。
		id, err := uuid.Parse(cmd.AssetID)
		// 坏编号直接忽略，不断开通道。
		if err != nil {
			return nil
		}
		// 把撤回交给本厂，失败则退出重拨。
		return applyRetract(id)
	// 目录搬家随指令正文落地，失败只记日志。
	case CmdFSApply:
		// 没人接收或没有目录正文就跳过。
		if applyFS == nil || len(cmd.Snapshot) == 0 {
			return nil
		}
		// 目录对齐失败只记日志，不断开。
		if err := applyFS(cmd.Snapshot); err != nil {
			// 记下目录对齐失败，连接继续留着。
			slog.Warn("apply platform fs", "err", err)
		}
		return nil
	// 只接受厂包和客户端包，正文不走这条。
	case CmdSoftware:
		// 别的种类忽略，避免把无关包拉下来。
		if cmd.Kind != "client_apk" && cmd.Kind != "factory_service" {
			return nil
		}
		// 只交种类和版本，正文由本厂另拉。
		acceptSoftware(applySoftware, cmd.Kind, cmd.Version, cmd.VersionName)
		return nil
	// 升档问询走上行回执，不在这里拉正文。
	case CmdAssetList, CmdAssetSnapshot, CmdFSList:
		// 问询走上行回执，失败则退出重拨。
		return replyRequest(cli, up, cmd, onRequest)
	// 不认识的指令忽略，不断开通道。
	default:
		return nil
	}
}

// 把租约钥交给本厂进程。
func applyLeaseCmd(applyLease LeaseHandler, cmd Cmd) error {
	// 先按更细的时间格式读到期时刻。
	na, err := time.Parse(time.RFC3339Nano, cmd.NotAfter)
	// 这一格式不对就改试到秒的格式。
	if err != nil {
		// 再按到秒的格式读，两种都不认才放弃。
		na, err = time.Parse(time.RFC3339, cmd.NotAfter)
	}
	// 到期时间认不出就丢掉这条租约。
	if err != nil {
		return nil
	}
	// 把解包钥和到期时间交给本厂进程。
	return applyLease(Lease{Key: cmd.Lease, NotAfter: na})
}

// 把绑定/作废指令收成设备意图。
func parseCmdClient(cmd Cmd) (ClientIntent, error) {
	// 设备身份不合法就当没有这条绑定。
	cid, err := uuid.Parse(cmd.ClientID)
	// 坏编号拒绝，避免落错设备。
	if err != nil {
		return ClientIntent{}, domain.ErrNotFound
	}
	return ClientIntent{
		Typ: cmd.Typ, ClientID: cid, Name: cmd.ClientName, ShortCode: cmd.ClientShortCode,
		DeviceSerial: cmd.DeviceSerial, PublicKey: cmd.PublicKey, Revision: cmd.BindingRevision,
	}, nil
}

// 经上行 Topic 回答升档问询。
func replyRequest(cli mqtt.Client, up string, cmd Cmd, onRequest RequestHandler) error {
	// 先带上问询编号，回执才能配对。
	reply := Cmd{ReqID: cmd.ReqID}
	// 没人回答就回找不到，不断开通道。
	if onRequest == nil {
		// 标明这是失败回执，平台不要当清单用。
		reply.Typ = "error"
		reply.Error = domain.ErrNotFound.Error()
		// 把回执发上上行，发不出就当通道不可达。
		return publishUp(cli, up, reply)
	}
	// 向本厂要清单或快照，失败就回错误。
	assets, snap, err := onRequest(cmd.Typ, cmd.ReqID, cmd.Kind, cmd.AssetID)
	// 本厂答不上就回错误，不断开去重试。
	if err != nil {
		// 标明本厂没有答上，平台不要当成功。
		reply.Typ = "error"
		reply.Error = err.Error()
		// 把失败回执发上上行，发不出就当不可达。
		return publishUp(cli, up, reply)
	}
	// 快照问询把正文放进快照字段。
	if cmd.Typ == CmdAssetSnapshot {
		// 标明快照已经答回。
		reply.Typ = CmdAssetSnapOK
		// 放上快照正文，清单字段留空。
		reply.Snapshot = snap
	} else if cmd.Typ == CmdFSList { // 目录问询把结果放进清单字段。
		// 标明目录已经答回。
		reply.Typ = CmdFSListOK
		// 把目录结果放进回执的清单里。
		reply.Assets = assets
	} else { // 其余问询按升档清单答回。
		// 标明清单已经答回。
		reply.Typ = CmdAssetListOK
		// 把可升档清单放进回执里。
		reply.Assets = assets
	}
	// 把答复发上上行，发不出就当不可达。
	return publishUp(cli, up, reply)
}

// 向本厂 up Topic 发一条回执。
func publishUp(cli mqtt.Client, up string, cmd Cmd) error {
	// 编不出回执就交回，避免发出空包。
	raw, err := json.Marshal(cmd)
	// 编码失败就交回调用方。
	if err != nil {
		return err
	}
	// 发到上行主题，平台靠它认回执。
	tok := cli.Publish(up, 1, false, raw)
	// 十秒发不出就当平台不可达。
	if !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
		return domain.ErrWANUnreachable
	}
	return nil
}

// 显式 MQTT 地址优先，否则按 WAN HTTP 主机拼 52183。
func resolveMQTT(mqttURL, wanHTTP string) (string, error) {
	// 去掉空白，空的才改按网页主机拼。
	mqttURL = strings.TrimSpace(mqttURL)
	// 显式地址优先，不再改写端口。
	if mqttURL != "" {
		return mqttURL, nil
	}
	// 从平台地址取出主机，拼默认代理端口。
	u, err := url.Parse(strings.TrimSpace(wanHTTP))
	// 没有主机就当不可达，避免拨错机器。
	if err != nil || u.Hostname() == "" {
		return "", domain.ErrWANUnreachable
	}
	// 按平台主机拼默认代理端口。
	return "tcp://" + net.JoinHostPort(u.Hostname(), "52183"), nil
}

// 本进程正在跑的前端/服务版本，名录在线时展示。
func presenceCmd() Cmd {
	return Cmd{
		Typ: CmdPresence, WebVersion: release.WebCode, WebVersionName: release.WebName,
		ServiceVersion: release.Code, ServiceVersionName: release.Name,
	}
}

// 把 Broker 拒绝收成未授权或不可达。
func mapMQTTConnect(err error) error {
	// 没有底层原因也当不可达。
	if err == nil {
		return domain.ErrWANUnreachable
	}
	// 把拒绝原因转成小写再认，避免大小写对不上。
	msg := strings.ToLower(err.Error())
	// 代理拒绝就当未授权，外层会拉长退避。
	if strings.Contains(msg, "not authorized") || strings.Contains(msg, "bad user") {
		return domain.ErrUnauthorized
	}
	return domain.ErrWANUnreachable
}

// 会话丢失一律当通道不可达，外层用新 HMAC 再拨。
func mapMQTTDisconnect(err error) error {
	// 断线同样收成未授权或不可达，促使外层重拨。
	return mapMQTTConnect(err)
}
