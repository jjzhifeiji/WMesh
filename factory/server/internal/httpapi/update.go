package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/service"
)

// 挂本厂当前版本、待确认厂服务包、超管确认、本厂副本清理、存储占用，以及平板拉客户端包。
func (h *Handler) mountUpdate(mux *http.ServeMux) {
	// 登录者查看当前已装的厂服务版本。
	mux.HandleFunc("GET /v1/factories/{id}/software/current", h.currentSoftware)
	// 超管查看待确认的包，没有则为空。
	mux.HandleFunc("GET /v1/factories/{id}/software/pending", h.pendingSoftware)
	// 超管向平台拉取，不等待这次落地完成。
	mux.HandleFunc("POST /v1/factories/{id}/software/sync", h.syncSoftware)
	// 超管确认之后才允许切换版本。
	mux.HandleFunc("POST /v1/factories/{id}/software/confirm", h.confirmSoftware)
	// 超管查看本机更换进行到哪一步。
	mux.HandleFunc("GET /v1/factories/{id}/software/apply", h.applySoftware)
	// 超管查看本厂已收副本，可按种类筛选。
	mux.HandleFunc("GET /v1/factories/{id}/software", h.listSoftware)
	// 删除不是最高也不是已装的旧副本。
	mux.HandleFunc("DELETE /v1/factories/{id}/software/{kind}/{version}", h.deleteSoftware)
	// 清理可以删除的本厂旧副本。
	mux.HandleFunc("POST /v1/factories/{id}/software/gc", h.pruneSoftware)
	// 请求本机清理无用镜像，可以只点一条。
	mux.HandleFunc("POST /v1/factories/{id}/software/images/prune", h.requestImagePrune)
	// 超管查看清镜像的进度和结果。
	mux.HandleFunc("GET /v1/factories/{id}/software/images/prune", h.imagePrune)
	// 超管查看磁盘、库和镜像占用。
	mux.HandleFunc("GET /v1/factories/{id}/software/storage", h.storageUsage)
	// 登录平板查看客户端包信息，不含字节。
	mux.HandleFunc("GET /v1/factories/{id}/pad/software/client", h.padClientSoftware)
	// 登录平板按版本下载包，摘要不对不给。
	mux.HandleFunc("GET /v1/factories/{id}/pad/software/client/{version}", h.pullPadClientSoftware)
}

// 当前已经装上的厂服务版本。
type currentSoftwareResp struct {
	Kind        string `json:"kind"`        // 固定 factory_service
	Version     int64  `json:"version"`     // 已确认安装版本；未装为 0
	VersionName string `json:"versionName"` // 已装版本名；未装为空
}

// 待确认的厂服务包，没有就为空。
type pendingSoftwareResp struct {
	Kind        string `json:"kind"`        // 固定 factory_service
	Version     int64  `json:"version"`     // 待确认版本
	VersionName string `json:"versionName"` // 给人看的版本名
}

// 超管确认要切换的种类和版本。
type confirmSoftwareReq struct {
	Kind    string `json:"kind"`    // 固定 factory_service
	Version int64  `json:"version"` // 与待确认版本一致
}

// 要向平台询问的软件种类。
type syncSoftwareReq struct {
	Kind string `json:"kind"` // factory_service / client_apk
}

// 平板能看见的客户端包信息，不含字节。
type padClientSoftwareResp struct {
	Kind        string `json:"kind"`        // 固定 client_apk
	Version     int64  `json:"version"`     // 本厂已收最高版本
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // 包文件 SHA-256
}

// 一份已收副本的元数据，不含安装字节。
type softwareItemResp struct {
	Kind        string `json:"kind"`        // factory_service / client_apk
	Version     int64  `json:"version"`     // 单调整数
	VersionName string `json:"versionName"` // 给人看的版本名
	Digest      []byte `json:"digest"`      // SHA-256
	ReceivedAt  string `json:"receivedAt"`  // 收到时间
	Keep        string `json:"keep"`        // latest / installed / 空则可清
}

// 点名清理一条，或显式声明清理全部。
type pruneImagesReq struct {
	Ref string `json:"ref"` // 只清这一条
	All bool   `json:"all"` // 清全部可清；须显式 true
}

// 要清理的软件种类，空着表示不限。
type pruneSoftwareReq struct {
	Kind string `json:"kind"` // 空则两类都清
}

// 这一次删掉了多少可以删的旧副本。
type pruneSoftwareResp struct {
	Deleted int `json:"deleted"` // 删掉的份数
}

