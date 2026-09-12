package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

func (h *Handler) mountAsset(mux *http.ServeMux) { // 平台级工艺/工程；厂内原件入口一律拒绝
	mux.HandleFunc("GET /v1/assets", h.listAssets)
	mux.HandleFunc("POST /v1/assets", h.createAsset)
	mux.HandleFunc("POST /v1/assets/promote", h.promoteAsset)
	mux.HandleFunc("POST /v1/assets/promote-from", h.promoteFromFactory)
	mux.HandleFunc("GET /v1/assets/{assetId}", h.getAsset)
	mux.HandleFunc("GET /v1/assets/{assetId}/content", h.readAssetContent)
	mux.HandleFunc("POST /v1/assets/{assetId}/rename", h.renameAsset)
	mux.HandleFunc("POST /v1/assets/{assetId}/content", h.updateAssetContent)
	mux.HandleFunc("POST /v1/assets/{assetId}/copyable", h.setAssetCopyable)
	mux.HandleFunc("POST /v1/assets/{assetId}/copy", h.copyAsset)
	mux.HandleFunc("POST /v1/assets/{assetId}/publish", h.publishAsset)
	mux.HandleFunc("POST /v1/assets/{assetId}/disable", h.disableAsset)
	mux.HandleFunc("POST /v1/assets/{assetId}/enable", h.enableAsset)
	mux.HandleFunc("POST /v1/assets/{assetId}/delete", h.deleteAsset)
	mux.HandleFunc("GET /v1/factories/{id}/promotable-assets", h.listPromotable)
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

type copyableReq struct {
	Expected int64 `json:"expected"` // 期望修订
	Copyable bool  `json:"copyable"` // 可否升档
}

type copyAssetReq struct {
	Name string `json:"name"` // 新工艺显示名
}

type renameAssetReq struct {
	Expected int64  `json:"expected"` // 期望修订
	Name     string `json:"name"`     // 新显示名
}

type contentReq struct {
	Expected int64  `json:"expected"` // 期望修订
	Content  string `json:"content"`  // 新正文
}

type contentResp struct {
	Content string `json:"content"` // UTF-8 正文
}

type factoryAssetReq struct {
	Name    string `json:"name"`    // 厂级显示名；此接口一律拒绝
	Content string `json:"content"` // 厂级正文；此接口一律拒绝
}

type promoteFromReq struct {
	FactoryID string `json:"factoryId"` // 源厂
	AssetID   string `json:"assetId"`   // 该厂可升档厂级身份
}

const channelAsk = 15 * time.Second // 问厂端列表/快照的等待上限

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

func (h *Handler) updateAssetContent(w http.ResponseWriter, r *http.Request) {
	assetID, err := parseAssetID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	var req contentReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	row, err := h.svc.Assets.UpdatePlatformAssetContent(r.Context(), bearer(r), assetID, req.Expected, []byte(req.Content))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) setAssetCopyable(w http.ResponseWriter, r *http.Request) {
	assetID, err := parseAssetID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	var req copyableReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	row, err := h.svc.Assets.SetPlatformCopyable(r.Context(), bearer(r), assetID, req.Expected, req.Copyable)
	if err != nil {
		writeErr(w, err)
		return
	}
	if row.Status == service.AssetAvailable {
		h.fanoutAvailable(r.Context())
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) copyAsset(w http.ResponseWriter, r *http.Request) {
	assetID, err := parseAssetID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	var req copyAssetReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	row, err := h.svc.Assets.CopyPlatformProcess(r.Context(), bearer(r), assetID, req.Name)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, row)
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
	h.fanoutAvailable(r.Context())
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) disableAsset(w http.ResponseWriter, r *http.Request) {
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
	row, err := h.svc.Assets.DisablePlatformAsset(r.Context(), bearer(r), assetID, req.Expected)
	if err != nil {
		writeErr(w, err)
		return
	}
	h.fanoutAvailable(r.Context())
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) enableAsset(w http.ResponseWriter, r *http.Request) {
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
	row, err := h.svc.Assets.ReenablePlatformAsset(r.Context(), bearer(r), assetID, req.Expected)
	if err != nil {
		writeErr(w, err)
		return
	}
	h.fanoutAvailable(r.Context())
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) deleteAsset(w http.ResponseWriter, r *http.Request) {
	assetID, err := parseAssetID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	if err := h.svc.Assets.DeletePlatformAsset(r.Context(), bearer(r), assetID); err != nil {
		writeErr(w, err)
		return
	}
	h.fanoutRetract(r.Context(), assetID)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
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

func (h *Handler) listPromotable(w http.ResponseWriter, r *http.Request) {
	if _, err := h.svc.RequireAdmin(r.Context(), bearer(r)); err != nil {
		writeErr(w, err)
		return
	}
	fid, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	if err := h.guardAskFactory(r.Context(), fid); err != nil {
		writeErr(w, err)
		return
	}
	kind := r.URL.Query().Get("kind")
	ctx, cancel := context.WithTimeout(r.Context(), channelAsk)
	defer cancel()
	msg, err := h.live.call(ctx, fid, channelMsg{Typ: "asset_list", Kind: kind})
	if err != nil {
		writeErr(w, err)
		return
	}
	rows := msg.Assets
	if rows == nil {
		rows = []promotableAsset{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h *Handler) promoteFromFactory(w http.ResponseWriter, r *http.Request) {
	if _, err := h.svc.RequireAdmin(r.Context(), bearer(r)); err != nil {
		writeErr(w, err)
		return
	}
	var req promoteFromReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	fid, err := uuid.Parse(req.FactoryID)
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	if _, err := uuid.Parse(req.AssetID); err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	if err := h.guardAskFactory(r.Context(), fid); err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), channelAsk)
	defer cancel()
	msg, err := h.live.call(ctx, fid, channelMsg{Typ: "asset_snapshot", AssetID: req.AssetID})
	if err != nil {
		writeErr(w, err)
		return
	}
	if msg.Snapshot == nil {
		writeErr(w, domain.ErrNotFound)
		return
	}
	row, err := h.svc.Assets.PromoteFromSnapshot(r.Context(), bearer(r), *msg.Snapshot)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

func (h *Handler) guardAskFactory(ctx context.Context, factoryID uuid.UUID) error {
	fac, err := h.svc.Store().FactoryByID(ctx, factoryID)
	if err != nil {
		return err
	}
	if fac.Status == service.FactoryRetired {
		return domain.ErrFactoryRetired
	}
	if fac.Status == service.FactoryDisabled {
		return domain.ErrFactoryDisabled
	}
	return nil
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
