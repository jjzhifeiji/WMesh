package httpapi

import (
	"net/http"
	"strings"

	"wmesh/factory/internal/hub"
)

// 本机认领、已认领工厂清单，以及给 Client 的局域网探活。
func (h *Handler) mountSite(mux *http.ServeMux) {
	// 返回本机已经认领的工厂清单。
	mux.HandleFunc("GET /v1/site", h.site)
	// 用建厂码认领工厂，并自设日常口令。
	mux.HandleFunc("POST /v1/site/claim", h.claim)
	// 给现场探活，并看设备号是否属于本厂。
	mux.HandleFunc("GET /v1/discover", h.discover)
}

// 本机还能不能认领，以及已经认领的工厂。
type siteResp struct {
	WANConfigured bool              `json:"wanConfigured"` // 配了 WAN 地址才能认领
	Factories     []hub.SiteFactory `json:"factories"`     // 本机已认领的工厂
}

// 认领用的建厂码，以及持有者自设口令。
type claimReq struct {
	EnrollmentCode string `json:"enrollmentCode"` // 一次性建厂码
	Password       string `json:"password"`       // 持有者自设日常密码
}

// 认领到的工厂，以及初始超管登录名。
type claimResp struct {
	FactoryID string `json:"factoryId"` // 认领到的工厂身份
	SALogin   string `json:"saLogin"`   // 初始超管登录名
}

// 本机已认领工厂清单。
func (h *Handler) site(w http.ResponseWriter, r *http.Request) {
	// 读取本机已经认领的工厂。
	rows, err := h.Hub.ListSite(r.Context())
	// 上一步没通过则停止，不把失败写成成功。
	if err != nil {
		// 把失败译成状态回给调用方，不写成功。
		writeErr(w, err)
		return
	}
	// 通过之后把结果回给调用方。
	writeJSON(w, http.StatusOK, siteResp{WANConfigured: h.WANURL != "", Factories: rows})
}

// 用建厂码认领并自设密码。
func (h *Handler) claim(w http.ResponseWriter, r *http.Request) {
	// 承接建厂码和自设口令。
	var req claimReq
	// 正文无法解析则拒绝，不进入业务。
	if err := decodeJSON(r, &req); err != nil {
		// 请求不合法，回非法请求并不进入业务。
		writeBadRequest(w, err)
		return
	}
	// 用建厂码认领，并让持有者自设口令。
	out, err := h.Hub.Claim(r.Context(), h.WANURL, req.EnrollmentCode, req.Password)
	// 上一步没通过则停止，不把失败写成成功。
	if err != nil {
		// 把失败译成状态回给调用方，不写成功。
		writeErr(w, err)
		return
	}
	// 创建成功，把新记录回给调用方。
	writeJSON(w, http.StatusCreated, claimResp{FactoryID: out.ID.String(), SALogin: out.SALogin})
}

// 给现场的厂址，以及这台设备是否本厂。
type discoverResp struct {
	Status    string             `json:"status"`    // ok 或 degraded，与探活同口径
	HTTPBase  string             `json:"httpBase"`  // 本机对外地址，给 Client 当厂地址
	Factories []hub.FactoryProbe `json:"factories"` // 已认领工厂；factoryName 是厂名，belongs 表示该设备号属本厂
}

// discover 给现场 Client 探活：厂身份、厂名、地址、该机械臂号是否本厂已钉设备。
func (h *Handler) discover(w http.ResponseWriter, r *http.Request) {
	// 沿用这次请求的取消信号，调用方断开就停。
	ctx := r.Context()
	// 先按健康组装探活结果，库失败再降级。
	resp := discoverResp{Status: "ok", HTTPBase: publicHTTPBase(r), Factories: []hub.FactoryProbe{}}
	// 被拒绝则停止，不继续写成功响应。
	if err := h.Hub.Ping(ctx); err != nil {
		// 库探活失败则标降级，名录仍尽量返回。
		resp.Status = "degraded"
	}
	// 带上机械臂号，用来判断是否属于本厂。
	rows, err := h.Hub.Discover(ctx, r.URL.Query().Get("deviceSerial"))
	// 上一步没通过则停止，不把失败写成成功。
	if err != nil {
		// 把失败译成状态回给调用方，不写成功。
		writeErr(w, err)
		return
	}
	// 名录读到了才填上，没有就保持空表。
	if rows != nil {
		// 把探到的工厂填上，没有则保持空表。
		resp.Factories = rows
	}
	// 通过之后把结果回给调用方。
	writeJSON(w, http.StatusOK, resp)
}

// publicHTTPBase 用请求 Host 拼厂地址，避免 Client 手填。
func publicHTTPBase(r *http.Request) string {
	// 默认按未加密来拼，看到加密再改。
	scheme := "http"
	if r.TLS != nil {
		// 改用这次连接或反代声明的协议。
		scheme = "https"
	}
	// 反代声明了协议就改用它，避免地址拼错。
	if p := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); p != "" {
		// 改用这次连接或反代声明的协议。
		scheme = p
	}
	// 取请求里的主机，空的再看反代。
	host := strings.TrimSpace(r.Host)
	// 请求没带主机就改看反代声明的主机。
	if host == "" {
		// 请求没有主机时，改用反代声明的主机。
		host = strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	}
	// 请求没带主机就改看反代声明的主机。
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}
