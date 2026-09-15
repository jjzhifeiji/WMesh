package httpapi

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/service"
)

const maxSoftwareUpload = 64 << 20 // 单次上传上限，挡住把通道和内存撑爆

// 挂软件发布与按厂下发。
func (h *Handler) mountUpdate(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/software", h.listSoftware)
	mux.HandleFunc("POST /v1/software", h.publishSoftware)
	mux.HandleFunc("POST /v1/software/distribute", h.distributeSoftware)
}

type softwareReleaseResp struct {
	Kind        string    `json:"kind"`        // factory_service / client_apk
	Version     int64     `json:"version"`     // 单调整数
	VersionName string    `json:"versionName"` // 给人看的版本名
	Digest      []byte    `json:"digest"`      // SHA-256
	CreatedAt   time.Time `json:"createdAt"`   // 首次发布
}

type publishSoftwareReq struct {
	Kind        string `json:"kind"`        // factory_service / client_apk
	Version     int64  `json:"version"`     // 单调整数
	VersionName string `json:"versionName"` // 给人看的版本名
	Content     []byte `json:"content"`     // 包字节；JSON 为 base64
}

type distributeSoftwareReq struct {
	Kind      string `json:"kind"`      // factory_service / client_apk
	Version   int64  `json:"version"`   // 已发布版本
	FactoryID string `json:"factoryId"` // 已认领目标厂
}

// 收成给前端的发布元数据，不含对象键。
func softwareReleaseJSON(row service.SoftwareRelease) softwareReleaseResp {
	return softwareReleaseResp{
		Kind: row.Kind, Version: row.Version, VersionName: row.VersionName,
		Digest: row.Digest, CreatedAt: row.CreatedAt,
	}
}

// 列出已发布软件版本。
func (h *Handler) listSoftware(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.Updates.ListSoftwareReleases(r.Context(), bearer(r), r.URL.Query().Get("kind"))
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]softwareReleaseResp, 0, len(rows))
	for _, row := range rows {
		out = append(out, softwareReleaseJSON(row))
	}
	writeJSON(w, http.StatusOK, out)
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

// 下到已认领厂，并立刻推给在线通道。
func (h *Handler) distributeSoftware(w http.ResponseWriter, r *http.Request) {
	var req distributeSoftwareReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	fid, err := uuid.Parse(req.FactoryID)
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	snap, err := h.svc.Updates.DistributeSoftware(r.Context(), bearer(r), req.Kind, req.Version, fid)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, softwareReleaseJSON(service.SoftwareRelease{
		Kind: snap.Kind, Version: snap.Version, VersionName: snap.VersionName, Digest: snap.Digest,
	}))
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
