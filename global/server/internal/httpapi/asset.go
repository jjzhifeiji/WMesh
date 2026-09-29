package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

// 挂平台级工艺/工程；厂内原件入口一律拒绝。
func (h *Handler) mountAsset(mux *http.ServeMux) {
	// 列出平台级元数据，不含正文。
	mux.HandleFunc("GET /v1/assets", h.listAssets)
	// 新建平台级工艺或工程，默认草稿。
	mux.HandleFunc("POST /v1/assets", h.createAsset)
	// 用快照升成平台级草稿。
	mux.HandleFunc("POST /v1/assets/promote", h.promoteAsset)
	// 向该厂要快照，再升成平台级。
	mux.HandleFunc("POST /v1/assets/promote-from", h.promoteFromFactory)
	// 读一条平台级元数据，不含正文。
	mux.HandleFunc("GET /v1/assets/{assetId}", h.getAsset)
	// 读平台级正文，没权限就拒绝。
	mux.HandleFunc("GET /v1/assets/{assetId}/content", h.readAssetContent)
	// 改显示名，稳定身份保持不变。
	mux.HandleFunc("POST /v1/assets/{assetId}/rename", h.renameAsset)
	// 改正文并重算摘要。
	mux.HandleFunc("POST /v1/assets/{assetId}/content", h.updateAssetContent)
	// 改可否复制，已可用则立刻下发。
	mux.HandleFunc("POST /v1/assets/{assetId}/copyable", h.setAssetCopyable)
	// 改作业类型，已经停用的不许改。
	mux.HandleFunc("POST /v1/assets/{assetId}/weld-kind", h.setAssetWeldKind)
	// 另存为新草稿，不看原来可否复制。
	mux.HandleFunc("POST /v1/assets/{assetId}/copy", h.copyAsset)
	// 草稿改为可用，并立刻下发。
	mux.HandleFunc("POST /v1/assets/{assetId}/publish", h.publishAsset)
	// 可用改为停用，并推修订。
	mux.HandleFunc("POST /v1/assets/{assetId}/disable", h.disableAsset)
	// 停用改回可用，并立刻下发。
	mux.HandleFunc("POST /v1/assets/{assetId}/enable", h.enableAsset)
	// 删除并撤回在线厂上的展示。
	mux.HandleFunc("POST /v1/assets/{assetId}/delete", h.deleteAsset)
	// 显式改平台级工程依赖。
	mux.HandleFunc("POST /v1/assets/{assetId}/deps", h.setAssetDeps)
	// 向该厂要可升档列表，不含正文。
	mux.HandleFunc("GET /v1/factories/{id}/promotable-assets", h.listPromotable)
	// 代建厂内原件，一律拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/assets", h.createFactoryAsset)
	// 代查厂内原件，一律拒绝。
	mux.HandleFunc("GET /v1/factories/{id}/assets/{assetId}", h.getFactoryAsset)
	// 代读厂内正文，一律拒绝。
	mux.HandleFunc("GET /v1/factories/{id}/assets/{assetId}/content", h.readFactoryAssetContent)
	// 代改厂内原件，一律拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}", h.updateFactoryAsset)
}

// 新建平台级工艺或工程的请求。
type createAssetReq struct {
	Kind     string             `json:"kind"`     // process / project
	Name     string             `json:"name"`     // 显示名
	Content  string             `json:"content"`  // UTF-8 正文
	WeldKind string             `json:"weldKind"` // 作业类型：single / multilayer / tbar
	Copyable *bool              `json:"copyable"` // 仅工艺；空则默认不可复制
	Deps     []service.AssetDep `json:"deps"`     // 可空；新建从参数补
	ParentID string             `json:"parentId"` // 目录父文件夹；空则挂根
}

// 变更时带上期望修订，对不上就拒绝。
type expectedReq struct {
	Expected int64 `json:"expected"` // 期望修订
}

// 改可否复制，必须对上当前修订。
type copyableReq struct {
	Expected int64 `json:"expected"` // 期望修订
	Copyable bool  `json:"copyable"` // 可否升档
}

// 改作业类型，必须对上当前修订。
type weldKindReq struct {
	Expected int64  `json:"expected"` // 期望修订
	WeldKind string `json:"weldKind"` // 作业类型：single / multilayer / tbar
}

// 另存为新草稿时用的新显示名。
type copyAssetReq struct {
	Name string `json:"name"` // 新显示名
}

