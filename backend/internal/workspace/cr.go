package workspace

import (
	"context"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"

	"github.com/thaihoc1310/tenantry/backend/internal/operation"
	"github.com/thaihoc1310/tenantry/backend/internal/outbox"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Outbox event types of a Workspace (IMPLEMENTATION-PLAN §5.1).
const (
	EventApply  = "workspace.apply"
	EventDelete = "workspace.delete"
)

// AnnotationSpecVersion carries workspaces.desired_generation on the CR, in
// the same server-side apply as the spec. metadata.generation cannot tell
// the worker which version of the database a status describes: it starts
// again at 1 when the CR is recreated and stays put when a spec comes back
// unchanged. The operator never reads it.
const AnnotationSpecVersion = "tenantry.io/spec-version"

// GroupSubjectPrefix is the prefix kube-apiserver gives OIDC groups
// (deploy/cluster/70-apiserver.sh): the subject of group org_x:group_y is
// oidc:org_x:group_y.
const GroupSubjectPrefix = "oidc:"

// Quota keys of spec.resources (PRODUCT-PLAN §14.2).
const (
	keyRequestsCPU     = "requests.cpu"
	keyRequestsMemory  = "requests.memory"
	keyLimitsCPU       = "limits.cpu"
	keyLimitsMemory    = "limits.memory"
	keyPods            = "pods"
	keyRequestsStorage = "requests.storage"
)

// BuildCR renders the PlatformWorkspace a Workspace row and the rows hanging
// off it describe, at the row's desired_generation. The CR holds resolved
// values only (group subjects, not group ids), so the controller never needs
// the database (02-data-model §5).
func BuildCR(ctx context.Context, q *store.Queries, ws store.Workspace) (*v1.PlatformWorkspace, error) {
	bindings, err := q.ListBindingsForCR(ctx, ws.ID)
	if err != nil {
		return nil, err
	}
	namespaces, err := q.ListWorkspaceNamespaces(ctx, ws.ID)
	if err != nil {
		return nil, err
	}
	classes, err := q.GetGrantStorageClasses(ctx, ws.ClusterGrantID)
	if err != nil {
		return nil, err
	}

	cr := &v1.PlatformWorkspace{
		TypeMeta: metav1.TypeMeta{APIVersion: v1.GroupVersion.String(), Kind: "PlatformWorkspace"},
		ObjectMeta: metav1.ObjectMeta{
			Name: ws.K8sName,
			Labels: map[string]string{
				v1.LabelOrganizationID: ws.OrgID,
				v1.LabelWorkspaceID:    ws.ID,
				v1.LabelClusterID:      ws.ClusterID,
			},
			Annotations: map[string]string{AnnotationSpecVersion: strconv.FormatInt(ws.DesiredGeneration, 10)},
		},
		Spec: v1.PlatformWorkspaceSpec{
			OrganizationID: ws.OrgID,
			WorkspaceID:    ws.ID,
			ClusterID:      ws.ClusterID,
			DisplayName:    ws.DisplayName,
			NamespaceQuota: ws.NamespaceQuota,
			Resources: v1.ResourceList{
				keyRequestsCPU:     *resource.NewMilliQuantity(ws.RequestsCpuMilli, resource.DecimalSI),
				keyRequestsMemory:  *resource.NewQuantity(ws.RequestsMemoryBytes, resource.BinarySI),
				keyLimitsCPU:       *resource.NewMilliQuantity(ws.LimitsCpuMilli, resource.DecimalSI),
				keyLimitsMemory:    *resource.NewQuantity(ws.LimitsMemoryBytes, resource.BinarySI),
				keyPods:            *resource.NewQuantity(int64(ws.Pods), resource.DecimalSI),
				keyRequestsStorage: *resource.NewQuantity(ws.RequestsStorageBytes, resource.BinarySI),
			},
			Compute:        v1.Compute{Mode: v1.ComputeMode(ws.ComputeMode)},
			StorageClasses: classes,
		},
	}
	if ws.SharedPoolID != nil {
		cr.Spec.Compute.SharedPoolID = *ws.SharedPoolID
	}
	for _, b := range bindings {
		cr.Spec.AccessBindings = append(cr.Spec.AccessBindings, v1.AccessBinding{
			GroupSubject: GroupSubjectPrefix + b.MachineName, Role: v1.WorkspaceRole(b.Role),
		})
	}
	prefix := ws.K8sName + "-"
	for _, n := range namespaces {
		switch {
		case n.State == nsDeleting:
			cr.Spec.NamespacesToDelete = append(cr.Spec.NamespacesToDelete, n.Name)
		case n.Source == SourcePortal:
			cr.Spec.Namespaces = append(cr.Spec.Namespaces, strings.TrimPrefix(n.Name, prefix))
		}
	}
	return cr, nil
}

// enqueueApply writes the CR of ws, as it now stands in the transaction of q,
// for delivery on behalf of op.
func enqueueApply(ctx context.Context, q *store.Queries, op store.Operation, ws store.Workspace) error {
	cr, err := BuildCR(ctx, q, ws)
	if err != nil {
		return err
	}
	_, err = outbox.Enqueue(ctx, q, op, outbox.Event{
		TargetClusterID: ws.ClusterID, Type: EventApply, Payload: cr, Version: ws.DesiredGeneration,
	})
	return err
}

// newOperation starts an operation on ws.
func newOperation(ctx context.Context, q *store.Queries, ws store.Workspace, typ, actor, correlationID string) (store.Operation, error) {
	return operation.Create(ctx, q, operation.New{
		OrgID: ws.OrgID, ResourceType: "Workspace", ResourceID: ws.ID, Type: typ,
		CorrelationID: correlationID, CreatedBy: actor,
	})
}
