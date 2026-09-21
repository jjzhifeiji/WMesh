package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

type fsFolderReq struct {
	ParentID string `json:"parentId"` // 父文件夹
	Name     string `json:"name"`     // 显示名
	Kind     string `json:"kind"`     // process / project；建根下文件夹时用
}

type fsRenameReq struct {
	Name string `json:"name"` // 新显示名
}

type fsMoveReq struct {
	ParentID string `json:"parentId"` // 新父文件夹
}

// 挂平台级目录树。
func (h *Handler) mountFS(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/fs", h.listFS)
	mux.HandleFunc("POST /v1/fs/folders", h.createFSFolder)
	mux.HandleFunc("POST /v1/fs/{nodeId}/rename", h.renameFSNode)
	mux.HandleFunc("POST /v1/fs/{nodeId}/move", h.moveFSNode)
	mux.HandleFunc("POST /v1/fs/{nodeId}/delete", h.deleteFSNode)
	mux.HandleFunc("POST /v1/fs/{nodeId}/copy", h.copyFSNode)
	mux.HandleFunc("GET /v1/factories/{id}/fs", h.listFactoryFS)
}

// 列出平台级目录，不含正文。
func (h *Handler) listFS(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.Assets.ListFS(r.Context(), bearer(r), r.URL.Query().Get("kind"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// 新建文件夹。
func (h *Handler) createFSFolder(w http.ResponseWriter, r *http.Request) {
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
	row, err := h.svc.Assets.CreateFSFolder(r.Context(), bearer(r), parentID, req.Name)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

// 改文件夹显示名。
func (h *Handler) renameFSNode(w http.ResponseWriter, r *http.Request) {
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
	row, err := h.svc.Assets.RenameFSNode(r.Context(), bearer(r), nodeID, req.Name)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// 把节点挂到另一个文件夹。
func (h *Handler) moveFSNode(w http.ResponseWriter, r *http.Request) {
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
	row, err := h.svc.Assets.MoveFSNode(r.Context(), bearer(r), nodeID, parentID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// 删文件夹或文件（文件走删资产）。
func (h *Handler) deleteFSNode(w http.ResponseWriter, r *http.Request) {
	nodeID, err := parseNodeID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	if err := h.svc.Assets.DeleteFSNode(r.Context(), bearer(r), nodeID); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// 复制文件夹或文件到目录；原件不动。
func (h *Handler) copyFSNode(w http.ResponseWriter, r *http.Request) {
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
	row, err := h.svc.Assets.CopyFSNode(r.Context(), bearer(r), nodeID, parentID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

// 经通道拉该厂目录结构，不含正文。
func (h *Handler) listFactoryFS(w http.ResponseWriter, r *http.Request) {
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
	msg, err := h.svc.CallFactory(ctx, fid, service.Cmd{Typ: service.CmdFSList, Kind: kind})
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(msg.Assets) == 0 {
		writeJSON(w, http.StatusOK, []struct{}{})
		return
	}
	// 原样回厂端目录 JSON，保留个人树主人名和厂内资产字段。
	var probe []json.RawMessage
	if err := json.Unmarshal(msg.Assets, &probe); err != nil {
		writeErr(w, domain.ErrFactoryOffline)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(msg.Assets)
}

// parseNodeID 从路径取出目录节点身份。
func parseNodeID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("nodeId"))
	if err != nil {
		return uuid.Nil, errInvalidID
	}
	return id, nil
}
