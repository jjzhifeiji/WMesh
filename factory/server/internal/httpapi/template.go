package httpapi

import (
	"encoding/json"
	"net/http"

	"wmesh/factory/internal/service"
)

// 本厂已收字段模版。
func (h *Handler) mountTemplate(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/factories/{id}/templates", h.getTemplate)
}

type templateResp struct {
	ID       string          `json:"id"`       // 与云端相同
	Kind     string          `json:"kind"`     // process / project
	Revision int64           `json:"revision"` // 已收修订
	Schema   json.RawMessage `json:"schema"`   // 字段表
	Digest   []byte          `json:"digest"`   // SHA-256
}

// 读本厂已收字段模版。
func (h *Handler) getTemplate(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		row, err := svc.Templates.GetTemplate(r.Context(), bearer(r), r.URL.Query().Get("kind"))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, templateResp{
			ID: row.ID.String(), Kind: row.Kind, Revision: row.Revision,
			Schema: json.RawMessage(row.Schema), Digest: row.Digest,
		})
	})
}
