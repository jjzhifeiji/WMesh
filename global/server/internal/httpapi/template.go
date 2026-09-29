package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"wmesh/global/internal/service"
)

// 挂工艺当前字段模版和独立工程模版。
func (h *Handler) mountTemplate(mux *http.ServeMux) {
	// 读工艺当前字段模版。
	mux.HandleFunc("GET /v1/templates", h.getTemplate)
	// 读代码里的默认工艺字段表。
	mux.HandleFunc("GET /v1/templates/builtin", h.getBuiltinTemplate)
	// 保存工艺字段表，并推给在线厂。
	mux.HandleFunc("POST /v1/templates", h.updateTemplate)
	// 列出当前各份独立工程模版。
	mux.HandleFunc("GET /v1/project-templates", h.listProjectTemplates)
	// 新建工程模版，可补建缺失的那份。
	mux.HandleFunc("POST /v1/project-templates", h.createProjectTemplate)
	// 改一份工程模版的名称和字段表。
	mux.HandleFunc("POST /v1/project-templates/{id}", h.updateProjectTemplate)
	// 删掉一份工程模版。
	mux.HandleFunc("DELETE /v1/project-templates/{id}", h.deleteProjectTemplate)
}

// 给前端的模版，含修订和内容摘要。
type templateResp struct {
	ID       string          `json:"id"`       // 稳定身份
	Kind     string          `json:"kind"`     // process / project
	Name     string          `json:"name"`     // 工程模版名称；工艺为空
	Revision int64           `json:"revision"` // 当前修订
	Schema   json.RawMessage `json:"schema"`   // 字段表
	Digest   []byte          `json:"digest"`   // SHA-256
}

// 保存工艺字段表，修订对不上就拒绝。
type updateTemplateReq struct {
	Kind     string          `json:"kind"`     // process
	Expected int64           `json:"expected"` // 期望修订
	Schema   json.RawMessage `json:"schema"`   // 字段表
}

// 新建工程模版，可带种子身份补建。
type createProjectTemplateReq struct {
	ID     string          `json:"id"`     // 可选；空库种子身份用于补建
	Name   string          `json:"name"`   // 名称
	Schema json.RawMessage `json:"schema"` // 对象字段表，可空
}

// 改工程模版的名称和字段表。
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
	// 读取种类，空则由服务决定是否拒绝。
	kind := r.URL.Query().Get("kind")
	// 读当前工艺字段表，未登录拒绝。
	row, err := h.svc.Templates.GetTemplate(r.Context(), bearer(r), kind)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, templateJSON(row))
}

// 代码里的默认工艺字段表，不改已落库。
type builtinTemplateResp struct {
	Kind   string          `json:"kind"`   // process
	Schema json.RawMessage `json:"schema"` // 代码默认字段表
}

// 读代码里的默认工艺字段表，不改已落库模版。
func (h *Handler) getBuiltinTemplate(w http.ResponseWriter, r *http.Request) {
	// 读取种类，空则由服务决定是否拒绝。
	kind := r.URL.Query().Get("kind")
	// 读代码默认字段表，不改已落库模版。
	raw, err := h.svc.Templates.BuiltinSchema(r.Context(), bearer(r), kind)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, builtinTemplateResp{Kind: kind, Schema: json.RawMessage(raw)})
}

// 保存工艺字段表并立刻推给在线厂。
func (h *Handler) updateTemplate(w http.ResponseWriter, r *http.Request) {
	// 接住请求体，解析失败再拒绝。
	var req updateTemplateReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 保存字段表并推给在线厂，修订不对则拒绝。
	row, err := h.svc.Templates.UpdateTemplate(r.Context(), bearer(r), req.Kind, req.Expected, req.Schema)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, templateJSON(row))
}

// 列出当前各份独立工程模版。
func (h *Handler) listProjectTemplates(w http.ResponseWriter, r *http.Request) {
	// 带会话去查，未登录由服务拒绝。
	rows, err := h.svc.Templates.ListProjectTemplates(r.Context(), bearer(r))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 按条数准备给前端的列表，空的也要回。
	out := make([]templateResp, 0, len(rows))
	// 逐条收成给前端的结果，空列表也要回。
	for _, row := range rows {
		// 收成给前端的模版，带上修订和摘要。
		out = append(out, templateJSON(row))
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, out)
}

// 新建一份工程模版；可带空库种子身份补建缺失份。
func (h *Handler) createProjectTemplate(w http.ResponseWriter, r *http.Request) {
	// 接住请求体，解析失败再拒绝。
	var req createProjectTemplateReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 先当不指定种子身份，带了才解析。
	var id uuid.UUID
	// 带了身份就要验，验不过不能登记。
	if req.ID != "" {
		// 解析稳定身份，格式不对就拒绝。
		parsed, err := uuid.Parse(req.ID)
		// 参数不合法就按坏请求拒绝。
		if err != nil {
			// 把不合法的原因回给调用方。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 种子身份合法才用来补建，避免另起一份。
		id = parsed
	}
	// 新建一份工程模版，重名则拒绝。
	row, err := h.svc.Templates.CreateProjectTemplate(r.Context(), bearer(r), id, req.Name, req.Schema)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 创建成功，把新记录交回调用方。
	writeJSON(w, http.StatusCreated, templateJSON(row))
}

// 改一份工程模版的名称和字段表。
func (h *Handler) updateProjectTemplate(w http.ResponseWriter, r *http.Request) {
	// 解析稳定身份，格式不对就拒绝。
	id, err := uuid.Parse(r.PathValue("id"))
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req updateProjectTemplateReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 改名称和字段表，修订不对则拒绝。
	row, err := h.svc.Templates.UpdateProjectTemplate(r.Context(), bearer(r), id, req.Expected, req.Name, req.Schema)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, templateJSON(row))
}

// 删掉一份工程模版。
func (h *Handler) deleteProjectTemplate(w http.ResponseWriter, r *http.Request) {
	// 解析稳定身份，格式不对就拒绝。
	id, err := uuid.Parse(r.PathValue("id"))
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 删掉这份模版，没有则拒绝。
	if err := h.svc.Templates.DeleteProjectTemplate(r.Context(), bearer(r), id); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
