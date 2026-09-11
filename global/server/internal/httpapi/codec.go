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

var errInvalidID = errors.New("invalid id")

type errorBody struct {
	Error string `json:"error"` // 英文业务错误，前端再译
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

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

func statusOf(err error) int {
	switch {
	case errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrInvalidCredentials),
		errors.Is(err, domain.ErrAccountPending), errors.Is(err, domain.ErrAccountDisabled),
		errors.Is(err, domain.ErrInvalidActivation), errors.Is(err, domain.ErrSessionExpired):
		return http.StatusUnauthorized
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrLastAdmin),
		errors.Is(err, domain.ErrInitialSAExists):
		return http.StatusForbidden
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrWANAdminExists), errors.Is(err, domain.ErrLoginNameTaken),
		errors.Is(err, domain.ErrAlreadyActivated), errors.Is(err, domain.ErrDuplicateAssignment),
		errors.Is(err, domain.ErrDuplicateRoleGrant), errors.Is(err, domain.ErrDuplicateSession),
		errors.Is(err, domain.ErrClientBound), errors.Is(err, domain.ErrClientKeyTaken),
		errors.Is(err, domain.ErrRevisionConflict):
		return http.StatusConflict
	case errors.Is(err, domain.ErrFactoryBootstrap):
		return http.StatusBadGateway
	case errors.Is(err, domain.ErrInvalidKey), errors.Is(err, domain.ErrUnbound),
		isDomain(err), errors.Is(err, errInvalidID):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func isDomain(err error) bool {
	for _, t := range []error{
		domain.ErrCycle, domain.ErrWorkContext, domain.ErrMultiParent, domain.ErrInvalidRoleScope,
		domain.ErrDisabledOrgType, domain.ErrDisabledOrgUnit, domain.ErrHasActiveUnits, domain.ErrHasActiveChildren,
		domain.ErrIntegrity, domain.ErrAssetNotAvailable, domain.ErrAssetNotCopyable, domain.ErrAssetDependency,
	} {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}

func parsePathID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, errInvalidID
	}
	return id, nil
}
