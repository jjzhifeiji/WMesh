package httpapi

import (
	"net/http"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

// 挂厂出站认领 HTTPS，以及厂钥 HTTPS 拉正文。
func (h *Handler) mountChannel(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/channel/enroll", h.channelEnroll)
	mux.HandleFunc("POST /v1/channel/claim", h.channelClaim)
	mux.HandleFunc("GET /v1/channel/index", h.channelIndex)
	mux.HandleFunc("GET /v1/channel/lease", h.channelLease)
	mux.HandleFunc("POST /v1/channel/lease", h.channelLease)
	mux.HandleFunc("GET /v1/channel/pull/closure/{assetId}", h.pullClosure)
	mux.HandleFunc("GET /v1/channel/pull/template/{templateId}", h.pullTemplate)
	mux.HandleFunc("GET /v1/channel/pull/software", h.pullSoftware)
}

type enrollReq struct {
	EnrollmentCode string `json:"enrollmentCode"` // 一次性建厂码
}

type enrollResp struct {
	FactoryID        string `json:"factoryId"`        // 工厂稳定身份
	Name             string `json:"name"`             // 工厂显示名
	FactoryShortCode string `json:"factoryShortCode"` // 本厂短码
	SAPersonID       string `json:"saPersonId"`       // 约定的初始超管身份
	SALogin          string `json:"saLogin"`          // 初始超管登录名
	SADisplay        string `json:"saDisplay"`        // 初始超管显示名
}

type claimReq struct {
	EnrollmentCode   string `json:"enrollmentCode"`   // 一次性建厂码
	FactoryPublicKey []byte `json:"factoryPublicKey"` // 本厂签发公钥
}

type claimResp struct {
	FactoryID        string `json:"factoryId"`        // 工厂稳定身份
	Status           string `json:"status"`           // 工厂治理状态：active / disabled / retired
	Revision         int64  `json:"revision"`         // 治理修订，厂端只向前
	FactoryShortCode string `json:"factoryShortCode"` // 本厂短码
}

// channelEnroll 用建厂码换待认领身份；码不对或已用完则拒绝，成功也不消耗建厂码。
func (h *Handler) channelEnroll(w http.ResponseWriter, r *http.Request) {
	var req enrollReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	offer, err := h.svc.OfferEnroll(r.Context(), req.EnrollmentCode)
	if err != nil {
		writeErr(w, err)
		return
	}
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
	var req claimReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	if req.EnrollmentCode == "" {
		writeErr(w, domain.ErrInvalidEnrollment)
		return
	}
	offer, err := h.svc.OfferEnroll(r.Context(), req.EnrollmentCode)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := h.svc.ConfirmEnroll(r.Context(), offer.FactoryID, req.FactoryPublicKey); err != nil {
		writeErr(w, err)
		return
	}
	out := claimResp{FactoryID: offer.FactoryID.String(), Status: service.FactoryActive}
	fac, err := h.svc.Store().FactoryByID(r.Context(), offer.FactoryID)
	if err == nil {
		out.Status = fac.Status
		out.Revision = fac.LifecycleRevision
		out.FactoryShortCode = fac.ShortCode
	}
	writeJSON(w, http.StatusOK, out)
}
