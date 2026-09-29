package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/service"
)

// 本厂已收字段模版。
func (h *Handler) mountTemplate(mux *http.ServeMux) {
	// 读取本厂已经收到的工艺字段模版。
	mux.HandleFunc("GET /v1/factories/{id}/templates", h.getTemplate)
	// 列出本厂已经收到的各份工程模版。
	mux.HandleFunc("GET /v1/factories/{id}/project-templates", h.listProjectTemplates)
	// 向平台要模版，通道不在也先返回。
	mux.HandleFunc("POST /v1/factories/{id}/templates/sync", h.syncTemplates)
}

// 已经收到的模版，字段表和摘要都在。
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
	// 读取本厂已经收到的工艺字段模版。
	h.withFactory(w, r, func(svc *service.Service) {
		// 读取种类，空着表示不按种类限制。
		row, err := svc.Templates.GetTemplate(r.Context(), bearer(r), r.URL.Query().Get("kind"))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, templateResp{
			ID: row.ID.String(), Kind: row.Kind, Name: row.Name, Revision: row.Revision,
			Schema: json.RawMessage(row.Schema), Digest: row.Digest,
		})
	})
}

// 列出本厂已收各份工程模版。
func (h *Handler) listProjectTemplates(w http.ResponseWriter, r *http.Request) {
	// 列出本厂已收各份工程模版。
	h.withFactory(w, r, func(svc *service.Service) {
		// 列出已经收到的各份工程模版。
		rows, err := svc.Templates.ListProjectTemplates(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 先留出列表，再逐条放进要返回的内容。
		out := make([]templateResp, 0, len(rows))
		// 逐条收成回包，不带不该给出的字节。
		for _, row := range rows {
			// 把这一项收进结果，供后面返回。
			out = append(out, templateResp{
				ID: row.ID.String(), Kind: row.Kind, Name: row.Name, Revision: row.Revision,
				Schema: json.RawMessage(row.Schema), Digest: row.Digest,
			})
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, out)
	})
}

// 进页时异步向 WAN 要当前模版；通道不在也立刻返回。
func (h *Handler) syncTemplates(w http.ResponseWriter, r *http.Request) {
	// 进页时异步向 WAN 要当前模版；通道不在也立刻返回。
	h.withFactory(w, r, func(svc *service.Service) {
		// 会话无效或账号停用则拒绝，不再继续。
		if _, err := svc.Auth.RequireActive(r.Context(), bearer(r)); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 读取种类，空着表示不按种类限制。
		kind := r.URL.Query().Get("kind")
		// 不是工艺也不是工程则当找不到。
		if kind != "" && kind != service.KindProcess && kind != service.KindProject {
			// 种类不对就当找不到，避免误向平台要。
			writeErr(w, domain.ErrNotFound)
			return
		}
		// 工厂编号必须合法，否则拒绝并不开库。
		fid, err := uuid.Parse(r.PathValue("id"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 向平台要当前快照，通道不在也先返回。
		h.Hub.RequestWANSync(fid, "sync_templates", kind)
		// 已经排上就先返回，不等后台做完。
		writeJSON(w, http.StatusAccepted, map[string]string{"ok": "true"})
	})
}
