package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

// Client 绑定、运行许可。
func (h *Handler) mountNode(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/factories/{id}/clients", h.listClients)
	mux.HandleFunc("POST /v1/factories/{id}/clients", h.registerClient)
	mux.HandleFunc("PATCH /v1/factories/{id}/clients/{clientId}", h.renameClient)
	mux.HandleFunc("POST /v1/factories/{id}/clients/{clientId}/device", h.registerDevice)
	mux.HandleFunc("POST /v1/factories/{id}/clients/{clientId}/login", h.loginOnClient)
	mux.HandleFunc("POST /v1/factories/{id}/pad/login", h.loginPad)
	mux.HandleFunc("POST /v1/factories/{id}/pad/login-log", h.reportPadLogin)
	mux.HandleFunc("POST /v1/factories/{id}/clients/{clientId}/void", h.voidClient)
	mux.HandleFunc("POST /v1/factories/{id}/clients/{clientId}/runtime", h.issueRuntime)
	mux.HandleFunc("POST /v1/factories/{id}/clients/{clientId}/runtime/revoke", h.revokeRuntime)
	mux.HandleFunc("GET /v1/factories/{id}/runtime-grants", h.listRuntime)
	mux.HandleFunc("GET /v1/factories/{id}/signing-key", h.signingPublicKey)
}

type acceptClientReq struct {
	ID              string `json:"id"`              // Client 稳定身份
	Name            string `json:"name"`            // 给人看的设备名
	PublicKey       string `json:"publicKey"`       // 本机公钥，可空
	BindingRevision int64  `json:"bindingRevision"` // 绑定修订，必须向前
}

type renameClientReq struct {
	Name string `json:"name"` // 给人看的新名字
}

type issueWindowReq struct {
	NotBefore string `json:"notBefore"` // RFC3339
	NotAfter  string `json:"notAfter"`  // RFC3339
}

type deviceSerialReq struct {
	DeviceSerial string `json:"deviceSerial"` // 从设备读到的机械臂识别号
}

type clientLoginReq struct {
	DeviceSerial       string `json:"deviceSerial"`       // 本次读到的机械臂识别号
	LoginName          string `json:"loginName"`          // 本厂登录名
	Password           string `json:"password"`           // 日常密码，不进审计
	AppVersion         int64  `json:"appVersion"`         // 示教器 versionCode；0 表示没报
	AppVersionName     string `json:"appVersionName"`     // 示教器 versionName
	DeviceModel        string `json:"deviceModel"`        // 平板型号
	DeviceManufacturer string `json:"deviceManufacturer"` // 平板厂商
	AndroidRelease     string `json:"androidRelease"`     // 平板系统版本
	NetworkName        string `json:"networkName"`        // 当时 WiFi 名
	ClientID           string `json:"clientId"`           // 已匹配本机；路径优先
}

type padLoginReq struct {
	LoginName          string `json:"loginName"`          // 本厂登录名
	Password           string `json:"password"`           // 日常密码，不进审计
	AppVersion         int64  `json:"appVersion"`         // 示教器 versionCode；0 表示没报
	AppVersionName     string `json:"appVersionName"`     // 示教器 versionName
	DeviceSerial       string `json:"deviceSerial"`       // 机械臂识别号；厂网登录时常空
	DeviceModel        string `json:"deviceModel"`        // 平板型号
	DeviceManufacturer string `json:"deviceManufacturer"` // 平板厂商
	AndroidRelease     string `json:"androidRelease"`     // 平板系统版本
	NetworkName        string `json:"networkName"`        // 当时 WiFi 名
	ClientID           string `json:"clientId"`           // 已匹配本机；未对臂为空
}

type padLoginLogReq struct {
	AppVersion         int64  `json:"appVersion"`         // 示教器 versionCode；0 表示没报
	AppVersionName     string `json:"appVersionName"`     // 示教器 versionName
	DeviceSerial       string `json:"deviceSerial"`       // 机械臂识别号
	DeviceModel        string `json:"deviceModel"`        // 平板型号
	DeviceManufacturer string `json:"deviceManufacturer"` // 平板厂商
	AndroidRelease     string `json:"androidRelease"`     // 平板系统版本
	NetworkName        string `json:"networkName"`        // 当时 WiFi 名
	ClientID           string `json:"clientId"`           // 已匹配本机
}

type signingKeyResp struct {
	PublicKey []byte `json:"publicKey"` // 本厂签发公钥，无私钥
}

// 列出本厂已绑定现场设备。
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

