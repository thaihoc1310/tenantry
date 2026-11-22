-- Workspaces (PRODUCT-PLAN §4.4, §17; architecture/02-data-model §4).

-- name: NestedWorkspaceSlug :one
-- A Workspace of the organization, not Deleted, whose slug equals this one
-- or nests with it at a '-' (solar and solar-prod): Capsule's
-- forceTenantPrefix would let one create Namespaces in the other's names.
-- Slugs hold only [a-z0-9-], so LIKE needs no escaping.
SELECT id, slug FROM workspaces
WHERE org_id = @org_id AND state <> 'Deleted'
  AND (slug = @slug OR slug LIKE @slug || '-%' OR @slug LIKE slug || '-%')
ORDER BY id
LIMIT 1;

-- name: GrantHasSharedPool :one
SELECT EXISTS (SELECT 1 FROM cluster_grant_shared_pools WHERE grant_id = $1 AND pool_id = $2);

-- name: ShareLockGroups :many
-- The live groups of the organization among ids, held FOR SHARE while they
-- are bound so none is deleted half-way (deleting takes them FOR UPDATE).
SELECT * FROM groups
WHERE org_id = @org_id AND id = ANY(@ids::text[]) AND deleted_at IS NULL
ORDER BY id
FOR SHARE;

-- name: CreateWorkspace :one
INSERT INTO workspaces (
    id, org_id, cluster_id, cluster_grant_id, slug, k8s_name, display_name,
    requests_cpu_milli, requests_memory_bytes, limits_cpu_milli, limits_memory_bytes,
    pods, requests_storage_bytes, namespace_quota, compute_mode, shared_pool_id
) VALUES (
    @id, @org_id, @cluster_id, @cluster_grant_id, @slug, @k8s_name, @display_name,
    @requests_cpu_milli, @requests_memory_bytes, @limits_cpu_milli, @limits_memory_bytes,
    @pods, @requests_storage_bytes, @namespace_quota, @compute_mode, sqlc.narg(shared_pool_id)
)
RETURNING *;

-- name: CreateWorkspaceBinding :exec
INSERT INTO workspace_group_bindings (workspace_id, group_id, org_id, role) VALUES ($1, $2, $3, $4);

-- name: CreateWorkspaceNamespace :one
INSERT INTO workspace_namespaces (id, workspace_id, cluster_id, name, source, state)
VALUES (@id, @workspace_id, @cluster_id, @name, @source, @state)
RETURNING *;

-- name: ResyncGroupMembers :execrows
-- The set of bindings changed, so whether each live member of these groups
-- belongs in tenantry:tenant-users may have changed: the group sync worker
-- works it out again (IMPLEMENTATION-PLAN §5.2).
UPDATE group_members
SET sync_status = 'Pending', sync_attempts = 0, next_sync_at = now(), last_error = '', updated_at = now()
WHERE group_id = ANY(@group_ids::text[]) AND deleted_at IS NULL;

-- name: GetWorkspace :one
SELECT * FROM workspaces WHERE id = $1 AND state <> 'Deleted';

-- name: ListWorkspaces :many
SELECT * FROM workspaces WHERE org_id = $1 AND state <> 'Deleted' ORDER BY id;

-- name: ListWorkspacesOfMember :many
-- The Workspaces a group of the user, both live, is bound to.
SELECT w.* FROM workspaces w
WHERE w.org_id = @org_id AND w.state <> 'Deleted'
  AND EXISTS (
    SELECT 1 FROM workspace_group_bindings b
    JOIN groups g ON g.id = b.group_id AND g.deleted_at IS NULL
    JOIN group_members gm ON gm.group_id = b.group_id AND gm.deleted_at IS NULL
    WHERE b.workspace_id = w.id AND gm.user_id = @user_id
  )
ORDER BY w.id;

-- name: WorkspaceRolesOfUser :many
-- The roles the user holds in the Workspace through live groups.
SELECT DISTINCT b.role FROM workspace_group_bindings b
JOIN groups g ON g.id = b.group_id AND g.deleted_at IS NULL
JOIN group_members gm ON gm.group_id = b.group_id AND gm.deleted_at IS NULL
WHERE b.workspace_id = @workspace_id AND gm.user_id = @user_id;

