package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
)

var errInvalidID = errors.New("invalid id") // 路径或 JSON 里的 UUID 无法解析

var errBodyTooLarge = errors.New("request body too large") // 正文超过本入口上限，回 413

// 正文上限与大正文读时限；超限回 413，匿名者占不住内存。
const (
	maxJSONBody     = 1 << 20  // 普通接口一兆，最大单份工程约 67KB
	maxBulkJSONBody = 32 << 20 // 旧文件导入，验过管理员才按此读
	// 焊汇总整表上送不限行数，只按 64MiB 挡异常包。
	maxWeldSummaryBody = 64 << 20
	bulkReadWindow     = 5 * time.Minute // 验过身份的大正文放宽读时限
)

// 出错时给前端的英文原因，细节不出网。
type errorBody struct {
	Error string `json:"error"` // 英文业务错误，前端再译
}

// 拒绝未知字段；正文按普通接口上限读。
func decodeJSON(r *http.Request, dst any) error {
	// 普通接口统一一兆上限，超了回 413。
	return decodeJSONLimit(r, dst, maxJSONBody)
}

// 按给定上限解码 JSON，拒绝未知字段；超限交回 MaxBytesError。
func decodeJSONLimit(r *http.Request, dst any, limit int64) error {
	// 用完就关上，避免连接或文件一直占着。
	defer r.Body.Close()
	// 声明长度已超上限就一字节不读，直接拒绝。
	if r.ContentLength > limit {
		return &http.MaxBytesError{Limit: limit}
	}
	// 按 JSON 读请求体，读到上限就停，坏包交给上一层拒绝。
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, limit))
	// 拒绝未知字段，避免多出来的人员字段混入。
	dec.DisallowUnknownFields()
	// 解码失败把原因交回，调用方再回坏请求。
	return dec.Decode(dst)
}

// 读完正文直接扔掉，不进内存；最多读到上限，好让客户端收到干净回包。
func discardBody(r *http.Request, limit int64) {
	// 声明长度已超上限，读了也白读，留给服务器断开。
	if r.ContentLength > limit {
		return
	}
	// 边读边扔，读到上限就停，不占内存。
	_, _ = io.Copy(io.Discard, http.MaxBytesReader(nil, r.Body, limit))
}

// 验过身份后单独放宽这次请求的读时限；不支持就沿用全局时限。
func extendRead(w http.ResponseWriter, d time.Duration) {
	// 穿过包装层改底层连接的读时限，出错就退回全局时限。
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(d))
}

// 取出会话令牌，不校验。
func bearer(r *http.Request) string {
	// 取出令牌原文，不在这里校验是否有效。
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// 写出 JSON 响应。
func writeJSON(w http.ResponseWriter, code int, v any) {
	// 标明 JSON，前端才按错误体解析。
	w.Header().Set("Content-Type", "application/json")
	// 先定状态码，再写正文。
	w.WriteHeader(code)
	// 把结果编成 JSON 交回调用方。
	_ = json.NewEncoder(w).Encode(v)
}

// 400 把解析错误回给前端；正文超限改回 413。
func writeBadRequest(w http.ResponseWriter, err error) {
	// 接住超限错误，JSON 和表单都可能包着它。
	var tooLarge *http.MaxBytesError
	// 正文超限单独回 413，前端才知道是太大。
	if errors.As(err, &tooLarge) {
		// 只回约定句子，不带上限细节。
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody{Error: errBodyTooLarge.Error()})
		return
	}
	// 把不合法原因按坏请求交回调用方。
	writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
}

// writeErr 只把业务错误原话给前端；5xx 细节只进日志，不出网。
func writeErr(w http.ResponseWriter, err error) {
	// 先把业务错误映成状态码。
	code := statusOf(err)
	// 默认把业务原话给前端，内部故障再改写。
	msg := err.Error()
	// 内部故障和建厂引导要改写对外的句子。
	switch {
	// 内部故障只记日志，对外改成笼统失败。
	case code == http.StatusInternalServerError:
		// 内部原因只进日志，不写进响应。
		slog.Error("request failed", "err", err)
		// 对外改成笼统或约定句子，内部原因只留日志。
		msg = "internal error"
	case errors.Is(err, domain.ErrFactoryBootstrap):
		// 建厂引导失败只记日志，对外仍用约定句子。
		slog.Error("factory bootstrap failed", "err", err)
		// 对外改成笼统或约定句子，内部原因只留日志。
		msg = domain.ErrFactoryBootstrap.Error()
	}
	// 把结果交回调用方。
	writeJSON(w, code, errorBody{Error: msg})
}

