package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

// 本机通道：登录后 inbox 与 HTTPS 拉闭包密文。
func (h *Handler) mountChannel(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/factories/{id}/clients/{clientId}/inbox", h.clientInbox)
	mux.HandleFunc("GET /v1/factories/{id}/clients/{clientId}/closures/{projectId}", h.pullClientClosure)
}

// 登录人在这台上要对账的策略和获准闭包清单，不含正文。
func (h *Handler) clientInbox(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		clientID, err := uuid.Parse(r.PathValue("clientId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		box, err := svc.Closure.ClientInbox(r.Context(), bearer(r), clientID)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, box)
	})
}

// 过站重封后的闭包密文；他机或未登录拒绝。
func (h *Handler) pullClientClosure(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		clientID, err := uuid.Parse(r.PathValue("clientId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		projectID, err := uuid.Parse(r.PathValue("projectId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		snap, err := svc.Closure.PullClientClosure(r.Context(), bearer(r), clientID, projectID)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, snap)
	})
}