-- name: ListBindingsForCR :many
-- A Workspace's bindings with the groups' Keycloak names, for its CR.
SELECT b.group_id, b.role, g.machine_name, g.display_name, g.sync_status
FROM workspace_group_bindings b JOIN groups g ON g.id = b.group_id
WHERE b.workspace_id = $1
ORDER BY g.machine_name;

-- name: ListWorkspaceNamespaces :many
SELECT * FROM workspace_namespaces WHERE workspace_id = $1 AND state <> 'Deleted' ORDER BY name;

-- name: GetGrantStorageClasses :one
SELECT allowed_storage_classes FROM organization_cluster_grants WHERE id = $1;

-- name: UserHasWorkspaceBinding :one
-- Whether the user belongs in tenantry:tenant-users: a member of a live
-- group bound to a Workspace not Deleted (PRODUCT-PLAN §9.3).
SELECT EXISTS (
    SELECT 1 FROM workspace_group_bindings b
    JOIN groups g ON g.id = b.group_id AND g.deleted_at IS NULL
    JOIN group_members gm ON gm.group_id = b.group_id AND gm.deleted_at IS NULL
    JOIN workspaces w ON w.id = b.workspace_id AND w.state <> 'Deleted'
    WHERE gm.user_id = $1
);

-- name: LockWorkspace :one
SELECT * FROM workspaces WHERE id = $1 AND state <> 'Deleted' FOR UPDATE;

-- name: ActiveWorkspaceOperation :one
-- The operation under way on a Workspace, if any: one at a time (04 §1.1).
SELECT id, type, state FROM operations
WHERE resource_type = 'Workspace' AND resource_id = $1 AND state IN ('Pending', 'Running', 'Blocked')
LIMIT 1;

-- name: BeginWorkspaceUpdate :one
-- A change that needs a new CR: the next spec version, and Updating until
-- the controller has reconciled it.
UPDATE workspaces SET desired_generation = desired_generation + 1, state = 'Updating', updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListWorkspaceBindings :many
SELECT b.group_id, b.role, g.display_name
FROM workspace_group_bindings b JOIN groups g ON g.id = b.group_id
WHERE b.workspace_id = $1
ORDER BY g.display_name, g.id;

-- name: UpsertWorkspaceBinding :exec
INSERT INTO workspace_group_bindings (workspace_id, group_id, org_id, role) VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, group_id) DO UPDATE SET role = EXCLUDED.role, updated_at = now();

-- name: DeleteWorkspaceBinding :exec
DELETE FROM workspace_group_bindings WHERE workspace_id = $1 AND group_id = $2;

-- name: WorkspacesBoundTo :many
-- Workspaces a group holds a role in.
SELECT workspace_id FROM workspace_group_bindings WHERE group_id = $1 ORDER BY workspace_id;

-- name: CountWorkspaceNamespaces :one
-- Every Namespace of the Workspace, however created: they all count against namespace_quota.
SELECT count(*)::int FROM workspace_namespaces WHERE workspace_id = $1 AND state <> 'Deleted';

-- name: GetWorkspaceNamespace :one
SELECT * FROM workspace_namespaces WHERE workspace_id = $1 AND name = $2 AND state <> 'Deleted';

-- name: SetNamespaceState :one
UPDATE workspace_namespaces SET state = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: GrantAllocations :many
-- What the Workspaces not Deleted hold of each grant (02-data-model §4):
-- CPU, memory and pods of Shared Workspaces only; Workspaces, Namespaces
-- (their namespace_quota) and storage of every Workspace.
SELECT cluster_grant_id,
       count(*)::int AS workspaces,
       coalesce(sum(namespace_quota), 0)::int AS namespaces,
       coalesce(sum(requests_cpu_milli) FILTER (WHERE compute_mode = 'Shared'), 0)::bigint AS requests_cpu_milli,
       coalesce(sum(requests_memory_bytes) FILTER (WHERE compute_mode = 'Shared'), 0)::bigint AS requests_memory_bytes,
       coalesce(sum(limits_cpu_milli) FILTER (WHERE compute_mode = 'Shared'), 0)::bigint AS limits_cpu_milli,
       coalesce(sum(limits_memory_bytes) FILTER (WHERE compute_mode = 'Shared'), 0)::bigint AS limits_memory_bytes,
       coalesce(sum(pods) FILTER (WHERE compute_mode = 'Shared'), 0)::int AS pods,
       coalesce(sum(requests_storage_bytes), 0)::bigint AS requests_storage_bytes
