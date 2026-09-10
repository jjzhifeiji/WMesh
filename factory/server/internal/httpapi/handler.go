// Package httpapi 把厂内应用服务适配成 JSON HTTP。不绕过 Service，不把 SQL 原文抛给前端。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/service"
)

// Handler 按 URL 里的工厂身份选库，并把会话头交给应用服务。
type Handler struct {
	Hub            *hub.Hub
	BootstrapToken string // 建厂引导共享口令，只用于 /internal/bootstrap
}

// New 组装厂内 HTTP 适配器。
func New(h *hub.Hub, bootstrapToken string) *Handler {
	return &Handler{Hub: h, BootstrapToken: bootstrapToken}
}

// Router 暴露建厂引导和厂内账号组织接口。
func (h *Handler) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/bootstrap", h.bootstrap)
	mux.HandleFunc("POST /v1/factories/{id}/login", h.login)
	mux.HandleFunc("POST /v1/factories/{id}/activate", h.activate)
	mux.HandleFunc("POST /v1/factories/{id}/logout", h.logout)
	mux.HandleFunc("GET /v1/factories/{id}/me", h.me)
	mux.HandleFunc("POST /v1/factories/{id}/me/password", h.changePassword)
	mux.HandleFunc("GET /v1/factories/{id}/catalog", h.catalog)
	mux.HandleFunc("POST /v1/factories/{id}/org-types", h.createOrgType)
	mux.HandleFunc("POST /v1/factories/{id}/org-types/{typeId}/disable", h.disableOrgType)
	mux.HandleFunc("POST /v1/factories/{id}/org-units", h.createOrgUnit)
	mux.HandleFunc("POST /v1/factories/{id}/org-units/{unitId}/disable", h.disableOrgUnit)
	mux.HandleFunc("POST /v1/factories/{id}/people", h.createPerson)
	mux.HandleFunc("POST /v1/factories/{id}/people/{personId}/disable", h.disablePerson)
	mux.HandleFunc("POST /v1/factories/{id}/grants", h.grantRole)
	mux.HandleFunc("POST /v1/factories/{id}/grants/{grantId}/revoke", h.revokeRole)
	mux.HandleFunc("POST /v1/factories/{id}/assignments", h.assign)
	mux.HandleFunc("POST /v1/factories/{id}/assignments/end", h.unassign)
	return mux
}

type bootReq struct {
	FactoryID string `json:"factoryId"` // 工厂稳定身份
	SALogin   string `json:"saLogin"`   // 初始超管登录名
	SADisplay string `json:"saDisplay"` // 初始超管显示名
}

type bootResp struct {
	PersonID        string `json:"personId"`        // 厂库账号身份
	ActivationToken string `json:"activationToken"` // 一次性激活口令，禁止写入审计
}

type loginReq struct {
	LoginName string `json:"loginName"` // 本厂登录名
	Password  string `json:"password"`  // 日常口令，不进审计
}

type tokenResp struct {
	Token string `json:"token"` // 会话令牌原文，只回给调用方
}

type activateReq struct {
	LoginName       string `json:"loginName"`       // 待启用登录名
	ActivationToken string `json:"activationToken"` // 一次性激活口令
	Password        string `json:"password"`        // 持有者自设日常口令
}

type passwordReq struct {
	Password string `json:"password"` // 新日常口令，不进审计
}

type nameReq struct {
	Name string `json:"name"` // 组织类型或节点显示名
}

type createUnitReq struct {
	TypeID   string  `json:"typeId"`   // 本厂组织类型
	Name     string  `json:"name"`     // 节点显示名
	ParentID *string `json:"parentId"` // 空表示直挂工厂
}

type createPersonReq struct {
	LoginName   string `json:"loginName"`   // 本厂登录名
	DisplayName string `json:"displayName"` // 显示名
}

type createdPersonResp struct {
	Account         service.Account `json:"account"`
	ActivationToken string          `json:"activationToken"` // 一次性，只给交付方
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

func (h *Handler) bootstrap(w http.ResponseWriter, r *http.Request) {
	if bearer(r) != h.BootstrapToken || h.BootstrapToken == "" {
		writeErr(w, domain.ErrUnauthorized)
		return
	}
	var req bootReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	fid, err := uuid.Parse(req.FactoryID)
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	personID, token, err := h.Hub.Bootstrap(r.Context(), fid, req.SALogin, req.SADisplay)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, bootResp{PersonID: personID.String(), ActivationToken: token})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req loginReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		token, err := svc.Login(r.Context(), req.LoginName, req.Password)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, tokenResp{Token: token})
	})
}

