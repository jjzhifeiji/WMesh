package httpapi

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

const maxSoftwareUpload = 256 << 20 // 服务 tar / APK；JSON 再 base64 会胀，64MiB 不够

// 挂软件发布、厂自拉、本机云端确认和存储占用。
func (h *Handler) mountUpdate(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/software", h.listSoftware)
	mux.HandleFunc("POST /v1/software", h.publishSoftware)
	mux.HandleFunc("GET /v1/software/pending", h.pendingWANSoftware)
	mux.HandleFunc("GET /v1/software/current", h.currentWANSoftware)
	mux.HandleFunc("POST /v1/software/confirm", h.confirmWANSoftware)
	mux.HandleFunc("GET /v1/software/apply", h.applyWANSoftware)
	mux.HandleFunc("GET /v1/software/latest", h.latestSoftware)
	mux.HandleFunc("DELETE /v1/software/{kind}/{version}", h.deleteSoftware)
	mux.HandleFunc("POST /v1/software/gc", h.pruneSoftware)
	mux.HandleFunc("POST /v1/software/images/prune", h.requestImagePrune)
	mux.HandleFunc("GET /v1/software/images/prune", h.imagePrune)
	mux.HandleFunc("GET /v1/software/storage", h.storageUsage)
}

type softwareReleaseResp struct {
	Kind        string    `json:"kind"`        // wan_service / factory_service / client_apk
	Version     int64     `json:"version"`     // 单调整数
	VersionName string    `json:"versionName"` // 给人看的版本名
	Digest      []byte    `json:"digest"`      // SHA-256
	CreatedAt   time.Time `json:"createdAt"`   // 首次发布
	Keep        string    `json:"keep"`        // latest / installed / 空则可清
}

type pruneSoftwareReq struct {
	Kind string `json:"kind"` // 空则三类都清
}

type pruneSoftwareResp struct {
	Deleted int `json:"deleted"` // 删掉的份数
}

type publishSoftwareReq struct {
	Kind        string `json:"kind"`        // wan_service / factory_service / client_apk
	Version     int64  `json:"version"`     // 单调整数
	VersionName string `json:"versionName"` // 给人看的版本名
	Content     []byte `json:"content"`     // 包字节；JSON 为 base64
}

type confirmSoftwareReq struct {
	Kind    string `json:"kind"`    // 固定 wan_service
	Version int64  `json:"version"` // 与待确认版本一致
}

// 收成给前端的发布元数据，不含对象键。
func softwareReleaseJSON(row service.SoftwareRelease) softwareReleaseResp {
	return softwareReleaseResp{
		Kind: row.Kind, Version: row.Version, VersionName: row.VersionName,
		Digest: row.Digest, CreatedAt: row.CreatedAt,
	}
}

// 带保留标记的发布行。
func softwareItemJSON(row service.SoftwareItem) softwareReleaseResp {
	out := softwareReleaseJSON(row.SoftwareRelease)
	out.Keep = row.Keep
	return out
}

// 列出已发布软件版本。
func (h *Handler) listSoftware(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.Updates.ListSoftwareItems(r.Context(), bearer(r), r.URL.Query().Get("kind"))
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]softwareReleaseResp, 0, len(rows))
	for _, row := range rows {
		out = append(out, softwareItemJSON(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// 清掉一份不是最高也不是已装的旧包。
func (h *Handler) deleteSoftware(w http.ResponseWriter, r *http.Request) {
	version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || version < 1 {
		writeBadRequest(w, domain.ErrInvalidName)
		return
	}
	if err := h.svc.Updates.DeleteSoftware(r.Context(), bearer(r), r.PathValue("kind"), version); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// 清掉可删的旧包。
func (h *Handler) pruneSoftware(w http.ResponseWriter, r *http.Request) {
	var req pruneSoftwareReq
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
	}
	n, err := h.svc.Updates.PruneSoftware(r.Context(), bearer(r), req.Kind)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pruneSoftwareResp{Deleted: n})
}

// 请本机 updater 清无用 app 镜像。
func (h *Handler) requestImagePrune(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Updates.RequestImagePrune(r.Context(), bearer(r)); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// 看清镜像结果。
func (h *Handler) imagePrune(w http.ResponseWriter, r *http.Request) {
	row, err := h.svc.Updates.ImagePruneProgress(r.Context(), bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// 超管看本机磁盘、对象存储、库和镜像占用。
func (h *Handler) storageUsage(w http.ResponseWriter, r *http.Request) {
	row, err := h.svc.Updates.StorageUsage(r.Context(), bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// 发布一条只向前的软件版本；JSON 或 multipart 均可。
func (h *Handler) publishSoftware(w http.ResponseWriter, r *http.Request) {
	kind, versionName, version, body, err := readSoftwareUpload(w, r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	row, err := h.svc.Updates.PublishSoftware(r.Context(), bearer(r), kind, versionName, version, body)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, softwareReleaseJSON(row))
}

// 管理员看待确认的云端服务包；没有则 null。
func (h *Handler) pendingWANSoftware(w http.ResponseWriter, r *http.Request) {
	row, err := h.svc.Updates.PendingWANSoftware(r.Context(), bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	if row == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, softwareReleaseJSON(*row))
}

// 管理员看本机已装的云端服务版本。
func (h *Handler) currentWANSoftware(w http.ResponseWriter, r *http.Request) {
	row, err := h.svc.Updates.CurrentWANSoftware(r.Context(), bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, softwareReleaseJSON(row))
}

// 管理员确认后才写待切换；落地成功才记已装。
func (h *Handler) confirmWANSoftware(w http.ResponseWriter, r *http.Request) {
	var req confirmSoftwareReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	if err := h.svc.Updates.ConfirmWANUpdate(r.Context(), bearer(r), req.Kind, req.Version); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// 管理员看本机更换进度。
func (h *Handler) applyWANSoftware(w http.ResponseWriter, r *http.Request) {
	row, err := h.svc.Updates.ApplyProgress(r.Context(), bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// 已认领厂看该种类当前最高版本，不含字节。
func (h *Handler) latestSoftware(w http.ResponseWriter, r *http.Request) {
	fid, err := h.factoryProof(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	row, err := h.svc.Updates.LatestSoftware(r.Context(), fid, r.URL.Query().Get("kind"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, softwareReleaseJSON(row))
}

// 读 JSON 或表单里的包文件；超上限拒绝。
func readSoftwareUpload(w http.ResponseWriter, r *http.Request) (kind, versionName string, version int64, body []byte, err error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSoftwareUpload)
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(maxSoftwareUpload); err != nil {
			return "", "", 0, nil, err
		}
		kind = r.FormValue("kind")
		versionName = r.FormValue("versionName")
		version, err = strconv.ParseInt(strings.TrimSpace(r.FormValue("version")), 10, 64)
		if err != nil {
			return "", "", 0, nil, err
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			return "", "", 0, nil, err
		}
		defer f.Close()
		body, err = io.ReadAll(f)
		if err != nil {
			return "", "", 0, nil, err
		}
		return kind, versionName, version, body, nil
	}
	var req publishSoftwareReq
	if err := decodeJSON(r, &req); err != nil {
		return "", "", 0, nil, err
	}
	return req.Kind, req.VersionName, req.Version, req.Content, nil
}
