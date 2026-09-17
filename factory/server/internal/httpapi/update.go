package httpapi

import (
	"net/http"
	"strconv"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/service"
)

// 挂本厂当前版本、待确认厂服务包、超管确认，以及平板拉客户端包。
func (h *Handler) mountUpdate(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/factories/{id}/software/current", h.currentSoftware)
	mux.HandleFunc("GET /v1/factories/{id}/software/pending", h.pendingSoftware)
	mux.HandleFunc("POST /v1/factories/{id}/software/confirm", h.confirmSoftware)
	mux.HandleFunc("GET /v1/factories/{id}/pad/software/client", h.padClientSoftware)
	mux.HandleFunc("GET /v1/factories/{id}/pad/software/client/{version}", h.pullPadClientSoftware)
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

type padClientSoftwareResp struct {
	Kind        string `json:"kind"`        // 固定 client_apk
	Version     int64  `json:"version"`     // 本厂已收最高版本
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // 包文件 SHA-256
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

// 登录平板看本厂已收的客户端包元数据，不含字节。
func (h *Handler) padClientSoftware(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		// 登录平板看本厂已收客户端包元数据。
		row, err := svc.Updates.PadClientSoftware(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		if row == nil {
			writeJSON(w, http.StatusOK, nil)
			return
		}
		writeJSON(w, http.StatusOK, padClientSoftwareResp{
			Kind: row.Kind, Version: row.Version, VersionName: row.VersionName, Digest: row.Digest,
		})
	})
}

// 登录平板按版本拉客户端包字节；摘要不对不给。
func (h *Handler) pullPadClientSoftware(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
		if err != nil || version < 1 {
			writeBadRequest(w, domain.ErrInvalidName)
			return
		}
		// 按版本拉 APK 字节。
		body, err := svc.Updates.PullPadClientSoftware(r.Context(), bearer(r), version)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeBytes(w, body)
	})
}
