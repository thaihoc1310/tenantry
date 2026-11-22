// Package apperr is the one error type services return when the caller did
// something wrong (IMPLEMENTATION-PLAN §3.3). It carries a stable Code the UI
// switches on; turning a Code into an HTTP status is the HTTP layer's job, so
// services stay transport-agnostic. Any other error is a bug or an outage and
// is reported as INTERNAL without its details.
package apperr

import "errors"

// Code values must also be listed in ErrorCode in openapi/tenantry.v1.yaml
// (enforced by a test in httpapi).
type Code string

const (
	Validation      Code = "VALIDATION_FAILED"
	Unauthenticated Code = "UNAUTHENTICATED"
	Forbidden       Code = "FORBIDDEN"
	NotFound        Code = "NOT_FOUND"
	Conflict        Code = "CONFLICT"
	// MethodNotAllowed is the router's, not a service's: the path exists but not with this method.
	MethodNotAllowed Code = "METHOD_NOT_ALLOWED"
	// IdempotencyKeyReused: the key came with another request before (IMPLEMENTATION-PLAN §3.4).
	IdempotencyKeyReused Code = "IDEMPOTENCY_KEY_REUSED"
	RateLimited          Code = "RATE_LIMITED"

	// Specific conflicts the UI handles differently from a generic CONFLICT.
	SlugTaken         Code = "SLUG_TAKEN"
	OrgRequestPending Code = "ORG_REQUEST_PENDING"
	ClusterNotReady   Code = "CLUSTER_NOT_READY"
	OrgNotActive      Code = "ORG_NOT_ACTIVE"

	// Limits of the organization's entitlement (PRODUCT-PLAN §5.1).
	EntitlementExceeded Code = "ENTITLEMENT_EXCEEDED"

	// Invitations (PRODUCT-PLAN §9.1).
	InvitationInvalid       Code = "INVITATION_INVALID"        // unknown, expired, used or revoked
	InvitationEmailMismatch Code = "INVITATION_EMAIL_MISMATCH" // sent to another verified email

	// Ownership transfer and leaving (PRODUCT-PLAN §9.2, §9.4).
	TransferNotAllowed   Code = "TRANSFER_NOT_ALLOWED"    // only the OrgOwner offers the organization
	RecipientNotOrgAdmin Code = "RECIPIENT_NOT_ORG_ADMIN" // only to one of its OrgAdmins
	TransferInvalid      Code = "TRANSFER_INVALID"        // no longer acceptable by this caller
	LeaveNotAllowed      Code = "LEAVE_NOT_ALLOWED"       // the OrgOwner transfers first
)

type Error struct {
	Code    Code
	Message string
	Details map[string]any // machine-readable context, e.g. {"field": "displayName"}
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

func New(code Code, msg string) *Error { return &Error{Code: code, Message: msg} }

// Invalid is a VALIDATION_FAILED error pointing at one request field.
func Invalid(field, reason string) *Error {
	return &Error{Code: Validation, Message: field + ": " + reason, Details: map[string]any{"field": field}}
}

// As finds an *Error in err's chain.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
