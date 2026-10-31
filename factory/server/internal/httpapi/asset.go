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
	// 查询当前账号可以用来新建的工作位置。
	// 按许可列出本厂工艺和工程，不含正文。
	mux.HandleFunc("GET /v1/factories/{id}/assets", h.listAssets)
	// 按级别新建工艺或工程，没带节点则直属。
	mux.HandleFunc("POST /v1/factories/{id}/assets", h.createAsset)
	// 读取一条元数据，并不解开正文。
	mux.HandleFunc("GET /v1/factories/{id}/assets/{assetId}", h.getAsset)
	// 读取正文，不可复制的平台工艺不给人看。
	mux.HandleFunc("GET /v1/factories/{id}/assets/{assetId}/content", h.readAssetContent)
	// 导出厂级快照，供平台升档使用。
	mux.HandleFunc("GET /v1/factories/{id}/assets/{assetId}/snapshot", h.exportAsset)
	// 修改显示名，资产身份保持不变。
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/rename", h.renameAsset)
	// 改正文并重算摘要，停用之后拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/content", h.updateAssetContent)
	// 修改作业类型，停用之后拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/weld-kind", h.setAssetWeldKind)
	// 把草稿改为可用，失败则保持原状。
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/publish", h.publishAsset)
	// 改为停用，停用期间不能改正文。
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/disable", h.disableAsset)
	// 把停用改回可用，身份仍然保留。
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/enable", h.enableAsset)
	// 删除未被工程依赖的记录，引用中则拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/delete", h.deleteAsset)
	// 把可复制工艺另存为新的草稿。
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/copy", h.copyAsset)
	// 把个人级升为厂级，不可复制则拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/promote", h.promoteAsset)
	// 改写本厂工程依赖，修订冲突则拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/assets/{assetId}/deps", h.setAssetDeps)
	// 向平台要工艺快照，通道不在也先返回。
	mux.HandleFunc("POST /v1/factories/{id}/assets/sync", h.syncAssets)
	// 平板新建，保存之后即为可用。
	mux.HandleFunc("POST /v1/factories/{id}/pad/assets", h.createPadAsset)
	// 示教器覆盖当前正文，不比对期望修订。
	mux.HandleFunc("POST /v1/factories/{id}/pad/assets/{assetId}/content", h.applyPadAsset)
}

// 新建工艺或工程，可以随后挂进目录。
type createAssetReq struct {
	Kind     string             `json:"kind"`     // process / project
	Level    string             `json:"level"`    // 没选文件夹时用：factory / personal，空按个人级；选了文件夹按文件夹所在的树
	Name     string             `json:"name"`     // 显示名
	Content  string             `json:"content"`  // UTF-8 正文
	WeldKind string             `json:"weldKind"` // 作业类型：single / multilayer / tbar
	Deps     []service.AssetDep `json:"deps"`     // 可空；新建从参数补
	ParentID string             `json:"parentId"` // 目录父文件夹；空则挂对应树的根
}

// 示教器要覆盖的正文，不带期望修订。
type padContentReq struct {
	Content string `json:"content"` // UTF-8 正文；不带期望修订
}

// 平板新建，身份和编号可以沿用本机。
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

// 调用方看到的修订，对不上就拒绝改。
type expectedReq struct {
	Expected int64 `json:"expected"` // 期望修订
}

// 新的显示名，以及调用方看到的修订。
type renameAssetReq struct {
	Expected int64  `json:"expected"` // 期望修订
	Name     string `json:"name"`     // 新显示名
}

// 新的正文，以及调用方看到的修订。
type contentReq struct {
	Expected int64  `json:"expected"` // 期望修订
	Content  string `json:"content"`  // 新正文
}

// 作业类型，以及调用方看到的修订。
type weldKindReq struct {
	Expected int64  `json:"expected"` // 期望修订
	WeldKind string `json:"weldKind"` // 作业类型：single / multilayer / tbar
}

// 另存工艺时使用的新显示名。
type copyAssetReq struct {
	Name string `json:"name"` // 新工艺显示名
}

// 新的工程依赖，以及调用方看到的修订。
type setDepsReq struct {
	Expected int64              `json:"expected"` // 期望修订
	Deps     []service.AssetDep `json:"deps"`     // 工程依赖
}

