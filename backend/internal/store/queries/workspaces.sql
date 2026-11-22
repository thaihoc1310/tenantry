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
SELECT id FROM operations
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