// 改工程依赖，必须对上当前修订。
type setDepsReq struct {
	Expected int64              `json:"expected"` // 期望修订
	Deps     []service.AssetDep `json:"deps"`     // 工程依赖
}

// 改显示名，必须对上当前修订。
type renameAssetReq struct {
	Expected int64  `json:"expected"` // 期望修订
	Name     string `json:"name"`     // 新显示名
}

// 改正文，必须对上当前修订。
type contentReq struct {
	Expected int64  `json:"expected"` // 期望修订
	Content  string `json:"content"`  // 新正文
}

// 读出的正文，只给已授权的调用方。
type contentResp struct {
	Content string `json:"content"` // UTF-8 正文
}

// 代管厂内原件的请求，此接口一律拒绝。
type factoryAssetReq struct {
	Name    string `json:"name"`    // 厂级显示名；此接口一律拒绝
	Content string `json:"content"` // 厂级正文；此接口一律拒绝
}

// 指定源厂和厂级身份，再去要快照升档。
type promoteFromReq struct {
	FactoryID string `json:"factoryId"` // 源厂
	AssetID   string `json:"assetId"`   // 该厂可升档厂级身份
}

const channelAsk = 15 * time.Second // 问厂端列表/快照的等待上限

// 列出平台级工艺/工程元数据，不含正文。
func (h *Handler) listAssets(w http.ResponseWriter, r *http.Request) {
	// 读取种类，空则由服务决定是否拒绝。
	rows, err := h.svc.Assets.ListPlatformAssets(r.Context(), bearer(r), r.URL.Query().Get("kind"))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, rows)
}

