package httpapi

import (
	"net/http"
)

// 挂名录、建厂和明确拒绝的代管入口。
func (h *Handler) mountDirectory(mux *http.ServeMux) {
	// 名录入口，不包含厂内人员。
	mux.HandleFunc("GET /v1/directory", h.directory)
	// 建厂入口，签发一次性建厂码。
	mux.HandleFunc("POST /v1/factories", h.createFactory)
	// 停用工厂，在线则立刻推给厂端。
	mux.HandleFunc("POST /v1/factories/{id}/disable", h.disableFactory)
	// 重新启用已经停用的工厂。
	mux.HandleFunc("POST /v1/factories/{id}/enable", h.enableFactory)
	// 未认领则从名录拿掉，已认领只注销。
	mux.HandleFunc("DELETE /v1/factories/{id}", h.deleteFactory)
	// 邀请第二名管理员，此入口一律拒绝。
	mux.HandleFunc("POST /v1/invite-wan-admin", h.inviteWANAdmin)
	// 代建厂内人员，一律拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/people", h.createFactoryPerson)
	// 代建厂内组织，一律拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/orgs", h.createFactoryOrg)
	// 代授厂内角色，一律拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/roles", h.grantFactoryRole)
	// 从云端查厂内人员，一律拒绝。
	mux.HandleFunc("GET /v1/factories/{id}/people", h.listFactoryPeople)
}

// 建厂时的厂名和初始超管，不反打厂网。
type createFactoryReq struct {
	Name      string `json:"name"`      // 工厂显示名
	SALogin   string `json:"saLogin"`   // 初始超管登录名
	SADisplay string `json:"saDisplay"` // 初始超管显示名
}

// 邀请第二名管理员，此接口一律拒绝。
type inviteReq struct {
	LoginName string `json:"loginName"` // 被邀请登录名；此接口一律拒绝
}

// 代建厂内人员，云端一律拒绝。
type factoryPersonReq struct {
	LoginName string `json:"loginName"` // 厂内登录名；WAN 不得代建
}

// 代建厂内组织，云端一律拒绝。
type factoryOrgReq struct {
	Name string `json:"name"` // 组织名；WAN 不得代建
}

// 代授厂内角色，云端一律拒绝。
type factoryRoleReq struct {
	Target string `json:"target"` // 授权目标；WAN 不得代授
}

// 返回工厂名录和初始超管身份，不含厂内人员。
func (h *Handler) directory(w http.ResponseWriter, r *http.Request) {
	// 取工厂名录，不含厂内人员。
	dir, err := h.svc.Factories.Directory(r.Context(), bearer(r))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, dir)
}

// 名录写下工厂并签发建厂码，不反打厂内网。
func (h *Handler) createFactory(w http.ResponseWriter, r *http.Request) {
	// 接住请求体，解析失败再拒绝。
	var req createFactoryReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 写下工厂并签发建厂码，不反打厂网。
	out, err := h.svc.Factories.CreateFactory(r.Context(), bearer(r), req.Name, req.SALogin, req.SADisplay)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 创建成功，把新记录交回调用方。
	writeJSON(w, http.StatusCreated, out)
}

// 停用工厂；在线则立刻推给厂端。
func (h *Handler) disableFactory(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	id, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 先停用，用来断言停用厂不能认领。
	out, err := h.svc.Factories.DisableFactory(r.Context(), bearer(r), id)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, out)
}

// 重新启用已停用的工厂。
func (h *Handler) enableFactory(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	id, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 重新启用，才能继续认领和注销。
	out, err := h.svc.Factories.EnableFactory(r.Context(), bearer(r), id)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, out)
}

// 未认领则从名录拿掉；已认领只注销。
func (h *Handler) deleteFactory(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	id, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 未认领则拿掉，已认领只注销。
	out, err := h.svc.Factories.DeleteFactory(r.Context(), bearer(r), id)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 已经从名录拿掉就无正文，注销才回结果。
	if out == nil {
		// 成功无正文，调用方不要当成失败。
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, out)
}

// 邀请第二 WAN 管理员，一律拒绝。
func (h *Handler) inviteWANAdmin(w http.ResponseWriter, r *http.Request) {
	// 接住请求体，解析失败再拒绝。
	var req inviteReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 代管厂内数据一律拒绝，不把结果写成成功。
	writeErr(w, h.svc.Factories.InviteWANAdmin(r.Context(), bearer(r), req.LoginName))
}

// 代建厂内人员，一律拒绝。
func (h *Handler) createFactoryPerson(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	id, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req factoryPersonReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 代管厂内数据一律拒绝，不把结果写成成功。
	writeErr(w, h.svc.Factories.CreateFactoryPerson(r.Context(), bearer(r), id, req.LoginName))
}

// 代建厂内组织，一律拒绝。
func (h *Handler) createFactoryOrg(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	id, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req factoryOrgReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 代管厂内数据一律拒绝，不把结果写成成功。
	writeErr(w, h.svc.Factories.CreateFactoryOrg(r.Context(), bearer(r), id, req.Name))
}

// 代授厂内角色，一律拒绝。
func (h *Handler) grantFactoryRole(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	id, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req factoryRoleReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 代管厂内数据一律拒绝，不把结果写成成功。
	writeErr(w, h.svc.Factories.GrantFactoryRole(r.Context(), bearer(r), id, req.Target))
}

// 从 WAN 查厂内人员，一律拒绝。
func (h *Handler) listFactoryPeople(w http.ResponseWriter, r *http.Request) {
	// 从路径取出身份，不合法就拒绝这次操作。
	id, err := parsePathID(r)
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 代管厂内数据一律拒绝，不把结果写成成功。
	writeErr(w, h.svc.Factories.ListFactoryPeople(r.Context(), bearer(r), id))
}
