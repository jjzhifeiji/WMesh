// Package httpapi 把厂内应用服务适配成 JSON HTTP。不绕过 Service，不把 SQL 原文抛给前端。
// 文件按域拆：auth / site / org / node / asset / template / update / policy / legacy；本文件只装配路由、探活和建厂引导。
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/secret"
	"wmesh/factory/internal/service"
)

// Handler 按 URL 里的工厂身份选库，并把会话头交给应用服务。
type Handler struct {
	Hub            *hub.Hub
	BootstrapToken string                      // 建厂引导共享密码，只用于 /internal/bootstrap
	WANURL         string                      // WAN 根地址，厂出站认领用；空则不能认领
	Version        string                      // 构建版本，随探活返回，便于核对升级是否生效
	OSSProbe       func(context.Context) error // 探对象存储是否在线；空表示本厂未接 OSS
}

// New 组装厂内 HTTP 适配器。
func New(h *hub.Hub, bootstrapToken, wanURL string) *Handler {
	return &Handler{Hub: h, BootstrapToken: bootstrapToken, WANURL: wanURL, Version: "dev"}
}

// Router 只装配探活、建厂引导和各域路由；未知 API 路径统一回 JSON 404。
func (h *Handler) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, _ *http.Request) { writeErr(w, domain.ErrNotFound) })
	mux.HandleFunc("/internal/", func(w http.ResponseWriter, _ *http.Request) { writeErr(w, domain.ErrNotFound) })
	mux.HandleFunc("POST /internal/bootstrap", h.bootstrap)
	h.mountSite(mux)
	h.mountAuth(mux)
	h.mountOrg(mux)
	h.mountNode(mux)
	h.mountAsset(mux)
	h.mountLegacy(mux)
	h.mountTemplate(mux)
	h.mountUpdate(mux)
	h.mountPolicy(mux)
	return mux
}

type bootReq struct {
	FactoryID string `json:"factoryId"` // 工厂稳定身份
	SALogin   string `json:"saLogin"`   // 初始超管登录名
	SADisplay string `json:"saDisplay"` // 初始超管显示名
}

type bootResp struct {
	PersonID        string `json:"personId"`        // 厂库账号身份
	ActivationToken string `json:"activationToken"` // 一次性 8 位激活码，禁止写入审计
}

type healthResp struct {
	Status  string `json:"status"`  // ok 或 degraded
	Version string `json:"version"` // 构建版本
	DB      string `json:"db"`      // ok 或 down
	OSS     string `json:"oss"`     // ok / down / off（未配置）
}

// healthz 库不通回 503 让编排判定不健康；OSS 掉线只标 degraded，不影响账号管理继续服务。
func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	resp := healthResp{Status: "ok", Version: h.Version, DB: "ok", OSS: "off"}
	code := http.StatusOK
	if err := h.Hub.Ping(ctx); err != nil {
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

// 用共享密码在目标厂库写入待启用初始超管；激活码只回调用方。
func (h *Handler) bootstrap(w http.ResponseWriter, r *http.Request) {
	// 共享密码恒定时间比对；没配密码时一律拒绝，不给空密码放行。
	if h.BootstrapToken == "" || !secret.Equal(bearer(r), h.BootstrapToken) {
		writeErr(w, domain.ErrUnauthorized)
		return
	}
	var req bootReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	fid, err := uuid.Parse(req.FactoryID)
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	// 建厂库并写入待启用初始超管。
	personID, token, err := h.Hub.Bootstrap(r.Context(), fid, req.SALogin, req.SADisplay)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, bootResp{PersonID: personID.String(), ActivationToken: token})
}

// 按 URL 工厂身份打开已有厂库；库不在就当未初始化。
func (h *Handler) withFactory(w http.ResponseWriter, r *http.Request, fn func(*service.Service)) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	// 打开已有厂库；库不在就当未初始化。
	svc, err := h.Hub.Service(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	fn(svc)
}
