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

	// Platform Admin (PRODUCT-PLAN §16.5).
	OwnerUserNotFound     Code = "OWNER_USER_NOT_FOUND"    // the owner of a new organization never logged in
	EntitlementBelowUsage Code = "ENTITLEMENT_BELOW_USAGE" // a limit below what is used, or maxGroups < 3
	CIDROverlap           Code = "CIDR_OVERLAP"            // a range overlapping another pool or the cluster's own

	// Workspaces (PRODUCT-PLAN §16.5, §17.1).
	ClusterNotGranted Code = "CLUSTER_NOT_GRANTED" // no grant of the organization on that cluster
	LastOwnerBinding  Code = "LAST_OWNER_BINDING"  // no group would hold TenantOwner any more
	GroupSyncPending  Code = "GROUP_SYNC_PENDING"  // a bound group is not in Keycloak yet
	WorkspaceNotReady Code = "WORKSPACE_NOT_READY" // not Ready, or an operation on it is under way

	// What Workspaces hold of a cluster grant (PRODUCT-PLAN §5.3, §5.5).
	ClusterGrantQuotaExceeded   Code = "CLUSTER_GRANT_QUOTA_EXCEEDED"   // a create or resize does not fit in the grant
	ClusterGrantBelowAllocation Code = "CLUSTER_GRANT_BELOW_ALLOCATION" // an Admin's change below what is held
	WorkspaceQuotaBelowUsage    Code = "WORKSPACE_QUOTA_BELOW_USAGE"    // a resize below what the Workspace uses

	OrganizationNotEmpty Code = "ORGANIZATION_NOT_EMPTY" // Workspaces or dedicated nodes left (04-state-machines §11)

	// Compute (PRODUCT-PLAN §6).
	ComputePoolCapacityExceeded       Code = "COMPUTE_POOL_CAPACITY_EXCEEDED"       // over a shared pool's ratio × reservable
	DedicatedNodeNotGranted           Code = "DEDICATED_NODE_NOT_GRANTED"           // not a live node grant of the organization there
	DedicatedNodeNotReady             Code = "DEDICATED_NODE_NOT_READY"             // the node is NotReady or cordoned
	DedicatedNodeInsufficientCapacity Code = "DEDICATED_NODE_INSUFFICIENT_CAPACITY" // the node cannot hold it

	// Network (PRODUCT-PLAN §7).
	CIDROutsideAllowedPool Code = "CIDR_OUTSIDE_ALLOWED_POOL" // a requested block outside the pool
	NetworkPoolExhausted   Code = "NETWORK_POOL_EXHAUSTED"    // no free block of that length left

	// Routes (PRODUCT-PLAN §7.6).
	RouteQuotaExceeded Code = "ROUTE_QUOTA_EXCEEDED" // the Workspace has 20 Routes
	RouteHostnameTaken Code = "ROUTE_HOSTNAME_TAKEN" // another Route holds the hostname
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
