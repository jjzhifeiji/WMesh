package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

// Client 绑定、运行许可。
func (h *Handler) mountNode(mux *http.ServeMux) {
	// 列出本厂已经绑定的现场设备。
	mux.HandleFunc("GET /v1/factories/{id}/clients", h.listClients)
	// 修改本厂设备的显示名。
	mux.HandleFunc("PATCH /v1/factories/{id}/clients/{clientId}", h.renameClient)
	// 本厂超管把读到的机械臂号钉到已绑定设备，已钉别的号拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/clients/{clientId}/device", h.registerDevice)
	// 在已钉号的本机登录并领取解封钥。
	mux.HandleFunc("POST /v1/factories/{id}/clients/{clientId}/login", h.loginOnClient)
	// 厂网登录，不校验当时的机械臂号。
	mux.HandleFunc("POST /v1/factories/{id}/pad/login", h.loginPad)
	// 补记一次现场，回到厂网才可能送到。
	mux.HandleFunc("POST /v1/factories/{id}/pad/login-log", h.reportPadLogin)
	// 作废绑定，这台设备不能再作业。
	mux.HandleFunc("POST /v1/factories/{id}/clients/{clientId}/void", h.voidClient)
	// 签发一段允许运行的时间窗。
	mux.HandleFunc("POST /v1/factories/{id}/clients/{clientId}/runtime", h.issueRuntime)
	// 收回运行许可，窗口内不再允许作业。
	mux.HandleFunc("POST /v1/factories/{id}/clients/{clientId}/runtime/revoke", h.revokeRuntime)
	// 列出本厂已经签发的运行许可。
	mux.HandleFunc("GET /v1/factories/{id}/runtime-grants", h.listRuntime)
	// 登录仍有效才返回签发公钥。
	mux.HandleFunc("GET /v1/factories/{id}/signing-key", h.signingPublicKey)
}

// 只改设备显示名，绑定身份不变。
type renameClientReq struct {
	Name string `json:"name"` // 给人看的新名字
}

// 运行许可签多少天；起止由服务器按自己的钟算。
type issueWindowReq struct {
	Days int `json:"days"` // 有效天数，1～365
}

// 要钉到这台设备上的机械臂号。
type deviceSerialReq struct {
	DeviceSerial string `json:"deviceSerial"` // 从设备读到的机械臂识别号
}

// 在已钉设备上登录的口令和现场。
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

// 厂网登录，不要求当时连着机械臂。
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

// 补记的一次示教器现场，可晚一点送到。
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

// 只回签发公钥，私钥不能离开厂内。
type signingKeyResp struct {
	PublicKey []byte `json:"publicKey"` // 本厂签发公钥，无私钥
}

// 列出本厂已绑定现场设备。
func (h *Handler) listClients(w http.ResponseWriter, r *http.Request) {
	// 列出本厂已绑定现场设备。
	h.withFactory(w, r, func(svc *service.Service) {
		// 列出本厂已经绑定的现场设备。
		rows, err := svc.Node.ListClients(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, rows)
	})
}

