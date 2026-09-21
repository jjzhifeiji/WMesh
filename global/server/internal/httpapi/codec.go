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

type errorBody struct {
	Error string `json:"error"` // 英文业务错误，前端再译
}

// 拒绝未知字段。
func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// 取出会话令牌，不校验。
func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// 写出 JSON 响应。
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// 400 把解析错误回给前端。
func writeBadRequest(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
}

// writeErr 只把业务错误原话给前端；5xx 细节只进日志，不出网。
func writeErr(w http.ResponseWriter, err error) {
	code := statusOf(err)
	msg := err.Error()
	switch {
	case code == http.StatusInternalServerError:
		slog.Error("request failed", "err", err)
		msg = "internal error"
	case errors.Is(err, domain.ErrFactoryBootstrap):
		slog.Error("factory bootstrap failed", "err", err)
		msg = domain.ErrFactoryBootstrap.Error()
	}
	writeJSON(w, code, errorBody{Error: msg})
}

// 把本侧业务错误映成 HTTP 状态；对不上的一律 500。
func statusOf(err error) int {
	switch {
	case errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrInvalidCredentials),
		errors.Is(err, domain.ErrAccountPending), errors.Is(err, domain.ErrAccountDisabled),
		errors.Is(err, domain.ErrInvalidActivation), errors.Is(err, domain.ErrSessionExpired),
		errors.Is(err, domain.ErrInvalidEnrollment):
		return http.StatusUnauthorized
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrLastAdmin),
		errors.Is(err, domain.ErrInitialSAExists), errors.Is(err, domain.ErrFactoryDisabled),
		errors.Is(err, domain.ErrFactoryRetired), errors.Is(err, domain.ErrContentLeaseExpired):
		return http.StatusForbidden
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrWANAdminExists), errors.Is(err, domain.ErrLoginNameTaken),
		errors.Is(err, domain.ErrAlreadyActivated), errors.Is(err, domain.ErrDuplicateAssignment),
		errors.Is(err, domain.ErrDuplicateRoleGrant), errors.Is(err, domain.ErrDuplicateSession),
		errors.Is(err, domain.ErrClientBound), errors.Is(err, domain.ErrClientKeyTaken),
		errors.Is(err, domain.ErrRevisionConflict), errors.Is(err, domain.ErrFactoryKeyExists),
		errors.Is(err, domain.ErrReferenced), errors.Is(err, domain.ErrDeviceSerialTaken),
		errors.Is(err, domain.ErrSoftwareInstallFailed), errors.Is(err, domain.ErrDuplicateName):
		return http.StatusConflict
	case errors.Is(err, domain.ErrFactoryBootstrap):
		return http.StatusBadGateway
	case errors.Is(err, domain.ErrFactoryOffline):
		return http.StatusServiceUnavailable
	case errors.Is(err, domain.ErrInvalidKey), errors.Is(err, domain.ErrUnbound),
		errors.Is(err, domain.ErrInvalidName), errors.Is(err, domain.ErrDeviceSerialRequired),
		isDomain(err), errors.Is(err, errInvalidID):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// 校验类业务错误一律当 400，避免把约束原文当 500。
func isDomain(err error) bool {
	for _, t := range []error{
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
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, errInvalidID
	}
	return id, nil
}
