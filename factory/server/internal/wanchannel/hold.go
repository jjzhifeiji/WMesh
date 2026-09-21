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
	mqtt.ERROR, mqtt.CRITICAL, mqtt.WARN, mqtt.DEBUG = log.New(io.Discard, "", 0), log.New(io.Discard, "", 0), log.New(io.Discard, "", 0), log.New(io.Discard, "", 0)
}

var leaseEvery = time.Hour           // 内容租约续期间隔
var aliveEvery = 20 * time.Second    // Broker 死后必须退回外层重拨，不能干等到租约点
var softwareEvery = 15 * time.Minute // 通道一直连着也要问有没有新包

// ClientSyncHandler 按索引里仍有效的绑定，把漏掉的本厂绑定作废。
type ClientSyncHandler func(keep []uuid.UUID) error

// Hold 用厂钥连 WAN MQTT，订下行、拉索引和正文，并回答升档问询。
func Hold(ctx context.Context, mqttURL, wanHTTP string, factoryID uuid.UUID, privateKey []byte, apply func(State) error, applyClient func(ClientIntent) error, syncClients ClientSyncHandler, applyClosure ClosureHandler, applyTemplate ClosureHandler, applySoftware ClosureHandler, applyRetract RetractHandler, applyFS ClosureHandler, applyLease LeaseHandler, onRequest RequestHandler, outbound <-chan SyncRequest) error {
	if len(privateKey) == 0 {
		return domain.ErrNotFound
	}
	broker, err := resolveMQTT(mqttURL, wanHTTP)
	if err != nil {
		return err
	}
	if strings.TrimSpace(wanHTTP) == "" {
		return domain.ErrWANUnreachable
	}
	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)
	opts.SetClientID(factoryID.String())
	opts.SetUsername(factoryID.String())
	opts.SetPassword(nodekey.SignMQTTPassword(privateKey, factoryID, time.Now().Unix()))
	opts.SetCleanSession(false)
	// HMAC 有时间窗，禁止 Paho 自动重连；断线必须退回外层用新签名再拨。
	opts.SetAutoReconnect(false)
	opts.SetKeepAlive(15 * time.Second)
	opts.SetPingTimeout(10 * time.Second)
	opts.SetConnectTimeout(10 * time.Second)
	lost := make(chan error, 1)
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		select {
		case lost <- err:
		default:
		}
	})
	cli := mqtt.NewClient(opts)
	tok := cli.Connect()
	if !tok.WaitTimeout(15*time.Second) || tok.Error() != nil {
		return mapMQTTConnect(tok.Error())
	}
	defer cli.Disconnect(250)

	down := "wan/" + factoryID.String() + "/down"
	up := "wan/" + factoryID.String() + "/up"
	msgs := make(chan []byte, 64)
	sub := cli.Subscribe(down, 1, func(_ mqtt.Client, m mqtt.Message) {
		payload := append([]byte(nil), m.Payload()...)
		select {
		case msgs <- payload:
		case <-ctx.Done():
		}
	})
	if !sub.WaitTimeout(10*time.Second) || sub.Error() != nil {
		return domain.ErrWANUnreachable
	}
	// 连上后把本进程前端/服务版本报给 WAN，名录只在在线时展示。
	if err := publishUp(cli, up, presenceCmd()); err != nil {
		return err
	}

	pull := newPuller(wanHTTP, factoryID, privateKey)
	var mu sync.Mutex
	handle := func(cmd Cmd) error {
		mu.Lock()
		defer mu.Unlock()
		return applyCmd(ctx, cli, up, pull, cmd, apply, applyClient, applyClosure, applyTemplate, applySoftware, applyRetract, applyFS, applyLease, onRequest)
	}

	if err := pullAndApply(ctx, pull, "", handle, syncClients, applySoftware); err != nil {
		return err
	}

	renew := time.NewTicker(leaseEvery)
	defer renew.Stop()
	alive := time.NewTicker(aliveEvery)
	defer alive.Stop()
	soft := time.NewTicker(softwareEvery)
	defer soft.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-lost:
			return mapMQTTDisconnect(err)
		case <-alive.C:
			if !cli.IsConnected() {
				return domain.ErrWANUnreachable
			}
			// 半开 TCP 上探活会失败，立刻退回外层重拨。
			if err := publishUp(cli, up, presenceCmd()); err != nil {
				return err
			}
		case raw := <-msgs:
			var cmd Cmd
			if json.Unmarshal(raw, &cmd) != nil {
				continue
			}
			if err := handle(cmd); err != nil {
				return err
			}
		case <-soft.C:
			// 通道一直连着也去问最高版，有新包就静默拉。
			syncSoftware(ctx, pull, applySoftware)
		case <-renew.C:
			if applyLease == nil {
				continue
			}
			var lr leaseResp
			if err := pull.post(ctx, "/v1/channel/lease", &lr); err != nil {
				slog.Warn("renew content lease", "err", err)
				continue
			}
			if err := applyLeaseCmd(applyLease, Cmd{Typ: CmdLease, Lease: lr.Lease, NotAfter: lr.NotAfter}); err != nil {
				slog.Warn("apply content lease", "err", err)
			}
		case req := <-outbound:
			kind := req.Kind
			if req.Typ == "sync_templates" && kind == "" {
				kind = ""
			}
			if err := pullAndApply(ctx, pull, kind, handle, syncClients, applySoftware); err != nil {
				slog.Warn("wan index sync", "err", err)
			}
		}
	}
}

