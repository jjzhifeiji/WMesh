// 内容模版接口：工艺模版和工程模版都是多份独立行，各有列表、新建、修改、删除；只做会话与 JSON 适配。
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"wmesh/global/internal/service"
)

// 挂内容模版的接口：工艺、工程各四个，路径只差前缀。
func (h *Handler) mountTemplate(mux *http.ServeMux) {
	// 工艺模版列表。
	mux.HandleFunc("GET /v1/process-templates", h.listTemplates(service.KindProcess))
	// 新建一份工艺模版。
	mux.HandleFunc("POST /v1/process-templates", h.createTemplate(service.KindProcess))
	// 改一份工艺模版。
	mux.HandleFunc("POST /v1/process-templates/{id}", h.updateTemplate(service.KindProcess))
	// 删一份工艺模版。
	mux.HandleFunc("DELETE /v1/process-templates/{id}", h.deleteTemplate(service.KindProcess))
	// 工程模版列表。
	mux.HandleFunc("GET /v1/project-templates", h.listTemplates(service.KindProject))
	// 新建一份工程模版。
	mux.HandleFunc("POST /v1/project-templates", h.createTemplate(service.KindProject))
	// 改一份工程模版。
	mux.HandleFunc("POST /v1/project-templates/{id}", h.updateTemplate(service.KindProject))
	// 删一份工程模版。
	mux.HandleFunc("DELETE /v1/project-templates/{id}", h.deleteTemplate(service.KindProject))
}

// 一份模版的回包。
type templateResp struct {
	ID       string          `json:"id"`       // 稳定身份
	Kind     string          `json:"kind"`     // process / project
	Name     string          `json:"name"`     // 模版名称
	Revision int64           `json:"revision"` // 当前修订
	Schema   json.RawMessage `json:"schema"`   // 字段表
	Digest   []byte          `json:"digest"`   // SHA-256
}

// 新建模版的请求体。
type createTemplateReq struct {
	ID     string          `json:"id"`     // 可选；预置身份用于补建
	Name   string          `json:"name"`   // 名称
	Schema json.RawMessage `json:"schema"` // 对象字段表，可空
}

// 修改模版的请求体。
type updateTemplateReq struct {
	Expected int64           `json:"expected"` // 期望修订
	Name     string          `json:"name"`     // 名称
	Schema   json.RawMessage `json:"schema"`   // 对象字段表
}

// 模版视图收成回包。
func templateJSON(t service.ContentTemplate) templateResp {
	return templateResp{
		ID: t.ID.String(), Kind: t.Kind, Name: t.Name, Revision: t.Revision,
		Schema: json.RawMessage(t.Schema), Digest: t.Digest,
	}
}

// 列出这一类的当前各份模版。
func (h *Handler) listTemplates(kind string) http.HandlerFunc {
	// 按种类转给对应的应用服务。
	return func(w http.ResponseWriter, r *http.Request) {
		// 准备接住列表和错误。
		var rows []service.ContentTemplate
		// 先留空，下面按种类填。
		var err error
		// 工艺和工程各走各的服务。
		if kind == service.KindProcess {
			// 读工艺各份。
			rows, err = h.svc.Templates.ListProcessTemplates(r.Context(), bearer(r))
			// 不是工艺就是工程，走工程那一边。
		} else {
			// 读工程各份。
			rows, err = h.svc.Templates.ListProjectTemplates(r.Context(), bearer(r))
		}
		// 没成功就按业务错误回写。
		if err != nil {
			// 按业务错误回写，内部细节不出网。
			writeErr(w, err)
			return
		}
		// 按数量先准备回包。
		out := make([]templateResp, 0, len(rows))
		// 逐份收成回包。
		for _, row := range rows {
			// 接进结果。
			out = append(out, templateJSON(row))
		}
		// 把结果交回调用方。
		writeJSON(w, http.StatusOK, out)
	}
}

