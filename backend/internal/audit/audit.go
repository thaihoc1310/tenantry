// Package audit writes the audit trail (PRODUCT-PLAN §21.3). Record runs in
// the caller's transaction, so an event exists exactly when the change it
// records was committed.
package audit

import (
	"cmp"
	"context"
	"encoding/json"

	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Actions recorded so far; the list grows with the endpoints.
const (
	Login                = "user.login"
	Logout               = "user.logout"
	OrganizationCreate   = "organization.create"
	OrganizationUpdate   = "organization.update"
	OrganizationApprove  = "organization.approve"
	OrganizationReject   = "organization.reject"
	OrganizationDelete   = "organization.delete"
	EntitlementUpdate    = "organization_entitlement.update"
	UserDisable          = "user.disable"
	UserEnable           = "user.enable"
	ClusterRegister      = "cluster.register"
	ClusterUpdate        = "cluster.update"
	ClusterGrantCreate   = "cluster_grant.create"
	ClusterGrantUpdate   = "cluster_grant.update"
	ClusterGrantDelete   = "cluster_grant.delete"
	PolicyUpdate         = "onboarding_policy.update"
	SharedPoolCreate     = "shared_pool.create"
	SharedPoolUpdate     = "shared_pool.update"
	NetworkPoolCreate    = "network_pool.create"
	NetworkPoolUpdate    = "network_pool.update"
	DedicatedNodesUpdate = "dedicated_nodes.update"
	OperationRetry       = "operation.retry"

	InvitationCreate = "invitation.create"
	InvitationRevoke = "invitation.revoke"
	InvitationAccept = "invitation.accept"

	MemberRoleChange = "organization_member.role_change"
	MemberRemove     = "organization_member.remove"
	MemberLeave      = "organization_member.leave"

	TransferPropose = "ownership_transfer.propose"
	TransferAccept  = "ownership_transfer.accept"
	TransferCancel  = "ownership_transfer.cancel"

	GroupCreate       = "group.create"
	GroupUpdate       = "group.update"
	GroupDelete       = "group.delete"
	GroupMemberAdd    = "group_member.add"
	GroupMemberRemove = "group_member.remove"

	WorkspaceCreate    = "workspace.create"
	WorkspaceUpdate    = "workspace.update"
	WorkspaceCordon    = "workspace.cordon"
	WorkspaceUncordon  = "workspace.uncordon"
	WorkspaceDelete    = "workspace.delete"  // asked for
	WorkspaceDeleted   = "workspace.deleted" // done: the cluster is clean
	KubeconfigDownload = "workspace.kubeconfig_download"
	OperationForce     = "operation.force"

	NetworkAllocationRelease = "network_allocation.release"
	WorkspaceBindingUpdate   = "workspace_binding.update"
	WorkspaceBindingDelete   = "workspace_binding.delete"
	NamespaceCreate          = "namespace.create"
	NamespaceDelete          = "namespace.delete"
)

// Results. An event written by the change it records Succeeded. A refused
// attempt worth keeping (an invitation accepted from the wrong account) is
// Refused, written in a transaction of its own after the refused one rolled back.
const (
	Succeeded = "Succeeded"
	Refused   = "Refused"
)

type Event struct {
	Actor      string // user id; empty when the platform acted on its own
	OrgID      string // empty outside any organization
	Action     string
	TargetType string // e.g. Organization
	TargetID   string
	Detail     map[string]any // e.g. {"before": {...}, "after": {...}}; never a token or a secret
	Result     string         // empty means Succeeded
}

func Record(ctx context.Context, q *store.Queries, e Event) error {
	detail, err := json.Marshal(e.Detail)
	if err != nil {
		return err
	}
	if e.Detail == nil {
		detail = []byte("{}")
	}
	return q.InsertAuditEvent(ctx, store.InsertAuditEventParams{
		ActorUserID: nullable(e.Actor),
		OrgID:       nullable(e.OrgID),
		Action:      e.Action,
		TargetType:  e.TargetType,
		TargetID:    e.TargetID,
		Result:      cmp.Or(e.Result, Succeeded),
		Detail:      detail,
	})
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
