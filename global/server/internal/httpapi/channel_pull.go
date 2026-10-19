package httpapi

import (
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

// 该厂当前该有的指令，不含正文。
type channelIndexResp struct {
	Cmds         []service.Cmd `json:"cmds"`                   // 当前该厂该有的指令，无正文
	WANPublicKey []byte        `json:"wanPublicKey,omitempty"` // 下行指令验签公钥，厂端经 HTTPS 钉住
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
	writeJSON(w, http.StatusOK, channelIndexResp{Cmds: cmds, WANPublicKey: h.svc.Channel.ChannelPublicKey()})
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
	// 新厂端要原始字节就流式转发，旧厂端照旧回 JSON。
	if wantsRawSoftware(r) {
		// 流式回包，支持从断点续传。
		h.streamSoftware(w, r, fid, kind, version)
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

// 软件包流式回包时带的响应头，厂端据此核摘要、对版本。
const (
	softwareDigestHeader  = "X-WMesh-Digest"  // 包文件 SHA-256，十六进制小写
	softwareVersionHeader = "X-WMesh-Version" // 包的单调整数版本
)

// 厂端声明要原始字节才走流式；没声明的旧厂端照旧回 JSON。
func wantsRawSoftware(r *http.Request) bool {
	// 只看 Accept 里有没有原始字节类型。
	return strings.Contains(r.Header.Get("Accept"), "application/octet-stream")
}

// 从对象存储流式转发原始字节；摘要和版本放响应头，带 Range 回 206。
func (h *Handler) streamSoftware(w http.ResponseWriter, r *http.Request, fid uuid.UUID, kind string, version int64) {
	// 起点不在零的是续传，不再记允许审计。
	pack, err := h.svc.Updates.OpenSoftware(r.Context(), fid, kind, version, rangeResumes(r.Header.Get("Range")))
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 写完就关上对象流，连接还给存储。
	defer pack.Body.Close()
	// 兜底清掉读截止；net/http 起后台读时已清过，这里不靠它。
	_ = http.NewResponseController(w).SetReadDeadline(time.Time{})
	// 摘要按十六进制写，头里和文件名里都好比。
	sum := hex.EncodeToString(pack.Digest)
	// 先定好类型，免得为猜类型多读一段再回头。
	w.Header().Set("Content-Type", "application/octet-stream")
	// 带上摘要，厂端下完据此核对。
	w.Header().Set(softwareDigestHeader, sum)
	// 带上版本，厂端核对拉到的是哪一版。
	w.Header().Set(softwareVersionHeader, strconv.FormatInt(pack.Version, 10))
	// 用摘要当实体标签，带 If-Range 的续传也能对上。
	w.Header().Set("ETag", `"`+sum+`"`)
	// 交给标准件处理整包、续传和越界。
	http.ServeContent(w, r, "", time.Time{}, pack.Body)
}

// 续传请求的起点不在零；没带或格式不对都不算续传。
func rangeResumes(header string) bool {
	// 只认字节单位的区间。
	spec, ok := strings.CutPrefix(strings.TrimSpace(header), "bytes=")
	// 没带或不是字节区间就不算续传。
	if !ok {
		return false
	}
	// 多段区间只看第一段的起点。
	first, _, _ := strings.Cut(spec, ",")
	// 拆出起点，没有横杠就不是合法区间。
	start, _, ok := strings.Cut(first, "-")
	// 不是合法区间就不算续传。
	if !ok {
		return false
	}
	// 去掉空白再看起点。
	start = strings.TrimSpace(start)
	// 只要末尾一段也是接着下，算续传。
	if start == "" {
		return true
	}
	// 起点收成整数，认不出就不算续传。
	n, err := strconv.ParseInt(start, 10, 64)
	return err == nil && n > 0
}
