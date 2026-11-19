// Package httpapi serves /api/v1: it implements the oapi-codegen strict
// interface generated from openapi/tenantry.v1.yaml and owns the HTTP-only
// concerns around it (request id, authentication, error bodies).
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// DevUserHeader names the caller by user id. It is the only way to log in
// until the OIDC session arrives in M3, and only works when Options.DevAuth is set.
const DevUserHeader = "X-Tenantry-Dev-User"

type Options struct {
	DevAuth bool // TENANTRY_AUTH_DEV_HEADER=true; never on outside local dev
}

type server struct {
	q *store.Queries
}

// Register mounts every /api/v1 operation on mux.
func Register(mux *http.ServeMux, db store.DBTX, opts Options) {
	q := store.New(db)
	api := oapi.NewStrictHandlerWithOptions(server{q: q}, nil, oapi.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, r, http.StatusBadRequest, oapi.VALIDATIONFAILED, err.Error())
		},
		ResponseErrorHandlerFunc: internalError,
	})
	oapi.HandlerWithOptions(api, oapi.StdHTTPServerOptions{
		BaseURL:     "/api/v1",
		BaseRouter:  mux,
		Middlewares: []oapi.MiddlewareFunc{authenticate(q, opts)},
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, r, http.StatusBadRequest, oapi.VALIDATIONFAILED, err.Error())
		},
	})
}

func (s server) GetOrganization(ctx context.Context, req oapi.GetOrganizationRequestObject) (oapi.GetOrganizationResponseObject, error) {
	row, err := s.q.GetOrganizationForMember(ctx, store.GetOrganizationForMemberParams{
		UserID:   userID(ctx),
		IDOrSlug: req.OrgId,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return oapi.GetOrganization404JSONResponse(errorBody(ctx, oapi.NOTFOUND, "organization not found")), nil
	}
	if err != nil {
		return nil, err
	}
	return oapi.GetOrganization200JSONResponse(toOrganization(row.Organization, row.Role)), nil
}

// ponytail: PATCH stays in the contract but lands in the next step; until then it answers 501.
func (s server) PatchOrganization(context.Context, oapi.PatchOrganizationRequestObject) (oapi.PatchOrganizationResponseObject, error) {
	return nil, errNotImplemented
}

var errNotImplemented = errors.New("not implemented yet")

func toOrganization(o store.Organization, role string) oapi.Organization {
	r := oapi.OrgRole(role)
	return oapi.Organization{
		Id:          o.ID,
		Slug:        o.Slug,
		DisplayName: o.DisplayName,
		Description: o.Description,
		OwnerUserId: o.OwnerUserID,
		Status:      oapi.OrganizationStatus(o.Status),
		ReviewNote:  o.ReviewNote,
		CreatedAt:   o.CreatedAt,
		MyRole:      &r,
	}
}

// --- context: request id and caller ---

type ctxKey int

const (
	requestIDKey ctxKey = iota
	userIDKey
)

// WithRequestID tags every request with an id that is echoed in the
// X-Request-Id header and in error bodies, so a user report can be matched to logs.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			id = rand.Text()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func requestID(ctx context.Context) string { s, _ := ctx.Value(requestIDKey).(string); return s }
func userID(ctx context.Context) string    { s, _ := ctx.Value(userIDKey).(string); return s }

// authenticate puts the caller's user id in the context or answers 401. The
// user must already exist and be Active: a disabled account loses access at once.
func authenticate(q *store.Queries, opts Options) oapi.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(DevUserHeader)
			if !opts.DevAuth || id == "" {
				writeError(w, r, http.StatusUnauthorized, oapi.UNAUTHENTICATED, "login required")
				return
			}
			if _, err := q.GetActiveUserID(r.Context(), id); err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					internalError(w, r, err)
					return
				}
				writeError(w, r, http.StatusUnauthorized, oapi.UNAUTHENTICATED, "unknown or disabled user")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDKey, id)))
		})
	}
}

// --- error bodies: every error is an ErrorResponse ---

func errorBody(ctx context.Context, code oapi.ErrorCode, msg string) oapi.ErrorResponse {
	return oapi.ErrorResponse{Code: code, Message: msg, RequestId: requestID(ctx)}
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code oapi.ErrorCode, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody(r.Context(), code, msg))
}

// internalError logs the cause and hides it from the client.
func internalError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errNotImplemented) {
		writeError(w, r, http.StatusNotImplemented, oapi.INTERNAL, err.Error())
		return
	}
	slog.ErrorContext(r.Context(), "request failed", "err", err, "path", r.URL.Path, "request_id", requestID(r.Context()))
	writeError(w, r, http.StatusInternalServerError, oapi.INTERNAL, "internal error")
}
