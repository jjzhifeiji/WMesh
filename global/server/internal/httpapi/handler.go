// Package httpapi 把 WAN 应用服务适配成 JSON HTTP。不绕过 Service，不把 SQL 原文抛给前端。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

// Handler 把会话头和应用服务接到路由上。
type Handler struct {
	svc *service.Service
}

// New 组装 WAN HTTP 适配器。
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// Router 只暴露名录、建厂和明确拒绝的代管入口。
func (h *Handler) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/login", h.login)
	mux.HandleFunc("POST /v1/logout", h.logout)
	mux.HandleFunc("GET /v1/me", h.me)
	mux.HandleFunc("GET /v1/directory", h.directory)
	mux.HandleFunc("POST /v1/factories", h.createFactory)
	mux.HandleFunc("POST /v1/invite-wan-admin", h.inviteWANAdmin)
	mux.HandleFunc("POST /v1/factories/{id}/people", h.createFactoryPerson)
	mux.HandleFunc("POST /v1/factories/{id}/orgs", h.createFactoryOrg)
	mux.HandleFunc("POST /v1/factories/{id}/roles", h.grantFactoryRole)
	mux.HandleFunc("GET /v1/factories/{id}/people", h.listFactoryPeople)
	return mux
}

type loginReq struct {
	LoginName string `json:"loginName"` // WAN 管理员登录名
	Password  string `json:"password"`  // 日常口令，不进审计
}

type tokenResp struct {
	Token string `json:"token"` // 会话令牌原文，只回给调用方
}

type meResp struct {
	ID        string `json:"id"`        // WAN 管理员稳定身份
	LoginName string `json:"loginName"` // 登录名
}

type createFactoryReq struct {
	Name      string `json:"name"`      // 工厂显示名
	SALogin   string `json:"saLogin"`   // 初始超管登录名
	SADisplay string `json:"saDisplay"` // 初始超管显示名
}

type inviteReq struct {
	LoginName string `json:"loginName"` // 被邀请登录名；此接口一律拒绝
}

type factoryPersonReq struct {
	LoginName string `json:"loginName"` // 厂内登录名；WAN 不得代建
}

type factoryOrgReq struct {
	Name string `json:"name"` // 组织名；WAN 不得代建
}

type factoryRoleReq struct {
	Target string `json:"target"` // 授权目标；WAN 不得代授
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	token, err := h.svc.Login(r.Context(), req.LoginName, req.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokenResp{Token: token})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Logout(r.Context(), bearer(r)); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	admin, err := h.svc.RequireAdmin(r.Context(), bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meResp{ID: admin.ID.String(), LoginName: admin.LoginName})
}

func (h *Handler) directory(w http.ResponseWriter, r *http.Request) {
	dir, err := h.svc.Directory(r.Context(), bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dir)
}

func (h *Handler) createFactory(w http.ResponseWriter, r *http.Request) {
	var req createFactoryReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	out, err := h.svc.CreateFactory(r.Context(), bearer(r), req.Name, req.SALogin, req.SADisplay)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *Handler) inviteWANAdmin(w http.ResponseWriter, r *http.Request) {
	var req inviteReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.InviteWANAdmin(r.Context(), bearer(r), req.LoginName))
}

func (h *Handler) createFactoryPerson(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	var req factoryPersonReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.CreateFactoryPerson(r.Context(), bearer(r), id, req.LoginName))
}

func (h *Handler) createFactoryOrg(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	var req factoryOrgReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.CreateFactoryOrg(r.Context(), bearer(r), id, req.Name))
}

func (h *Handler) grantFactoryRole(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	var req factoryRoleReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.GrantFactoryRole(r.Context(), bearer(r), id, req.Target))
}

func (h *Handler) listFactoryPeople(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.ListFactoryPeople(r.Context(), bearer(r), id))
}

func parsePathID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, errInvalidID
	}
	return id, nil
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