// 把本侧业务错误映成 HTTP 状态；对不上的一律 500。
func statusOf(err error) int {
	// 按业务错误选状态码，对不上当内部故障。
	switch {
	// 未登录、凭证错或会话失效，一律未授权。
	case errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrInvalidCredentials),
		errors.Is(err, domain.ErrAccountPending), errors.Is(err, domain.ErrAccountDisabled),
		errors.Is(err, domain.ErrInvalidActivation), errors.Is(err, domain.ErrSessionExpired),
		errors.Is(err, domain.ErrInvalidEnrollment):
		return http.StatusUnauthorized
	// 没权限、最后超管或厂已停用，一律禁止。
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrLastAdmin),
		errors.Is(err, domain.ErrInitialSAExists), errors.Is(err, domain.ErrFactoryDisabled),
		errors.Is(err, domain.ErrFactoryRetired), errors.Is(err, domain.ErrContentLeaseExpired):
		return http.StatusForbidden
	// 找不到资源，回找不到而不是内部故障。
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	// 重名、冲突或仍被引用，一律拒绝覆盖。
	case errors.Is(err, domain.ErrWANAdminExists), errors.Is(err, domain.ErrLoginNameTaken),
		errors.Is(err, domain.ErrAlreadyActivated), errors.Is(err, domain.ErrDuplicateAssignment),
		errors.Is(err, domain.ErrDuplicateRoleGrant), errors.Is(err, domain.ErrDuplicateSession),
		errors.Is(err, domain.ErrClientBound), errors.Is(err, domain.ErrClientKeyTaken),
		errors.Is(err, domain.ErrRevisionConflict), errors.Is(err, domain.ErrFactoryKeyExists),
		errors.Is(err, domain.ErrReferenced), errors.Is(err, domain.ErrDeviceSerialTaken),
		errors.Is(err, domain.ErrSoftwareInstallFailed), errors.Is(err, domain.ErrDuplicateName):
		return http.StatusConflict
	// 建厂引导失败当上游故障，细节只进日志。
	case errors.Is(err, domain.ErrFactoryBootstrap):
		return http.StatusBadGateway
	// 厂不在线，暂时不能问也不能下发。
	case errors.Is(err, domain.ErrFactoryOffline):
		return http.StatusServiceUnavailable
	// 登录或匿名入口超频，回请求过多。
	case errors.Is(err, domain.ErrTooManyAttempts), errors.Is(err, domain.ErrTooManyRequests):
		return http.StatusTooManyRequests
	// 参数或钥不合法，以及校验失败，一律坏请求。
	case errors.Is(err, domain.ErrInvalidKey), errors.Is(err, domain.ErrUnbound),
		errors.Is(err, domain.ErrInvalidName), errors.Is(err, domain.ErrDeviceSerialRequired),
		isDomain(err), errors.Is(err, errInvalidID):
		return http.StatusBadRequest
	// 对不上的错误当内部故障，不把原文抛出。
	default:
		return http.StatusInternalServerError
	}
}

// 校验类业务错误一律当 400，避免把约束原文当 500。
func isDomain(err error) bool {
	// 逐项对照校验类错误，命中就不当内部故障。
	for _, t := range []error{
		// 这些都是校验失败，应回坏请求而不是内部故障。
		domain.ErrCycle, domain.ErrWorkContext, domain.ErrMultiParent, domain.ErrInvalidRoleScope,
		domain.ErrDisabledOrgType, domain.ErrDisabledOrgUnit, domain.ErrHasActiveUnits, domain.ErrHasActiveChildren,
		domain.ErrIntegrity, domain.ErrAssetNotAvailable, domain.ErrAssetNotCopyable, domain.ErrAssetDependency,
		domain.ErrInvalidWeldKind, domain.ErrWeldKindMismatch,
		domain.ErrTemplateInvalid, domain.ErrAssetCodeMissing, domain.ErrAssetCodeConflict, domain.ErrOriginCodeExhausted,
		domain.ErrStaleRevision,
	} {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}

// 解析路径里的工厂身份。
func parsePathID(r *http.Request) (uuid.UUID, error) {
	// 解析稳定身份，格式不对就拒绝。
	id, err := uuid.Parse(r.PathValue("id"))
	// 失败把原因交回去，避免留下残缺结果。
	if err != nil {
		return uuid.Nil, errInvalidID
	}
	return id, nil
}
