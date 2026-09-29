package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
)

var errInvalidID = errors.New("invalid id") // 路径或 JSON 里的 UUID 无法解析

// 出错时给前端的英文原因，细节不出网。
type errorBody struct {
	Error string `json:"error"` // 英文业务错误，前端再译
}

// 拒绝未知字段。
func decodeJSON(r *http.Request, dst any) error {
	// 用完就关上，避免连接或文件一直占着。
	defer r.Body.Close()
	// 按 JSON 读请求体，坏包交给上一层拒绝。
	dec := json.NewDecoder(r.Body)
	// 拒绝未知字段，避免多出来的人员字段混入。
	dec.DisallowUnknownFields()
	// 解码失败把原因交回，调用方再回坏请求。
	return dec.Decode(dst)
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

// 400 把解析错误回给前端。
func writeBadRequest(w http.ResponseWriter, err error) {
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
