package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

// 挂本厂到 WAN 通道现状，管理页横幅轮询。
func (h *Handler) mountWANStatus(mux *http.ServeMux) {
	// 已登录即可看，断线时每页顶上都要提示。
	mux.HandleFunc("GET /v1/factories/{id}/wan-status", h.wanStatus)
}

// 交回通道是否连着、最近连上时间和内容租约到期。
func (h *Handler) wanStatus(w http.ResponseWriter, r *http.Request) {
	// 开这一厂的库，库不在就当未初始化。
	h.withFactory(w, r, func(svc *service.Service) {
		// 会话无效或账号停用则拒绝，不再继续。
		if _, err := svc.Auth.RequireActive(r.Context(), bearer(r)); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// withFactory 已验过编号，这里直接取。
		fid := uuid.MustParse(r.PathValue("id"))
		// 把这一厂的通道现状交回。
		writeJSON(w, http.StatusOK, h.Hub.WANStatus(fid))
	})
}
