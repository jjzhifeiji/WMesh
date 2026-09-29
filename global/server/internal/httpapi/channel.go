package httpapi

import (
	"net/http"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

// 挂厂出站认领 HTTPS，以及厂钥 HTTPS 拉正文。
func (h *Handler) mountChannel(mux *http.ServeMux) {
	// 用建厂码换身份，成功也不消耗码。
	mux.HandleFunc("POST /v1/channel/enroll", h.channelEnroll)
	// 交上厂钥并作废建厂码。
	mux.HandleFunc("POST /v1/channel/claim", h.channelClaim)
	// 回连或进页时补拉当前指令。
	mux.HandleFunc("GET /v1/channel/index", h.channelIndex)
	// 签发或续期内容租约。
	mux.HandleFunc("GET /v1/channel/lease", h.channelLease)
	// 签发或续期内容租约。
	mux.HandleFunc("POST /v1/channel/lease", h.channelLease)
	// 已认领厂按厂钥拉一条密封闭包。
	mux.HandleFunc("GET /v1/channel/pull/closure/{assetId}", h.pullClosure)
	// 已认领厂按厂钥拉一份内容模版。
	mux.HandleFunc("GET /v1/channel/pull/template/{templateId}", h.pullTemplate)
	// 已认领厂按版本拉当前最高包。
	mux.HandleFunc("GET /v1/channel/pull/software", h.pullSoftware)
}

// 用一次性建厂码换待认领身份。
type enrollReq struct {
	EnrollmentCode string `json:"enrollmentCode"` // 一次性建厂码
}

// 待认领的厂和初始超管，成功也不消耗码。
type enrollResp struct {
	FactoryID        string `json:"factoryId"`        // 工厂稳定身份
	Name             string `json:"name"`             // 工厂显示名
	FactoryShortCode string `json:"factoryShortCode"` // 本厂短码
	SAPersonID       string `json:"saPersonId"`       // 约定的初始超管身份
	SALogin          string `json:"saLogin"`          // 初始超管登录名
	SADisplay        string `json:"saDisplay"`        // 初始超管显示名
}

// 交上厂钥并作废建厂码的请求。
type claimReq struct {
	EnrollmentCode   string `json:"enrollmentCode"`   // 一次性建厂码
	FactoryPublicKey []byte `json:"factoryPublicKey"` // 本厂签发公钥
}

// 认领之后的厂身份、治理状态和短码。
type claimResp struct {
	FactoryID        string `json:"factoryId"`        // 工厂稳定身份
	Status           string `json:"status"`           // 工厂治理状态：active / disabled / retired
	Revision         int64  `json:"revision"`         // 治理修订，厂端只向前
	FactoryShortCode string `json:"factoryShortCode"` // 本厂短码
}

// channelEnroll 用建厂码换待认领身份；码不对或已用完则拒绝，成功也不消耗建厂码。
func (h *Handler) channelEnroll(w http.ResponseWriter, r *http.Request) {
	// 接住请求体，解析失败再拒绝。
	var req enrollReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 用建厂码换待认领身份，码错就拒绝。
	offer, err := h.svc.OfferEnroll(r.Context(), req.EnrollmentCode)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, enrollResp{
		FactoryID:        offer.FactoryID.String(),
		Name:             offer.Name,
		FactoryShortCode: offer.ShortCode,
		SAPersonID:       offer.SAPersonID.String(),
		SALogin:          offer.SALogin,
		SADisplay:        offer.SADisplay,
	})
}

// channelClaim 交厂钥并作废建厂码；须再验一次建厂码，不信客户端自报厂身份。
func (h *Handler) channelClaim(w http.ResponseWriter, r *http.Request) {
	// 接住请求体，解析失败再拒绝。
	var req claimReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 没带建厂码就拒绝，不信客户端自报厂。
	if req.EnrollmentCode == "" {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, domain.ErrInvalidEnrollment)
		return
	}
	// 用建厂码换待认领身份，码错就拒绝。
	offer, err := h.svc.OfferEnroll(r.Context(), req.EnrollmentCode)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 交钥没登记成功，认领就不能算完成。
	if err := h.svc.ConfirmEnroll(r.Context(), offer.FactoryID, req.FactoryPublicKey); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 先按在营组装，再改成库里的真实状态。
	out := claimResp{FactoryID: offer.FactoryID.String(), Status: service.FactoryActive}
	// 再读治理状态，读不到就不补展示字段。
	fac, err := h.svc.Store().FactoryByID(r.Context(), offer.FactoryID)
	// 读到厂档才补真实状态，读不到仍回认领结果。
	if err == nil {
		// 改用库里的治理状态，不信客户端自报。
		out.Status = fac.Status
		// 带回治理修订，厂端只向前接收。
		out.Revision = fac.LifecycleRevision
		// 带回本厂短码，供厂端展示。
		out.FactoryShortCode = fac.ShortCode
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, out)
}
