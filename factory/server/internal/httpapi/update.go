package httpapi

import (
	"net/http"

	"wmesh/factory/internal/service"
)

// 挂本厂当前版本、待确认厂服务包与超管确认。
func (h *Handler) mountUpdate(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/factories/{id}/software/current", h.currentSoftware)
	mux.HandleFunc("GET /v1/factories/{id}/software/pending", h.pendingSoftware)
	mux.HandleFunc("POST /v1/factories/{id}/software/confirm", h.confirmSoftware)
}

type currentSoftwareResp struct {
	Kind        string `json:"kind"`        // 固定 factory_service
	Version     int64  `json:"version"`     // 已确认安装版本；未装为 0
	VersionName string `json:"versionName"` // 已装版本名；未装为空
}

type pendingSoftwareResp struct {
	Kind        string `json:"kind"`        // 固定 factory_service
	Version     int64  `json:"version"`     // 待确认版本
	VersionName string `json:"versionName"` // 给人看的版本名
}

type confirmSoftwareReq struct {
	Kind    string `json:"kind"`    // 固定 factory_service
	Version int64  `json:"version"` // 与待确认版本一致
}

// 本厂登录者看当前已装的厂服务版本。
func (h *Handler) currentSoftware(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		row, err := svc.Updates.CurrentFactorySoftware(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, currentSoftwareResp{
			Kind: row.Kind, Version: row.Version, VersionName: row.VersionName,
		})
	})
}

// 超管看待确认的厂服务包；没有则 null。
func (h *Handler) pendingSoftware(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		row, err := svc.Updates.PendingFactorySoftware(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		if row == nil {
			writeJSON(w, http.StatusOK, nil)
			return
		}
		writeJSON(w, http.StatusOK, pendingSoftwareResp{
			Kind: row.Kind, Version: row.Version, VersionName: row.VersionName,
		})
	})
}

// 超管确认后才替换厂服务；失败保持旧版本。
func (h *Handler) confirmSoftware(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req confirmSoftwareReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		if err := svc.Updates.ConfirmFactoryUpdate(r.Context(), bearer(r), req.Kind, req.Version); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
