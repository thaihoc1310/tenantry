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

	apperr.SlugTaken:         http.StatusConflict,
	apperr.OrgRequestPending: http.StatusConflict,
}

// writeError answers with an ErrorResponse. An *apperr.Error is the caller's
// fault and is shown as is; anything else is logged and hidden behind INTERNAL.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	body := oapi.ErrorResponse{Code: oapi.INTERNAL, Message: "internal error", RequestId: requestID(r.Context())}
	status := http.StatusInternalServerError
	if e, ok := apperr.As(err); ok && statusOf[e.Code] != 0 {
		body.Code, body.Message, status = oapi.ErrorCode(e.Code), e.Message, statusOf[e.Code]
		if e.Details != nil {
			body.Details = &e.Details
		}
	} else {
		slog.ErrorContext(r.Context(), "request failed", "err", err, "route", routePattern(r), "request_id", body.RequestId)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