FROM workspaces
WHERE cluster_grant_id = ANY(@grant_ids::text[]) AND state <> 'Deleted'
GROUP BY cluster_grant_id;

-- name: CountWorkspaces :one
SELECT count(*)::int FROM workspaces WHERE org_id = $1 AND state <> 'Deleted';

-- name: RetryWorkspace :one
-- A retry after a failure the controller reported: the next spec version,
-- back to Provisioning or Updating (04-state-machines §1.3).
UPDATE workspaces SET desired_generation = desired_generation + 1, state = @state, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: SharedPoolsInUse :many
-- Shared pools Workspaces of the grant, not Deleted, run on.
SELECT DISTINCT shared_pool_id::text FROM workspaces
WHERE cluster_grant_id = $1 AND state <> 'Deleted' AND shared_pool_id IS NOT NULL
ORDER BY 1;

-- name: CountGrantWorkspaces :one
-- Every Workspace row of the grant, Deleted ones waiting for their CIDR's
-- quarantine included: the grant's foreign key keeps it while any is left.
SELECT count(*)::int FROM workspaces WHERE cluster_grant_id = $1;

-- name: UpdateWorkspaceSpec :one
-- A PATCH: display name, quota and namespaceQuota (PRODUCT-PLAN §17.2).
UPDATE workspaces
SET display_name = @display_name, requests_cpu_milli = @requests_cpu_milli, requests_memory_bytes = @requests_memory_bytes,
    limits_cpu_milli = @limits_cpu_milli, limits_memory_bytes = @limits_memory_bytes, pods = @pods,
    requests_storage_bytes = @requests_storage_bytes, namespace_quota = @namespace_quota, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: SetWorkspaceCordoned :one
-- Cordon or uncordon: the next spec version; the state stays until the
-- controller has done it (04-state-machines §1.1).
UPDATE workspaces SET cordoned = @cordoned, desired_generation = desired_generation + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: SetWorkspaceState :one
UPDATE workspaces SET state = @state, updated_at = now() WHERE id = @id RETURNING *;

-- name: BoundGroupIDs :many
SELECT group_id FROM workspace_group_bindings WHERE workspace_id = $1 ORDER BY group_id;

-- name: DeleteWorkspaceRow :exec
-- The last of a deleted Workspace: bindings and Namespaces go with it.
DELETE FROM workspaces WHERE id = $1;

-- name: ListDeletingWorkspaces :many
SELECT * FROM workspaces WHERE cluster_id = $1 AND state = 'Deleting' ORDER BY id;

-- name: WorkspacesOfGrant :many
SELECT id FROM workspaces WHERE cluster_grant_id = $1 AND state <> 'Deleted' ORDER BY id;

-- name: BumpWorkspaceVersion :one
-- A new spec version with the state as it is: a change riding on an
-- operation under way, or one that leaves a Cordoned Workspace cordoned.
UPDATE workspaces SET desired_generation = desired_generation + 1, updated_at = now() WHERE id = $1 RETURNING *;

-- name: ListWorkspaceOperations :many
-- A Workspace's operations, newest first.
SELECT * FROM operations
WHERE resource_type = 'Workspace' AND resource_id = @workspace_id
  AND (sqlc.narg(cursor)::text IS NULL OR id < sqlc.narg(cursor))
ORDER BY id DESC
LIMIT @page_size;

-- name: CreateNodeReservation :exec
INSERT INTO workspace_node_reservations (id, workspace_id, org_id, cluster_id, node_grant_id,
    reserved_cpu_milli, reserved_memory_bytes, reserved_pods)
VALUES (@id, @workspace_id, @org_id, @cluster_id, @node_grant_id, @reserved_cpu_milli, @reserved_memory_bytes, @reserved_pods);

-- name: GetNodeReservation :one
-- A Workspace's reservation with the node it is on.
SELECT r.*, g.node_name FROM workspace_node_reservations r JOIN dedicated_node_grants g ON g.id = r.node_grant_id
WHERE r.workspace_id = $1;

