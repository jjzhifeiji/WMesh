package httpapi

import (
	"encoding/json"
	"net/http"

	"wmesh/factory/internal/service"
)

// Client 策略：本厂一行，超管读写。
func (h *Handler) mountPolicy(mux *http.ServeMux) {
	// 读取示教器策略，不是超管就拒绝。
	mux.HandleFunc("GET /v1/factories/{id}/client-policy", h.getClientPolicy)
	// 超管修改策略并推给已经绑定的设备。
	mux.HandleFunc("PUT /v1/factories/{id}/client-policy", h.putClientPolicy)
}

// 超管提交的策略，没写的项保持原样。
type clientPolicyPutReq struct {
	MaxCachedProjects int             `json:"maxCachedProjects"` // 每 Client 工程份上限，≥1
	CacheScope        string          `json:"cacheScope"`        // current / all
	PersistUnwrapKey  bool            `json:"persistUnwrapKey"`  // 解封钥可否落盘
	KeyTTLSeconds     int64           `json:"keyTtlSeconds"`     // 登录时效秒；0 表示直到退出
	EncryptPouch      *bool           `json:"encryptPouch"`      // 本机袋是否 SQLCipher；省略则保留
	Extra             json.RawMessage `json:"extra"`             // 本厂扩展键；可省略则保留原值
}

// 读本厂 Client 策略；非超管拒绝。
func (h *Handler) getClientPolicy(w http.ResponseWriter, r *http.Request) {
	// 读取示教器策略，不是超管就拒绝。
	h.withFactory(w, r, func(svc *service.Service) {
		// 读取本厂示教器策略，不是超管就拒绝。
		row, err := svc.Closure.GetClientPolicy(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, row)
	})
}

// 超管改本厂一行策略并升高修订；成功后推给已绑定 Client。
func (h *Handler) putClientPolicy(w http.ResponseWriter, r *http.Request) {
	// 超管改策略并升高修订，随后推给设备。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接这次要改的示教器策略。
		var req clientPolicyPutReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 省略 encryptPouch 时沿用现行值，避免改别的项时把加密关掉。
		cur, err := svc.Closure.GetClientPolicy(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 没带这项就保持原来的加密选择。
		encrypt := cur.EncryptPouch
		// 这次写了是否加密才覆盖，没写就保留。
		if req.EncryptPouch != nil {
			// 调用方这次明确改了是否加密库。
			encrypt = *req.EncryptPouch
		}
		// 超管改策略并升高修订，随后推给设备。
		row, err := svc.Closure.SetClientPolicy(r.Context(), bearer(r), service.ClientPolicy{
			MaxCachedProjects: req.MaxCachedProjects,
			CacheScope:        req.CacheScope,
			PersistUnwrapKey:  req.PersistUnwrapKey,
			KeyTTLSeconds:     req.KeyTTLSeconds,
			EncryptPouch:      encrypt,
			Extra:             req.Extra,
		})
		// 失败则停止，不把这一步当成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, row)
	})
}
