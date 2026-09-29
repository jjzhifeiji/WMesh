package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

// 本机通道：登录后 inbox 与 HTTPS 拉闭包密文。
func (h *Handler) mountChannel(mux *http.ServeMux) {
	// 拉取这台设备的对账清单，不含正文。
	mux.HandleFunc("GET /v1/factories/{id}/clients/{clientId}/inbox", h.clientInbox)
	// 拉取过站密文，他机或未登录一律拒绝。
	mux.HandleFunc("GET /v1/factories/{id}/clients/{clientId}/closures/{projectId}", h.pullClientClosure)
	// 厂网登录后拉取与列表一致的资产清单。
	mux.HandleFunc("GET /v1/factories/{id}/pad/inbox", h.padClientInbox)
	// 厂网登录后拉取工程明文或工艺包。
	mux.HandleFunc("GET /v1/factories/{id}/pad/closures/{assetId}", h.padPullClientClosure)
}

// 登录人在这台上要对账的策略和获准闭包清单，不含正文。
func (h *Handler) clientInbox(w http.ResponseWriter, r *http.Request) {
	// 登录人在这台上要对账的策略和获准闭包清单，不含正文。
	h.withFactory(w, r, func(svc *service.Service) {
		// 设备编号必须合法，否则拒绝这次办理。
		clientID, err := uuid.Parse(r.PathValue("clientId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 拉取这台设备要对账的清单，不含正文。
		box, err := svc.Closure.ClientInbox(r.Context(), bearer(r), clientID)
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

// 过站重封后的闭包密文；他机或未登录拒绝。
func (h *Handler) pullClientClosure(w http.ResponseWriter, r *http.Request) {
	// 过站重封后的闭包密文；他机或未登录拒绝。
	h.withFactory(w, r, func(svc *service.Service) {
		// 设备编号必须合法，否则拒绝这次办理。
		clientID, err := uuid.Parse(r.PathValue("clientId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 工程编号必须合法，否则拒绝拉错包。
		projectID, err := uuid.Parse(r.PathValue("projectId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 拉取过站密文，他机或未登录一律拒绝。
		snap, err := svc.Closure.PullClientClosure(r.Context(), bearer(r), clientID, projectID)
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

// 厂网登录后拉与厂端列表同一份资产，不含正文。
func (h *Handler) padClientInbox(w http.ResponseWriter, r *http.Request) {
	// 厂网登录后拉与厂端列表同一份资产，不含正文。
	h.withFactory(w, r, func(svc *service.Service) {
		// 厂网登录后拉取与列表同一份清单。
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

// 厂网登录后拉工程明文或工艺过站包。
func (h *Handler) padPullClientClosure(w http.ResponseWriter, r *http.Request) {
	// 厂网登录后拉工程明文或工艺过站包。
	h.withFactory(w, r, func(svc *service.Service) {
		// 资产编号必须合法，否则拒绝这次办理。
		assetID, err := uuid.Parse(r.PathValue("assetId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 厂网登录后拉取工程明文或工艺包。
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
