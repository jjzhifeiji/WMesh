package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

// 新建文件夹时的父级和显示名。
type fsFolderReq struct {
	ParentID string `json:"parentId"` // 父文件夹
	Name     string `json:"name"`     // 显示名
	Kind     string `json:"kind"`     // process / project；建根下文件夹时用
}

// 目录节点要改成的新显示名。
type fsRenameReq struct {
	Name string `json:"name"` // 新显示名
}

// 目录节点要挂到的新父文件夹。
type fsMoveReq struct {
	ParentID string `json:"parentId"` // 新父文件夹
}

// 挂平台级目录树。
func (h *Handler) mountFS(mux *http.ServeMux) {
	// 列出平台级目录，不含正文。
	mux.HandleFunc("GET /v1/fs", h.listFS)
	// 新建文件夹，父级不合法就拒绝。
	mux.HandleFunc("POST /v1/fs/folders", h.createFSFolder)
	// 改目录节点的显示名。
	mux.HandleFunc("POST /v1/fs/{nodeId}/rename", h.renameFSNode)
	// 把节点挂到另一个文件夹。
	mux.HandleFunc("POST /v1/fs/{nodeId}/move", h.moveFSNode)
	// 删文件夹或文件，文件走删资产。
	mux.HandleFunc("POST /v1/fs/{nodeId}/delete", h.deleteFSNode)
	// 复制到目标目录，原件保持不动。
	mux.HandleFunc("POST /v1/fs/{nodeId}/copy", h.copyFSNode)
	// 经通道拉该厂目录，不含正文。
	mux.HandleFunc("GET /v1/factories/{id}/fs", h.listFactoryFS)
}

// 列出平台级目录，不含正文。
func (h *Handler) listFS(w http.ResponseWriter, r *http.Request) {
	// 读取种类，空则由服务决定是否拒绝。
	rows, err := h.svc.Assets.ListFS(r.Context(), bearer(r), r.URL.Query().Get("kind"))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, rows)
}

// 新建文件夹。
func (h *Handler) createFSFolder(w http.ResponseWriter, r *http.Request) {
	// 接住请求体，解析失败再拒绝。
	var req fsFolderReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 解析稳定身份，格式不对就拒绝。
	parentID, err := uuid.Parse(req.ParentID)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 在父级下新建文件夹，父级非法则拒绝。
	row, err := h.svc.Assets.CreateFSFolder(r.Context(), bearer(r), parentID, req.Name)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 创建成功，把新记录交回调用方。
	writeJSON(w, http.StatusCreated, row)
}

// 改文件夹显示名。
func (h *Handler) renameFSNode(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	nodeID, err := parseNodeID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req fsRenameReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 改显示名，节点不存在则拒绝。
	row, err := h.svc.Assets.RenameFSNode(r.Context(), bearer(r), nodeID, req.Name)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 把节点挂到另一个文件夹。
func (h *Handler) moveFSNode(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	nodeID, err := parseNodeID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req fsMoveReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 解析稳定身份，格式不对就拒绝。
	parentID, err := uuid.Parse(req.ParentID)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 挂到新父级，成环则拒绝。
	row, err := h.svc.Assets.MoveFSNode(r.Context(), bearer(r), nodeID, parentID)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 删文件夹或文件（文件走删资产）。
func (h *Handler) deleteFSNode(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	nodeID, err := parseNodeID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 删除节点，文件则连资产一起删。
	if err := h.svc.Assets.DeleteFSNode(r.Context(), bearer(r), nodeID); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// 复制文件夹或文件到目录；原件不动。
func (h *Handler) copyFSNode(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	nodeID, err := parseNodeID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req fsMoveReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 解析稳定身份，格式不对就拒绝。
	parentID, err := uuid.Parse(req.ParentID)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 复制到目标目录，原件保持不动。
	row, err := h.svc.Assets.CopyFSNode(r.Context(), bearer(r), nodeID, parentID)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 创建成功，把新记录交回调用方。
	writeJSON(w, http.StatusCreated, row)
}

// 经通道拉该厂目录结构，不含正文。
func (h *Handler) listFactoryFS(w http.ResponseWriter, r *http.Request) {
	// 不是云端管理员就拒绝，厂端会话不行。
	if _, err := h.svc.RequireAdmin(r.Context(), bearer(r)); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 从路径取出身份，不合法就拒绝这次操作。
	fid, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 厂已停用或注销，就不再问厂端。
	if err := h.guardAskFactory(r.Context(), fid); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 读取种类，空则由服务决定是否拒绝。
	kind := r.URL.Query().Get("kind")
	// 问厂端限时，超时就当厂不在线。
	ctx, cancel := context.WithTimeout(r.Context(), channelAsk)
	// 离开时取消限时，避免协程继续占着。
	defer cancel()
	// 向该厂要目录，超时就当不在线。
	msg, err := h.svc.CallFactory(ctx, fid, service.Cmd{Typ: service.CmdFSList, Kind: kind})
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 厂没回目录就给空列表，不当成故障。
	if len(msg.Assets) == 0 {
		// 厂没回目录，回空列表而不是失败。
		writeJSON(w, http.StatusOK, []struct{}{})
		return
	}
	// 原样回厂端目录 JSON，保留个人树主人名和厂内资产字段。
	var probe []json.RawMessage
	// 厂端目录或列表解不开，就当厂不在线。
	if err := json.Unmarshal(msg.Assets, &probe); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, domain.ErrFactoryOffline)
		return
	}
	// 原样声明 JSON，保留厂端目录字段。
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// 先写成功，再原样送出厂端目录。
	w.WriteHeader(http.StatusOK)
	// 原样送出厂端目录，字段不在这里改。
	_, _ = w.Write(msg.Assets)
}

// parseNodeID 从路径取出目录节点身份。
func parseNodeID(r *http.Request) (uuid.UUID, error) {
	// 解析稳定身份，格式不对就拒绝。
	id, err := uuid.Parse(r.PathValue("nodeId"))
	// 失败把原因交回去，避免留下残缺结果。
	if err != nil {
		return uuid.Nil, errInvalidID
	}
	return id, nil
}
