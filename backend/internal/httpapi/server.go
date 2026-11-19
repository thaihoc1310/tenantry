// Package httpapi serves /api/v1: it implements the oapi-codegen strict
// interface generated from openapi/tenantry.v1.yaml and owns the HTTP-only
// concerns around it (request id, authentication, error bodies).
package httpapi

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/jackc/pgx/v5"
	validator "github.com/oapi-codegen/nethttp-middleware"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/org"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// DevUserHeader names the caller by user id. It is the only way to log in
// until the OIDC session arrives in M3, and only works when Options.DevAuth is set.
const DevUserHeader = "X-Tenantry-Dev-User"

type Options struct {
	DevAuth bool // TENANTRY_AUTH_DEV_HEADER=true; never on outside local dev
}

type server struct {
	orgs *org.Service
}

// Register mounts every /api/v1 operation on mux.
func Register(mux *http.ServeMux, db store.DBTX, opts Options) {
	q := store.New(db)
	badRequest := func(w http.ResponseWriter, r *http.Request, err error) {
		writeError(w, r, apperr.New(apperr.Validation, err.Error()))
	}
	api := oapi.NewStrictHandlerWithOptions(server{orgs: org.NewService(q)}, nil, oapi.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: badRequest,
		// Handlers return errors as-is; writeError turns them into responses.
		ResponseErrorHandlerFunc: writeError,
	})
	oapi.HandlerWithOptions(api, oapi.StdHTTPServerOptions{
		BaseURL:    "/api/v1",
		BaseRouter: mux,
		// The last entry runs first: an anonymous caller gets 401 before any 400.
		Middlewares:      []oapi.MiddlewareFunc{validate(), authenticate(q, opts)},
		ErrorHandlerFunc: badRequest,
	})
}

// validate checks each request against the embedded OpenAPI document
// (maxLength, unknown fields, ...): the strict server only decodes JSON.
func validate() oapi.MiddlewareFunc {
	spec, err := oapi.GetSpec()
	if err != nil {
		panic(err) // the embedded spec is covered by TestEmbeddedSpecIsValid
	}
	return validator.OapiRequestValidatorWithOptions(spec, &validator.Options{
		DoNotValidateServers: true, // match on the path only, not the Host header
		Prefix:               "/api/v1",
		ErrorHandlerWithOpts: func(_ context.Context, err error, w http.ResponseWriter, r *http.Request, _ validator.ErrorHandlerOpts) {
			// A schema error would otherwise print the whole schema and the
			// submitted value; keep the reason and point at the field instead.
			var se *openapi3.SchemaError
			if !errors.As(err, &se) {
				writeError(w, r, apperr.New(apperr.Validation, err.Error()))
			} else if field := strings.Join(se.JSONPointer(), "."); field != "" {
				writeError(w, r, apperr.Invalid(field, se.Reason))
			} else {
				writeError(w, r, apperr.New(apperr.Validation, se.Reason))
			}
		},
	})
}

func (s server) GetOrganization(ctx context.Context, req oapi.GetOrganizationRequestObject) (oapi.GetOrganizationResponseObject, error) {
	o, err := s.orgs.Get(ctx, userID(ctx), req.OrgId)
	if err != nil {
		return nil, err
	}
	return oapi.GetOrganization200JSONResponse(toOrganization(o)), nil
}

func (s server) PatchOrganization(ctx context.Context, req oapi.PatchOrganizationRequestObject) (oapi.PatchOrganizationResponseObject, error) {
	o, err := s.orgs.Update(ctx, userID(ctx), req.OrgId, org.Patch{
		DisplayName: req.Body.DisplayName,
		Description: req.Body.Description,
	})
	if err != nil {
		return nil, err
	}
	return oapi.PatchOrganization200JSONResponse(toOrganization(o)), nil
}

func toOrganization(o org.Org) oapi.Organization {
	r := oapi.OrgRole(o.Role)
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
				writeError(w, r, apperr.New(apperr.Unauthenticated, "login required"))
				return
			}
			if _, err := q.GetActiveUserID(r.Context(), id); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					err = apperr.New(apperr.Unauthenticated, "unknown or disabled user")
				}
				writeError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDKey, id)))
		})
	}
}
