package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

func (h *Handler) mountAsset(mux *http.ServeMux) { // 本厂工艺/工程
	mux.HandleFunc("GET /v1/factories/{id}/asset-author-context", h.assetAuthorContext)
	mux.HandleFunc("GET /v1/factories/{id}/assets", h.listAssets)
	mux.HandleFunc("POST /v1/factories/{id}/assets", h.createAsset)
	mux.HandleFunc("GET /v1/factories/{id}/assets/{assetId}", h.getAsset)
	mux.HandleFunc("GET /v1/factories/{id}/assets/{assetId}/content", h.readAssetContent)
	mux.HandleFunc("GET /v1/factories/{id}/assets/{assetId}/snapshot", h.exportAsset)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/rename", h.renameAsset)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/content", h.updateAssetContent)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/copyable", h.setAssetCopyable)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/publish", h.publishAsset)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/disable", h.disableAsset)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/promote", h.promoteAsset)
}

type createAssetReq struct {
	Kind      string             `json:"kind"`      // process / project
	Level     string             `json:"level"`     // factory / personal
	Name      string             `json:"name"`      // 显示名
	Content   string             `json:"content"`   // UTF-8 正文
	Direct    bool               `json:"direct"`    // 直属工厂
	OrgUnitID *string            `json:"orgUnitId"` // 与 Direct 互斥
	Deps      []service.AssetDep `json:"deps"`      // 工程依赖；工艺必须空
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

type contentResp struct {
	Content string `json:"content"` // UTF-8 正文
}

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
		var row service.Asset
		switch {
		case req.Kind == service.KindProcess && req.Level == service.AssetLevelFactory:
			row, err = svc.Assets.CreateFactoryProcess(r.Context(), bearer(r), wc, req.Name, content)
		case req.Kind == service.KindProcess && req.Level == service.AssetLevelPersonal:
			row, err = svc.Assets.CreatePersonalProcess(r.Context(), bearer(r), wc, req.Name, content)
		case req.Kind == service.KindProject && req.Level == service.AssetLevelFactory:
			row, err = svc.Assets.CreateFactoryProject(r.Context(), bearer(r), wc, req.Name, content, req.Deps)
		case req.Kind == service.KindProject && req.Level == service.AssetLevelPersonal:
			row, err = svc.Assets.CreatePersonalProject(r.Context(), bearer(r), wc, req.Name, content, req.Deps)
		default:
			writeBadRequest(w, errInvalidID)
			return
		}
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, row)
	})
}

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

func parseWorkContext(direct bool, orgUnitID *string) (service.WorkContext, error) {
	id, err := parseOptUUID(orgUnitID)
	if err != nil {
		return service.WorkContext{}, err
	}
	return service.WorkContext{Direct: direct, OrgUnitID: id}, nil
}