// 只在允许阅读时才回给调用方的正文。
type contentResp struct {
	Content string `json:"content"` // UTF-8 正文
}

// 按许可列本厂工艺/工程元数据，不含正文。
func (h *Handler) listAssets(w http.ResponseWriter, r *http.Request) {
	// 按许可列本厂工艺/工程元数据，不含正文。
	h.withFactory(w, r, func(svc *service.Service) {
		// 读取种类，空着表示不按种类限制。
		rows, err := svc.Assets.ListAssets(r.Context(), bearer(r), r.URL.Query().Get("kind"))
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

// 进工艺/工程页时异步向 WAN 要当前平台级快照；通道不在也立刻返回。
func (h *Handler) syncAssets(w http.ResponseWriter, r *http.Request) {
	// 进工艺/工程页时异步向 WAN 要当前平台级快照；通道不在也立刻返回。
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
		h.Hub.RequestWANSync(fid, "sync_closures", kind)
		// 已经排上就先返回，不等后台做完。
		writeJSON(w, http.StatusAccepted, map[string]string{"ok": "true"})
	})
}

// 按级别分流创建；没带节点就记工厂直属。
func (h *Handler) createAsset(w http.ResponseWriter, r *http.Request) {
	// 按级别分流创建；没带节点就记工厂直属。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接新建请求，多出来的字段会拒绝。
		var req createAssetReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 先解析目标文件夹，格式不对就不建资产。
		parentID, err := parseParentID(req.ParentID)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 目标文件夹交给业务，和建资产同一笔提交。
		wc := service.WorkContext{FolderID: parentID}
		// 正文按原始字节保存，适配层不改编码。
		content := []byte(req.Content)
		// 厂端建的都可复制，保密只对平台级副本。
		copyable := true
		// 选了文件夹就按文件夹所在的树定级别，不另选。
		if parentID != uuid.Nil {
			// 读出文件夹所在的树，平台树不能建。
			lvl, err := svc.Assets.FolderLevel(r.Context(), bearer(r), parentID)
			// 文件夹不在或不能建就拒绝。
			if err != nil {
				// 把失败译成状态回给调用方，不写成功。
				writeErr(w, err)
				return
			}
			// 改用文件夹所在树的级别。
			req.Level = lvl
		}
		// 没选文件夹又没给级别就按个人级。
		if req.Level == "" {
			// 默认建在自己名下。
			req.Level = service.AssetLevelPersonal
		}
		// 接住按种类和级别创建出来的那一行。
		var row service.Asset
		// 按种类和级别分流，对不上就拒绝创建。
		switch {
		// 厂级工艺走厂级创建，并记下归属。
		case req.Kind == service.KindProcess && req.Level == service.AssetLevelFactory:
			// 按级别创建工艺，无权则由业务拒绝。
			row, err = svc.Assets.CreateProcess(r.Context(), bearer(r), wc, service.AssetLevelFactory, req.Name, content, copyable, req.WeldKind)
		// 个人工艺只归当前账号，不进厂级。
		case req.Kind == service.KindProcess && req.Level == service.AssetLevelPersonal:
			// 按级别创建工艺，无权则由业务拒绝。
			row, err = svc.Assets.CreateProcess(r.Context(), bearer(r), wc, service.AssetLevelPersonal, req.Name, content, copyable, req.WeldKind)
		// 厂级工程记下直属或节点归属。
		case req.Kind == service.KindProject && req.Level == service.AssetLevelFactory:
			// 创建厂级工程，并记下直属或节点。
			row, err = svc.Assets.CreateFactoryProject(r.Context(), bearer(r), wc, req.Name, content, req.Deps, req.WeldKind)
		// 个人工程只归当前账号。
		case req.Kind == service.KindProject && req.Level == service.AssetLevelPersonal:
			// 创建只归当前账号的个人工程。
			row, err = svc.Assets.CreatePersonalProject(r.Context(), bearer(r), wc, req.Name, content, req.Deps, req.WeldKind)
		// 种类和级别对不上则拒绝，不写入。
		default:
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 失败则停止，不把这一步当成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 创建成功，把新记录回给调用方。
		writeJSON(w, http.StatusCreated, row)
	})
}

