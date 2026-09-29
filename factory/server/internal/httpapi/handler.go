// Package httpapi 把厂内应用服务适配成 JSON HTTP。不绕过 Service，不把 SQL 原文抛给前端。
// 文件按域拆：auth / site / org / node / asset / fs / template / update / policy / legacy / stats；本文件只装配路由、探活和建厂引导。
package httpapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/release"
	"wmesh/factory/internal/platform/secret"
	"wmesh/factory/internal/service"
)

// Handler 按 URL 里的工厂身份选库，并把会话头交给应用服务。
type Handler struct {
	Hub            *hub.Hub                    // 厂库枢纽，按工厂身份打开库
	BootstrapToken string                      // 建厂引导共享密码，只用于 /internal/bootstrap
	WANURL         string                      // WAN 根地址，厂出站认领用；空则不能认领
	Version        string                      // 构建串，随探活 build 返回
	VersionCode    int64                       // 本进程版本号，来自 release 常量
	VersionName    string                      // 本进程版本名，来自 release 常量
	OSSProbe       func(context.Context) error // 探对象存储是否在线；空表示本厂未接 OSS
	ClientMQTTURL  string                      // 回给平板的 MQTT 地址；空则按请求主机拼
	ClientMQTTPort string                      // 登录回包 MQTT 端口；空则 52184
}

// clientMQTTURL 登录回给平板的 Broker 地址；显式配置优先，否则用本次请求主机。
func (h *Handler) clientMQTTURL(r *http.Request) string {
	// 已经配置了通道地址就直接用，不再猜主机。
	if u := strings.TrimSpace(h.ClientMQTTURL); u != "" {
		return u
	}
	// 先取请求主机，有端口再剥掉。
	host := r.Host
	// 主机带了端口就剥掉，避免拼出两段端口。
	if hst, _, err := net.SplitHostPort(host); err == nil {
		// 只留主机，端口单独再拼上去。
		host = hst
	}
	// 读取配置的端口，空着就用默认。
	port := strings.TrimSpace(h.ClientMQTTPort)
	// 没配端口就改用示教器惯用的那个口。
	if port == "" {
		// 没配端口时写成示教器惯用的口。
		port = "52184"
	}
	// 用这次的主机和端口拼出通道地址。
	return "tcp://" + net.JoinHostPort(host, port)
}

// New 组装厂内 HTTP 适配器。
func New(h *hub.Hub, bootstrapToken, wanURL string) *Handler {
	return &Handler{Hub: h, BootstrapToken: bootstrapToken, WANURL: wanURL, Version: "dev", VersionCode: release.Code, VersionName: release.Name}
}

// Router 只装配探活、建厂引导和各域路由；未知 API 路径统一回 JSON 404。
func (h *Handler) Router() http.Handler {
	// 先建空路由表，再把探活和各域挂上。
	mux := http.NewServeMux()
	// 把探活挂到健康检查，库不通要让编排看见。
	mux.HandleFunc("GET /healthz", h.healthz)
	// 业务路径没有登记则按找不到拒绝。
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, _ *http.Request) { writeErr(w, domain.ErrNotFound) })
	// 内部路径没有登记则按找不到拒绝。
	mux.HandleFunc("/internal/", func(w http.ResponseWriter, _ *http.Request) { writeErr(w, domain.ErrNotFound) })
	// 把建厂引导挂到内部路径，只认共享密码。
	mux.HandleFunc("POST /internal/bootstrap", h.bootstrap)
	// 挂上认领、名录和局域网探活。
	h.mountSite(mux)
	// 挂上登录、激活和会话。
	h.mountAuth(mux)
	// 挂上名册、组织和角色分配。
	h.mountOrg(mux)
	// 挂上设备绑定和运行许可。
	h.mountNode(mux)
	// 挂上工艺和工程的办理入口。
	h.mountAsset(mux)
	// 挂上目录的查询和整理。
	h.mountFS(mux)
	// 挂上旧示教器文件导入。
	h.mountLegacy(mux)
	// 挂上字段模版的读取和同步。
	h.mountTemplate(mux)
	// 挂上软件包、清理和占用查询。
	h.mountUpdate(mux)
	// 挂上示教器策略，仅超管可改。
	h.mountPolicy(mux)
	// 挂上收件箱和闭包下载。
	h.mountChannel(mux)
	// 挂上焊事实汇入和厂端报表。
	h.mountStats(mux)
	return mux
}

// 建厂引导要写入的工厂和初始超管。
type bootReq struct {
	FactoryID string `json:"factoryId"` // 工厂稳定身份
	SALogin   string `json:"saLogin"`   // 初始超管登录名
	SADisplay string `json:"saDisplay"` // 初始超管显示名
}

