package httpapi

import (
	"net/http"

	"wmesh/global/internal/platform/domain"
)

// 挂现场申请和云端审批入口。
func (h *Handler) mountOnboarding(mux *http.ServeMux) {
	// 现场申请接入新厂，匿名可调。
	mux.HandleFunc("POST /v1/channel/onboarding", h.applyOnboarding)
	// 现场凭申请密钥查进度。
	mux.HandleFunc("POST /v1/channel/onboarding/status", h.onboardingStatus)
	// 已认领的厂申请找回初始超管密码，要厂钥签名。
	mux.HandleFunc("POST /v1/channel/sa-reset", h.requestSAReset)
	// 厂端设新密码前用掉找回单，要厂钥签名。
	mux.HandleFunc("POST /v1/channel/sa-reset/consume", h.consumeSAReset)
	// 云端待接入列表。
	mux.HandleFunc("GET /v1/onboarding", h.pendingOnboarding)
	// 批准一条申请。
	mux.HandleFunc("POST /v1/onboarding/{id}/approve", h.approveOnboarding)
	// 拒绝一条申请。
	mux.HandleFunc("POST /v1/onboarding/{id}/reject", h.rejectOnboarding)
}

// 现场申请接入时填的厂名和初始超管，不含密码。
type applyOnboardingReq struct {
	FactoryName        string `json:"factoryName"`        // 工厂显示名
	SALogin            string `json:"saLogin"`            // 初始超管登录名
	SADisplay          string `json:"saDisplay"`          // 初始超管显示名
	ServiceVersionName string `json:"serviceVersionName"` // 厂端服务版本名
}

// 只带申请密钥的请求。
type onboardingSecretReq struct {
	Secret string `json:"secret"` // 申请密钥原文
}

// 申请找回时厂端报的版本。
type saResetReq struct {
	ServiceVersionName string `json:"serviceVersionName"` // 厂端服务版本名
}

// 批准时管理员可修正厂名。
type approveOnboardingReq struct {
	Name string `json:"name"` // 修正后的厂名；空则用现场填的
}

// 现场申请接入新厂。
func (h *Handler) applyOnboarding(w http.ResponseWriter, r *http.Request) {
	// 匿名可调，同一来源超频先挡。
	if !h.anonGate.allow(r) {
		// 回请求过多，不读正文也不写申请。
		writeErr(w, domain.ErrTooManyRequests)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req applyOnboardingReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 写入待批准申请，来源地址给管理员核对。
	out, err := h.svc.Channel.ApplyOnboarding(r.Context(), req.FactoryName, req.SALogin, req.SADisplay, req.ServiceVersionName, peerKey(r.RemoteAddr))
	// 没成功就按业务错误回写。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 创建成功，密钥原文只回这一次。
	writeJSON(w, http.StatusCreated, out)
}

// 现场凭申请密钥查进度。
func (h *Handler) onboardingStatus(w http.ResponseWriter, r *http.Request) {
	// 等待页会反复轮询，用单独的宽松闸。
	if !h.pollGate.allow(r) {
		// 回请求过多，页面稍后再试。
		writeErr(w, domain.ErrTooManyRequests)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req onboardingSecretReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 按密钥查进度，失效的只回 expired。
	out, err := h.svc.Channel.OnboardingStatus(r.Context(), req.Secret)
	// 没成功就按业务错误回写。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把进度交回现场。
	writeJSON(w, http.StatusOK, out)
}

// 已认领的厂申请找回初始超管密码。
func (h *Handler) requestSAReset(w http.ResponseWriter, r *http.Request) {
	// 同一来源超频先挡。
	if !h.anonGate.allow(r) {
		// 回请求过多，不验签也不写申请。
		writeErr(w, domain.ErrTooManyRequests)
		return
	}
	// 用厂钥签名钉死是哪家厂，外部不能替别的厂发起。
	fid, err := h.factoryProof(r)
	// 签名不对就拒绝。
	if err != nil {
		// 按业务错误回写。
		writeErr(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req saResetReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 写入待批准的找回单。
	out, err := h.svc.Channel.RequestSAReset(r.Context(), fid, req.ServiceVersionName, peerKey(r.RemoteAddr))
	// 没成功就按业务错误回写。
	if err != nil {
		// 按业务错误回写。
		writeErr(w, err)
		return
	}
	// 创建成功，密钥原文只回这一次。
	writeJSON(w, http.StatusCreated, out)
}

// 厂端设新密码前用掉找回单。
func (h *Handler) consumeSAReset(w http.ResponseWriter, r *http.Request) {
	// 用厂钥签名钉死是哪家厂。
	fid, err := h.factoryProof(r)
	// 签名不对就拒绝。
	if err != nil {
		// 按业务错误回写。
		writeErr(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req onboardingSecretReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 用掉找回单，只成功一次。
	if err := h.svc.Channel.ConsumeSAReset(r.Context(), fid, req.Secret); err != nil {
		// 按业务错误回写。
		writeErr(w, err)
		return
	}
	// 成功无正文。
	w.WriteHeader(http.StatusNoContent)
}

// 云端待接入列表。
func (h *Handler) pendingOnboarding(w http.ResponseWriter, r *http.Request) {
	// 只有 WAN 管理员能读。
	out, err := h.svc.Factories.PendingOnboarding(r.Context(), bearer(r))
	// 没成功就按业务错误回写。
	if err != nil {
		// 按业务错误回写。
		writeErr(w, err)
		return
	}
	// 把列表交回页面。
	writeJSON(w, http.StatusOK, out)
}

// 批准一条申请。
func (h *Handler) approveOnboarding(w http.ResponseWriter, r *http.Request) {
	// 从路径取出申请身份。
	id, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req approveOnboardingReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 批准申请，新厂此时才写入名录。
	out, err := h.svc.Factories.ApproveOnboarding(r.Context(), bearer(r), id, req.Name)
	// 没成功就按业务错误回写。
	if err != nil {
		// 按业务错误回写。
		writeErr(w, err)
		return
	}
	// 把结果交回页面。
	writeJSON(w, http.StatusOK, out)
}

// 拒绝一条申请。
func (h *Handler) rejectOnboarding(w http.ResponseWriter, r *http.Request) {
	// 从路径取出申请身份。
	id, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 拒绝申请，现场可以修改后重新申请。
	out, err := h.svc.Factories.RejectOnboarding(r.Context(), bearer(r), id)
	// 没成功就按业务错误回写。
	if err != nil {
		// 按业务错误回写。
		writeErr(w, err)
		return
	}
	// 把结果交回页面。
	writeJSON(w, http.StatusOK, out)
}
