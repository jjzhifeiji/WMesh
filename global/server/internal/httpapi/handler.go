// Package httpapi 把 WAN 应用服务适配成 JSON HTTP。不绕过 Service，不把 SQL 原文抛给前端。
// 文件按域拆：auth / directory / channel / client / asset / fs / template / update / stats / legacy；本文件只装配路由和探活。
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/release"
	"wmesh/global/internal/service"
)

// Handler 把会话头和应用服务接到路由上。
type Handler struct {
	svc         *service.Service            // 云端应用服务，适配层只转调。
	version     string                      // 构建串，随探活 build 返回
	versionCode int64                       // 本进程版本号，来自 release 常量
	versionName string                      // 本进程版本名，来自 release 常量
	OSSProbe    func(context.Context) error // 探对象存储是否在线；空表示未接 OSS
}

// New 组装 WAN HTTP 适配器。
func New(svc *service.Service, version string) *Handler {
	return &Handler{svc: svc, version: version, versionCode: release.Code, versionName: release.Name}
}

// Router 只装配探活和各域路由；未知 API 路径统一回 JSON 404。
func (h *Handler) Router() http.Handler {
	// 新建路由表，只挂探活和各域入口。
	mux := http.NewServeMux()
	// 探活入口，库不通要让编排摘流。
	mux.HandleFunc("GET /healthz", h.healthz)
	// 未知接口回找不到，避免落到默认页面。
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, _ *http.Request) { writeErr(w, domain.ErrNotFound) })
	// 挂上登录、登出和改密。
	h.mountAuth(mux)
	// 挂上名录和建厂，代管入口一律拒绝。
	h.mountDirectory(mux)
	// 挂上认领，以及按厂钥拉正文。
	h.mountChannel(mux)
	// 挂上现场设备，离线授权一律拒绝。
	h.mountClient(mux)
	// 挂上平台级工艺工程，代管一律拒绝。
	h.mountAsset(mux)
	// 挂上平台级目录树。
	h.mountFS(mux)
	// 挂上工艺字段表和工程模版。
	h.mountTemplate(mux)
	// 挂上软件发布和本机确认。
	h.mountUpdate(mux)
	// 挂上焊汇总上送和跨厂报表。
	h.mountStats(mux)
	// 挂上旧示教器文件导入。
	h.mountLegacy(mux)
	return mux
}

// 探活结果，库和对象存储分开报。
type healthResp struct {
	Status      string `json:"status"`      // ok 或 degraded
	Version     int64  `json:"version"`     // 版本号
	VersionName string `json:"versionName"` // 版本名
	Build       string `json:"build"`       // 构建串
	DB          string `json:"db"`          // ok 或 down
	OSS         string `json:"oss"`         // ok / down / off（未配置）
}

// healthz 库不通回 503 让编排判定不健康；OSS 掉线只标 degraded，不影响名录管理继续服务。
func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	// 探活最多等三秒，免得编排被拖住。
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	// 离开时取消限时，避免协程继续占着。
	defer cancel()
	// 先按库正常、存储未配置来组装探活。
	resp := healthResp{Status: "ok", Version: h.versionCode, VersionName: h.versionName, Build: h.version, DB: "ok", OSS: "off"}
	// 先当健康，库失败再改成不可用。
	code := http.StatusOK
	// 库探活失败就把本机标成不健康。
	if err := h.svc.Ping(ctx); err != nil {
		// 库不通则整机不健康，编排才会摘流。
		resp.Status, resp.DB, code = "degraded", "down", http.StatusServiceUnavailable
		// 库探活失败记一笔，响应里只标不健康。
		slog.Warn("healthz db ping failed", "err", err)
	}
	// 接了对象存储才探，没接就标未配置。
	if h.OSSProbe != nil {
		// 先当对象存储正常，探活失败再改降级。
		resp.OSS = "ok"
		if err := h.OSSProbe(ctx); err != nil {
			// 存储掉线只标降级，名录仍可继续服务。
			resp.Status, resp.OSS = "degraded", "down"
			slog.Warn("healthz oss probe failed", "err", err)
		}
	}
	// 把结果交回调用方。
	writeJSON(w, code, resp)
}
