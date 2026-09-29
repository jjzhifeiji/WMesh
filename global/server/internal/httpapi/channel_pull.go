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

// 该厂当前该有的指令，不含正文。
type channelIndexResp struct {
	Cmds []service.Cmd `json:"cmds"` // 当前该厂该有的指令，无正文
}

// 内容租约钥和到期时间，按云端钟。
type channelLeaseResp struct {
	Typ      string `json:"typ"`      // lease
	Lease    []byte `json:"lease"`    // 内容租约钥 L
	NotAfter string `json:"notAfter"` // 到期 RFC3339，WAN 钟
}

// 用厂钥签名头钉死工厂身份。
func (h *Handler) factoryProof(r *http.Request) (uuid.UUID, error) {
	// 读取厂身份，不是稳定号就当未授权。
	fid, err := uuid.Parse(r.Header.Get("X-WMesh-Factory"))
	// 失败把原因交回去，避免留下残缺结果。
	if err != nil {
		return uuid.Nil, domain.ErrUnauthorized
	}
	// 带上签名时刻，过期会被拒绝。
	unix, err := strconv.ParseInt(r.Header.Get("X-WMesh-Time"), 10, 64)
	// 失败把原因交回去，避免留下残缺结果。
	if err != nil {
		return uuid.Nil, domain.ErrUnauthorized
	}
	// 带上厂钥签名，对不上就未授权。
	sig, err := base64.StdEncoding.DecodeString(r.Header.Get("X-WMesh-Sign"))
	// 失败把原因交回去，避免留下残缺结果。
	if err != nil {
		return uuid.Nil, domain.ErrUnauthorized
	}
	// 厂钥证明没过就拒绝，不当成已认领厂。
	if err := h.svc.Channel.VerifyFactoryProof(r.Context(), fid, unix, sig); err != nil {
		return uuid.Nil, err
	}
	return fid, nil
}

// 回连或进页补拉当前指令清单。
func (h *Handler) channelIndex(w http.ResponseWriter, r *http.Request) {
	// 留下厂或资产身份，后面用来认领或升档。
	fid, err := h.factoryProof(r)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 读取种类，空则由服务决定是否拒绝。
	cmds, err := h.svc.IndexForFactory(r.Context(), fid, r.URL.Query().Get("kind"))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 没有指令时回空列表，避免被当成失败。
	if cmds == nil {
		// 没有指令时改成空列表，避免被当成失败。
		cmds = []service.Cmd{}
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, channelIndexResp{Cmds: cmds})
}

// 签发或续期内容租约。
func (h *Handler) channelLease(w http.ResponseWriter, r *http.Request) {
	// 留下厂或资产身份，后面用来认领或升档。
	fid, err := h.factoryProof(r)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 签发或续期内容租约，厂钥不对就拒绝。
	lease, err := h.svc.IssueContentLease(r.Context(), fid)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, channelLeaseResp{
		Typ: service.CmdLease, Lease: lease.Key, NotAfter: lease.NotAfter.UTC().Format(time.RFC3339Nano),
	})
}

// 拉一条密封闭包。
func (h *Handler) pullClosure(w http.ResponseWriter, r *http.Request) {
	// 留下厂或资产身份，后面用来认领或升档。
	fid, err := h.factoryProof(r)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 解析稳定身份，格式不对就拒绝。
	assetID, err := uuid.Parse(r.PathValue("assetId"))
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 拉一条密封闭包，无租约或无权则拒绝。
	snap, err := h.svc.Closure.PullClosureForFactory(r.Context(), fid, assetID)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, snap)
}

// 拉一份内容模版。
func (h *Handler) pullTemplate(w http.ResponseWriter, r *http.Request) {
	// 留下厂或资产身份，后面用来认领或升档。
	fid, err := h.factoryProof(r)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 解析稳定身份，格式不对就拒绝。
	tid, err := uuid.Parse(r.PathValue("templateId"))
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, errInvalidID)
		return
	}
	// 拉一份模版，未认领就拒绝。
	snap, err := h.svc.Templates.PullTemplateForFactory(r.Context(), fid, tid)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, snap)
}

// 已认领厂按版本拉当前最高厂包或客户端包。
func (h *Handler) pullSoftware(w http.ResponseWriter, r *http.Request) {
	// 留下厂或资产身份，后面用来认领或升档。
	fid, err := h.factoryProof(r)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 读取种类，空则由服务决定是否拒绝。
	kind := r.URL.Query().Get("kind")
	// 取出查询或路径，供后面判定。
	version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, domain.ErrNotFound)
		return
	}
	// 按版本拉包，没有这份就当找不到。
	offer, err := h.svc.Updates.PullSoftware(r.Context(), fid, kind, version)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, offer)
}
