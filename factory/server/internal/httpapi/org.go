package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

// 名册、组织、人员、角色。
func (h *Handler) mountOrg(mux *http.ServeMux) {
	// 超管看全厂名册，其他人只能看自己。
	mux.HandleFunc("GET /v1/factories/{id}/catalog", h.catalog)
	// 新建组织节点，没有上级就挂在工厂。
	mux.HandleFunc("POST /v1/factories/{id}/org-units", h.createOrgUnit)
	// 停用组织，还有有效下级则拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/org-units/{unitId}/disable", h.disableOrgUnit)
	// 重新启用已经停用的组织节点。
	mux.HandleFunc("POST /v1/factories/{id}/org-units/{unitId}/enable", h.enableOrgUnit)
	// 删除没有下级、也没有人归在这里的组织节点。
	mux.HandleFunc("DELETE /v1/factories/{id}/org-units/{unitId}", h.deleteOrgUnit)
	// 改组织节点的显示名。
	mux.HandleFunc("POST /v1/factories/{id}/org-units/{unitId}/rename", h.renameOrgUnit)
	// 把组织节点改挂到另一个上级，空表示直挂工厂。
	mux.HandleFunc("POST /v1/factories/{id}/org-units/{unitId}/move", h.moveOrgUnit)
	// 超管新建账号，默认口令直接有效。
	mux.HandleFunc("POST /v1/factories/{id}/people", h.createPerson)
	// 超管查看示教器现场，不含口令和令牌。
	mux.HandleFunc("GET /v1/factories/{id}/people/{personId}/logins", h.listPersonLogins)
	// 停用人员，最后一名有效超管不能停。
	mux.HandleFunc("POST /v1/factories/{id}/people/{personId}/disable", h.disablePerson)
	// 恢复已停用人员，使其可以再登录。
	mux.HandleFunc("POST /v1/factories/{id}/people/{personId}/enable", h.enablePerson)
	// 把口令打回默认，旧会话立刻作废。
	mux.HandleFunc("POST /v1/factories/{id}/people/{personId}/reset-password", h.resetPersonPassword)
	// 设置这个人退出后是否保留本机库。
	mux.HandleFunc("POST /v1/factories/{id}/people/{personId}/keep-pouch", h.setKeepPouch)
	// 设置这个人的所属组织，空表示厂直属。
	mux.HandleFunc("POST /v1/factories/{id}/people/{personId}/org", h.setPersonOrg)
	// 超管改别人的显示名。
	mux.HandleFunc("POST /v1/factories/{id}/people/{personId}/display-name", h.setPersonDisplayName)
	// 授予固定角色，都覆盖整厂。
	mux.HandleFunc("POST /v1/factories/{id}/grants", h.grantRole)
	// 收回角色，最后一名厂级超管不能收。
	mux.HandleFunc("POST /v1/factories/{id}/grants/{grantId}/revoke", h.revokeRole)
}

// 新组织的名称，上级可以不填。
type createUnitReq struct {
	Name     string  `json:"name"`     // 节点显示名
	ParentID *string `json:"parentId"` // 空表示直挂工厂
}

// 新账号的登录名和给人看的名字。
type createPersonReq struct {
	LoginName   string `json:"loginName"`   // 本厂登录名
	DisplayName string `json:"displayName"` // 显示名
}

// 退出之后是否保留示教器上的库。
type keepPouchReq struct {
	KeepPouch bool `json:"keepPouch"` // 退出后是否留下这个人的库文件
}

// 新建账号的结果，里面没有口令原文。
type createdPersonResp struct {
	Account service.Account `json:"account"` // 新建/重置后的账号视图，不含密码
}

// 要授予的角色和是哪一个人。
type grantReq struct {
	PersonID string `json:"personId"` // 被授权人员
	Role     string `json:"role"`     // 三种固定角色之一
}

// 人员的所属组织，空表示厂直属。
type personOrgReq struct {
	OrgUnitID *string `json:"orgUnitId"` // 本厂有效节点；空表示厂直属
}

// 组织节点的新名字。
type renameUnitReq struct {
	Name string `json:"name"` // 新显示名
}

// 组织节点的新上级。
type moveUnitReq struct {
	ParentID *string `json:"parentId"` // 空表示直挂工厂
}

// 超管看全厂名册，其他人只看自己。
func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	// 超管看全厂名册，其他人只看自己。
	h.withFactory(w, r, func(svc *service.Service) {
		// 读取名册，超管见全厂，其他人只见自己。
		cat, err := svc.Org.Catalog(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, cat)
	})
}

