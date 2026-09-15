package httpapi

import (
	"net/http"
	"strings"

	"wmesh/factory/internal/hub"
)

// 本机认领、已认领工厂清单，以及给 Client 的局域网探活。
func (h *Handler) mountSite(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/site", h.site)
	mux.HandleFunc("POST /v1/site/claim", h.claim)
	mux.HandleFunc("GET /v1/discover", h.discover)
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

type discoverResp struct {
	Status    string             `json:"status"`    // ok 或 degraded，与探活同口径
	HTTPBase  string             `json:"httpBase"`  // 本机对外地址，给 Client 当厂地址
	Factories []hub.FactoryProbe `json:"factories"` // 已认领工厂；belongs 表示该设备号属本厂
}

// discover 给现场 Client 探活：厂身份、地址、该机械臂号是否本厂已钉设备。
func (h *Handler) discover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp := discoverResp{Status: "ok", HTTPBase: publicHTTPBase(r), Factories: []hub.FactoryProbe{}}
	if err := h.Hub.Ping(ctx); err != nil {
		resp.Status = "degraded"
	}
	rows, err := h.Hub.Discover(ctx, r.URL.Query().Get("deviceSerial"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if rows != nil {
		resp.Factories = rows
	}
	writeJSON(w, http.StatusOK, resp)
}

// publicHTTPBase 用请求 Host 拼厂地址，避免 Client 手填。
func publicHTTPBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); p != "" {
		scheme = p
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		host = strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}
