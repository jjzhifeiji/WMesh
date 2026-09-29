package httpapi

import (
	"errors"
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
	// 列出已发布的软件版本。
	mux.HandleFunc("GET /v1/software", h.listSoftware)
	// 发布一条只向前的软件版本。
	mux.HandleFunc("POST /v1/software", h.publishSoftware)
	// 看待确认的云端服务包，没有则空。
	mux.HandleFunc("GET /v1/software/pending", h.pendingWANSoftware)
	// 看本机已经装上的云端服务版本。
	mux.HandleFunc("GET /v1/software/current", h.currentWANSoftware)
	// 管理员确认后才写入待切换。
	mux.HandleFunc("POST /v1/software/confirm", h.confirmWANSoftware)
	// 看本机更换云端包的进度。
	mux.HandleFunc("GET /v1/software/apply", h.applyWANSoftware)
	// 已认领厂看该种类当前最高版本。
	mux.HandleFunc("GET /v1/software/latest", h.latestSoftware)
	// 删掉既不是最高也不是已装的旧包。
	mux.HandleFunc("DELETE /v1/software/{kind}/{version}", h.deleteSoftware)
	// 清掉可以删除的旧包。
	mux.HandleFunc("POST /v1/software/gc", h.pruneSoftware)
	// 请本机清无用镜像，可以只点一条。
	mux.HandleFunc("POST /v1/software/images/prune", h.requestImagePrune)
	// 查看清镜像的进度和结果。
	mux.HandleFunc("GET /v1/software/images/prune", h.imagePrune)
	// 超管查看磁盘、库和镜像占用。
	mux.HandleFunc("GET /v1/software/storage", h.storageUsage)
}

// 给前端的发布元数据，不含对象键。
type softwareReleaseResp struct {
	Kind        string    `json:"kind"`        // wan_service / factory_service / client_apk
	Version     int64     `json:"version"`     // 单调整数
	VersionName string    `json:"versionName"` // 给人看的版本名
	Digest      []byte    `json:"digest"`      // SHA-256
	CreatedAt   time.Time `json:"createdAt"`   // 首次发布
	Keep        string    `json:"keep"`        // latest / installed / 空则可清
}

// 清镜像须点名，或显式声明全部。
type pruneImagesReq struct {
	Ref string `json:"ref"` // 只清这一条
	All bool   `json:"all"` // 清全部可清；须显式 true
}

// 清旧包，可只清一种，空则三类都清。
type pruneSoftwareReq struct {
	Kind string `json:"kind"` // 空则三类都清
}

// 这次实际删掉的旧包份数。
type pruneSoftwareResp struct {
	Deleted int `json:"deleted"` // 删掉的份数
}

// 用 JSON 发布的软件包，超限拒绝。
type publishSoftwareReq struct {
	Kind        string `json:"kind"`        // wan_service / factory_service / client_apk
	Version     int64  `json:"version"`     // 单调整数
	VersionName string `json:"versionName"` // 给人看的版本名
	Content     []byte `json:"content"`     // 包字节；JSON 为 base64
}

// 确认本机待切换的云端包，版本须一致。
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
	// 收成发布元数据，对象键不给前端。
	out := softwareReleaseJSON(row.SoftwareRelease)
	// 标上能否清掉，避免误删已装版本。
	out.Keep = row.Keep
	return out
}

// 列出已发布软件版本。
func (h *Handler) listSoftware(w http.ResponseWriter, r *http.Request) {
	// 读取种类，空则由服务决定是否拒绝。
	rows, err := h.svc.Updates.ListSoftwareItems(r.Context(), bearer(r), r.URL.Query().Get("kind"))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 按条数准备给前端的列表，空的也要回。
	out := make([]softwareReleaseResp, 0, len(rows))
	// 逐条收成给前端的结果，空列表也要回。
	for _, row := range rows {
		// 收成带保留标记的发布行，不含对象键。
		out = append(out, softwareItemJSON(row))
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, out)
}

