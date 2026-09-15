package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"wmesh/global/internal/service"
)

// 挂工艺当前字段模版和独立工程模版。
func (h *Handler) mountTemplate(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/templates", h.getTemplate)
	mux.HandleFunc("GET /v1/templates/builtin", h.getBuiltinTemplate)
	mux.HandleFunc("POST /v1/templates", h.updateTemplate)
	mux.HandleFunc("GET /v1/project-templates", h.listProjectTemplates)
	mux.HandleFunc("POST /v1/project-templates", h.createProjectTemplate)
	mux.HandleFunc("POST /v1/project-templates/{id}", h.updateProjectTemplate)
	mux.HandleFunc("DELETE /v1/project-templates/{id}", h.deleteProjectTemplate)
}

type templateResp struct {
	ID       string          `json:"id"`       // 稳定身份
	Kind     string          `json:"kind"`     // process / project
	Name     string          `json:"name"`     // 工程模版名称；工艺为空
	Revision int64           `json:"revision"` // 当前修订
	Schema   json.RawMessage `json:"schema"`   // 字段表
	Digest   []byte          `json:"digest"`   // SHA-256
}

type updateTemplateReq struct {
	Kind     string          `json:"kind"`     // process
	Expected int64           `json:"expected"` // 期望修订
	Schema   json.RawMessage `json:"schema"`   // 字段表
}

type createProjectTemplateReq struct {
	Name   string          `json:"name"`   // 名称
	Schema json.RawMessage `json:"schema"` // 对象字段表，可空
}

type updateProjectTemplateReq struct {
	Expected int64           `json:"expected"` // 期望修订
	Name     string          `json:"name"`     // 名称
	Schema   json.RawMessage `json:"schema"`   // 对象字段表
}

// 收成给前端的模版 JSON。
func templateJSON(t service.ContentTemplate) templateResp {
	return templateResp{
		ID: t.ID.String(), Kind: t.Kind, Name: t.Name, Revision: t.Revision,
		Schema: json.RawMessage(t.Schema), Digest: t.Digest,
	}
}

// 读工艺当前字段模版。
func (h *Handler) getTemplate(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	row, err := h.svc.Templates.GetTemplate(r.Context(), bearer(r), kind)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, templateJSON(row))
}

type builtinTemplateResp struct {
	Kind   string          `json:"kind"`   // process
	Schema json.RawMessage `json:"schema"` // 代码默认字段表
}

// 读代码里的默认工艺字段表，不改已落库模版。
func (h *Handler) getBuiltinTemplate(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	raw, err := h.svc.Templates.BuiltinSchema(r.Context(), bearer(r), kind)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, builtinTemplateResp{Kind: kind, Schema: json.RawMessage(raw)})
}

// 保存工艺字段表并立刻推给在线厂。
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
	writeJSON(w, http.StatusOK, templateJSON(row))
}

// 列出当前各份独立工程模版。
func (h *Handler) listProjectTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.Templates.ListProjectTemplates(r.Context(), bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]templateResp, 0, len(rows))
	for _, row := range rows {
		out = append(out, templateJSON(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// 新建一份工程模版。
func (h *Handler) createProjectTemplate(w http.ResponseWriter, r *http.Request) {
	var req createProjectTemplateReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	row, err := h.svc.Templates.CreateProjectTemplate(r.Context(), bearer(r), req.Name, req.Schema)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, templateJSON(row))
}

// 改一份工程模版的名称和字段表。
func (h *Handler) updateProjectTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	var req updateProjectTemplateReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	row, err := h.svc.Templates.UpdateProjectTemplate(r.Context(), bearer(r), id, req.Expected, req.Name, req.Schema)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, templateJSON(row))
}

// 删掉一份工程模版。
func (h *Handler) deleteProjectTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	if err := h.svc.Templates.DeleteProjectTemplate(r.Context(), bearer(r), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
