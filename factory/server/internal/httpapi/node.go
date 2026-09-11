package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

func (h *Handler) mountNode(mux *http.ServeMux) { // Client 绑定、运行许可、人员离线授权
	mux.HandleFunc("GET /v1/factories/{id}/clients", h.listClients)
	mux.HandleFunc("POST /v1/factories/{id}/clients", h.registerClient)
	mux.HandleFunc("POST /v1/factories/{id}/clients/{clientId}/void", h.voidClient)
	mux.HandleFunc("POST /v1/factories/{id}/clients/{clientId}/runtime", h.issueRuntime)
	mux.HandleFunc("POST /v1/factories/{id}/clients/{clientId}/runtime/revoke", h.revokeRuntime)
	mux.HandleFunc("GET /v1/factories/{id}/runtime-grants", h.listRuntime)
	mux.HandleFunc("GET /v1/factories/{id}/person-offline-grants", h.listPersonGrants)
	mux.HandleFunc("POST /v1/factories/{id}/person-offline-grants", h.issuePerson)
	mux.HandleFunc("GET /v1/factories/{id}/signing-key", h.signingPublicKey)
}

type acceptClientReq struct {
	ID              string `json:"id"`              // Client 稳定身份
	PublicKey       string `json:"publicKey"`       // 本机公钥，base64 或 hex
	BindingRevision int64  `json:"bindingRevision"` // 绑定修订，必须向前
}

type issueWindowReq struct {
	NotBefore string `json:"notBefore"` // RFC3339
	NotAfter  string `json:"notAfter"`  // RFC3339
}

type issuePersonReq struct {
	PersonID  string `json:"personId"`  // 本厂账号
	ClientID  string `json:"clientId"`  // 已绑定 Client
	NotBefore string `json:"notBefore"` // RFC3339
	NotAfter  string `json:"notAfter"`  // RFC3339
}

type signingKeyResp struct {
	PublicKey []byte `json:"publicKey"` // 本厂签发公钥，无私钥
}

func (h *Handler) listClients(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		rows, err := svc.Node.ListClients(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	})
}

func (h *Handler) registerClient(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req acceptClientReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		cid, err := uuid.Parse(req.ID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		pub, err := decodePublicKey(req.PublicKey)
		if err != nil {
			writeErr(w, err)
			return
		}
		row, err := svc.Node.RegisterBinding(r.Context(), bearer(r), cid, pub, req.BindingRevision)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, row)
	})
}

func (h *Handler) voidClient(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		clientID, err := uuid.Parse(r.PathValue("clientId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		if err := svc.Node.VoidClientBinding(r.Context(), bearer(r), clientID); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *Handler) issueRuntime(w http.ResponseWriter, r *http.Request) {
	h.writeRuntime(w, r, true)
}

func (h *Handler) revokeRuntime(w http.ResponseWriter, r *http.Request) {
	h.writeRuntime(w, r, false)
}

func (h *Handler) writeRuntime(w http.ResponseWriter, r *http.Request, canRun bool) {
	h.withFactory(w, r, func(svc *service.Service) {
		clientID, err := uuid.Parse(r.PathValue("clientId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		var req issueWindowReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		nb, na, err := parseWindow(req.NotBefore, req.NotAfter)
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		var cred service.RuntimeCred
		if canRun {
			cred, err = svc.Node.IssueRuntimeGrant(r.Context(), bearer(r), clientID, nb, na)
		} else {
			cred, err = svc.Node.RevokeRuntimeGrant(r.Context(), bearer(r), clientID, nb, na)
		}
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, service.RuntimeGrantView{
			ClientID: cred.ClientID, Revision: cred.Revision, CanRun: cred.CanRun,
			NotBefore: cred.NotBefore, NotAfter: cred.NotAfter,
		})
	})
}

func (h *Handler) listRuntime(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		rows, err := svc.Node.ListRuntimeGrants(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	})
}

func (h *Handler) issuePerson(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req issuePersonReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		personID, err := uuid.Parse(req.PersonID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		clientID, err := uuid.Parse(req.ClientID)
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		nb, na, err := parseWindow(req.NotBefore, req.NotAfter)
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		cred, err := svc.Node.IssuePersonOfflineGrant(r.Context(), bearer(r), personID, clientID, nb, na)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, personJSON(cred))
	})
}

func (h *Handler) listPersonGrants(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		rows, err := svc.Node.ListPersonGrantViews(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	})
}

func (h *Handler) signingPublicKey(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		if _, err := svc.Auth.RequireActive(r.Context(), bearer(r)); err != nil {
			writeErr(w, err)
			return
		}
		pub, err := svc.Node.SigningPublicKey(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, signingKeyResp{PublicKey: pub})
	})
}

func parseWindow(notBefore, notAfter string) (time.Time, time.Time, error) {
	nb, err := parseTime(notBefore)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	na, err := parseTime(notAfter)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return nb, na, nil
}

func personJSON(c service.PersonCred) service.PersonGrantView {
	orgs, roles := c.OrgSnapshot, c.RolesSnapshot
	if orgs == nil {
		orgs = []service.OrgOption{}
	}
	if roles == nil {
		roles = []service.RoleSnapshot{}
	}
	return service.PersonGrantView{
		PersonID: c.PersonID, ClientID: c.ClientID, LoginName: c.LoginName,
		AllowDirect: c.AllowDirect, OrgSnapshot: orgs, RolesSnapshot: roles,
		Active: c.Active, NotBefore: c.NotBefore, NotAfter: c.NotAfter, Revision: c.Revision,
	}
}