// 先领租约再按索引逐条落地；有效厂再对账绑定。
func pullAndApply(ctx context.Context, pull *puller, kind string, handle func(Cmd) error, syncClients ClientSyncHandler, applySoftware ClosureHandler) error {
	var lr leaseResp
	if err := pull.get(ctx, "/v1/channel/lease", &lr); err != nil {
		return err
	}
	if err := handle(Cmd{Typ: CmdLease, Lease: lr.Lease, NotAfter: lr.NotAfter}); err != nil {
		return err
	}
	path := "/v1/channel/index"
	if kind != "" {
		path += "?kind=" + url.QueryEscape(kind)
	}
	var idx indexResp
	if err := pull.get(ctx, path, &idx); err != nil {
		return err
	}
	active := true
	var keep []uuid.UUID
	for _, cmd := range idx.Cmds {
		if cmd.Typ == CmdFactoryState && cmd.Status != "" && cmd.Status != "active" {
			active = false
		}
		if err := handle(cmd); err != nil {
			return err
		}
		if cmd.Typ == CmdClientBind {
			id, err := uuid.Parse(cmd.ClientID)
			if err == nil {
				keep = append(keep, id)
			}
		}
	}
	if active {
		// 回连补拉厂服务包和漏掉的 APK。
		syncSoftware(ctx, pull, applySoftware)
	}
	if syncClients == nil || !active {
		return nil
	}
	return syncClients(keep)
}