// 本厂登录者看当前已装的厂服务版本。
func (h *Handler) currentSoftware(w http.ResponseWriter, r *http.Request) {
	// 本厂登录者看当前已装的厂服务版本。
	h.withFactory(w, r, func(svc *service.Service) {
		// 查看当前已经装上的厂服务版本。
		row, err := svc.Updates.CurrentFactorySoftware(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, currentSoftwareResp{
			Kind: row.Kind, Version: row.Version, VersionName: row.VersionName,
		})
	})
}

// 超管看待确认的厂服务包；没有则 null。
func (h *Handler) pendingSoftware(w http.ResponseWriter, r *http.Request) {
	// 超管看待确认的包，没有就回空。
	h.withFactory(w, r, func(svc *service.Service) {
		// 查看待确认的包，没有则告诉调用方为空。
		row, err := svc.Updates.PendingFactorySoftware(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 没有可给的包就回空，不当成错误。
		if row == nil {
			// 没有可给的记录就回空，不当成错误。
			writeJSON(w, http.StatusOK, nil)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, pendingSoftwareResp{
			Kind: row.Kind, Version: row.Version, VersionName: row.VersionName,
		})
	})
}

// 超管触发后台问最高版并静默拉；提示仍只看本厂完整副本。
func (h *Handler) syncSoftware(w http.ResponseWriter, r *http.Request) {
	// 超管触发后台问最高版并静默拉；提示仍只看本厂完整副本。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接要向平台询问的种类。
		var req syncSoftwareReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 向平台询问并拉取，不等待落地完成。
		row, err := svc.Updates.SyncFactorySoftware(r.Context(), bearer(r), req.Kind)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, pendingSoftwareResp{
			Kind: row.Kind, Version: row.Version, VersionName: row.VersionName,
		})
	})
}

// 超管确认后才写待切换；落地成功才记已装。
func (h *Handler) confirmSoftware(w http.ResponseWriter, r *http.Request) {
	// 超管确认后才写待切换；落地成功才记已装。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接要确认的种类和版本。
		var req confirmSoftwareReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Updates.ConfirmFactoryUpdate(r.Context(), bearer(r), req.Kind, req.Version); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 办理成功，且没有正文需要返回。
		w.WriteHeader(http.StatusNoContent)
	})
}

// 超管看本机更换进度。
func (h *Handler) applySoftware(w http.ResponseWriter, r *http.Request) {
	// 超管看本机更换进度。
	h.withFactory(w, r, func(svc *service.Service) {
		// 查看本机更换进行到哪一步。
		row, err := svc.Updates.ApplyProgress(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, row)
	})
}

// 超管看本厂已收副本，按种类分列。
func (h *Handler) listSoftware(w http.ResponseWriter, r *http.Request) {
	// 超管看本厂已收副本，按种类分列。
	h.withFactory(w, r, func(svc *service.Service) {
		// 读取种类，空着表示不按种类限制。
		rows, err := svc.Updates.ListFactorySoftware(r.Context(), bearer(r), r.URL.Query().Get("kind"))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 先留出列表，再逐条放进要返回的内容。
		out := make([]softwareItemResp, 0, len(rows))
		// 逐条收成回包，不带不该给出的字节。
		for _, row := range rows {
			// 把这一项收进结果，供后面返回。
			out = append(out, softwareItemResp{
				Kind: row.Kind, Version: row.Version, VersionName: row.VersionName,
				Digest: row.Digest, ReceivedAt: row.ReceivedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
				Keep: row.Keep,
			})
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, out)
	})
}

// 清掉一份不是最高也不是已装的本厂旧副本。
func (h *Handler) deleteSoftware(w http.ResponseWriter, r *http.Request) {
	// 清掉一份不是最高也不是已装的本厂旧副本。
	h.withFactory(w, r, func(svc *service.Service) {
		// 从路径读取版本，不是数字则拒绝。
		version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
		// 版本不是正整数则拒绝，避免下错或删错。
		if err != nil || version < 1 {
			// 名称或版本不合法，回非法请求。
			writeBadRequest(w, domain.ErrInvalidName)
			return
		}
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Updates.DeleteFactorySoftware(r.Context(), bearer(r), r.PathValue("kind"), version); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 办理成功，且没有正文需要返回。
		w.WriteHeader(http.StatusNoContent)
	})
}

