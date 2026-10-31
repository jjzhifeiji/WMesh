package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

// 设备名录、昵称和示教器登录。
func (h *Handler) mountNode(mux *http.ServeMux) {
	// 列出本厂已经绑定的现场设备。
	mux.HandleFunc("GET /v1/factories/{id}/clients", h.listClients)
	// 修改本厂设备的昵称，设备名由 WAN 定。
	mux.HandleFunc("PATCH /v1/factories/{id}/clients/{clientId}", h.renameClient)
	// 厂网登录，不校验当时的机械臂号。
	mux.HandleFunc("POST /v1/factories/{id}/pad/login", h.loginPad)
	// 补记一次现场，回到厂网才可能送到。
	mux.HandleFunc("POST /v1/factories/{id}/pad/login-log", h.reportPadLogin)
}

// 只改设备昵称，设备名和绑定身份不变。
type renameClientReq struct {
	Name string `json:"name"` // 新昵称
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

// loginSnap 把示教器自报收成现场；已核设备优先，非法 clientId 丢掉。
func loginSnap(kind string, version int64, versionName, serial, model, mfr, android, wifi, clientRaw string, verified *uuid.UUID) service.LoginSnap {
	// 路径里已核对过的设备优先，自报号不能盖掉它。
	cid := verified
	// 没有已核设备才看自报，非法的当没传。
	if cid == nil {
		// 自报号能解析才采用，非法则仍留空。
		cid = parseLooseUUID(clientRaw)
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