// 清掉一份不是最高也不是已装的旧包。
func (h *Handler) deleteSoftware(w http.ResponseWriter, r *http.Request) {
	// 从路径取版本，不是正整数就拒绝。
	version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	// 参数不合法就按坏请求拒绝。
	if err != nil || version < 1 {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, domain.ErrInvalidName)
		return
	}
	// 从路径取种类，不认识就拒绝。
	if err := h.svc.Updates.DeleteSoftware(r.Context(), bearer(r), r.PathValue("kind"), version); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 成功无正文，调用方不要当成失败。
	w.WriteHeader(http.StatusNoContent)
}

// 清掉可删的旧包。
func (h *Handler) pruneSoftware(w http.ResponseWriter, r *http.Request) {
	// 接住请求体，解析失败再拒绝。
	var req pruneSoftwareReq
	// 有正文才解析种类，空体表示三类都清。
	if r.ContentLength > 0 {
		// 未知字段或坏 JSON 按坏请求拒绝。
		if err := decodeJSON(r, &req); err != nil {
			// 把不合法的原因回给调用方。
			writeBadRequest(w, err)
			return
		}
	}
	// 清掉可删旧包，未登录拒绝。
	n, err := h.svc.Updates.PruneSoftware(r.Context(), bearer(r), req.Kind)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, pruneSoftwareResp{Deleted: n})
}

// 请本机 updater 清无用 app 镜像；可点名单条。
func (h *Handler) requestImagePrune(w http.ResponseWriter, r *http.Request) {
	// 解析点名或全部，空请求必须拒绝。
	ref, err := imagePruneRef(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 请本机清镜像，未登录或参数错则拒绝。
	if err := h.svc.Updates.RequestImagePrune(r.Context(), bearer(r), ref); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 已收下清理请求，完成与否另查进度。
	w.WriteHeader(http.StatusAccepted)
}

// 点名须带 ref；全部须 all=true。空请求拒绝，避免误清光。
func imagePruneRef(r *http.Request) (string, error) {
	// 接住请求体，解析失败再拒绝。
	var req pruneImagesReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 空体不能当成清全部，以免误删镜像。
		if errors.Is(err, io.EOF) {
			return "", domain.ErrInvalidName
		}
		return "", err
	}
	// 去掉空白，空引用不能当成已经点名。
	ref := strings.TrimSpace(req.Ref)
	// 点了名就只清这一条，不再看全部。
	if ref != "" {
		return ref, nil
	}
	// 显式声明全部才清光，否则拒绝。
	if req.All {
		return "", nil
	}
	return "", domain.ErrInvalidName
}