// 拉当前最高厂服务包和客户端包；没有或失败只记日志。
func syncSoftware(ctx context.Context, pull *puller, applySoftware ClosureHandler) {
	if applySoftware == nil {
		return
	}
	for _, kind := range []string{"factory_service", "client_apk"} {
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
	if applySoftware == nil || version < 1 {
		return
	}
	snap, err := json.Marshal(Cmd{Typ: CmdSoftware, Kind: kind, Version: version, VersionName: versionName})
	if err != nil {
		return
	}
	if err := applySoftware(snap); err != nil {
		slog.Warn("accept software", "kind", kind, "err", err)
	}
}

// 按指令类型拉正文或回答升档问询。
func applyCmd(ctx context.Context, cli mqtt.Client, up string, pull *puller, cmd Cmd, apply func(State) error, applyClient func(ClientIntent) error, applyClosure ClosureHandler, applyTemplate ClosureHandler, applySoftware ClosureHandler, applyRetract RetractHandler, applyFS ClosureHandler, applyLease LeaseHandler, onRequest RequestHandler) error {
	switch cmd.Typ {
	case CmdLease:
		if applyLease == nil || len(cmd.Lease) == 0 {
			return nil
		}
		if err := applyLeaseCmd(applyLease, cmd); err != nil {
			slog.Warn("apply content lease", "err", err)
		}
		return nil
	case CmdFactoryState:
		if apply == nil {
			return nil
		}
		return apply(State{Status: cmd.Status, Revision: cmd.Revision, ShortCode: cmd.ShortCode, Name: cmd.FactoryName})
	case CmdClientBind, CmdClientVoid:
		if applyClient == nil {
			return nil
		}
		in, err := parseCmdClient(cmd)
		if err != nil {
			slog.Warn("client cmd", "err", err)
			return nil
		}
		return applyClient(in)
	case CmdClosure:
		if applyClosure == nil || cmd.AssetID == "" {
			return nil
		}
		var snap json.RawMessage
		if err := pull.get(ctx, "/v1/channel/pull/closure/"+cmd.AssetID, &snap); err != nil {
			slog.Warn("pull closure", "err", err)
			return nil
		}
		return applyClosure(snap)
	case CmdTemplate:
		if applyTemplate == nil || cmd.TemplateID == "" {
			return nil
		}
		var snap json.RawMessage
		if err := pull.get(ctx, "/v1/channel/pull/template/"+cmd.TemplateID, &snap); err != nil {
			slog.Warn("pull template", "err", err)
			return nil
		}
		return applyTemplate(snap)
	case CmdRetract:
		if applyRetract == nil || cmd.AssetID == "" {
			return nil
		}
		id, err := uuid.Parse(cmd.AssetID)
		if err != nil {
			return nil
		}
		return applyRetract(id)
	case CmdFSApply:
		if applyFS == nil || len(cmd.Snapshot) == 0 {
			return nil
		}
		if err := applyFS(cmd.Snapshot); err != nil {
			slog.Warn("apply platform fs", "err", err)
		}
		return nil
	case CmdSoftware:
		if cmd.Kind != "client_apk" && cmd.Kind != "factory_service" {
			return nil
		}
		acceptSoftware(applySoftware, cmd.Kind, cmd.Version, cmd.VersionName)
		return nil
	case CmdAssetList, CmdAssetSnapshot, CmdFSList:
		return replyRequest(cli, up, cmd, onRequest)
	default:
		return nil
	}
}

// 把租约钥交给本厂进程。
func applyLeaseCmd(applyLease LeaseHandler, cmd Cmd) error {
	na, err := time.Parse(time.RFC3339Nano, cmd.NotAfter)
	if err != nil {
		na, err = time.Parse(time.RFC3339, cmd.NotAfter)
	}
	if err != nil {
		return nil
	}
	return applyLease(Lease{Key: cmd.Lease, NotAfter: na})
}

// 把绑定/作废指令收成设备意图。
func parseCmdClient(cmd Cmd) (ClientIntent, error) {
	cid, err := uuid.Parse(cmd.ClientID)
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
	reply := Cmd{ReqID: cmd.ReqID}
	if onRequest == nil {
		reply.Typ = "error"
		reply.Error = domain.ErrNotFound.Error()
		return publishUp(cli, up, reply)
	}
	assets, snap, err := onRequest(cmd.Typ, cmd.ReqID, cmd.Kind, cmd.AssetID)
	if err != nil {
		reply.Typ = "error"
		reply.Error = err.Error()
		return publishUp(cli, up, reply)
	}
	if cmd.Typ == CmdAssetSnapshot {
		reply.Typ = CmdAssetSnapOK
		reply.Snapshot = snap
	} else if cmd.Typ == CmdFSList {
		reply.Typ = CmdFSListOK
		reply.Assets = assets
	} else {
		reply.Typ = CmdAssetListOK
		reply.Assets = assets
	}
	return publishUp(cli, up, reply)
}

// 向本厂 up Topic 发一条回执。
func publishUp(cli mqtt.Client, up string, cmd Cmd) error {
	raw, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	tok := cli.Publish(up, 1, false, raw)
	if !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
		return domain.ErrWANUnreachable
	}
	return nil
}

// 显式 MQTT 地址优先，否则按 WAN HTTP 主机拼 52183。
func resolveMQTT(mqttURL, wanHTTP string) (string, error) {
	mqttURL = strings.TrimSpace(mqttURL)
	if mqttURL != "" {
		return mqttURL, nil
	}
	u, err := url.Parse(strings.TrimSpace(wanHTTP))
	if err != nil || u.Hostname() == "" {
		return "", domain.ErrWANUnreachable
	}
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
	if err == nil {
		return domain.ErrWANUnreachable
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "not authorized") || strings.Contains(msg, "bad user") {
		return domain.ErrUnauthorized
	}
	return domain.ErrWANUnreachable
}

// 会话丢失一律当通道不可达，外层用新 HMAC 再拨。
func mapMQTTDisconnect(err error) error {
	return mapMQTTConnect(err)
}
