package httpapi

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

type channelIndexResp struct {
	Cmds []service.Cmd `json:"cmds"` // 当前该厂该有的指令，无正文
}

type channelLeaseResp struct {
	Typ      string `json:"typ"`      // lease
	Lease    []byte `json:"lease"`     // 内容租约钥 L
	NotAfter string `json:"notAfter"` // 到期 RFC3339，WAN 钟
}

// 用厂钥签名头钉死工厂身份。
func (h *Handler) factoryProof(r *http.Request) (uuid.UUID, error) {
	fid, err := uuid.Parse(r.Header.Get("X-WMesh-Factory"))
	if err != nil {
		return uuid.Nil, domain.ErrUnauthorized
	}
	unix, err := strconv.ParseInt(r.Header.Get("X-WMesh-Time"), 10, 64)
	if err != nil {
		return uuid.Nil, domain.ErrUnauthorized
	}
	sig, err := base64.StdEncoding.DecodeString(r.Header.Get("X-WMesh-Sign"))
	if err != nil {
		return uuid.Nil, domain.ErrUnauthorized
	}
	if err := h.svc.Channel.VerifyFactoryProof(r.Context(), fid, unix, sig); err != nil {
		return uuid.Nil, err
	}
	return fid, nil
}

// 回连或进页补拉当前指令清单。
func (h *Handler) channelIndex(w http.ResponseWriter, r *http.Request) {
	fid, err := h.factoryProof(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	cmds, err := h.svc.IndexForFactory(r.Context(), fid, r.URL.Query().Get("kind"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if cmds == nil {
		cmds = []service.Cmd{}
	}
	writeJSON(w, http.StatusOK, channelIndexResp{Cmds: cmds})
}

// 签发或续期内容租约。
func (h *Handler) channelLease(w http.ResponseWriter, r *http.Request) {
	fid, err := h.factoryProof(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	lease, err := h.svc.IssueContentLease(r.Context(), fid)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, channelLeaseResp{
		Typ: service.CmdLease, Lease: lease.Key, NotAfter: lease.NotAfter.UTC().Format(time.RFC3339Nano),
	})
}

// 拉一条密封闭包。
func (h *Handler) pullClosure(w http.ResponseWriter, r *http.Request) {
	fid, err := h.factoryProof(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	assetID, err := uuid.Parse(r.PathValue("assetId"))
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	snap, err := h.svc.Closure.PullClosureForFactory(r.Context(), fid, assetID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// 拉一份内容模版。
func (h *Handler) pullTemplate(w http.ResponseWriter, r *http.Request) {
	fid, err := h.factoryProof(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	tid, err := uuid.Parse(r.PathValue("templateId"))
	if err != nil {
		writeBadRequest(w, errInvalidID)
		return
	}
	snap, err := h.svc.Templates.PullTemplateForFactory(r.Context(), fid, tid)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// 拉已下到本厂的一份软件包。
func (h *Handler) pullSoftware(w http.ResponseWriter, r *http.Request) {
	fid, err := h.factoryProof(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	kind := r.URL.Query().Get("kind")
	version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
	if err != nil {
		writeErr(w, domain.ErrNotFound)
		return
	}
	snap, err := h.svc.Updates.PullSoftwareForFactory(r.Context(), fid, kind, version)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}