// 平板新建：个人级或管理员的厂级，保存即为可用。
func (h *Handler) createPadAsset(w http.ResponseWriter, r *http.Request) {
	// 平板新建：个人级或管理员的厂级，保存即为可用。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接平板新建，身份可以沿用本机。
		var req padCreateAssetReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 先空着编号，解析成功再采用。
		var id uuid.UUID
		// 带了本机身份才解析，空着则由厂端发号。
		if strings.TrimSpace(req.ID) != "" {
			// 带来的身份必须合法，否则拒绝新建。
			parsed, err := uuid.Parse(req.ID)
			// 解析失败则拒绝，不用这个残缺的值继续。
			if err != nil {
				// 编号不合法，回非法请求并不进入业务。
				writeBadRequest(w, errInvalidID)
				return
			}
			// 采用本机已经发出的资产身份。
			id = parsed
		}
		// 先解析目标文件夹，格式不对就不建资产。
		parentID, err := parseParentID(req.ParentID)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 正文按原始字节保存；目录挂不上由业务退回树根。
		row, err := svc.Assets.CreatePadIn(r.Context(), bearer(r), req.Level, req.Kind, req.Name, []byte(req.Content), id, req.Code, req.Deps, parentID, req.WeldKind)
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

// 示教器盖当前行，不比对期望修订。
func (h *Handler) applyPadAsset(w http.ResponseWriter, r *http.Request) {
	// 示教器盖当前行，不比对期望修订。
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		// 承接要覆盖的正文，不要求带修订。
		var req padContentReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 正文按原始字节保存，适配层不改编码。
		row, err := svc.Assets.ApplyAppContent(r.Context(), bearer(r), assetID, []byte(req.Content))
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

// 读元数据，不解包正文。
func (h *Handler) getAsset(w http.ResponseWriter, r *http.Request) {
	// 读元数据，不解包正文。
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		// 读取元数据，并不解开正文。
		row, err := svc.Assets.GetAsset(r.Context(), bearer(r), assetID)
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

// 读正文并核对摘要；不可复制的平台级工艺不给人看。
func (h *Handler) readAssetContent(w http.ResponseWriter, r *http.Request) {
	// 读正文并核对摘要；不可复制的平台级工艺不给人看。
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		// 读取正文并核对摘要，无权则拒绝。
		body, err := svc.Assets.ReadAssetContent(r.Context(), bearer(r), assetID)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, contentResp{Content: string(body)})
	})
}

// 导出厂级快照给 WAN 升档。
func (h *Handler) exportAsset(w http.ResponseWriter, r *http.Request) {
	// 导出厂级快照给 WAN 升档。
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		// 导出厂级快照，供平台升档使用。
		snap, err := svc.Assets.ExportAssetSnapshot(r.Context(), bearer(r), assetID)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, snap)
	})
}

