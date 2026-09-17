package httpapi

import (
	"encoding/json"
	"net/http"

	"wmesh/factory/internal/service"
)

// Client 策略：本厂一行，超管读写。
func (h *Handler) mountPolicy(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/factories/{id}/client-policy", h.getClientPolicy)
	mux.HandleFunc("PUT /v1/factories/{id}/client-policy", h.putClientPolicy)
}

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
	h.withFactory(w, r, func(svc *service.Service) {
		row, err := svc.Closure.GetClientPolicy(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 超管改本厂一行策略并升高修订；成功后推给已绑定 Client。
func (h *Handler) putClientPolicy(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req clientPolicyPutReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		// 省略 encryptPouch 时沿用现行值，避免改别的项时把加密关掉。
		cur, err := svc.Closure.GetClientPolicy(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		encrypt := cur.EncryptPouch
		if req.EncryptPouch != nil {
			encrypt = *req.EncryptPouch
		}
		row, err := svc.Closure.SetClientPolicy(r.Context(), bearer(r), service.ClientPolicy{
			MaxCachedProjects: req.MaxCachedProjects,
			CacheScope:        req.CacheScope,
			PersistUnwrapKey:  req.PersistUnwrapKey,
			KeyTTLSeconds:     req.KeyTTLSeconds,
			EncryptPouch:      encrypt,
			Extra:             req.Extra,
		})
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}
