// Package httpapi 把厂内应用服务适配成 JSON HTTP。不绕过 Service，不把 SQL 原文抛给前端。
// 文件按域拆：auth / org / node / asset；本文件只装配路由、探活和建厂引导。
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
	BootstrapToken string                      // 建厂引导共享口令，只用于 /internal/bootstrap
	Version        string                      // 构建版本，随探活返回，便于核对升级是否生效
	OSSProbe       func(context.Context) error // 探对象存储是否在线；空表示本厂未接 OSS
}

// New 组装厂内 HTTP 适配器。
func New(h *hub.Hub, bootstrapToken string) *Handler {
	return &Handler{Hub: h, BootstrapToken: bootstrapToken, Version: "dev"}
}

// Router 只装配探活、建厂引导和各域路由；未知 API 路径统一回 JSON 404。
func (h *Handler) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, _ *http.Request) { writeErr(w, domain.ErrNotFound) })
	mux.HandleFunc("/internal/", func(w http.ResponseWriter, _ *http.Request) { writeErr(w, domain.ErrNotFound) })
	mux.HandleFunc("POST /internal/bootstrap", h.bootstrap)
	h.mountAuth(mux)
	h.mountOrg(mux)
	h.mountNode(mux)
	h.mountAsset(mux)
	return mux
}

type bootReq struct {
	FactoryID string `json:"factoryId"` // 工厂稳定身份
	SALogin   string `json:"saLogin"`   // 初始超管登录名
	SADisplay string `json:"saDisplay"` // 初始超管显示名
}

type bootResp struct {
	PersonID        string `json:"personId"`        // 厂库账号身份
	ActivationToken string `json:"activationToken"` // 一次性激活口令，禁止写入审计
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

func (h *Handler) bootstrap(w http.ResponseWriter, r *http.Request) {
	// 共享口令恒定时间比对；没配口令时一律拒绝，不给空口令放行。
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
	personID, token, err := h.Hub.Bootstrap(r.Context(), fid, req.SALogin, req.SADisplay)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, bootResp{PersonID: personID.String(), ActivationToken: token})
}

func (h *Handler) withFactory(w http.ResponseWriter, r *http.Request, fn func(*service.Service)) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	svc, err := h.Hub.Service(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	fn(svc)
}