// 改显示名，身份不变，修订升高。
func (h *Handler) renameAsset(w http.ResponseWriter, r *http.Request) {
	// 改显示名，身份不变，修订升高。
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		// 承接新名字和调用方看到的修订。
		var req renameAssetReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 修改显示名，资产身份保持不变。
		row, err := svc.Assets.RenameAsset(r.Context(), bearer(r), assetID, req.Expected, req.Name)
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

// 改正文并重算摘要；停用后拒绝。
func (h *Handler) updateAssetContent(w http.ResponseWriter, r *http.Request) {
	// 改正文并重算摘要；停用后拒绝。
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		// 承接新正文和调用方看到的修订。
		var req contentReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 正文按原始字节保存，适配层不改编码。
		row, err := svc.Assets.UpdateAssetContent(r.Context(), bearer(r), assetID, req.Expected, []byte(req.Content))
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

// 未停用的本厂工艺或工程可改作业类型。
func (h *Handler) setAssetWeldKind(w http.ResponseWriter, r *http.Request) {
	// 未停用的本厂工艺或工程可改作业类型。
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		// 承接作业类型和调用方看到的修订。
		var req weldKindReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 修改作业类型，停用之后拒绝。
		row, err := svc.Assets.SetAssetWeldKind(r.Context(), bearer(r), assetID, req.Expected, req.WeldKind)
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

// 草稿改为可用。
func (h *Handler) publishAsset(w http.ResponseWriter, r *http.Request) {
	// 把草稿改为可用，停用之后拒绝。
	h.withExpected(w, r, func(svc *service.Service, assetID uuid.UUID, expected int64) {
		// 把草稿改为可用，失败则保持原状。
		row, err := svc.Assets.PublishAsset(r.Context(), bearer(r), assetID, expected)
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

// 可用改为停用；停用期间不得改正文或升档。
func (h *Handler) disableAsset(w http.ResponseWriter, r *http.Request) {
	// 可用改为停用；停用期间不得改正文或升档。
	h.withExpected(w, r, func(svc *service.Service, assetID uuid.UUID, expected int64) {
		// 改为停用，停用期间不能改正文。
		row, err := svc.Assets.DisableAsset(r.Context(), bearer(r), assetID, expected)
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

// 停用改回可用。
func (h *Handler) enableAsset(w http.ResponseWriter, r *http.Request) {
	// 把停用改回可用，身份仍然保留。
	h.withExpected(w, r, func(svc *service.Service, assetID uuid.UUID, expected int64) {
		// 把停用改回可用，身份仍然保留。
		row, err := svc.Assets.ReenableAsset(r.Context(), bearer(r), assetID, expected)
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

// 未被工程依赖则可删。
func (h *Handler) deleteAsset(w http.ResponseWriter, r *http.Request) {
	// 未被依赖才删除，仍被引用则拒绝。
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Assets.DeleteAsset(r.Context(), bearer(r), assetID); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	})
}

// 可复制工艺另存为新草稿。
func (h *Handler) copyAsset(w http.ResponseWriter, r *http.Request) {
	// 可复制工艺另存为新草稿。
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		// 承接另存之后要用的新显示名。
		var req copyAssetReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 把可复制工艺另存成新的草稿。
		row, err := svc.Assets.CopyProcess(r.Context(), bearer(r), assetID, req.Name)
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

// 升档要放进的厂级文件夹，空则挂厂级树根。
type promoteReq struct {
	FolderID string `json:"folderId"` // 厂级文件夹；空挂树根
}

// 把未停用个人级复制一份到所选厂级文件夹，落成草稿。
func (h *Handler) promoteAsset(w http.ResponseWriter, r *http.Request) {
	// 把未停用个人级复制一份到所选厂级文件夹，落成草稿。
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		// 承接目标文件夹，旧客户端不带正文时挂树根。
		var req promoteReq
		// 带了正文才解析，空正文按树根。
		if r.ContentLength != 0 {
			// 正文无法解析则拒绝，不进入业务。
			if err := decodeJSON(r, &req); err != nil {
				// 请求不合法，回非法请求并不进入业务。
				writeBadRequest(w, err)
				return
			}
		}
		// 解析目标文件夹，格式不对就不升。
		folderID, err := parseParentID(req.FolderID)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 复制一份厂级草稿。
		row, err := svc.Assets.PromoteToFactory(r.Context(), bearer(r), assetID, folderID)
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

// 显式改本厂工程依赖。
func (h *Handler) setAssetDeps(w http.ResponseWriter, r *http.Request) {
	// 显式改本厂工程依赖。
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		// 承接新依赖和调用方看到的修订。
		var req setDepsReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 改写工程依赖，修订对不上则拒绝。
		row, err := svc.Closure.SetProjectDeps(r.Context(), bearer(r), assetID, req.Expected, req.Deps)
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

// 解析资产身份后转给业务。
func (h *Handler) withAsset(w http.ResponseWriter, r *http.Request, fn func(*service.Service, uuid.UUID)) {
	// 解析资产身份后转给业务。
	h.withFactory(w, r, func(svc *service.Service) {
		// 资产编号必须合法，否则拒绝这次办理。
		assetID, err := uuid.Parse(r.PathValue("assetId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 前置已经满足，把这次请求交给业务。
		fn(svc, assetID)
	})
}

// 先解析资产身份和期望修订再调业务。
func (h *Handler) withExpected(w http.ResponseWriter, r *http.Request, fn func(*service.Service, uuid.UUID, int64)) {
	// 先解析资产身份和期望修订再调业务。
	h.withAsset(w, r, func(svc *service.Service, assetID uuid.UUID) {
		// 承接期望修订，对不上就拒绝修改。
		var req expectedReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 前置已经满足，把这次请求交给业务。
		fn(svc, assetID, req.Expected)
	})
}