// 建厂结果，激活码只回给交付方。
type bootResp struct {
	PersonID        string `json:"personId"`        // 厂库账号身份
	ActivationToken string `json:"activationToken"` // 一次性 8 位激活码，禁止写入审计
}

// 探活结果，库和对象存储分开标记。
type healthResp struct {
	Status      string `json:"status"`      // ok 或 degraded
	Version     int64  `json:"version"`     // 版本号
	VersionName string `json:"versionName"` // 版本名
	Build       string `json:"build"`       // 构建串
	DB          string `json:"db"`          // ok 或 down
	OSS         string `json:"oss"`         // ok / down / off（未配置）
}

// healthz 库不通回 503 让编排判定不健康；OSS 掉线只标 degraded，不影响账号管理继续服务。
func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	// 给这次探活限时，免得库卡住拖死接口。
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	// 离开时取消限时，避免计时器一直留着。
	defer cancel()
	// 先按全健康填写，库或存储失败再降级。
	resp := healthResp{Status: "ok", Version: h.VersionCode, VersionName: h.VersionName, Build: h.Version, DB: "ok", OSS: "off"}
	// 默认按正常返回，库探活失败再改。
	code := http.StatusOK
	// 被拒绝则停止，不继续写成功响应。
	if err := h.Hub.Ping(ctx); err != nil {
		// 库不通则标降级，并让编排视为不健康。
		resp.Status, resp.DB, code = "degraded", "down", http.StatusServiceUnavailable
		// 探活失败只记日志，响应里不写内部原因。
		slog.Warn("healthz db ping failed", "err", err)
	}
	// 接了对象存储才去探，没接不算故障。
	if h.OSSProbe != nil {
		// 已配置存储则先标正常，探测失败再改。
		resp.OSS = "ok"
		if err := h.OSSProbe(ctx); err != nil {
			// 存储探活失败则标降级，接口仍可服务。
			resp.Status, resp.OSS = "degraded", "down"
			slog.Warn("healthz oss probe failed", "err", err)
		}
	}
	// 通过之后把结果回给调用方。
	writeJSON(w, code, resp)
}

// 用共享密码在目标厂库写入待启用初始超管；激活码只回调用方。
func (h *Handler) bootstrap(w http.ResponseWriter, r *http.Request) {
	// 共享密码恒定时间比对；没配密码时一律拒绝，不给空密码放行。
	if h.BootstrapToken == "" || !secret.Equal(bearer(r), h.BootstrapToken) {
		// 没有凭证或对不上则拒绝，不继续办理。
		writeErr(w, domain.ErrUnauthorized)
		return
	}
	// 承接建厂参数，未知字段会拒绝。
	var req bootReq
	// 正文无法解析则拒绝，不进入业务。
	if err := decodeJSON(r, &req); err != nil {
		// 请求不合法，回非法请求并不进入业务。
		writeBadRequest(w, err)
		return
	}
	// 编号必须合法，否则拒绝以免办错对象。
	fid, err := uuid.Parse(req.FactoryID)
	// 解析失败则拒绝，不用这个残缺的值继续。
	if err != nil {
		// 编号不合法，回非法请求并不进入业务。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 建厂库并写入待启用初始超管。
	personID, token, err := h.Hub.Bootstrap(r.Context(), fid, req.SALogin, req.SADisplay)
	// 上一步没通过则停止，不把失败写成成功。
	if err != nil {
		// 把失败译成状态回给调用方，不写成功。
		writeErr(w, err)
		return
	}
	// 创建成功，把新记录回给调用方。
	writeJSON(w, http.StatusCreated, bootResp{PersonID: personID.String(), ActivationToken: token})
}

// 按 URL 工厂身份打开已有厂库；库不在就当未初始化。
func (h *Handler) withFactory(w http.ResponseWriter, r *http.Request, fn func(*service.Service)) {
	// 工厂编号必须合法，否则拒绝并不开库。
	id, err := uuid.Parse(r.PathValue("id"))
	// 解析失败则拒绝，不用这个残缺的值继续。
	if err != nil {
		// 编号不合法，回非法请求并不进入业务。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 打开已有厂库；库不在就当未初始化。
	svc, err := h.Hub.Service(r.Context(), id)
	// 上一步没通过则停止，不把失败写成成功。
	if err != nil {
		// 把失败译成状态回给调用方，不写成功。
		writeErr(w, err)
		return
	}
	// 前置已经满足，把这次请求交给业务。
	fn(svc)
}