// 看清镜像结果。
func (h *Handler) imagePrune(w http.ResponseWriter, r *http.Request) {
	// 读取清镜像进度，未登录拒绝。
	row, err := h.svc.Updates.ImagePruneProgress(r.Context(), bearer(r))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 超管看本机磁盘、对象存储、库和镜像占用。
func (h *Handler) storageUsage(w http.ResponseWriter, r *http.Request) {
	// 读取占用，不是超管就拒绝。
	row, err := h.svc.Updates.StorageUsage(r.Context(), bearer(r))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 发布一条只向前的软件版本；JSON 或 multipart 均可。
func (h *Handler) publishSoftware(w http.ResponseWriter, r *http.Request) {
	// 读 JSON 或表单里的包，超限或坏包就拒绝。
	kind, versionName, version, body, err := readSoftwareUpload(w, r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 发布只向前的版本，回退版本拒绝。
	row, err := h.svc.Updates.PublishSoftware(r.Context(), bearer(r), kind, versionName, version, body)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 创建成功，把新记录交回调用方。
	writeJSON(w, http.StatusCreated, softwareReleaseJSON(row))
}

// 管理员看待确认的云端服务包；没有则 null。
func (h *Handler) pendingWANSoftware(w http.ResponseWriter, r *http.Request) {
	// 看待确认的云端包，没有不算故障。
	row, err := h.svc.Updates.PendingWANSoftware(r.Context(), bearer(r))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 没有待确认的包就回空，不当成故障。
	if row == nil {
		// 没有待确认的包，回空给前端。
		writeJSON(w, http.StatusOK, nil)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, softwareReleaseJSON(*row))
}

// 管理员看本机已装的云端服务版本。
func (h *Handler) currentWANSoftware(w http.ResponseWriter, r *http.Request) {
	// 看本机已装版本，未登录拒绝。
	row, err := h.svc.Updates.CurrentWANSoftware(r.Context(), bearer(r))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, softwareReleaseJSON(row))
}

// 管理员确认后才写待切换；落地成功才记已装。
func (h *Handler) confirmWANSoftware(w http.ResponseWriter, r *http.Request) {
	// 接住请求体，解析失败再拒绝。
	var req confirmSoftwareReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 确认后才写待切换，版本不一致则拒绝。
	if err := h.svc.Updates.ConfirmWANUpdate(r.Context(), bearer(r), req.Kind, req.Version); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 成功无正文，调用方不要当成失败。
	w.WriteHeader(http.StatusNoContent)
}

// 管理员看本机更换进度。
func (h *Handler) applyWANSoftware(w http.ResponseWriter, r *http.Request) {
	// 看本机更换进度，未登录拒绝。
	row, err := h.svc.Updates.ApplyProgress(r.Context(), bearer(r))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 已认领厂看该种类当前最高版本，不含字节。
func (h *Handler) latestSoftware(w http.ResponseWriter, r *http.Request) {
	// 留下厂或资产身份，后面用来认领或升档。
	fid, err := h.factoryProof(r)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 读取种类，空则由服务决定是否拒绝。
	row, err := h.svc.Updates.LatestSoftware(r.Context(), fid, r.URL.Query().Get("kind"))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, softwareReleaseJSON(row))
}

// 读 JSON 或表单里的包文件；超上限拒绝。
func readSoftwareUpload(w http.ResponseWriter, r *http.Request) (kind, versionName string, version int64, body []byte, err error) {
	// 超过上限直接掐断，避免大包拖垮进程。
	r.Body = http.MaxBytesReader(w, r.Body, maxSoftwareUpload)
	// 看是表单还是 JSON，两条路不能混。
	ct := r.Header.Get("Content-Type")
	// 表单上传走文件，其余按 JSON 包体。
	if strings.HasPrefix(ct, "multipart/form-data") {
		// 表单读失败或超限，整包拒绝。
		if err := r.ParseMultipartForm(maxSoftwareUpload); err != nil {
			return "", "", 0, nil, err
		}
		// 从表单取出种类，缺了由服务拒绝。
		kind = r.FormValue("kind")
		// 取出给人看的版本名。
		versionName = r.FormValue("versionName")
		// 去掉空白，空串表示这次不带公钥。
		version, err = strconv.ParseInt(strings.TrimSpace(r.FormValue("version")), 10, 64)
		// 失败把原因交回去，避免留下残缺结果。
		if err != nil {
			return "", "", 0, nil, err
		}
		// 取出包文件，没有文件就拒绝发布。
		f, _, err := r.FormFile("file")
		// 失败把原因交回去，避免留下残缺结果。
		if err != nil {
			return "", "", 0, nil, err
		}
		// 用完就关上，避免连接或文件一直占着。
		defer f.Close()
		// 读完全包，读失败就拒绝这次发布。
		body, err = io.ReadAll(f)
		// 失败把原因交回去，避免留下残缺结果。
		if err != nil {
			return "", "", 0, nil, err
		}
		return kind, versionName, version, body, nil
	}
	// 接住请求体，解析失败再拒绝。
	var req publishSoftwareReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		return "", "", 0, nil, err
	}
	return req.Kind, req.VersionName, req.Version, req.Content, nil
}
