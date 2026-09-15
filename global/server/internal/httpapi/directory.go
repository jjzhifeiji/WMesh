package httpapi

import (
	"net/http"
)

// 挂名录、建厂和明确拒绝的代管入口。
func (h *Handler) mountDirectory(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/directory", h.directory)
	mux.HandleFunc("POST /v1/factories", h.createFactory)
	mux.HandleFunc("POST /v1/factories/{id}/disable", h.disableFactory)
	mux.HandleFunc("POST /v1/factories/{id}/enable", h.enableFactory)
	mux.HandleFunc("DELETE /v1/factories/{id}", h.deleteFactory)
	mux.HandleFunc("POST /v1/invite-wan-admin", h.inviteWANAdmin)
	mux.HandleFunc("POST /v1/factories/{id}/people", h.createFactoryPerson)
	mux.HandleFunc("POST /v1/factories/{id}/orgs", h.createFactoryOrg)
	mux.HandleFunc("POST /v1/factories/{id}/roles", h.grantFactoryRole)
	mux.HandleFunc("GET /v1/factories/{id}/people", h.listFactoryPeople)
}

type createFactoryReq struct {
	Name      string `json:"name"`      // 工厂显示名
	SALogin   string `json:"saLogin"`   // 初始超管登录名
	SADisplay string `json:"saDisplay"` // 初始超管显示名
}

type inviteReq struct {
	LoginName string `json:"loginName"` // 被邀请登录名；此接口一律拒绝
}

type factoryPersonReq struct {
	LoginName string `json:"loginName"` // 厂内登录名；WAN 不得代建
}

type factoryOrgReq struct {
	Name string `json:"name"` // 组织名；WAN 不得代建
}

type factoryRoleReq struct {
	Target string `json:"target"` // 授权目标；WAN 不得代授
}

// 返回工厂名录和初始超管身份，不含厂内人员。
func (h *Handler) directory(w http.ResponseWriter, r *http.Request) {
	dir, err := h.svc.Factories.Directory(r.Context(), bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dir)
}

// 名录写下工厂并签发建厂码，不反打厂内网。
func (h *Handler) createFactory(w http.ResponseWriter, r *http.Request) {
	var req createFactoryReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	out, err := h.svc.Factories.CreateFactory(r.Context(), bearer(r), req.Name, req.SALogin, req.SADisplay)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// 停用工厂；在线则立刻推给厂端。
func (h *Handler) disableFactory(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	out, err := h.svc.Factories.DisableFactory(r.Context(), bearer(r), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// 重新启用已停用的工厂。
func (h *Handler) enableFactory(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	out, err := h.svc.Factories.EnableFactory(r.Context(), bearer(r), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// 未认领则从名录拿掉；已认领只注销。
func (h *Handler) deleteFactory(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	out, err := h.svc.Factories.DeleteFactory(r.Context(), bearer(r), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if out == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// 邀请第二 WAN 管理员，一律拒绝。
func (h *Handler) inviteWANAdmin(w http.ResponseWriter, r *http.Request) {
	var req inviteReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.Factories.InviteWANAdmin(r.Context(), bearer(r), req.LoginName))
}

// 代建厂内人员，一律拒绝。
func (h *Handler) createFactoryPerson(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	var req factoryPersonReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.Factories.CreateFactoryPerson(r.Context(), bearer(r), id, req.LoginName))
}

// 代建厂内组织，一律拒绝。
func (h *Handler) createFactoryOrg(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	var req factoryOrgReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.Factories.CreateFactoryOrg(r.Context(), bearer(r), id, req.Name))
}

// 代授厂内角色，一律拒绝。
func (h *Handler) grantFactoryRole(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	var req factoryRoleReq
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.Factories.GrantFactoryRole(r.Context(), bearer(r), id, req.Target))
}

// 从 WAN 查厂内人员，一律拒绝。
func (h *Handler) listFactoryPeople(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	writeErr(w, h.svc.Factories.ListFactoryPeople(r.Context(), bearer(r), id))
}
