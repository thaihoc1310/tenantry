package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

// statusOf is the only place an error code meets an HTTP status.
var statusOf = map[apperr.Code]int{
	apperr.Validation:       http.StatusBadRequest,
	apperr.Unauthenticated:  http.StatusUnauthorized,
	apperr.Forbidden:        http.StatusForbidden,
	apperr.NotFound:         http.StatusNotFound,
	apperr.Conflict:         http.StatusConflict,
	apperr.MethodNotAllowed: http.StatusMethodNotAllowed,

	apperr.IdempotencyKeyReused: http.StatusUnprocessableEntity,
	apperr.RateLimited:          http.StatusTooManyRequests,

	apperr.SlugTaken:         http.StatusConflict,
	apperr.OrgRequestPending: http.StatusConflict,
	apperr.ClusterNotReady:   http.StatusConflict,
	apperr.OrgNotActive:      http.StatusConflict,
}

// writeError answers with an ErrorResponse.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, body := describe(r, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// describe turns err into a status and body. An *apperr.Error is the
// caller's fault and is shown as is; anything else is logged and hidden
// behind INTERNAL.
func describe(r *http.Request, err error) (int, oapi.ErrorResponse) {
	body := oapi.ErrorResponse{Code: oapi.INTERNAL, Message: "internal error", RequestId: requestID(r.Context())}
	if e, ok := apperr.As(err); ok && statusOf[e.Code] != 0 {
		body.Code, body.Message = oapi.ErrorCode(e.Code), e.Message
		if e.Details != nil {
			body.Details = &e.Details
		}
		return statusOf[e.Code], body
	}
	slog.ErrorContext(r.Context(), "request failed", "err", err, "route", routePattern(r), "request_id", body.RequestId)
	return http.StatusInternalServerError, body
}
