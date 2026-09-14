package httpapi

import (
	"net/http"

	"wmesh/factory/internal/hub"
)

// 本机认领与已认领工厂清单。
func (h *Handler) mountSite(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/site", h.site)
	mux.HandleFunc("POST /v1/site/claim", h.claim)
}

type siteResp struct {
	WANConfigured bool              `json:"wanConfigured"` // 配了 WAN 地址才能认领
	Factories     []hub.SiteFactory `json:"factories"`     // 本机已认领的工厂
}

type claimReq struct {
	EnrollmentCode string `json:"enrollmentCode"` // 一次性建厂码
	Password       string `json:"password"`       // 持有者自设日常密码
}

type claimResp struct {
	FactoryID string `json:"factoryId"` // 认领到的工厂身份
	SALogin   string `json:"saLogin"`   // 初始超管登录名
}

// 本机已认领工厂清单。
func (h *Handler) site(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Hub.ListSite(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, siteResp{WANConfigured: h.WANURL != "", Factories: rows})
}

// 用建厂码认领并自设密码。
func (h *Handler) claim(w http.ResponseWriter, r *http.Request) {
	var req claimReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	out, err := h.Hub.Claim(r.Context(), h.WANURL, req.EnrollmentCode, req.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, claimResp{FactoryID: out.ID.String(), SALogin: out.SALogin})
}
