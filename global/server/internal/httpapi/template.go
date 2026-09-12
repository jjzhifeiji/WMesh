package httpapi

import (
	"encoding/json"
	"net/http"

	"wmesh/global/internal/service"
)

func (h *Handler) mountTemplate(mux *http.ServeMux) { // 工艺/工程当前字段模版
	mux.HandleFunc("GET /v1/templates", h.getTemplate)
	mux.HandleFunc("POST /v1/templates", h.updateTemplate)
}

type templateResp struct {
	ID       string          `json:"id"`       // 稳定身份
	Kind     string          `json:"kind"`     // process / project
	Revision int64           `json:"revision"` // 当前修订
	Schema   json.RawMessage `json:"schema"`   // 字段表
	Digest   []byte          `json:"digest"`   // SHA-256
}

type updateTemplateReq struct {
	Kind     string          `json:"kind"`     // process / project
	Expected int64           `json:"expected"` // 期望修订
	Schema   json.RawMessage `json:"schema"`   // 字段表
}

func templateJSON(t service.ContentTemplate) templateResp {
	return templateResp{
		ID: t.ID.String(), Kind: t.Kind, Revision: t.Revision,
		Schema: json.RawMessage(t.Schema), Digest: t.Digest,
	}
}

func (h *Handler) getTemplate(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	row, err := h.svc.Templates.GetTemplate(r.Context(), bearer(r), kind)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, templateJSON(row))
}

func (h *Handler) updateTemplate(w http.ResponseWriter, r *http.Request) {
	var req updateTemplateReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	row, err := h.svc.Templates.UpdateTemplate(r.Context(), bearer(r), req.Kind, req.Expected, req.Schema)
	if err != nil {
		writeErr(w, err)
		return
	}
	h.fanoutTemplates(r.Context())
	writeJSON(w, http.StatusOK, templateJSON(row))
}
