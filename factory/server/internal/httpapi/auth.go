package httpapi

import (
	"net/http"

	"wmesh/factory/internal/service"
)

// 本厂登录、会话、本人资料。
func (h *Handler) mountAuth(mux *http.ServeMux) {
	// 校验口令并建立会话，失败不记口令。
	mux.HandleFunc("POST /v1/factories/{id}/login", h.login)
	// 结束当前会话，令牌无效则拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/logout", h.logout)
	// 返回当前登录人，未登录则拒绝。
	mux.HandleFunc("GET /v1/factories/{id}/me", h.me)
	// 只修改自己的口令，未登录则拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/me/password", h.changePassword)
	// 只修改自己的显示名，登录名不变。
	mux.HandleFunc("POST /v1/factories/{id}/me/display-name", h.setMyDisplayName)
}

// 新的显示名。
type displayNameReq struct {
	DisplayName string `json:"displayName"` // 显示名，去首尾空白后 1～64 字
}

// 本人改显示名。
func (h *Handler) setMyDisplayName(w http.ResponseWriter, r *http.Request) {
	// 本人改显示名。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接新的显示名。
		var req displayNameReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 改自己的显示名。
		acc, err := svc.Auth.SetMyDisplayName(r.Context(), bearer(r), req.DisplayName)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把改后的账号回给调用方。
		writeJSON(w, http.StatusOK, acc)
	})
}

// 登录名和口令，口令不能写进日志。
type loginReq struct {
	LoginName string `json:"loginName"` // 本厂登录名
	Password  string `json:"password"`  // 日常密码，不进审计
}

// 登录成功后只把会话令牌回给调用方。
type tokenResp struct {
	Token string `json:"token"` // 会话令牌原文，只回给调用方
}

// 当前口令和新的日常口令，都不能写进审计。
type passwordReq struct {
	CurrentPassword string `json:"currentPassword"` // 当前日常密码；网页必填，示教器暂不带
	Password        string `json:"password"`        // 新日常密码，不进审计
}

// 校验密码开会话。
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	// 校验口令并开会话，失败不记口令。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接登录名和口令，口令不进日志。
		var req loginReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 校验口令并开会话，失败不把口令记下。
		token, err := svc.Auth.Login(r.Context(), req.LoginName, req.Password)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, tokenResp{Token: token})
	})
}

// 结束会话。
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	// 结束当前会话，令牌无效则拒绝。
	h.withFactory(w, r, func(svc *service.Service) {
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Auth.Logout(r.Context(), bearer(r)); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 办理成功，且没有正文需要返回。
		w.WriteHeader(http.StatusNoContent)
	})
}

// 返回当前登录人。
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	// 返回当前登录人，未登录则拒绝。
	h.withFactory(w, r, func(svc *service.Service) {
		// 先确认会话仍有效，过期或停用则拒绝。
		acc, err := svc.Auth.RequireActive(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, acc)
	})
}

// 改自己的日常密码。
func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	// 只修改自己的口令，未登录则拒绝。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接当前口令和新口令，都不能写进审计。
		var req passwordReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Auth.ChangePassword(r.Context(), bearer(r), req.CurrentPassword, req.Password); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 办理成功，且没有正文需要返回。
		w.WriteHeader(http.StatusNoContent)
	})
}
