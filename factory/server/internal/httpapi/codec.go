package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
)

var errInvalidID = errors.New("invalid id") // 路径或 JSON 里的 UUID 无法解析

// 错误回包，只放可以展示给调用方的原因。
type errorBody struct {
	Error string `json:"error"` // 英文业务错误，前端再译
}

// 拒绝未知字段。
func decodeJSON(r *http.Request, dst any) error {
	// 读完就关闭正文，避免连接一直被占。
	defer r.Body.Close()
	// 按文本对象读取正文。
	dec := json.NewDecoder(r.Body)
	// 多出来的字段直接拒绝，避免悄悄忽略。
	dec.DisallowUnknownFields()
	// 解进目标，失败就把原因交回调用方。
	return dec.Decode(dst)
}

// 取出会话令牌，不校验。
func bearer(r *http.Request) string {
	// 去掉令牌前缀，是否有效交给业务判断。
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// 写出软件包字节，支持 Range 续传。
func writeBytes(w http.ResponseWriter, r *http.Request, body []byte) {
	// 按安装包声明类型，而不是文本对象。
	w.Header().Set("Content-Type", "application/octet-stream")
	// 按区间写出字节，支持没下完再续。
	http.ServeContent(w, r, "client.apk", time.Time{}, bytes.NewReader(body))
}

// 写出 JSON 响应。
func writeJSON(w http.ResponseWriter, code int, v any) {
	// 声明正文是文本对象，便于按字段解析。
	w.Header().Set("Content-Type", "application/json")
	// 先落下状态码，再写正文。
	w.WriteHeader(code)
	// 写成文本对象，失败也不再改状态码。
	_ = json.NewEncoder(w).Encode(v)
}

// 400 把解析错误回给前端。
func writeBadRequest(w http.ResponseWriter, err error) {
	// 通过之后把结果回给调用方。
	writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
}

// writeErr 只把业务错误原话给前端；5xx 细节只进日志，不出网。
func writeErr(w http.ResponseWriter, err error) {
	// 默认按正常返回，库探活失败再改。
	code := statusOf(err)
	// 先取出原因，内部错误再换成笼统话。
	msg := err.Error()
	// 不是预期则本测失败。
	if code == http.StatusInternalServerError {
		// 崩溃只记日志，响应里不带栈。
		slog.Error("request failed", "err", err)
		// 对外只说内部错误，细节留在日志里。
		msg = "internal error"
	}
	// 通过之后把结果回给调用方。
	writeJSON(w, code, errorBody{Error: msg})
}

// 把本侧业务错误映成 HTTP 状态；对不上的一律 500。
func statusOf(err error) int {
	// 按业务错误选择状态，对不上当内部故障。
	switch {
	// 未登录或凭证无效时回未授权。
	case errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrInvalidCredentials),
		errors.Is(err, domain.ErrAccountPending), errors.Is(err, domain.ErrAccountDisabled),
		errors.Is(err, domain.ErrInvalidActivation), errors.Is(err, domain.ErrSessionExpired),
		errors.Is(err, domain.ErrInvalidEnrollment):
		return http.StatusUnauthorized
	// 平台暂时不可达，回上游故障。
	case errors.Is(err, domain.ErrWANUnreachable):
		return http.StatusBadGateway
	// 无权，或动到最后一名超管，则回禁止。
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrLastAdmin),
		errors.Is(err, domain.ErrInitialSAExists), errors.Is(err, domain.ErrFactoryDisabled),
		errors.Is(err, domain.ErrFactoryRetired), errors.Is(err, domain.ErrContentLeaseExpired):
		return http.StatusForbidden
	// 目标不存在时回找不到。
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	// 重名、重复或仍被引用时回冲突。
	case errors.Is(err, domain.ErrWANAdminExists), errors.Is(err, domain.ErrLoginNameTaken),
		errors.Is(err, domain.ErrAlreadyActivated), errors.Is(err, domain.ErrDuplicateAssignment),
		errors.Is(err, domain.ErrDuplicateRoleGrant), errors.Is(err, domain.ErrDuplicateSession),
		errors.Is(err, domain.ErrReferenced), errors.Is(err, domain.ErrRevisionConflict),
		errors.Is(err, domain.ErrSigningKeyExists), errors.Is(err, domain.ErrSoftwareInstallFailed),
		errors.Is(err, domain.ErrDeviceSerialTaken), errors.Is(err, domain.ErrDuplicateName):
		return http.StatusConflict
	// 修订过期或参数不合法时回非法请求。
	case errors.Is(err, domain.ErrStaleRevision), errors.Is(err, domain.ErrBindingVoid),
		errors.Is(err, domain.ErrInvalidKey), errors.Is(err, domain.ErrClientKeyMismatch),
		errors.Is(err, domain.ErrInvalidName),
		errors.Is(err, domain.ErrDeviceSerialRequired), errors.Is(err, domain.ErrDeviceSerialMismatch),
		isDomain(err), errors.Is(err, errInvalidID):
		return http.StatusBadRequest
	// 对不上的错误一律当成内部故障。
	default:
		return http.StatusInternalServerError
	}
}

// 校验类业务错误一律当 400，避免把约束原文当 500。
func isDomain(err error) bool {
	// 逐项对照校验错误，命中就不当内部故障。
	for _, t := range []error{
		// 这些都属于参数或依赖不合法，应按非法请求。
		domain.ErrCycle, domain.ErrWorkContext, domain.ErrMultiParent, domain.ErrInvalidRoleScope,
		domain.ErrDisabledOrgUnit, domain.ErrHasActiveChildren,
		domain.ErrIntegrity, domain.ErrAssetNotAvailable, domain.ErrAssetNotCopyable, domain.ErrAssetDependency,
		domain.ErrInvalidWeldKind, domain.ErrWeldKindMismatch,
		domain.ErrTemplateInvalid, domain.ErrAssetCodeMissing, domain.ErrAssetCodeConflict, domain.ErrOriginCodeExhausted,
	} {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}

// 空字符串当没传；非法 UUID 拒绝。
func parseOptUUID(p *string) (*uuid.UUID, error) {
	// 没传编号就当不筛选，不因此报错。
	if p == nil || *p == "" {
		return nil, nil
	}
	// 可选编号非法则拒绝，不能当成没传。
	id, err := uuid.Parse(*p)
	// 解析失败则拒绝，不用这个残缺的值继续。
	if err != nil {
		return nil, errInvalidID
	}
	return &id, nil
}