// 新建平台级工艺或工程，默认草稿。
func (h *Handler) createAsset(w http.ResponseWriter, r *http.Request) {
	// 接住请求体，解析失败再拒绝。
	var req createAssetReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 正文按字节交给服务，不当成文件路径。
	content := []byte(req.Content)
	// 没声明可否复制时，默认不可复制。
	copyable := false
	// 调用方声明了可否复制，就按声明而不是默认。
	if req.Copyable != nil {
		// 调用方明确声明了，就不再用默认。
		copyable = *req.Copyable
	}
	// 先留空结果，按种类填入，失败再拒绝。
	var row service.Asset
	// 错误留到分支里再赋值，避免未赋值就用。
	var err error
	// 按工艺或工程分流，别的种类直接拒绝。
	switch req.Kind {
	// 工艺按可否复制和作业类型新建草稿。
	case service.KindProcess:
		// 按工艺新建草稿，默认可否复制由请求定。
		row, err = h.svc.Assets.CreatePlatformProcessWith(r.Context(), bearer(r), req.Name, content, copyable, req.WeldKind)
	// 工程带上依赖新建草稿。
	case service.KindProject:
		// 按工程新建草稿，并带上依赖。
		row, err = h.svc.Assets.CreatePlatformProject(r.Context(), bearer(r), req.Name, content, req.Deps, req.WeldKind)
	// 种类不对按坏请求拒绝，避免写成脏数据。
	default:
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 挂到目录失败，这次创建不算完成。
	if err := h.placeNewAsset(r, row.ID, req.ParentID); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 创建成功，把新记录交回调用方。
	writeJSON(w, http.StatusCreated, row)
}

// placeNewAsset 新建后挂到指定文件夹；空父节点则留在根。
func (h *Handler) placeNewAsset(r *http.Request, assetID uuid.UUID, parent string) error {
	// 没指定父文件夹就留在根，不算失败。
	if strings.TrimSpace(parent) == "" {
		return nil
	}
	// 解析稳定身份，格式不对就拒绝。
	parentID, err := uuid.Parse(parent)
	// 失败把原因交回去，避免留下残缺结果。
	if err != nil {
		return errInvalidID
	}
	// 把新建的挂进指定文件夹，失败则创建不算完。
	_, err = h.svc.Assets.MoveAssetInto(r.Context(), bearer(r), assetID, parentID)
	return err
}

// 读平台级元数据。
func (h *Handler) getAsset(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 读取平台级元数据，没权限就拒绝。
	row, err := h.svc.Assets.GetPlatformAsset(r.Context(), bearer(r), assetID)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 读平台级正文。
func (h *Handler) readAssetContent(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 读取平台级正文，不可见就拒绝。
	body, err := h.svc.Assets.ReadPlatformAssetContent(r.Context(), bearer(r), assetID)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, contentResp{Content: string(body)})
}

// 改平台级显示名，身份不变。
func (h *Handler) renameAsset(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req renameAssetReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 改显示名，修订对不上就拒绝。
	row, err := h.svc.Assets.RenamePlatformAsset(r.Context(), bearer(r), assetID, req.Expected, req.Name)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 改正文并重算摘要。
func (h *Handler) updateAssetContent(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req contentReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 改正文并重算摘要，修订对不上就拒绝。
	row, err := h.svc.Assets.UpdatePlatformAssetContent(r.Context(), bearer(r), assetID, req.Expected, []byte(req.Content))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 改可否复制；已可用则立刻下发。
func (h *Handler) setAssetCopyable(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req copyableReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 改可否复制，已可用会立刻下发。
	row, err := h.svc.Assets.SetPlatformCopyable(r.Context(), bearer(r), assetID, req.Expected, req.Copyable)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 未停用的工艺或工程可改作业类型。
func (h *Handler) setAssetWeldKind(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req weldKindReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 改作业类型，停用的不允许改。
	row, err := h.svc.Assets.SetPlatformWeldKind(r.Context(), bearer(r), assetID, req.Expected, req.WeldKind)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 平台级工艺或工程另存为新草稿，不看可复制。
func (h *Handler) copyAsset(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req copyAssetReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 另存为新草稿，不看原来可否复制。
	row, err := h.svc.Assets.CopyPlatformProcess(r.Context(), bearer(r), assetID, req.Name)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 创建成功，把新记录交回调用方。
	writeJSON(w, http.StatusCreated, row)
}

// 显式改平台级工程依赖。
func (h *Handler) setAssetDeps(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req setDepsReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 改工程依赖，修订对不上就拒绝。
	row, err := h.svc.Closure.SetPlatformProjectDeps(r.Context(), bearer(r), assetID, req.Expected, req.Deps)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 草稿改为可用并立刻下发。
func (h *Handler) publishAsset(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req expectedReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 改为可用并下发，修订对不上就拒绝。
	row, err := h.svc.Assets.PublishPlatformAsset(r.Context(), bearer(r), assetID, req.Expected)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 可用改为停用并推修订。
func (h *Handler) disableAsset(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req expectedReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 改为停用并推修订，修订对不上就拒绝。
	row, err := h.svc.Assets.DisablePlatformAsset(r.Context(), bearer(r), assetID, req.Expected)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 停用改回可用并立刻下发。
func (h *Handler) enableAsset(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req expectedReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 改回可用并下发，修订对不上就拒绝。
	row, err := h.svc.Assets.ReenablePlatformAsset(r.Context(), bearer(r), assetID, req.Expected)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 删平台级并撤回在线厂展示。
func (h *Handler) deleteAsset(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 删除并撤回展示，仍被引用则拒绝。
	if err := h.svc.Assets.DeletePlatformAsset(r.Context(), bearer(r), assetID); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// 用快照升成平台级草稿。
func (h *Handler) promoteAsset(w http.ResponseWriter, r *http.Request) {
	// 接住升档快照，坏包按坏请求拒绝。
	var snap service.AssetSnapshot
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &snap); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 用快照做成平台级草稿，不合格就拒绝。
	row, err := h.svc.Assets.PromoteFromSnapshot(r.Context(), bearer(r), snap)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 创建成功，把新记录交回调用方。
	writeJSON(w, http.StatusCreated, row)
}

// 升档清单元数据，不含正文。
type promotableAsset struct {
	ID       string `json:"id"`       // 稳定身份
	Kind     string `json:"kind"`     // process / project
	Level    string `json:"level"`    // factory / personal / platform
	Name     string `json:"name"`     // 显示名
	Code     string `json:"code"`     // 只读编号
	Revision int64  `json:"revision"` // 当前修订
	Digest   []byte `json:"digest"`   // 内容摘要
	Status   string `json:"status"`   // draft / available / disabled
	Copyable bool   `json:"copyable"` // 原样带回
	WeldKind string `json:"weldKind"` // 作业类型
}

// 经 MQTT 向该厂要升档列表，不含正文。
func (h *Handler) listPromotable(w http.ResponseWriter, r *http.Request) {
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
	// 向该厂要可升档列表，不含正文。
	msg, err := h.svc.CallFactory(ctx, fid, service.Cmd{Typ: service.CmdAssetList, Kind: kind})
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 先给空列表，厂没回就不要当成失败。
	rows := []promotableAsset{}
	// 厂回了列表才解开，空的保持空数组。
	if len(msg.Assets) > 0 {
		// 厂端目录或列表解不开，就当厂不在线。
		if err := json.Unmarshal(msg.Assets, &rows); err != nil {
			// 按业务错误回写，内部细节不出网。
			writeErr(w, domain.ErrFactoryOffline)
			return
		}
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, rows)
}

// 经 MQTT 取该厂快照，再在 WAN 做成平台级。
func (h *Handler) promoteFromFactory(w http.ResponseWriter, r *http.Request) {
	// 不是云端管理员就拒绝，厂端会话不行。
	if _, err := h.svc.RequireAdmin(r.Context(), bearer(r)); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req promoteFromReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 解析稳定身份，格式不对就拒绝。
	fid, err := uuid.Parse(req.FactoryID)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 身份不是合法的稳定号，就拒绝。
	if _, err := uuid.Parse(req.AssetID); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 厂已停用或注销，就不再问厂端。
	if err := h.guardAskFactory(r.Context(), fid); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 问厂端限时，超时就当厂不在线。
	ctx, cancel := context.WithTimeout(r.Context(), channelAsk)
	// 离开时取消限时，避免协程继续占着。
	defer cancel()
	// 向该厂要快照，没有就不能升档。
	msg, err := h.svc.CallFactory(ctx, fid, service.Cmd{Typ: service.CmdAssetSnapshot, AssetID: req.AssetID})
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 厂没回快照就当没有这份，不能升档。
	if len(msg.Snapshot) == 0 {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, domain.ErrNotFound)
		return
	}
	// 接住升档快照，坏包按坏请求拒绝。
	var snap service.AssetSnapshot
	// 快照解不开就不能升档，避免写入残缺稿。
	if err := json.Unmarshal(msg.Snapshot, &snap); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, domain.ErrNotFound)
		return
	}
	// 用快照做成平台级草稿，不合格就拒绝。
	row, err := h.svc.Assets.PromoteFromSnapshot(r.Context(), bearer(r), snap)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 创建成功，把新记录交回调用方。
	writeJSON(w, http.StatusCreated, row)
}

// 停用或注销的厂不再问升档。
func (h *Handler) guardAskFactory(ctx context.Context, factoryID uuid.UUID) error {
	// 再读治理状态，读不到就不补展示字段。
	fac, err := h.svc.Store().FactoryByID(ctx, factoryID)
	// 失败把原因交回去，避免留下残缺结果。
	if err != nil {
		return err
	}
	// 已注销的厂不再问，避免升档到无效厂。
	if fac.Status == service.FactoryRetired {
		return domain.ErrFactoryRetired
	}
	// 已停用的厂不再问，先启用才能升档。
	if fac.Status == service.FactoryDisabled {
		return domain.ErrFactoryDisabled
	}
	return nil
}

// 代建厂内原件，一律拒绝。
func (h *Handler) createFactoryAsset(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	id, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req factoryAssetReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 代管厂内数据一律拒绝，不把结果写成成功。
	writeErr(w, h.svc.Assets.CreateFactoryProcess(r.Context(), bearer(r), id, req.Name, []byte(req.Content)))
}

// 代查厂内原件，一律拒绝。
func (h *Handler) getFactoryAsset(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	fid, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 代管厂内数据一律拒绝，不把结果写成成功。
	writeErr(w, h.svc.Assets.GetFactoryAsset(r.Context(), bearer(r), fid, assetID))
}

// 代读厂内正文，一律拒绝。
func (h *Handler) readFactoryAssetContent(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	fid, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 代管厂内数据一律拒绝，不把结果写成成功。
	writeErr(w, h.svc.Assets.ReadFactoryAssetContent(r.Context(), bearer(r), fid, assetID))
}

// 代改厂内原件，一律拒绝。
func (h *Handler) updateFactoryAsset(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	fid, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 从路径取出身份，不合法就拒绝这次操作。
	assetID, err := parseAssetID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 代管厂内数据一律拒绝，不把结果写成成功。
	writeErr(w, h.svc.Assets.UpdateFactoryAsset(r.Context(), bearer(r), fid, assetID))
}

// 解析路径里的资产身份。
func parseAssetID(r *http.Request) (uuid.UUID, error) {
	// 解析稳定身份，格式不对就拒绝。
	id, err := uuid.Parse(r.PathValue("assetId"))
	// 失败把原因交回去，避免留下残缺结果。
	if err != nil {
		return uuid.Nil, errInvalidID
	}
	return id, nil
}