// 新建这一类的一份模版。
func (h *Handler) createTemplate(kind string) http.HandlerFunc {
	// 按种类转给对应的应用服务。
	return func(w http.ResponseWriter, r *http.Request) {
		// 接住请求体，解析失败再拒绝。
		var req createTemplateReq
		// 未知字段或坏 JSON 按坏请求拒绝。
		if err := decodeJSON(r, &req); err != nil {
			// 把不合法的原因回给调用方。
			writeBadRequest(w, err)
			return
		}
		// 身份可空，空就由服务端发号。
		var id uuid.UUID
		// 给了身份才解析。
		if req.ID != "" {
			// 解析身份，格式不对就拒绝。
			parsed, err := uuid.Parse(req.ID)
			// 参数不合法就按坏请求拒绝。
			if err != nil {
				// 把不合法的原因回给调用方。
				writeBadRequest(w, errInvalidID)
				return
			}
			// 记下解析出的身份。
			id = parsed
		}
		// 准备接住新建结果和错误。
		var row service.ContentTemplate
		// 先留空，下面按种类填。
		var err error
		// 工艺和工程各走各的服务。
		if kind == service.KindProcess {
			// 新建工艺模版。
			row, err = h.svc.Templates.CreateProcessTemplate(r.Context(), bearer(r), id, req.Name, req.Schema)
			// 不是工艺就是工程，走工程那一边。
		} else {
			// 新建工程模版。
			row, err = h.svc.Templates.CreateProjectTemplate(r.Context(), bearer(r), id, req.Name, req.Schema)
		}
		// 没成功就按业务错误回写，不假装完成。
		if err != nil {
			// 按业务错误回写，内部细节不出网。
			writeErr(w, err)
			return
		}
		// 把结果交回调用方。
		writeJSON(w, http.StatusCreated, templateJSON(row))
	}
}

// 改这一类里一份模版的名称和字段表。
func (h *Handler) updateTemplate(kind string) http.HandlerFunc {
	// 按种类转给对应的应用服务。
	return func(w http.ResponseWriter, r *http.Request) {
		// 解析稳定身份，格式不对就拒绝。
		id, err := uuid.Parse(r.PathValue("id"))
		// 参数不合法就按坏请求拒绝。
		if err != nil {
			// 把不合法的原因回给调用方。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 接住请求体，解析失败再拒绝。
		var req updateTemplateReq
		// 未知字段或坏 JSON 按坏请求拒绝。
		if err := decodeJSON(r, &req); err != nil {
			// 把不合法的原因回给调用方。
			writeBadRequest(w, err)
			return
		}
		// 准备接住改后的这一份。
		var row service.ContentTemplate
		// 工艺和工程各走各的服务。
		if kind == service.KindProcess {
			// 改工艺模版，修订不对则拒绝。
			row, err = h.svc.Templates.UpdateProcessTemplate(r.Context(), bearer(r), id, req.Expected, req.Name, req.Schema)
			// 不是工艺就是工程，走工程那一边。
		} else {
			// 改工程模版，修订不对则拒绝。
			row, err = h.svc.Templates.UpdateProjectTemplate(r.Context(), bearer(r), id, req.Expected, req.Name, req.Schema)
		}
		// 没成功就按业务错误回写，不假装完成。
		if err != nil {
			// 按业务错误回写，内部细节不出网。
			writeErr(w, err)
			return
		}
		// 把结果交回调用方。
		writeJSON(w, http.StatusOK, templateJSON(row))
	}
}

// 删掉这一类里的一份模版。
func (h *Handler) deleteTemplate(kind string) http.HandlerFunc {
	// 按种类转给对应的应用服务。
	return func(w http.ResponseWriter, r *http.Request) {
		// 解析稳定身份，格式不对就拒绝。
		id, err := uuid.Parse(r.PathValue("id"))
		// 参数不合法就按坏请求拒绝。
		if err != nil {
			// 把不合法的原因回给调用方。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 工艺和工程各走各的服务。
		if kind == service.KindProcess {
			// 删工艺模版，没有则拒绝。
			err = h.svc.Templates.DeleteProcessTemplate(r.Context(), bearer(r), id)
			// 不是工艺就是工程，走工程那一边。
		} else {
			// 删工程模版，没有则拒绝。
			err = h.svc.Templates.DeleteProjectTemplate(r.Context(), bearer(r), id)
		}
		// 没成功就按业务错误回写。
		if err != nil {
			// 按业务错误回写，内部细节不出网。
			writeErr(w, err)
			return
		}
		// 把结果交回调用方。
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
