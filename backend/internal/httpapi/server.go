// Package httpapi serves /api/v1: it implements the oapi-codegen strict
// interface generated from openapi/tenantry.v1.yaml and owns the HTTP-only
// concerns around it (request id, authentication, error bodies).
package httpapi

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	validator "github.com/oapi-codegen/nethttp-middleware"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/org"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
	"github.com/thaihoc1310/tenantry/backend/internal/user"
)

// Dev auth stands in for the OIDC session until M3 and only works when
// Options.DevAuth is set. DevUserHeader names the caller by user id;
// DevRolesHeader lists the Keycloak realm roles a token would carry,
// comma-separated (e.g. "platform-admin").
const (
	DevUserHeader  = "X-Tenantry-Dev-User"
	DevRolesHeader = "X-Tenantry-Dev-Roles"
)

// RolePlatformAdmin is the Keycloak realm role of Platform Admins (PRODUCT-PLAN §4.1).
const RolePlatformAdmin = "platform-admin"

type Options struct {
	DevAuth bool // TENANTRY_AUTH_DEV_HEADER=true; never on outside local dev
}

type server struct {
	orgs *org.Service
}

// Register mounts every /api/v1 operation on r.
func Register(r chi.Router, db *pgxpool.Pool, opts Options) {
	badRequest := func(w http.ResponseWriter, r *http.Request, err error) {
		writeError(w, r, apperr.New(apperr.Validation, err.Error()))
	}
	api := oapi.NewStrictHandlerWithOptions(server{orgs: org.NewService(db)}, nil, oapi.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: badRequest,
		// Handlers return errors as-is; writeError turns them into responses.
		ResponseErrorHandlerFunc: writeError,
	})
	oapi.HandlerWithOptions(api, oapi.ChiServerOptions{
		BaseURL:    "/api/v1",
		BaseRouter: r,
		// The last entry runs first: an anonymous caller gets 401 before any 400.
		Middlewares:      []oapi.MiddlewareFunc{validate(), authenticate(user.NewService(store.New(db)), opts)},
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

func (s server) GetMe(ctx context.Context, _ oapi.GetMeRequestObject) (oapi.GetMeResponseObject, error) {
	c := caller(ctx)
	return oapi.GetMe200JSONResponse{
		Id:              c.user.ID,
		Email:           c.user.Email,
		DisplayName:     c.user.DisplayName,
		IsPlatformAdmin: c.platformAdmin,
	}, nil
}

func (s server) ListOrganizations(ctx context.Context, req oapi.ListOrganizationsRequestObject) (oapi.ListOrganizationsResponseObject, error) {
	var cursor string
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	var limit int
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	page, err := s.orgs.List(ctx, userID(ctx), cursor, limit)
	if err != nil {
		return nil, err
	}
	resp := oapi.OrganizationList{Items: make([]oapi.Organization, 0, len(page.Items))} // [] rather than null when empty
	for _, o := range page.Items {
		resp.Items = append(resp.Items, toOrganization(o))
	}
	if page.NextCursor != "" {
		resp.NextCursor = &page.NextCursor
	}
	return oapi.ListOrganizations200JSONResponse(resp), nil
}

func (s server) CreateOrganization(ctx context.Context, req oapi.CreateOrganizationRequestObject) (oapi.CreateOrganizationResponseObject, error) {
	r := org.Request{Slug: req.Body.Slug, DisplayName: req.Body.DisplayName}
	if req.Body.Description != nil {
		r.Description = *req.Body.Description
	}
	o, err := s.orgs.Create(ctx, userID(ctx), r)
	if err != nil {
		return nil, err
	}
	return oapi.CreateOrganization201JSONResponse{
		Body:    toOrganization(o),
		Headers: oapi.CreateOrganization201ResponseHeaders{Location: "/api/v1/organizations/" + o.ID},
	}, nil
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
	principalKey
)

// principal is the authenticated caller. Only authenticate builds it, so
// swapping dev auth for OIDC in M3 changes nothing downstream.
type principal struct {
	user          store.User
	platformAdmin bool
}

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
func caller(ctx context.Context) principal { p, _ := ctx.Value(principalKey).(principal); return p }
func userID(ctx context.Context) string    { return caller(ctx).user.ID }

// authenticate puts the caller in the context or answers 401. The user must
// already exist and be Active: a disabled account loses access at once.
func authenticate(users *user.Service, opts Options) oapi.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(DevUserHeader)
			if !opts.DevAuth || id == "" {
				writeError(w, r, apperr.New(apperr.Unauthenticated, "login required"))
				return
			}
			u, err := users.Active(r.Context(), id)
			if err != nil {
				writeError(w, r, err)
				return
			}
			roles := strings.Split(r.Header.Get(DevRolesHeader), ",")
			for i := range roles {
				roles[i] = strings.TrimSpace(roles[i])
			}
			p := principal{user: u, platformAdmin: slices.Contains(roles, RolePlatformAdmin)}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, p)))
		})
	}
}