// 本厂超管把读到的机械臂号钉到已绑定设备，已钉别的号拒绝。
func (h *Handler) registerDevice(w http.ResponseWriter, r *http.Request) {
	// 把读到的机械臂号钉上，空号则拒绝。
	h.withFactory(w, r, func(svc *service.Service) {
		// 设备编号必须合法，否则拒绝这次办理。
		clientID, err := uuid.Parse(r.PathValue("clientId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 承接要钉上的机械臂号。
		var req deviceSerialReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 把机械臂号钉到设备上，空号则拒绝。
		row, err := svc.Node.RegisterDevice(r.Context(), bearer(r), clientID, req.DeviceSerial)
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

// 本厂账号在已钉设备号的本机登录并领取解封钥。
func (h *Handler) loginOnClient(w http.ResponseWriter, r *http.Request) {
	// 本厂账号在已钉设备号的本机登录并领取解封钥。
	h.withFactory(w, r, func(svc *service.Service) {
		// 设备编号必须合法，否则拒绝这次办理。
		clientID, err := uuid.Parse(r.PathValue("clientId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 承接本机登录的口令和现场。
		var req clientLoginReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 在已钉号的本机登录，并领取解封钥。
		sess, err := svc.Node.LoginOnClient(r.Context(), clientID, req.DeviceSerial, req.LoginName, req.Password)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 先记下这次登录的设备，自报合法再替换。
		cid := clientID
		// 补记现场，记失败也不推翻刚完成的登录。
		_ = svc.Node.RecordAppLogin(r.Context(), sess.Account.ID, loginSnap(service.LoginKindClient, req.AppVersion, req.AppVersionName, req.DeviceSerial, req.DeviceModel, req.DeviceManufacturer, req.AndroidRelease, req.NetworkName, req.ClientID, &cid))
		// 决定回给平板的通道地址，配置优先。
		sess.MqttURL = h.clientMQTTURL(r)
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, sess)
	})
}

// 厂网登录：不校验机械臂号，回登录人解封钥和本厂设备名录。
func (h *Handler) loginPad(w http.ResponseWriter, r *http.Request) {
	// 厂网登录：不校验机械臂号，回登录人解封钥和本厂设备名录。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接厂网登录，不要求机械臂号。
		var req padLoginReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 厂网登录，不核对当时是否连着机械臂。
		sess, err := svc.Node.LoginPad(r.Context(), req.LoginName, req.Password)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 补记现场，记失败也不推翻刚完成的登录。
		_ = svc.Node.RecordAppLogin(r.Context(), sess.Account.ID, loginSnap(service.LoginKindPad, req.AppVersion, req.AppVersionName, req.DeviceSerial, req.DeviceModel, req.DeviceManufacturer, req.AndroidRelease, req.NetworkName, req.ClientID, nil))
		// 决定回给平板的通道地址，配置优先。
		sess.MqttURL = h.clientMQTTURL(r)
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, sess)
	})
}

// 示教器补记一次现场；匹配设备号后回厂网才可能送到。
func (h *Handler) reportPadLogin(w http.ResponseWriter, r *http.Request) {
	// 示教器补记一次现场；匹配设备号后回厂网才可能送到。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接要补记的现场信息。
		var req padLoginLogReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 补记自己的现场，令牌无效则拒绝。
		err := svc.Node.RecordOwnAppLogin(r.Context(), bearer(r), loginSnap(service.LoginKindPad, req.AppVersion, req.AppVersionName, req.DeviceSerial, req.DeviceModel, req.DeviceManufacturer, req.AndroidRelease, req.NetworkName, req.ClientID, nil))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 办理成功，且没有正文需要返回。
		w.WriteHeader(http.StatusNoContent)
	})
}

// 改本厂设备显示名。
func (h *Handler) renameClient(w http.ResponseWriter, r *http.Request) {
	// 只改本厂设备显示名，绑定身份不变。
	h.withFactory(w, r, func(svc *service.Service) {
		// 设备编号必须合法，否则拒绝这次办理。
		clientID, err := uuid.Parse(r.PathValue("clientId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 承接设备的新显示名。
		var req renameClientReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 只改设备显示名，绑定身份不变。
		row, err := svc.Node.RenameClient(r.Context(), bearer(r), clientID, req.Name)
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

// 作废本厂绑定，设备不再可用。
func (h *Handler) voidClient(w http.ResponseWriter, r *http.Request) {
	// 作废绑定之后，这台设备不能再作业。
	h.withFactory(w, r, func(svc *service.Service) {
		// 设备编号必须合法，否则拒绝这次办理。
		clientID, err := uuid.Parse(r.PathValue("clientId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 被拒绝则停止，不继续写成功响应。
		if err := svc.Node.VoidClientBinding(r.Context(), bearer(r), clientID); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 办理成功，且没有正文需要返回。
		w.WriteHeader(http.StatusNoContent)
	})
}

// 签发运行许可窗口。
func (h *Handler) issueRuntime(w http.ResponseWriter, r *http.Request) {
	// 按签发去办，天数不合法会在里面拒绝。
	h.writeRuntime(w, r, true)
}

// 收回运行许可窗口。
func (h *Handler) revokeRuntime(w http.ResponseWriter, r *http.Request) {
	// 按收回去办，天数不合法会在里面拒绝。
	h.writeRuntime(w, r, false)
}

// 签发或收回运行许可窗口。
func (h *Handler) writeRuntime(w http.ResponseWriter, r *http.Request, canRun bool) {
	// 签发或收回运行许可窗口。
	h.withFactory(w, r, func(svc *service.Service) {
		// 设备编号必须合法，否则拒绝这次办理。
		clientID, err := uuid.Parse(r.PathValue("clientId"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 编号不合法，回非法请求并不进入业务。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 承接许可天数；带旧的起止字段会被当未知字段拒绝。
		var req issueWindowReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 先放空许可，签发或收回之后再填。
		var cred service.RuntimeCred
		// 允许运行才签发窗口，否则改为收回。
		if canRun {
			// 签发允许运行的天数，起止由服务器定。
			cred, err = svc.Node.IssueRuntimeGrant(r.Context(), bearer(r), clientID, req.Days)
			// 不签发时改为收回，同一窗口不能两可。
		} else {
			// 收回运行许可，起止同样由服务器定。
			cred, err = svc.Node.RevokeRuntimeGrant(r.Context(), bearer(r), clientID, req.Days)
		}
		// 失败则停止，不把这一步当成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 创建成功，把新记录回给调用方。
		writeJSON(w, http.StatusCreated, service.RuntimeGrantView{
			ClientID: cred.ClientID, Revision: cred.Revision, CanRun: cred.CanRun,
			NotBefore: cred.NotBefore, NotAfter: cred.NotAfter,
		})
	})
}

// 列出本厂运行许可。
func (h *Handler) listRuntime(w http.ResponseWriter, r *http.Request) {
	// 列出本厂运行许可，未登录则拒绝。
	h.withFactory(w, r, func(svc *service.Service) {
		// 列出本厂已经签发的运行许可。
		rows, err := svc.Node.ListRuntimeGrants(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, rows)
	})
}

// 登录有效才返回本厂签发公钥，无私钥。
func (h *Handler) signingPublicKey(w http.ResponseWriter, r *http.Request) {
	// 登录有效才返回本厂签发公钥，无私钥。
	h.withFactory(w, r, func(svc *service.Service) {
		// 会话无效或账号停用则拒绝，不再继续。
		if _, err := svc.Auth.RequireActive(r.Context(), bearer(r)); err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 取出签发公钥，私钥不会离开厂内。
		pub, err := svc.Node.SigningPublicKey(r.Context())
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, signingKeyResp{PublicKey: pub})
	})
}

// loginSnap 把示教器自报收成现场；非法 clientId 丢掉，不挡登录。
func loginSnap(kind string, version int64, versionName, serial, model, mfr, android, wifi, clientRaw string, fallback *uuid.UUID) service.LoginSnap {
	// 先用调用方给的设备，能解析再替换。
	cid := fallback
	// 自报的设备号能解析才采用，非法则保留。
	if parsed := parseLooseUUID(clientRaw); parsed != nil {
		// 改用解析出来的设备，非法的不会走到这。
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
	// 去掉空白再判断有没有设备号。
	raw = strings.TrimSpace(raw)
	// 空设备号当成没传，不因此拒绝登录。
	if raw == "" {
		return nil
	}
	// 能解析才当作设备，非法就当没传。
	id, err := uuid.Parse(raw)
	// 解析失败则拒绝，不用这个残缺的值继续。
	if err != nil {
		return nil
	}
	return &id
}
