package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/id"
)

// 挂现场设备名录与分配；离线授权查询一律拒绝。
func (h *Handler) mountClient(mux *http.ServeMux) {
	// 列出已经登记的现场设备。
	mux.HandleFunc("GET /v1/clients", h.listClients)
	// 登记现场设备，识别号必填。
	mux.HandleFunc("POST /v1/clients", h.registerClient)
	// 改名之后把新名字推给所在厂。
	mux.HandleFunc("PATCH /v1/clients/{id}", h.renameClient)
	// 分配之后立刻把绑定推给目标厂。
	mux.HandleFunc("POST /v1/clients/{id}/assign", h.assignClient)
	// 改分先通知旧厂作废，再绑新厂。
	mux.HandleFunc("POST /v1/clients/{id}/rebind", h.rebindClient)
	// 查厂内人员离线授权，一律拒绝。
	mux.HandleFunc("GET /v1/factories/{id}/offline-grants", h.listFactoryOfflineGrants)
}

// 登记现场设备，机械臂识别号必填。
type registerClientReq struct {
	ID           string `json:"id"`           // 稳定身份；空则由服务发号
	Name         string `json:"name"`         // 给人看的名字
	DeviceSerial string `json:"deviceSerial"` // 机械臂识别号，登记必填
	FactoryID    string `json:"factoryId"`    // 可当场分给这家厂
	PublicKey    string `json:"publicKey"`    // 可选；现场上线后再登记
}

// 现场设备改名，改完推给所在厂。
type renameClientReq struct {
	Name string `json:"name"` // 给人看的新名字
}

// 把设备分到一家厂，空厂拒绝。
type assignClientReq struct {
	FactoryID string `json:"factoryId"` // 分到的工厂
}

// 列出已登记现场设备。
func (h *Handler) listClients(w http.ResponseWriter, r *http.Request) {
	// 带会话去查，未登录由服务拒绝。
	rows, err := h.svc.Clients.ListClients(r.Context(), bearer(r))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, rows)
}

// 解析可选公钥后登记，并立刻推给已分配的厂。
func (h *Handler) registerClient(w http.ResponseWriter, r *http.Request) {
	// 接住请求体，解析失败再拒绝。
	var req registerClientReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 先当没指定，后面再解析或发新号。
	cid := uuid.Nil
	// 带了身份就要验，验不过不能登记。
	if req.ID != "" {
		// 先声明错误，解析失败再写入。
		var err error
		// 解析稳定身份，格式不对就拒绝。
		cid, err = uuid.Parse(req.ID)
		// 参数不合法就按坏请求拒绝。
		if err != nil {
			// 把不合法的原因回给调用方。
			writeBadRequest(w, errInvalidID)
			return
		}
		// 没指定身份就由云端发号，避免空号入库。
	} else {
		// 没指定身份就发新号，保证一机一号。
		cid = id.New()
	}
	// 先当没指定，后面再解析或发新号。
	fid := uuid.Nil
	// 带了目标厂就要验，验不过不能分配。
	if req.FactoryID != "" {
		// 先声明错误，解析失败再写入。
		var err error
		// 解析稳定身份，格式不对就拒绝。
		fid, err = uuid.Parse(req.FactoryID)
		// 参数不合法就按坏请求拒绝。
		if err != nil {
			// 把不合法的原因回给调用方。
			writeBadRequest(w, errInvalidID)
			return
		}
	}
	// 解析可选公钥，格式不对就拒绝登记。
	pub, err := decodeOptionalPublicKey(req.PublicKey)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 登记并推给已分配的厂，识别号必填。
	row, err := h.svc.Clients.RegisterClient(r.Context(), bearer(r), req.Name, cid, fid, pub, req.DeviceSerial)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 创建成功，把新记录交回调用方。
	writeJSON(w, http.StatusCreated, row)
}

// 改名后把新名字推给所在厂。
func (h *Handler) renameClient(w http.ResponseWriter, r *http.Request) {
	// 解析稳定身份，格式不对就拒绝。
	cid, err := uuid.Parse(r.PathValue("id"))
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req renameClientReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 改名并推给所在厂，没有这台就拒绝。
	row, err := h.svc.Clients.RenameClient(r.Context(), bearer(r), cid, req.Name)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 分配后立刻推绑定给目标厂。
func (h *Handler) assignClient(w http.ResponseWriter, r *http.Request) {
	// 解析稳定身份，格式不对就拒绝。
	cid, err := uuid.Parse(r.PathValue("id"))
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req assignClientReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 解析稳定身份，格式不对就拒绝。
	fid, err := uuid.Parse(req.FactoryID)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 分配并立刻推绑定，厂不存在则拒绝。
	row, err := h.svc.Clients.AssignClient(r.Context(), bearer(r), cid, fid)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 改分后先通知旧厂作废，再通知新厂绑定。
func (h *Handler) rebindClient(w http.ResponseWriter, r *http.Request) {
	// 解析稳定身份，格式不对就拒绝。
	cid, err := uuid.Parse(r.PathValue("id"))
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req assignClientReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 解析稳定身份，格式不对就拒绝。
	fid, err := uuid.Parse(req.FactoryID)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 先通知旧厂作废，再通知新厂绑定。
	row, err := h.svc.Clients.RebindClient(r.Context(), bearer(r), cid, fid)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, row)
}

// 从 WAN 查厂内人员离线授权，一律拒绝。
func (h *Handler) listFactoryOfflineGrants(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	fid, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 代管厂内数据一律拒绝，不把结果写成成功。
	writeErr(w, h.svc.Clients.ListPersonOfflineGrants(r.Context(), bearer(r), fid))
}
