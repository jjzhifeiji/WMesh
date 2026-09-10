package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/id"
)

type bindClientReq struct {
	ID        string `json:"id"`        // Client 稳定身份；空则由服务发号
	FactoryID string `json:"factoryId"` // 要绑到的工厂
	PublicKey string `json:"publicKey"` // 本机公钥，base64 或 hex
}

type rebindClientReq struct {
	FactoryID string `json:"factoryId"` // 改绑到的工厂
}

func (h *Handler) listClients(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.ListClients(r.Context(), bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h *Handler) bindClient(w http.ResponseWriter, r *http.Request) {
	var req bindClientReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	fid, err := uuid.Parse(req.FactoryID)
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	cid := uuid.Nil
	if req.ID != "" {
		cid, err = uuid.Parse(req.ID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
	} else {
		cid = id.New()
	}
	pub, err := decodePublicKey(req.PublicKey)
	if err != nil {
		writeErr(w, err)
		return
	}
	row, err := h.svc.BindClient(r.Context(), bearer(r), cid, fid, pub)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

func (h *Handler) rebindClient(w http.ResponseWriter, r *http.Request) {
	cid, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	var req rebindClientReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	fid, err := uuid.Parse(req.FactoryID)
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	row, err := h.svc.RebindClient(r.Context(), bearer(r), cid, fid)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) listFactoryOfflineGrants(w http.ResponseWriter, r *http.Request) {
	fid, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.ListPersonOfflineGrants(r.Context(), bearer(r), fid))
}
