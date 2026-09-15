// Package httpapi 把 WAN 应用服务适配成 JSON HTTP。不绕过 Service，不把 SQL 原文抛给前端。
// 文件按域拆：auth / directory / channel / client / asset / template / update；本文件只装配路由和探活。
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

// Handler 把会话头和应用服务接到路由上。
type Handler struct {
	svc      *service.Service
	version  string                      // 构建版本，随探活返回，便于核对升级是否生效
	OSSProbe func(context.Context) error // 探对象存储是否在线；空表示未接 OSS
}

// New 组装 WAN HTTP 适配器。
func New(svc *service.Service, version string) *Handler {
	return &Handler{svc: svc, version: version}
}

// Router 只装配探活和各域路由；未知 API 路径统一回 JSON 404。
func (h *Handler) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, _ *http.Request) { writeErr(w, domain.ErrNotFound) })
	h.mountAuth(mux)
	h.mountDirectory(mux)
	h.mountChannel(mux)
	h.mountClient(mux)
	h.mountAsset(mux)
	h.mountTemplate(mux)
	h.mountUpdate(mux)
	return mux
}

type healthResp struct {
	Status  string `json:"status"`  // ok 或 degraded
	Version string `json:"version"` // 构建版本
	DB      string `json:"db"`      // ok 或 down
	OSS     string `json:"oss"`     // ok / down / off（未配置）
}

// healthz 库不通回 503 让编排判定不健康；OSS 掉线只标 degraded，不影响名录管理继续服务。
func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	resp := healthResp{Status: "ok", Version: h.version, DB: "ok", OSS: "off"}
	code := http.StatusOK
	if err := h.svc.Ping(ctx); err != nil {
		resp.Status, resp.DB, code = "degraded", "down", http.StatusServiceUnavailable
		slog.Warn("healthz db ping failed", "err", err)
	}
	if h.OSSProbe != nil {
		resp.OSS = "ok"
		if err := h.OSSProbe(ctx); err != nil {
			resp.Status, resp.OSS = "degraded", "down"
			slog.Warn("healthz oss probe failed", "err", err)
		}
	}
	writeJSON(w, code, resp)
}
