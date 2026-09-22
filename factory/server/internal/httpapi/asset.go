package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/service"
)

// 本厂工艺/工程。
func (h *Handler) mountAsset(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/factories/{id}/asset-author-context", h.assetAuthorContext)
	mux.HandleFunc("GET /v1/factories/{id}/assets", h.listAssets)
	mux.HandleFunc("POST /v1/factories/{id}/assets", h.createAsset)
	mux.HandleFunc("GET /v1/factories/{id}/assets/{assetId}", h.getAsset)
	mux.HandleFunc("GET /v1/factories/{id}/assets/{assetId}/content", h.readAssetContent)
	mux.HandleFunc("GET /v1/factories/{id}/assets/{assetId}/snapshot", h.exportAsset)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/rename", h.renameAsset)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/content", h.updateAssetContent)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/copyable", h.setAssetCopyable)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/weld-kind", h.setAssetWeldKind)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/publish", h.publishAsset)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/disable", h.disableAsset)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/enable", h.enableAsset)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/delete", h.deleteAsset)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/copy", h.copyAsset)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/promote", h.promoteAsset)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/deps", h.setAssetDeps)
	mux.HandleFunc("POST /v1/factories/{id}/assets/sync", h.syncAssets)
	mux.HandleFunc("POST /v1/factories/{id}/pad/assets", h.createPadAsset)
	mux.HandleFunc("POST /v1/factories/{id}/pad/assets/{assetId}/content", h.applyPadAsset)
}

type createAssetReq struct {
	Kind      string             `json:"kind"`      // process / project
	Level     string             `json:"level"`     // factory / personal
	Name      string             `json:"name"`      // 显示名
	Content   string             `json:"content"`   // UTF-8 正文
	WeldKind  string             `json:"weldKind"`  // 作业类型：single / multilayer / tbar
	Copyable  *bool              `json:"copyable"`  // 仅工艺；空则默认可复制
	Direct    bool               `json:"direct"`    // 兼容旧客户端；未带节点时按工厂直属
	OrgUnitID *string            `json:"orgUnitId"` // 未传则记工厂直属
	Deps      []service.AssetDep `json:"deps"`      // 可空；新建从参数补
	ParentID  string             `json:"parentId"`  // 目录父文件夹；空则挂对应树的根
}

type padContentReq struct {
	Content string `json:"content"` // UTF-8 正文；不带期望修订
}

type padCreateAssetReq struct {
	Kind     string             `json:"kind"`     // process / project
	Name     string             `json:"name"`     // 显示名
	Content  string             `json:"content"`  // UTF-8 正文
	WeldKind string             `json:"weldKind"` // 作业类型
	ID       string             `json:"id"`       // 本机已发身份；空则厂端发号
	Code     string             `json:"code"`     // 本机只读编号；空则厂端发号
	Deps     []service.AssetDep `json:"deps"`     // 工程可空；从正文补
	ParentID string             `json:"parentId"` // 目录父文件夹；空则挂个人根
	Level    string             `json:"level"`    // factory / personal；空按个人级
}

type expectedReq struct {
	Expected int64 `json:"expected"` // 期望修订
}

type renameAssetReq struct {
	Expected int64  `json:"expected"` // 期望修订
	Name     string `json:"name"`     // 新显示名
}

type contentReq struct {
	Expected int64  `json:"expected"` // 期望修订
	Content  string `json:"content"`  // 新正文
}

type copyableReq struct {
	Expected int64 `json:"expected"` // 期望修订
	Copyable bool  `json:"copyable"` // 可否升档
}

type weldKindReq struct {
	Expected int64  `json:"expected"` // 期望修订
	WeldKind string `json:"weldKind"` // 作业类型：single / multilayer / tbar
}

type copyAssetReq struct {
	Name string `json:"name"` // 新工艺显示名
}

type setDepsReq struct {
	Expected int64              `json:"expected"` // 期望修订
	Deps     []service.AssetDep `json:"deps"`     // 工程依赖
}

type contentResp struct {
	Content string `json:"content"` // UTF-8 正文
}