func (h *Handler) activate(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req activateReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		if err := svc.Activate(r.Context(), req.LoginName, req.ActivationToken, req.Password); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		if err := svc.Logout(r.Context(), bearer(r)); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		acc, err := svc.RequireActive(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, acc)
	})
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req passwordReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		if err := svc.ChangePassword(r.Context(), bearer(r), req.Password); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		cat, err := svc.Catalog(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, cat)
	})
}

func (h *Handler) createOrgType(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req nameReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.CreateOrgType(r.Context(), bearer(r), req.Name)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, row)
	})
}

func (h *Handler) disableOrgType(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		typeID, err := uuid.Parse(r.PathValue("typeId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		if err := svc.DisableOrgType(r.Context(), bearer(r), typeID); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) createOrgUnit(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req createUnitReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		typeID, err := uuid.Parse(req.TypeID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		parentID, err := parseOptUUID(req.ParentID)
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.CreateOrgUnit(r.Context(), bearer(r), typeID, req.Name, parentID)
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
		if err := svc.DisableOrgUnit(r.Context(), bearer(r), unitID); err != nil {
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
		acc, token, err := svc.CreatePerson(r.Context(), bearer(r), req.LoginName, req.DisplayName)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, createdPersonResp{Account: acc, ActivationToken: token})
	})
}

func (h *Handler) disablePerson(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		personID, err := uuid.Parse(r.PathValue("personId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		if err := svc.DisableAccount(r.Context(), bearer(r), personID); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
		row, err := svc.GrantRole(r.Context(), bearer(r), personID, req.Role, req.ScopeKind, orgID)
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
		if err := svc.RevokeRole(r.Context(), bearer(r), grantID); err != nil {
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
		if err := svc.Assign(r.Context(), bearer(r), personID, unitID); err != nil {
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
		if err := svc.Unassign(r.Context(), bearer(r), personID, unitID); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) withFactory(w http.ResponseWriter, r *http.Request, fn func(*service.Service)) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	svc, err := h.Hub.Service(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	fn(svc)
}

func parseOptUUID(p *string) (*uuid.UUID, error) {
	if p == nil || *p == "" {
		return nil, nil
	}
	id, err := uuid.Parse(*p)
	if err != nil {
		return nil, errInvalidID
	}
	return &id, nil
}

var errInvalidID = errors.New("invalid id")

type errorBody struct {
	Error string `json:"error"` // 英文业务错误，前端再译
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeBadRequest(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
}

func writeErr(w http.ResponseWriter, err error) {
	code := statusOf(err)
	msg := err.Error()
	if code == http.StatusInternalServerError {
		msg = "internal error"
	}
	writeJSON(w, code, errorBody{Error: msg})
}

func statusOf(err error) int {
	switch {
	case errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrInvalidCredentials),
		errors.Is(err, domain.ErrAccountPending), errors.Is(err, domain.ErrAccountDisabled),
		errors.Is(err, domain.ErrInvalidActivation), errors.Is(err, domain.ErrSessionExpired):
		return http.StatusUnauthorized
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrLastAdmin),
		errors.Is(err, domain.ErrInitialSAExists):
		return http.StatusForbidden
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrWANAdminExists), errors.Is(err, domain.ErrLoginNameTaken),
		errors.Is(err, domain.ErrAlreadyActivated), errors.Is(err, domain.ErrDuplicateAssignment),
		errors.Is(err, domain.ErrDuplicateRoleGrant), errors.Is(err, domain.ErrDuplicateSession):
		return http.StatusConflict
	case isDomain(err), errors.Is(err, errInvalidID):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func isDomain(err error) bool {
	for _, t := range []error{
		domain.ErrCycle, domain.ErrWorkContext, domain.ErrMultiParent, domain.ErrInvalidRoleScope,
		domain.ErrDisabledOrgType, domain.ErrDisabledOrgUnit, domain.ErrHasActiveUnits, domain.ErrHasActiveChildren,
	} {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}
