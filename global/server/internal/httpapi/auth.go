package httpapi

import (
	"net/http"
)

// 挂登录、登出、当前管理员和改密码。
func (h *Handler) mountAuth(mux *http.ServeMux) {
	// 登录入口，密码不对不发会话。
	mux.HandleFunc("POST /v1/login", h.login)
	// 登出入口，结束当前这次会话。
	mux.HandleFunc("POST /v1/logout", h.logout)
	// 当前管理员入口，未登录拒绝。
	mux.HandleFunc("GET /v1/me", h.me)
	// 改密入口，只改自己的日常密码。
	mux.HandleFunc("POST /v1/me/password", h.changePassword)
}

// 云端管理员登录时提交的账号和密码。
type loginReq struct {
	LoginName string `json:"loginName"` // WAN 管理员登录名
	Password  string `json:"password"`  // 日常密码，不进审计
}

// 登录成功后只回给调用方的会话。
type tokenResp struct {
	Token string `json:"token"` // 会话令牌原文，只回给调用方
}

// 当前云端管理员的身份，未登录没有。
type meResp struct {
	ID        string `json:"id"`        // WAN 管理员稳定身份
	LoginName string `json:"loginName"` // 登录名
}

// 校验密码开会话。
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	// 接住请求体，解析失败再拒绝。
	var req loginReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 校验密码开会话，不对就不发令牌。
	token, err := h.svc.Auth.Login(r.Context(), req.LoginName, req.Password)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, tokenResp{Token: token})
}

// 结束会话。
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	// 结束这次会话，会话无效则拒绝。
	if err := h.svc.Auth.Logout(r.Context(), bearer(r)); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 成功无正文，调用方不要当成失败。
	w.WriteHeader(http.StatusNoContent)
}

// 返回当前管理员。
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	// 确认当前会话是管理员，否则拒绝。
	admin, err := h.svc.Auth.RequireAdmin(r.Context(), bearer(r))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, meResp{ID: admin.ID.String(), LoginName: admin.LoginName})
}

// 管理员改自己的日常密码，不进审计。
type passwordReq struct {
	Password string `json:"password"` // 新日常密码，不进审计
}

// 改自己的日常密码。
func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	// 接住请求体，解析失败再拒绝。
	var req passwordReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 改自己的日常密码，未登录拒绝。
	if err := h.svc.Auth.ChangePassword(r.Context(), bearer(r), req.Password); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 成功无正文，调用方不要当成失败。
	w.WriteHeader(http.StatusNoContent)
}