// 给出当前账号可用来创建资产的工作位置。
func (h *Handler) assetAuthorContext(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		out, err := svc.Assets.AuthorContext(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}

// 按许可列本厂工艺/工程元数据，不含正文。
func (h *Handler) listAssets(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		rows, err := svc.Assets.ListAssets(r.Context(), bearer(r), r.URL.Query().Get("kind"))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	})
}

// 进工艺/工程页时异步向 WAN 要当前平台级快照；通道不在也立刻返回。
func (h *Handler) syncAssets(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		if _, err := svc.Auth.RequireActive(r.Context(), bearer(r)); err != nil {
			writeErr(w, err)
			return
		}
		kind := r.URL.Query().Get("kind")
		if kind != "" && kind != service.KindProcess && kind != service.KindProject {
			writeErr(w, domain.ErrNotFound)
			return
		}
		fid, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		h.Hub.RequestWANSync(fid, "sync_closures", kind)
		writeJSON(w, http.StatusAccepted, map[string]string{"ok": "true"})
	})
}

// 按级别分流创建；没带节点就记工厂直属。
func (h *Handler) createAsset(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req createAssetReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		wc, err := parseWorkContext(req.Direct, req.OrgUnitID)
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		content := []byte(req.Content)
		copyable := true
		if req.Copyable != nil {
			copyable = *req.Copyable
		}
		var row service.Asset
		switch {
		case req.Kind == service.KindProcess && req.Level == service.AssetLevelFactory:
			row, err = svc.Assets.CreateProcess(r.Context(), bearer(r), wc, service.AssetLevelFactory, req.Name, content, copyable, req.WeldKind)
		case req.Kind == service.KindProcess && req.Level == service.AssetLevelPersonal:
			row, err = svc.Assets.CreateProcess(r.Context(), bearer(r), wc, service.AssetLevelPersonal, req.Name, content, copyable, req.WeldKind)
		case req.Kind == service.KindProject && req.Level == service.AssetLevelFactory:
			row, err = svc.Assets.CreateFactoryProject(r.Context(), bearer(r), wc, req.Name, content, req.Deps, req.WeldKind)
		case req.Kind == service.KindProject && req.Level == service.AssetLevelPersonal:
			row, err = svc.Assets.CreatePersonalProject(r.Context(), bearer(r), wc, req.Name, content, req.Deps, req.WeldKind)
		default:
			writeBadRequest(w, errInvalidID)
			return
		}
		if err != nil {
			writeErr(w, err)
			return
		}
		if err := placeNewAsset(r, svc, row.ID, req.ParentID); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, row)
	})
}

// 平板新建：个人级或管理员的厂级，保存即为可用。
func (h *Handler) createPadAsset(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req padCreateAssetReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		var id uuid.UUID
		if strings.TrimSpace(req.ID) != "" {
			parsed, err := uuid.Parse(req.ID)
			if err != nil {
				writeBadRequest(w, errInvalidID)
				return
			}
			id = parsed
		}
		row, err := svc.Assets.CreatePad(r.Context(), bearer(r), req.Level, req.Kind, req.Name, []byte(req.Content), id, req.Code, req.Deps, req.WeldKind)
		if err != nil {
			writeErr(w, err)
			return
		}
		if err := placeNewAsset(r, svc, row.ID, req.ParentID); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, row)
	})
}

// 示教器盖当前行，不比对期望修订。
func (h *Handler) applyPadAsset(w http.ResponseWriter, r *http.Request) {
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		var req padContentReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.Assets.ApplyAppContent(r.Context(), bearer(r), assetID, []byte(req.Content))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 读元数据，不解包正文。
func (h *Handler) getAsset(w http.ResponseWriter, r *http.Request) {
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		row, err := svc.Assets.GetAsset(r.Context(), bearer(r), assetID)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 读正文并核对摘要；不可复制的平台级工艺不给人看。
func (h *Handler) readAssetContent(w http.ResponseWriter, r *http.Request) {
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		body, err := svc.Assets.ReadAssetContent(r.Context(), bearer(r), assetID)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, contentResp{Content: string(body)})
	})
}

// 导出厂级快照给 WAN 升档。
func (h *Handler) exportAsset(w http.ResponseWriter, r *http.Request) {
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		snap, err := svc.Assets.ExportAssetSnapshot(r.Context(), bearer(r), assetID)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, snap)
	})
}

