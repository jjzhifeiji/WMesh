package httpapi

import (
	"net/http"

	"wmesh/factory/internal/service"
)

func (h *Handler) mountAuth(mux *http.ServeMux) { // 本厂登录、激活、会话
	mux.HandleFunc("POST /v1/factories/{id}/login", h.login)
	mux.HandleFunc("POST /v1/factories/{id}/activate", h.activate)
	mux.HandleFunc("POST /v1/factories/{id}/logout", h.logout)
	mux.HandleFunc("GET /v1/factories/{id}/me", h.me)
	mux.HandleFunc("POST /v1/factories/{id}/me/password", h.changePassword)
}

type loginReq struct {
	LoginName string `json:"loginName"` // 本厂登录名
	Password  string `json:"password"`  // 日常密码，不进审计
}

type tokenResp struct {
	Token string `json:"token"` // 会话令牌原文，只回给调用方
}

type activateReq struct {
	LoginName       string `json:"loginName"`       // 待启用登录名
	ActivationToken string `json:"activationToken"` // 一次性 8 位激活码
	Password        string `json:"password"`        // 持有者自设日常密码
}

type passwordReq struct {
	Password string `json:"password"` // 新日常密码，不进审计
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req loginReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		token, err := svc.Auth.Login(r.Context(), req.LoginName, req.Password)
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
		if err := svc.Auth.Activate(r.Context(), req.LoginName, req.ActivationToken, req.Password); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		if err := svc.Auth.Logout(r.Context(), bearer(r)); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		acc, err := svc.Auth.RequireActive(r.Context(), bearer(r))
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
		if err := svc.Auth.ChangePassword(r.Context(), bearer(r), req.Password); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
