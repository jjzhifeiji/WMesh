package httpapi

import (
	"net/http"
	"strconv"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/service"
)

// 挂本厂当前版本、待确认厂服务包、超管确认、本厂副本清理、存储占用，以及平板拉客户端包。
func (h *Handler) mountUpdate(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/factories/{id}/software/current", h.currentSoftware)
	mux.HandleFunc("GET /v1/factories/{id}/software/pending", h.pendingSoftware)
	mux.HandleFunc("POST /v1/factories/{id}/software/sync", h.syncSoftware)
	mux.HandleFunc("POST /v1/factories/{id}/software/confirm", h.confirmSoftware)
	mux.HandleFunc("GET /v1/factories/{id}/software/apply", h.applySoftware)
	mux.HandleFunc("GET /v1/factories/{id}/software", h.listSoftware)
	mux.HandleFunc("DELETE /v1/factories/{id}/software/{kind}/{version}", h.deleteSoftware)
	mux.HandleFunc("POST /v1/factories/{id}/software/gc", h.pruneSoftware)
	mux.HandleFunc("POST /v1/factories/{id}/software/images/prune", h.requestImagePrune)
	mux.HandleFunc("GET /v1/factories/{id}/software/images/prune", h.imagePrune)
	mux.HandleFunc("GET /v1/factories/{id}/software/storage", h.storageUsage)
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

type syncSoftwareReq struct {
	Kind string `json:"kind"` // factory_service / client_apk
}

type padClientSoftwareResp struct {
	Kind        string `json:"kind"`        // 固定 client_apk
	Version     int64  `json:"version"`     // 本厂已收最高版本
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // 包文件 SHA-256
}

type softwareItemResp struct {
	Kind        string `json:"kind"`        // factory_service / client_apk
	Version     int64  `json:"version"`     // 单调整数
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // SHA-256
	ReceivedAt  string `json:"receivedAt"`  // 收到时间
	Keep        string `json:"keep"`        // latest / installed / 空则可清
}

type pruneSoftwareReq struct {
	Kind string `json:"kind"` // 空则两类都清
}

type pruneSoftwareResp struct {
	Deleted int `json:"deleted"` // 删掉的份数
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

// 超管触发后台问最高版并静默拉；提示仍只看本厂完整副本。
func (h *Handler) syncSoftware(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req syncSoftwareReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.Updates.SyncFactorySoftware(r.Context(), bearer(r), req.Kind)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, pendingSoftwareResp{
			Kind: row.Kind, Version: row.Version, VersionName: row.VersionName,
		})
	})
}

// 超管确认后才写待切换；落地成功才记已装。
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

// 超管看本机更换进度。
func (h *Handler) applySoftware(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		row, err := svc.Updates.ApplyProgress(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 超管看本厂已收副本，按种类分列。
func (h *Handler) listSoftware(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		rows, err := svc.Updates.ListFactorySoftware(r.Context(), bearer(r), r.URL.Query().Get("kind"))
		if err != nil {
			writeErr(w, err)
			return
		}
		out := make([]softwareItemResp, 0, len(rows))
		for _, row := range rows {
			out = append(out, softwareItemResp{
				Kind: row.Kind, Version: row.Version, VersionName: row.VersionName,
				Digest: row.Digest, ReceivedAt: row.ReceivedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
				Keep: row.Keep,
			})
		}
		writeJSON(w, http.StatusOK, out)
	})
}

// 清掉一份不是最高也不是已装的本厂旧副本。
func (h *Handler) deleteSoftware(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
		if err != nil || version < 1 {
			writeBadRequest(w, domain.ErrInvalidName)
			return
		}
		if err := svc.Updates.DeleteFactorySoftware(r.Context(), bearer(r), r.PathValue("kind"), version); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// 清掉可删的本厂旧副本。
func (h *Handler) pruneSoftware(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req pruneSoftwareReq
		if r.ContentLength > 0 {
			if err := decodeJSON(r, &req); err != nil {
				writeBadRequest(w, err)
				return
			}
		}
		n, err := svc.Updates.PruneFactorySoftware(r.Context(), bearer(r), req.Kind)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, pruneSoftwareResp{Deleted: n})
	})
}

// 请本机 updater 清无用 app 镜像。
func (h *Handler) requestImagePrune(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		if err := svc.Updates.RequestImagePrune(r.Context(), bearer(r)); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
}

// 超管看清镜像结果。
func (h *Handler) imagePrune(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		row, err := svc.Updates.ImagePruneProgress(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 超管看本机磁盘、对象存储、本厂库和镜像占用。
func (h *Handler) storageUsage(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		row, err := svc.Updates.StorageUsage(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
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
