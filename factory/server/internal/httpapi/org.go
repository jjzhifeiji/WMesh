package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

func (h *Handler) mountOrg(mux *http.ServeMux) { // 名册、组织、人员、角色、分配
	mux.HandleFunc("GET /v1/factories/{id}/catalog", h.catalog)
	mux.HandleFunc("POST /v1/factories/{id}/org-units", h.createOrgUnit)
	mux.HandleFunc("POST /v1/factories/{id}/org-units/{unitId}/disable", h.disableOrgUnit)
	mux.HandleFunc("POST /v1/factories/{id}/org-units/{unitId}/enable", h.enableOrgUnit)
	mux.HandleFunc("DELETE /v1/factories/{id}/org-units/{unitId}", h.deleteOrgUnit)
	mux.HandleFunc("POST /v1/factories/{id}/people", h.createPerson)
	mux.HandleFunc("POST /v1/factories/{id}/people/{personId}/disable", h.disablePerson)
	mux.HandleFunc("POST /v1/factories/{id}/people/{personId}/enable", h.enablePerson)
	mux.HandleFunc("POST /v1/factories/{id}/people/{personId}/reset-password", h.resetPersonPassword)
	mux.HandleFunc("POST /v1/factories/{id}/grants", h.grantRole)
	mux.HandleFunc("POST /v1/factories/{id}/grants/{grantId}/revoke", h.revokeRole)
	mux.HandleFunc("POST /v1/factories/{id}/assignments", h.assign)
	mux.HandleFunc("POST /v1/factories/{id}/assignments/end", h.unassign)
}

type createUnitReq struct {
	Name     string  `json:"name"`     // 节点显示名
	ParentID *string `json:"parentId"` // 空表示直挂工厂
}

type createPersonReq struct {
	LoginName   string `json:"loginName"`   // 本厂登录名
	DisplayName string `json:"displayName"` // 显示名
}

type createdPersonResp struct {
	Account service.Account `json:"account"` // 新建/重置后的账号视图，不含密码
}

type grantReq struct {
	PersonID  string  `json:"personId"`  // 被授权人员
	Role      string  `json:"role"`      // 六种固定角色之一
	ScopeKind string  `json:"scopeKind"` // factory 或 org_unit
	OrgUnitID *string `json:"orgUnitId"` // Factory 作用域必须为空
}

type assignReq struct {
	PersonID  string `json:"personId"`  // 被分配人员
	OrgUnitID string `json:"orgUnitId"` // 本厂有效节点
}

func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		cat, err := svc.Org.Catalog(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, cat)
	})
}

func (h *Handler) createOrgUnit(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req createUnitReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		parentID, err := parseOptUUID(req.ParentID)
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.Org.CreateOrgUnit(r.Context(), bearer(r), req.Name, parentID)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, row)
	})
}

func (h *Handler) disableOrgUnit(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		unitID, err := uuid.Parse(r.PathValue("unitId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		if err := svc.Org.DisableOrgUnit(r.Context(), bearer(r), unitID); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) enableOrgUnit(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		unitID, err := uuid.Parse(r.PathValue("unitId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		if err := svc.Org.EnableOrgUnit(r.Context(), bearer(r), unitID); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) deleteOrgUnit(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		unitID, err := uuid.Parse(r.PathValue("unitId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		if err := svc.Org.DeleteOrgUnit(r.Context(), bearer(r), unitID); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) createPerson(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req createPersonReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		acc, err := svc.Org.CreatePerson(r.Context(), bearer(r), req.LoginName, req.DisplayName)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, createdPersonResp{Account: acc})
	})
}

func (h *Handler) disablePerson(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		personID, err := uuid.Parse(r.PathValue("personId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		if err := svc.Auth.DisableAccount(r.Context(), bearer(r), personID); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) enablePerson(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		personID, err := uuid.Parse(r.PathValue("personId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		if err := svc.Auth.EnableAccount(r.Context(), bearer(r), personID); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) resetPersonPassword(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		personID, err := uuid.Parse(r.PathValue("personId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		acc, err := svc.Auth.ResetPassword(r.Context(), bearer(r), personID)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, createdPersonResp{Account: acc})
	})
}

func (h *Handler) grantRole(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req grantReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		personID, err := uuid.Parse(req.PersonID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		orgID, err := parseOptUUID(req.OrgUnitID)
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.Org.GrantRole(r.Context(), bearer(r), personID, req.Role, req.ScopeKind, orgID)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, row)
	})
}

func (h *Handler) revokeRole(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		grantID, err := uuid.Parse(r.PathValue("grantId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		if err := svc.Org.RevokeRole(r.Context(), bearer(r), grantID); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) assign(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req assignReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		personID, err := uuid.Parse(req.PersonID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		unitID, err := uuid.Parse(req.OrgUnitID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		if err := svc.Org.Assign(r.Context(), bearer(r), personID, unitID); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) unassign(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req assignReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		personID, err := uuid.Parse(req.PersonID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		unitID, err := uuid.Parse(req.OrgUnitID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		if err := svc.Org.Unassign(r.Context(), bearer(r), personID, unitID); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
