package httpapi

import (
	"net/http"
)

func (h *Handler) mountAuth(mux *http.ServeMux) { // WAN 管理员登录、会话与改密码
	mux.HandleFunc("POST /v1/login", h.login)
	mux.HandleFunc("POST /v1/logout", h.logout)
	mux.HandleFunc("GET /v1/me", h.me)
	mux.HandleFunc("POST /v1/me/password", h.changePassword)
}

type loginReq struct {
	LoginName string `json:"loginName"` // WAN 管理员登录名
	Password  string `json:"password"`  // 日常密码，不进审计
}

type tokenResp struct {
	Token string `json:"token"` // 会话令牌原文，只回给调用方
}

type meResp struct {
	ID        string `json:"id"`        // WAN 管理员稳定身份
	LoginName string `json:"loginName"` // 登录名
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	token, err := h.svc.Auth.Login(r.Context(), req.LoginName, req.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokenResp{Token: token})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Auth.Logout(r.Context(), bearer(r)); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	admin, err := h.svc.Auth.RequireAdmin(r.Context(), bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meResp{ID: admin.ID.String(), LoginName: admin.LoginName})
}

type passwordReq struct {
	Password string `json:"password"` // 新日常密码，不进审计
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var req passwordReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	if err := h.svc.Auth.ChangePassword(r.Context(), bearer(r), req.Password); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