// 清掉可删的本厂旧副本。
func (h *Handler) pruneSoftware(w http.ResponseWriter, r *http.Request) {
	// 清掉可删的本厂旧副本。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接要清理的种类，空着表示不限。
		var req pruneSoftwareReq
		// 有正文才解析种类，空请求表示不限。
		if r.ContentLength > 0 {
			// 正文无法解析则拒绝，不进入业务。
			if err := decodeJSON(r, &req); err != nil {
				// 请求不合法，回非法请求并不进入业务。
				writeBadRequest(w, err)
				return
			}
		}
		// 清理可以删除的旧副本，在用的留下。
		n, err := svc.Updates.PruneFactorySoftware(r.Context(), bearer(r), req.Kind)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, pruneSoftwareResp{Deleted: n})
	})
}

// 请本机 updater 清无用 app 镜像；可点名单条。
func (h *Handler) requestImagePrune(w http.ResponseWriter, r *http.Request) {
	// 请本机清理无用镜像，可以只点一条。
	h.withFactory(w, r, func(svc *service.Service) {
		// 按这条样例解析清理范围。
		ref, err := imagePruneRef(r)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Updates.RequestImagePrune(r.Context(), bearer(r), ref); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 已经交给本机处理，先回已受理。
		w.WriteHeader(http.StatusAccepted)
	})
}

// 点名须带 ref；全部须 all=true。空请求拒绝，避免误清光。
func imagePruneRef(r *http.Request) (string, error) {
	// 承接点名或全部清理的标记。
	var req pruneImagesReq
	// 正文无法解析则拒绝，不进入业务。
	if err := decodeJSON(r, &req); err != nil {
		// 空正文当成没写参数，拒绝以免误清全部。
		if errors.Is(err, io.EOF) {
			return "", domain.ErrInvalidName
		}
		return "", err
	}
	// 去掉空白后才算点名，纯空白当成没写。
	ref := strings.TrimSpace(req.Ref)
	// 点了名就只清这一条，不再扩大范围。
	if ref != "" {
		return ref, nil
	}
	// 只有显式要求全部，才允许不带名字。
	if req.All {
		return "", nil
	}
	return "", domain.ErrInvalidName
}

// 超管看清镜像结果。
func (h *Handler) imagePrune(w http.ResponseWriter, r *http.Request) {
	// 超管查看清镜像进度，其他人拒绝。
	h.withFactory(w, r, func(svc *service.Service) {
		// 查看清镜像的进度，未完成也能看。
		row, err := svc.Updates.ImagePruneProgress(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, row)
	})
}

// 超管看本机磁盘、对象存储、本厂库和镜像占用。
func (h *Handler) storageUsage(w http.ResponseWriter, r *http.Request) {
	// 超管看本机磁盘、对象存储、本厂库和镜像占用。
	h.withFactory(w, r, func(svc *service.Service) {
		// 查看磁盘、对象存储、库和镜像各占多少。
		row, err := svc.Updates.StorageUsage(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, row)
	})
}

// 登录平板看本厂已收的客户端包元数据，不含字节。
func (h *Handler) padClientSoftware(w http.ResponseWriter, r *http.Request) {
	// 登录平板看本厂已收的客户端包元数据，不含字节。
	h.withFactory(w, r, func(svc *service.Service) {
		// 登录平板看本厂已收客户端包元数据。
		row, err := svc.Updates.PadClientSoftware(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 没有可给的包就回空，不当成错误。
		if row == nil {
			// 没有可给的记录就回空，不当成错误。
			writeJSON(w, http.StatusOK, nil)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, padClientSoftwareResp{
			Kind: row.Kind, Version: row.Version, VersionName: row.VersionName, Digest: row.Digest,
		})
	})
}

// 登录平板按版本拉客户端包字节；摘要不对不给。
func (h *Handler) pullPadClientSoftware(w http.ResponseWriter, r *http.Request) {
	// 登录平板按版本拉客户端包字节；摘要不对不给。
	h.withFactory(w, r, func(svc *service.Service) {
		// 从路径读取版本，不是数字则拒绝。
		version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
		// 版本不是正整数则拒绝，避免下错或删错。
		if err != nil || version < 1 {
			// 名称或版本不合法，回非法请求。
			writeBadRequest(w, domain.ErrInvalidName)
			return
		}
		// 按版本拉 APK 字节。
		body, err := svc.Updates.PullPadClientSoftware(r.Context(), bearer(r), version)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 按区间写出安装包，摘要已在业务核对过。
		writeBytes(w, r, body)
	})
}
