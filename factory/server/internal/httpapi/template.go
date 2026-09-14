package httpapi

import (
	"encoding/json"
	"net/http"

	"wmesh/factory/internal/service"
)

// 本厂已收字段模版。
func (h *Handler) mountTemplate(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/factories/{id}/templates", h.getTemplate)
	mux.HandleFunc("GET /v1/factories/{id}/project-templates", h.listProjectTemplates)
}

type templateResp struct {
	ID       string          `json:"id"`       // 与云端相同
	Kind     string          `json:"kind"`     // process / project
	Name     string          `json:"name"`     // 工程模版名称；工艺为空
	Revision int64           `json:"revision"` // 已收修订
	Schema   json.RawMessage `json:"schema"`   // 字段表
	Digest   []byte          `json:"digest"`   // SHA-256
}

// 读本厂已收工艺字段模版。
func (h *Handler) getTemplate(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		row, err := svc.Templates.GetTemplate(r.Context(), bearer(r), r.URL.Query().Get("kind"))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, templateResp{
			ID: row.ID.String(), Kind: row.Kind, Name: row.Name, Revision: row.Revision,
			Schema: json.RawMessage(row.Schema), Digest: row.Digest,
		})
	})
}

// 列出本厂已收各份工程模版。
func (h *Handler) listProjectTemplates(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		rows, err := svc.Templates.ListProjectTemplates(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		out := make([]templateResp, 0, len(rows))
		for _, row := range rows {
			out = append(out, templateResp{
				ID: row.ID.String(), Kind: row.Kind, Name: row.Name, Revision: row.Revision,
				Schema: json.RawMessage(row.Schema), Digest: row.Digest,
			})
		}
		writeJSON(w, http.StatusOK, out)
	})
}
