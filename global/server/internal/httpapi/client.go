package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/id"
)

// 挂现场设备名录与分配；离线授权查询一律拒绝。
func (h *Handler) mountClient(mux *http.ServeMux) {
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

// 列出已登记现场设备。
func (h *Handler) listClients(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.Clients.ListClients(r.Context(), bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// 解析可选公钥后登记，并立刻推给已分配的厂。
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
	writeJSON(w, http.StatusCreated, row)
}

// 改名后把新名字推给所在厂。
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
	writeJSON(w, http.StatusOK, row)
}

// 分配后立刻推绑定给目标厂。
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
	writeJSON(w, http.StatusOK, row)
}

// 改分后先通知旧厂作废，再通知新厂绑定。
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
	row, err := h.svc.Clients.RebindClient(r.Context(), bearer(r), cid, fid)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// 从 WAN 查厂内人员离线授权，一律拒绝。
func (h *Handler) listFactoryOfflineGrants(w http.ResponseWriter, r *http.Request) {
	fid, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.Clients.ListPersonOfflineGrants(r.Context(), bearer(r), fid))
}
