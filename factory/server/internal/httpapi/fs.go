package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

// 新文件夹的名称，以及挂在哪个节点下。
type fsFolderReq struct {
	ParentID string `json:"parentId"` // 父文件夹
	Name     string `json:"name"`     // 显示名
}

// 文件夹要改成的显示名，身份不变。
type fsRenameReq struct {
	Name string `json:"name"` // 新显示名
}

// 要把节点挪到或复制到的父文件夹。
type fsMoveReq struct {
	ParentID string `json:"parentId"` // 新父文件夹
}

// 挂本厂目录树。
func (h *Handler) mountFS(mux *http.ServeMux) {
	// 列出当前人能看见的目录，不含正文。
	mux.HandleFunc("GET /v1/factories/{id}/fs", h.listFS)
	// 新建文件夹，平台级副本树会拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/fs/folders", h.createFSFolder)
	// 只改文件夹显示名，节点身份不变。
	mux.HandleFunc("POST /v1/factories/{id}/fs/{nodeId}/rename", h.renameFSNode)
	// 把节点挂到另一个文件夹下。
	mux.HandleFunc("POST /v1/factories/{id}/fs/{nodeId}/move", h.moveFSNode)
	// 删除文件夹或本厂原件，副本会拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/fs/{nodeId}/delete", h.deleteFSNode)
	// 复制到可写目录，原来的那份不动。
	mux.HandleFunc("POST /v1/factories/{id}/fs/{nodeId}/copy", h.copyFSNode)
}

// 列出当前人能看见的目录，不含正文。
func (h *Handler) listFS(w http.ResponseWriter, r *http.Request) {
	// 列出当前人能看见的目录，不含正文。
	h.withFactory(w, r, func(svc *service.Service) {
		// 读取种类，空着表示不按种类限制。
		rows, err := svc.Assets.ListFS(r.Context(), bearer(r), r.URL.Query().Get("kind"))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, rows)
	})
}

// 新建文件夹；平台级副本树拒绝。
func (h *Handler) createFSFolder(w http.ResponseWriter, r *http.Request) {
	// 新建文件夹；平台级副本树拒绝。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接文件夹名称和父节点。
		var req fsFolderReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 目标文件夹必须合法，否则拒绝。
		parentID, err := uuid.Parse(req.ParentID)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 新建文件夹，平台级副本上会拒绝。
		row, err := svc.Assets.CreateFSFolder(r.Context(), bearer(r), parentID, req.Name)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 创建成功，把新记录回给调用方。
		writeJSON(w, http.StatusCreated, row)
	})
}

// 改文件夹显示名。
func (h *Handler) renameFSNode(w http.ResponseWriter, r *http.Request) {
	// 只改文件夹显示名，节点身份不变。
	h.withFactory(w, r, func(svc *service.Service) {
		// 从路径取出目录节点，不是合法编号则拒绝。
		nodeID, err := parseNodeID(r)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 承接文件夹的新显示名。
		var req fsRenameReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 只改文件夹显示名，节点身份不变。
		row, err := svc.Assets.RenameFSNode(r.Context(), bearer(r), nodeID, req.Name)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, row)
	})
}

// 把节点挂到另一个文件夹。
func (h *Handler) moveFSNode(w http.ResponseWriter, r *http.Request) {
	// 把节点挂到另一个文件夹。
	h.withFactory(w, r, func(svc *service.Service) {
		// 从路径取出目录节点，不是合法编号则拒绝。
		nodeID, err := parseNodeID(r)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 承接要挪去的父文件夹。
		var req fsMoveReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 目标文件夹必须合法，否则拒绝。
		parentID, err := uuid.Parse(req.ParentID)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 把节点挂到另一个文件夹下面。
		row, err := svc.Assets.MoveFSNode(r.Context(), bearer(r), nodeID, parentID)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, row)
	})
}

// 删文件夹或本厂原件；平台级副本拒绝。
func (h *Handler) deleteFSNode(w http.ResponseWriter, r *http.Request) {
	// 删文件夹或本厂原件；平台级副本拒绝。
	h.withFactory(w, r, func(svc *service.Service) {
		// 从路径取出目录节点，不是合法编号则拒绝。
		nodeID, err := parseNodeID(r)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Assets.DeleteFSNode(r.Context(), bearer(r), nodeID); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

// 复制文件夹或文件到可写目录；原件不动。
func (h *Handler) copyFSNode(w http.ResponseWriter, r *http.Request) {
	// 复制文件夹或文件到可写目录；原件不动。
	h.withFactory(w, r, func(svc *service.Service) {
		// 从路径取出目录节点，不是合法编号则拒绝。
		nodeID, err := parseNodeID(r)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 承接要挪去的父文件夹。
		var req fsMoveReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 目标文件夹必须合法，否则拒绝。
		parentID, err := uuid.Parse(req.ParentID)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 复制到可写目录，原来的那份保持不动。
		row, err := svc.Assets.CopyFSNode(r.Context(), bearer(r), nodeID, parentID)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 创建成功，把新记录回给调用方。
		writeJSON(w, http.StatusCreated, row)
	})
}

// parseNodeID 从路径取出目录节点身份。
func parseNodeID(r *http.Request) (uuid.UUID, error) {
	// 目录编号必须合法，否则拒绝这次办理。
	id, err := uuid.Parse(r.PathValue("nodeId"))
	// 解析失败则拒绝，不用这个残缺的值继续。
	if err != nil {
		return uuid.Nil, errInvalidID
	}
	return id, nil
}

// placeNewAsset 新建后挂到指定文件夹；空父节点则留在对应树根。
func placeNewAsset(r *http.Request, svc *service.Service, assetID uuid.UUID, parent string) error {
	// 没指定父级就留在树根，不算失败。
	if strings.TrimSpace(parent) == "" {
		return nil
	}
	// 父文件夹必须合法，否则不把记录挂上去。
	parentID, err := uuid.Parse(parent)
	// 解析失败则拒绝，不用这个残缺的值继续。
	if err != nil {
		return errInvalidID
	}
	// 把记录挂进指定文件夹，挂不上则失败。
	_, err = svc.Assets.MoveAssetInto(r.Context(), bearer(r), assetID, parentID)
	return err
}