// 挂本厂组织节点；无父则直挂工厂。
func (h *Handler) createOrgUnit(w http.ResponseWriter, r *http.Request) {
	// 挂本厂组织节点；无父则直挂工厂。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接组织名称和可选上级。
		var req createUnitReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 解析可选上级，空着就表示挂在工厂下。
		parentID, err := parseOptUUID(req.ParentID)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 新建组织节点，没有上级就挂在工厂。
		row, err := svc.Org.CreateOrgUnit(r.Context(), bearer(r), req.Name, parentID)
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

// 停用组织节点；有有效子节点则拒绝。
func (h *Handler) disableOrgUnit(w http.ResponseWriter, r *http.Request) {
	// 停用组织节点；有有效子节点则拒绝。
	h.withFactory(w, r, func(svc *service.Service) {
		// 组织编号必须合法，否则拒绝这次办理。
		unitID, err := uuid.Parse(r.PathValue("unitId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Org.DisableOrgUnit(r.Context(), bearer(r), unitID); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 办理成功，且没有正文需要返回。
		w.WriteHeader(http.StatusNoContent)
	})
}

// 重新启用组织节点。
func (h *Handler) enableOrgUnit(w http.ResponseWriter, r *http.Request) {
	// 重新启用组织节点，使其可以再分配。
	h.withFactory(w, r, func(svc *service.Service) {
		// 组织编号必须合法，否则拒绝这次办理。
		unitID, err := uuid.Parse(r.PathValue("unitId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Org.EnableOrgUnit(r.Context(), bearer(r), unitID); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 办理成功，且没有正文需要返回。
		w.WriteHeader(http.StatusNoContent)
	})
}

// 无引用才删组织节点。
func (h *Handler) deleteOrgUnit(w http.ResponseWriter, r *http.Request) {
	// 无引用才删组织节点。
	h.withFactory(w, r, func(svc *service.Service) {
		// 组织编号必须合法，否则拒绝这次办理。
		unitID, err := uuid.Parse(r.PathValue("unitId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Org.DeleteOrgUnit(r.Context(), bearer(r), unitID); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 办理成功，且没有正文需要返回。
		w.WriteHeader(http.StatusNoContent)
	})
}

// 超管建本厂账号，默认密码直接有效。
func (h *Handler) createPerson(w http.ResponseWriter, r *http.Request) {
	// 超管建本厂账号，默认密码直接有效。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接新账号的登录名和显示名。
		var req createPersonReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 超管新建账号，默认口令立即可用。
		acc, err := svc.Org.CreatePerson(r.Context(), bearer(r), req.LoginName, req.DisplayName)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 创建成功，把新记录回给调用方。
		writeJSON(w, http.StatusCreated, createdPersonResp{Account: acc})
	})
}

// 超管看这个人的示教器登录现场；不含密码和令牌。
func (h *Handler) listPersonLogins(w http.ResponseWriter, r *http.Request) {
	// 超管看这个人的示教器登录现场；不含密码和令牌。
	h.withFactory(w, r, func(svc *service.Service) {
		// 人员编号必须合法，否则拒绝这次办理。
		personID, err := uuid.Parse(r.PathValue("personId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 查看示教器现场，不返回口令和令牌。
		rows, err := svc.Org.ListPersonLogins(r.Context(), bearer(r), personID)
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

// 停用人员；最后一名有效超管不可停。
func (h *Handler) disablePerson(w http.ResponseWriter, r *http.Request) {
	// 停用人员；最后一名有效超管不可停。
	h.withFactory(w, r, func(svc *service.Service) {
		// 人员编号必须合法，否则拒绝这次办理。
		personID, err := uuid.Parse(r.PathValue("personId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Auth.DisableAccount(r.Context(), bearer(r), personID); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 办理成功，且没有正文需要返回。
		w.WriteHeader(http.StatusNoContent)
	})
}

// 恢复已停用人员。
func (h *Handler) enablePerson(w http.ResponseWriter, r *http.Request) {
	// 恢复已停用人员，使其可以再登录。
	h.withFactory(w, r, func(svc *service.Service) {
		// 人员编号必须合法，否则拒绝这次办理。
		personID, err := uuid.Parse(r.PathValue("personId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Auth.EnableAccount(r.Context(), bearer(r), personID); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 办理成功，且没有正文需要返回。
		w.WriteHeader(http.StatusNoContent)
	})
}

// 超管按人设置退出后是否保留示教器库文件。
func (h *Handler) setKeepPouch(w http.ResponseWriter, r *http.Request) {
	// 超管按人设置退出后是否保留示教器库文件。
	h.withFactory(w, r, func(svc *service.Service) {
		// 人员编号必须合法，否则拒绝这次办理。
		personID, err := uuid.Parse(r.PathValue("personId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 承接退出后是否保留本机库。
		var req keepPouchReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 设置退出后是否保留示教器上的库。
		acc, err := svc.Org.SetKeepPouch(r.Context(), bearer(r), personID, req.KeepPouch)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, createdPersonResp{Account: acc})
	})
}

// 把他人密码改回默认，旧会话立刻作废。
func (h *Handler) resetPersonPassword(w http.ResponseWriter, r *http.Request) {
	// 把他人密码改回默认，旧会话立刻作废。
	h.withFactory(w, r, func(svc *service.Service) {
		// 人员编号必须合法，否则拒绝这次办理。
		personID, err := uuid.Parse(r.PathValue("personId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 把他人口令打回默认，旧会话立刻作废。
		acc, err := svc.Auth.ResetPassword(r.Context(), bearer(r), personID)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, createdPersonResp{Account: acc})
	})
}

// 在作用域内授固定角色。
func (h *Handler) grantRole(w http.ResponseWriter, r *http.Request) {
	// 授固定角色。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接角色和人员。
		var req grantReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 编号必须合法，否则拒绝以免办错对象。
		personID, err := uuid.Parse(req.PersonID)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 授予固定角色，重复则拒绝。
		row, err := svc.Org.GrantRole(r.Context(), bearer(r), personID, req.Role)
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

// 收回角色；最后一名厂级超管不能收。
func (h *Handler) revokeRole(w http.ResponseWriter, r *http.Request) {
	// 收回角色；最后一名厂级超管不能收。
	h.withFactory(w, r, func(svc *service.Service) {
		// 授权编号必须合法，否则拒绝这次办理。
		grantID, err := uuid.Parse(r.PathValue("grantId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Org.RevokeRole(r.Context(), bearer(r), grantID); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 办理成功，且没有正文需要返回。
		w.WriteHeader(http.StatusNoContent)
	})
}

// 设置人员的所属组织。
func (h *Handler) setPersonOrg(w http.ResponseWriter, r *http.Request) {
	// 超管设置人员的所属组织。
	h.withFactory(w, r, func(svc *service.Service) {
		// 人员编号必须合法，否则拒绝这次办理。
		personID, err := uuid.Parse(r.PathValue("personId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 承接所属组织。
		var req personOrgReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 解析可选节点，空着就表示厂直属。
		unitID, err := parseOptUUID(req.OrgUnitID)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 改所属组织。
		acc, err := svc.Org.SetPersonOrg(r.Context(), bearer(r), personID, unitID)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, createdPersonResp{Account: acc})
	})
}

// 超管改别人的显示名。
func (h *Handler) setPersonDisplayName(w http.ResponseWriter, r *http.Request) {
	// 超管改别人的显示名。
	h.withFactory(w, r, func(svc *service.Service) {
		// 人员编号必须合法，否则拒绝这次办理。
		personID, err := uuid.Parse(r.PathValue("personId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 承接新的显示名。
		var req displayNameReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 改显示名。
		acc, err := svc.Org.SetPersonDisplayName(r.Context(), bearer(r), personID, req.DisplayName)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, createdPersonResp{Account: acc})
	})
}

// 改组织节点显示名。
func (h *Handler) renameOrgUnit(w http.ResponseWriter, r *http.Request) {
	// 改组织节点显示名。
	h.withFactory(w, r, func(svc *service.Service) {
		// 组织编号必须合法，否则拒绝这次办理。
		unitID, err := uuid.Parse(r.PathValue("unitId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 承接新名字。
		var req renameUnitReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Org.RenameOrgUnit(r.Context(), bearer(r), unitID, req.Name); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 办理成功，且没有正文需要返回。
		w.WriteHeader(http.StatusNoContent)
	})
}

// 把组织节点改挂到另一个上级。
func (h *Handler) moveOrgUnit(w http.ResponseWriter, r *http.Request) {
	// 把组织节点改挂到另一个上级。
	h.withFactory(w, r, func(svc *service.Service) {
		// 组织编号必须合法，否则拒绝这次办理。
		unitID, err := uuid.Parse(r.PathValue("unitId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 承接新上级。
		var req moveUnitReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 解析可选上级，空着就表示挂在工厂下。
		parentID, err := parseOptUUID(req.ParentID)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Org.ReparentOrgUnit(r.Context(), bearer(r), unitID, parentID); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 办理成功，且没有正文需要返回。
		w.WriteHeader(http.StatusNoContent)
	})
}
