package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

// 本机通道：登录后 inbox 与 HTTPS 拉闭包密文。
func (h *Handler) mountChannel(mux *http.ServeMux) {
	// 厂网登录后拉取按登录人裁过的资产清单。
	mux.HandleFunc("GET /v1/factories/{id}/pad/inbox", h.padClientInbox)
	// 厂网登录后拉取工程或工艺的过站包。
	mux.HandleFunc("GET /v1/factories/{id}/pad/closures/{assetId}", h.padPullClientClosure)
}

// 厂网登录后拉按登录人裁过的资产清单，不含正文。
func (h *Handler) padClientInbox(w http.ResponseWriter, r *http.Request) {
	// 厂网登录后拉按登录人裁过的资产清单，不含正文。
	h.withFactory(w, r, func(svc *service.Service) {
		// 厂网登录后拉取按登录人裁过的清单。
		box, err := svc.Closure.PadClientInbox(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, box)
	})
}

// 厂网登录后拉工程或工艺过站包，正文都是信封。
func (h *Handler) padPullClientClosure(w http.ResponseWriter, r *http.Request) {
	// 厂网登录后拉工程或工艺过站包，正文都是信封。
	h.withFactory(w, r, func(svc *service.Service) {
		// 资产编号必须合法，否则拒绝这次办理。
		assetID, err := uuid.Parse(r.PathValue("assetId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 厂网登录后拉取工程或工艺的过站包。
		snap, err := svc.Closure.PadPullClientClosure(r.Context(), bearer(r), assetID)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, snap)
	})
}
