package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"wmesh/global/internal/service"
)

func (h *Handler) mountAsset(mux *http.ServeMux) { // 平台级工艺/工程；厂内原件入口一律拒绝
	mux.HandleFunc("GET /v1/assets", h.listAssets)
	mux.HandleFunc("POST /v1/assets", h.createAsset)
	mux.HandleFunc("POST /v1/assets/promote", h.promoteAsset)
	mux.HandleFunc("GET /v1/assets/{assetId}", h.getAsset)
	mux.HandleFunc("GET /v1/assets/{assetId}/content", h.readAssetContent)
	mux.HandleFunc("POST /v1/assets/{assetId}/rename", h.renameAsset)
	mux.HandleFunc("POST /v1/assets/{assetId}/publish", h.publishAsset)
	mux.HandleFunc("POST /v1/factories/{id}/assets", h.createFactoryAsset)
	mux.HandleFunc("GET /v1/factories/{id}/assets/{assetId}", h.getFactoryAsset)
	mux.HandleFunc("GET /v1/factories/{id}/assets/{assetId}/content", h.readFactoryAssetContent)
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}", h.updateFactoryAsset)
}

type createAssetReq struct {
	Kind    string             `json:"kind"`    // process / project
	Name    string             `json:"name"`    // 显示名
	Content string             `json:"content"` // UTF-8 正文
	Deps    []service.AssetDep `json:"deps"`    // 工程依赖；工艺必须空
}

type expectedReq struct {
	Expected int64 `json:"expected"` // 期望修订
}

type renameAssetReq struct {
	Expected int64  `json:"expected"` // 期望修订
	Name     string `json:"name"`     // 新显示名
}

type contentResp struct {
	Content string `json:"content"` // UTF-8 正文
}

type factoryAssetReq struct {
	Name    string `json:"name"`    // 厂级显示名；此接口一律拒绝
	Content string `json:"content"` // 厂级正文；此接口一律拒绝
}

func (h *Handler) listAssets(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.Assets.ListPlatformAssets(r.Context(), bearer(r), r.URL.Query().Get("kind"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h *Handler) createAsset(w http.ResponseWriter, r *http.Request) {
	var req createAssetReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	content := []byte(req.Content)
	var row service.Asset
	var err error
	switch req.Kind {
	case service.KindProcess:
		row, err = h.svc.Assets.CreatePlatformProcess(r.Context(), bearer(r), req.Name, content)
	case service.KindProject:
		row, err = h.svc.Assets.CreatePlatformProject(r.Context(), bearer(r), req.Name, content, req.Deps)
	default:
		writeBadRequest(w, errInvalidID)
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

func (h *Handler) getAsset(w http.ResponseWriter, r *http.Request) {
	assetID, err := parseAssetID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	row, err := h.svc.Assets.GetPlatformAsset(r.Context(), bearer(r), assetID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) readAssetContent(w http.ResponseWriter, r *http.Request) {
	assetID, err := parseAssetID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	body, err := h.svc.Assets.ReadPlatformAssetContent(r.Context(), bearer(r), assetID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, contentResp{Content: string(body)})
}

func (h *Handler) renameAsset(w http.ResponseWriter, r *http.Request) {
	assetID, err := parseAssetID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	var req renameAssetReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	row, err := h.svc.Assets.RenamePlatformAsset(r.Context(), bearer(r), assetID, req.Expected, req.Name)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) publishAsset(w http.ResponseWriter, r *http.Request) {
	assetID, err := parseAssetID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	var req expectedReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	row, err := h.svc.Assets.PublishPlatformAsset(r.Context(), bearer(r), assetID, req.Expected)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) promoteAsset(w http.ResponseWriter, r *http.Request) {
	var snap service.AssetSnapshot
	if err := decodeJSON(r, &snap); err != nil {
		writeBadRequest(w, err)
		return
	}
	row, err := h.svc.Assets.PromoteFromSnapshot(r.Context(), bearer(r), snap)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

func (h *Handler) createFactoryAsset(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	var req factoryAssetReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.Assets.CreateFactoryProcess(r.Context(), bearer(r), id, req.Name, []byte(req.Content)))
}

func (h *Handler) getFactoryAsset(w http.ResponseWriter, r *http.Request) {
	fid, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.Assets.GetFactoryAsset(r.Context(), bearer(r), fid, assetID))
}

func (h *Handler) readFactoryAssetContent(w http.ResponseWriter, r *http.Request) {
	fid, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.Assets.ReadFactoryAssetContent(r.Context(), bearer(r), fid, assetID))
}

func (h *Handler) updateFactoryAsset(w http.ResponseWriter, r *http.Request) {
	fid, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.Assets.UpdateFactoryAsset(r.Context(), bearer(r), fid, assetID))
}

func parseAssetID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("assetId"))
	if err != nil {
		return uuid.Nil, errInvalidID
	}
	return id, nil
}
