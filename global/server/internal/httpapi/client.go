package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/id"
)

func (h *Handler) mountClient(mux *http.ServeMux) { // 现场设备名录与分配；离线授权查询一律拒绝
	mux.HandleFunc("GET /v1/clients", h.listClients)
	mux.HandleFunc("POST /v1/clients", h.registerClient)
	mux.HandleFunc("PATCH /v1/clients/{id}", h.renameClient)
	mux.HandleFunc("POST /v1/clients/{id}/assign", h.assignClient)
	mux.HandleFunc("POST /v1/clients/{id}/rebind", h.rebindClient)
	mux.HandleFunc("GET /v1/factories/{id}/offline-grants", h.listFactoryOfflineGrants)
}

type registerClientReq struct {
	ID        string `json:"id"`        // 稳定身份；空则由服务发号
	Name      string `json:"name"`      // 给人看的名字
	FactoryID string `json:"factoryId"` // 可当场分给这家厂
	PublicKey string `json:"publicKey"` // 可选；现场上线后再登记
}

type renameClientReq struct {
	Name string `json:"name"` // 给人看的新名字
}

type assignClientReq struct {
	FactoryID string `json:"factoryId"` // 分到的工厂
}

func (h *Handler) listClients(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.Clients.ListClients(r.Context(), bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h *Handler) registerClient(w http.ResponseWriter, r *http.Request) {
	var req registerClientReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	cid := uuid.Nil
	if req.ID != "" {
		var err error
		cid, err = uuid.Parse(req.ID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
	} else {
		cid = id.New()
	}
	fid := uuid.Nil
	if req.FactoryID != "" {
		var err error
		fid, err = uuid.Parse(req.FactoryID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
	}
	pub, err := decodeOptionalPublicKey(req.PublicKey)
	if err != nil {
		writeErr(w, err)
		return
	}
	row, err := h.svc.Clients.RegisterClient(r.Context(), bearer(r), req.Name, cid, fid, pub)
	if err != nil {
		writeErr(w, err)
		return
	}
	h.pushClientToFactory(row, nil)
	writeJSON(w, http.StatusCreated, row)
}

func (h *Handler) renameClient(w http.ResponseWriter, r *http.Request) {
	cid, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	var req renameClientReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	row, err := h.svc.Clients.RenameClient(r.Context(), bearer(r), cid, req.Name)
	if err != nil {
		writeErr(w, err)
		return
	}
	h.pushClientToFactory(row, nil)
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) assignClient(w http.ResponseWriter, r *http.Request) {
	cid, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	var req assignClientReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	fid, err := uuid.Parse(req.FactoryID)
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	row, err := h.svc.Clients.AssignClient(r.Context(), bearer(r), cid, fid)
	if err != nil {
		writeErr(w, err)
		return
	}
	h.pushClientToFactory(row, nil)
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) rebindClient(w http.ResponseWriter, r *http.Request) {
	cid, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	var req assignClientReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	fid, err := uuid.Parse(req.FactoryID)
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	prev, _ := h.svc.Clients.ClientByID(r.Context(), cid)
	row, err := h.svc.Clients.RebindClient(r.Context(), bearer(r), cid, fid)
	if err != nil {
		writeErr(w, err)
		return
	}
	var old *uuid.UUID
	if prev.FactoryID != nil {
		old = prev.FactoryID
	}
	h.pushClientToFactory(row, old)
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) listFactoryOfflineGrants(w http.ResponseWriter, r *http.Request) {
	fid, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.Clients.ListPersonOfflineGrants(r.Context(), bearer(r), fid))
}