// 改显示名，身份不变，修订升高。
func (h *Handler) renameAsset(w http.ResponseWriter, r *http.Request) {
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		var req renameAssetReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.Assets.RenameAsset(r.Context(), bearer(r), assetID, req.Expected, req.Name)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 改正文并重算摘要；停用后拒绝。
func (h *Handler) updateAssetContent(w http.ResponseWriter, r *http.Request) {
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		var req contentReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.Assets.UpdateAssetContent(r.Context(), bearer(r), assetID, req.Expected, []byte(req.Content))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 未停用即可改可复制，发布后也能改回。
func (h *Handler) setAssetCopyable(w http.ResponseWriter, r *http.Request) {
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		var req copyableReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.Assets.SetAssetCopyable(r.Context(), bearer(r), assetID, req.Expected, req.Copyable)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 未停用的本厂工艺或工程可改作业类型。
func (h *Handler) setAssetWeldKind(w http.ResponseWriter, r *http.Request) {
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		var req weldKindReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.Assets.SetAssetWeldKind(r.Context(), bearer(r), assetID, req.Expected, req.WeldKind)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 草稿改为可用。
func (h *Handler) publishAsset(w http.ResponseWriter, r *http.Request) {
	h.withExpected(w, r, func(svc *service.Service, assetID uuid.UUID, expected int64) {
		row, err := svc.Assets.PublishAsset(r.Context(), bearer(r), assetID, expected)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 可用改为停用；停用期间不得改正文或升档。
func (h *Handler) disableAsset(w http.ResponseWriter, r *http.Request) {
	h.withExpected(w, r, func(svc *service.Service, assetID uuid.UUID, expected int64) {
		row, err := svc.Assets.DisableAsset(r.Context(), bearer(r), assetID, expected)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 停用改回可用。
func (h *Handler) enableAsset(w http.ResponseWriter, r *http.Request) {
	h.withExpected(w, r, func(svc *service.Service, assetID uuid.UUID, expected int64) {
		row, err := svc.Assets.ReenableAsset(r.Context(), bearer(r), assetID, expected)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 未被工程依赖则可删。
func (h *Handler) deleteAsset(w http.ResponseWriter, r *http.Request) {
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		if err := svc.Assets.DeleteAsset(r.Context(), bearer(r), assetID); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	})
}

// 可复制工艺另存为新草稿。
func (h *Handler) copyAsset(w http.ResponseWriter, r *http.Request) {
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		var req copyAssetReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.Assets.CopyProcess(r.Context(), bearer(r), assetID, req.Name)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, row)
	})
}

// 把可复制的未停用个人级升为厂级；草稿也可升。
func (h *Handler) promoteAsset(w http.ResponseWriter, r *http.Request) {
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		row, err := svc.Assets.PromoteToFactory(r.Context(), bearer(r), assetID)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, row)
	})
}

// 显式改本厂工程依赖。
func (h *Handler) setAssetDeps(w http.ResponseWriter, r *http.Request) {
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		var req setDepsReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.Closure.SetProjectDeps(r.Context(), bearer(r), assetID, req.Expected, req.Deps)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 解析资产身份后转给业务。
func (h *Handler) withAsset(w http.ResponseWriter, r *http.Request, fn func(*service.Service, uuid.UUID)) {
	h.withFactory(w, r, func(svc *service.Service) {
		assetID, err := uuid.Parse(r.PathValue("assetId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		fn(svc, assetID)
	})
}

// 先解析资产身份和期望修订再调业务。
func (h *Handler) withExpected(w http.ResponseWriter, r *http.Request, fn func(*service.Service, uuid.UUID, int64)) {
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		var req expectedReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		fn(svc, assetID, req.Expected)
	})
}

// 把直属或节点收成工作上下文。
func parseWorkContext(direct bool, orgUnitID *string) (service.WorkContext, error) {
	id, err := parseOptUUID(orgUnitID)
	if err != nil {
		return service.WorkContext{}, err
	}
	// 工艺/工程不再让人选位置；没带节点就记工厂直属。
	if !direct && id == nil {
		return service.WorkContext{Direct: true}, nil
	}
	return service.WorkContext{Direct: direct, OrgUnitID: id}, nil
}