// 解析可选公钥后按修订向前登记绑定。
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
		pub, err := decodeOptionalPublicKey(req.PublicKey)
		if err != nil {
			writeErr(w, err)
			return
		}
		// 按修订向前落本厂绑定，公钥可空。
		row, err := svc.Node.RegisterBinding(r.Context(), bearer(r), cid, req.Name, pub, req.BindingRevision)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, row)
	})
}

// 把读到的机械臂号钉到已绑定 Client；空号拒绝。
func (h *Handler) registerDevice(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		clientID, err := uuid.Parse(r.PathValue("clientId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		var req deviceSerialReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.Node.RegisterDevice(r.Context(), clientID, req.DeviceSerial)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 本厂账号在已钉设备号的本机登录并领取解封钥。
func (h *Handler) loginOnClient(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		clientID, err := uuid.Parse(r.PathValue("clientId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		var req clientLoginReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		sess, err := svc.Node.LoginOnClient(r.Context(), clientID, req.DeviceSerial, req.LoginName, req.Password)
		if err != nil {
			writeErr(w, err)
			return
		}
		cid := clientID
		_ = svc.Node.RecordAppLogin(r.Context(), sess.Account.ID, loginSnap(service.LoginKindClient, req.AppVersion, req.AppVersionName, req.DeviceSerial, req.DeviceModel, req.DeviceManufacturer, req.AndroidRelease, req.NetworkName, req.ClientID, &cid))
		sess.MqttURL = h.clientMQTTURL(r)
		writeJSON(w, http.StatusOK, sess)
	})
}

// 厂网登录：不校验机械臂号，回登录人解封钥和本厂设备名录。
func (h *Handler) loginPad(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req padLoginReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		sess, err := svc.Node.LoginPad(r.Context(), req.LoginName, req.Password)
		if err != nil {
			writeErr(w, err)
			return
		}
		_ = svc.Node.RecordAppLogin(r.Context(), sess.Account.ID, loginSnap(service.LoginKindPad, req.AppVersion, req.AppVersionName, req.DeviceSerial, req.DeviceModel, req.DeviceManufacturer, req.AndroidRelease, req.NetworkName, req.ClientID, nil))
		sess.MqttURL = h.clientMQTTURL(r)
		writeJSON(w, http.StatusOK, sess)
	})
}

// 示教器补记一次现场；匹配设备号后回厂网才可能送到。
func (h *Handler) reportPadLogin(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req padLoginLogReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		err := svc.Node.RecordOwnAppLogin(r.Context(), bearer(r), loginSnap(service.LoginKindPad, req.AppVersion, req.AppVersionName, req.DeviceSerial, req.DeviceModel, req.DeviceManufacturer, req.AndroidRelease, req.NetworkName, req.ClientID, nil))
		if err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// 改本厂设备显示名。
func (h *Handler) renameClient(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		clientID, err := uuid.Parse(r.PathValue("clientId"))
		if err != nil {
			writeBadRequest(w, errInvalidID)
			return
		}
		var req renameClientReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		row, err := svc.Node.RenameClient(r.Context(), bearer(r), clientID, req.Name)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, row)
	})
}

// 作废本厂绑定，设备不再可用。
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

// 签发运行许可窗口。
func (h *Handler) issueRuntime(w http.ResponseWriter, r *http.Request) {
	h.writeRuntime(w, r, true)
}

// 收回运行许可窗口。
func (h *Handler) revokeRuntime(w http.ResponseWriter, r *http.Request) {
	h.writeRuntime(w, r, false)
}

// 签发或收回运行许可窗口。
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

// 列出本厂运行许可。
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

// 登录有效才返回本厂签发公钥，无私钥。
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

// 把许可窗口收成 UTC。
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

// loginSnap 把示教器自报收成现场；非法 clientId 丢掉，不挡登录。
func loginSnap(kind string, version int64, versionName, serial, model, mfr, android, wifi, clientRaw string, fallback *uuid.UUID) service.LoginSnap {
	cid := fallback
	if parsed := parseLooseUUID(clientRaw); parsed != nil {
		cid = parsed
	}
	return service.LoginSnap{
		Kind:               kind,
		AppVersion:         version,
		AppVersionName:     versionName,
		DeviceSerial:       serial,
		DeviceModel:        model,
		DeviceManufacturer: mfr,
		AndroidRelease:     android,
		NetworkName:        wifi,
		ClientID:           cid,
	}
}

// parseLooseUUID 空或非法都当没传。
func parseLooseUUID(raw string) *uuid.UUID {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	return &id
}