-- name: SetReservationAmount :exec
UPDATE workspace_node_reservations
SET reserved_cpu_milli = @reserved_cpu_milli, reserved_memory_bytes = @reserved_memory_bytes, reserved_pods = @reserved_pods, updated_at = now()
WHERE workspace_id = @workspace_id;

-- name: SetReservationState :exec
UPDATE workspace_node_reservations SET state = @state, updated_at = now() WHERE workspace_id = @workspace_id AND state <> @state;

-- name: WorkspaceNodeOvercommitted :one
-- Whether a Dedicated Workspace's node holds more than it can (drift: a
-- larger system reserve, a smaller node; 04-state-machines §5.3).
SELECT EXISTS (
    SELECT 1 FROM workspace_node_reservations r JOIN dedicated_node_grants g ON g.id = r.node_grant_id
    WHERE r.workspace_id = $1 AND g.capacity_state = 'Overcommitted'
);

-- name: GrantHasNetworkPool :one
SELECT EXISTS (SELECT 1 FROM cluster_grant_network_pools WHERE grant_id = $1 AND pool_id = $2);

-- name: FirstGrantNetworkPool :one
-- The network pool a Workspace gets when it names none: the grant's first.
SELECT pool_id FROM cluster_grant_network_pools WHERE grant_id = $1 ORDER BY pool_id LIMIT 1;

-- name: ClusterAllocatedCIDRs :many
-- Every block of the cluster not Released, whoever holds it: taken.
SELECT cidr FROM network_allocations WHERE cluster_id = $1 AND state <> 'Released';

-- name: CreateNetworkAllocation :one
INSERT INTO network_allocations (id, workspace_id, cluster_id, pool_id, cidr)
VALUES (@id, @workspace_id, @cluster_id, @pool_id, @cidr)
RETURNING *;

-- name: GetNetworkAllocation :one
SELECT * FROM network_allocations WHERE workspace_id = $1;

-- name: SetAllocationState :exec
UPDATE network_allocations
SET state = @state, quarantine_until = sqlc.narg(quarantine_until), updated_at = now()
WHERE workspace_id = @workspace_id;

-- name: MarkWorkspaceDeleted :exec
-- A deleted Workspace whose CIDR waits out its quarantine: out of every
-- sum, its k8s_name still held (02-data-model §4).
UPDATE workspaces SET state = 'Deleted', deleted_at = now(), updated_at = now() WHERE id = $1;

-- name: ReleaseQuarantined :many
-- Blocks past their quarantine are free again; the Workspaces holding
-- them go for good, with what hangs off them (02-data-model §4).
UPDATE network_allocations a SET state = 'Released', quarantine_until = NULL, updated_at = now()
FROM workspaces w
WHERE a.workspace_id = w.id AND a.state = 'Quarantined' AND a.quarantine_until <= now()
RETURNING a.workspace_id, a.cidr, w.org_id, w.k8s_name;

-- name: DeleteWorkspaceRows :exec
DELETE FROM workspaces WHERE id = ANY(@ids::text[]);

-- name: NetworkPoolsInUse :many
-- Network pools Workspaces of the grant hold a block in (not Released).
SELECT DISTINCT a.pool_id FROM network_allocations a JOIN workspaces w ON w.id = a.workspace_id
WHERE w.cluster_grant_id = $1 AND a.state <> 'Released'
ORDER BY 1;

-- name: PoolAllocatedCIDRs :many
SELECT cidr FROM network_allocations WHERE pool_id = $1 AND state <> 'Released' ORDER BY cidr;

-- name: OrgBlocksInPool :many
-- An organization's own address blocks in a pool, not Released.
SELECT w.k8s_name, a.cidr, a.state FROM network_allocations a JOIN workspaces w ON w.id = a.workspace_id
WHERE a.pool_id = @pool_id AND w.org_id = @org_id AND a.state <> 'Released'
ORDER BY a.cidr;

-- name: GetNetworkPool :one
SELECT * FROM network_pools WHERE id = $1;

-- name: GetCluster :one
SELECT * FROM clusters WHERE id = $1;
