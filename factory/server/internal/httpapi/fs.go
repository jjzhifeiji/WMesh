package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

type fsFolderReq struct {
	ParentID string `json:"parentId"` // 父文件夹
	Name     string `json:"name"`     // 显示名
}

type fsRenameReq struct {
	Name string `json:"name"` // 新显示名
}

type fsMoveReq struct {
	ParentID string `json:"parentId"` // 新父文件夹
}

// 挂本厂目录树。
func (h *Handler) mountFS(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/factories/{id}/fs", h.listFS)
	mux.HandleFunc("POST /v1/factories/{id}/fs/folders", h.createFSFolder)
	mux.HandleFunc("POST /v1/factories/{id}/fs/{nodeId}/rename", h.renameFSNode)
	mux.HandleFunc("POST /v1/factories/{id}/fs/{nodeId}/move", h.moveFSNode)
	mux.HandleFunc("POST /v1/factories/{id}/fs/{nodeId}/delete", h.deleteFSNode)
	mux.HandleFunc("POST /v1/factories/{id}/fs/{nodeId}/copy", h.copyFSNode)
}

// 列出当前人能看见的目录，不含正文。
func (h *Handler) listFS(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		rows, err := svc.Assets.ListFS(r.Context(), bearer(r), r.URL.Query().Get("kind"))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	})
}

// 新建文件夹；平台级副本树拒绝。
func (h *Handler) createFSFolder(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req fsFolderReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		parentID, err := uuid.Parse(req.ParentID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		row, err := svc.Assets.CreateFSFolder(r.Context(), bearer(r), parentID, req.Name)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, row)
	})
}

// 改文件夹显示名。
func (h *Handler) renameFSNode(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		nodeID, err := parseNodeID(r)
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		var req fsRenameReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.Assets.RenameFSNode(r.Context(), bearer(r), nodeID, req.Name)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 把节点挂到另一个文件夹。
func (h *Handler) moveFSNode(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		nodeID, err := parseNodeID(r)
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		var req fsMoveReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		parentID, err := uuid.Parse(req.ParentID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		row, err := svc.Assets.MoveFSNode(r.Context(), bearer(r), nodeID, parentID)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 删文件夹或本厂原件；平台级副本拒绝。
func (h *Handler) deleteFSNode(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		nodeID, err := parseNodeID(r)
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		if err := svc.Assets.DeleteFSNode(r.Context(), bearer(r), nodeID); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

// 复制文件夹或文件到可写目录；原件不动。
func (h *Handler) copyFSNode(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		nodeID, err := parseNodeID(r)
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		var req fsMoveReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		parentID, err := uuid.Parse(req.ParentID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		row, err := svc.Assets.CopyFSNode(r.Context(), bearer(r), nodeID, parentID)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, row)
	})
}

// parseNodeID 从路径取出目录节点身份。
func parseNodeID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("nodeId"))
	if err != nil {
		return uuid.Nil, errInvalidID
	}
	return id, nil
}

// placeNewAsset 新建后挂到指定文件夹；空父节点则留在对应树根。
func placeNewAsset(r *http.Request, svc *service.Service, assetID uuid.UUID, parent string) error {
	if strings.TrimSpace(parent) == "" {
		return nil
	}
	parentID, err := uuid.Parse(parent)
	if err != nil {
		return errInvalidID
	}
	_, err = svc.Assets.MoveAssetInto(r.Context(), bearer(r), assetID, parentID)
	return err
}
