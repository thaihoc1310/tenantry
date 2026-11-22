-- The cluster registry, onboarding policy and cluster grants as a Platform
-- Admin edits them (M5, PRODUCT-PLAN §16.5).

-- name: ListClusters :many
SELECT * FROM clusters ORDER BY id;

-- name: CreateCluster :one
-- A cluster starts Registered; an Admin marks it Ready once checked.
INSERT INTO clusters (id, display_name, status, proxy_endpoint, console_url, apps_domain, reserved_cidrs)
VALUES ($1, $2, 'Registered', $3, $4, $5, $6)
RETURNING *;

-- name: LockCluster :one
-- Held while the cluster or its network pools change, so overlap checks
-- see every pool (02-data-model §3).
SELECT * FROM clusters WHERE id = $1 FOR UPDATE;

-- name: UpdateCluster :one
UPDATE clusters
SET display_name = @display_name, status = @status, proxy_endpoint = @proxy_endpoint, console_url = @console_url,
    apps_domain = @apps_domain, reserved_cidrs = @reserved_cidrs, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: NetworkPoolRangesOfCluster :many
SELECT id, cidrs FROM network_pools WHERE cluster_id = $1 ORDER BY id;

-- name: UpdateActiveOnboardingPolicy :one
UPDATE onboarding_policies
SET default_cluster_id = $1, entitlement_template = $2, grant_template = $3, updated_at = now()
WHERE active
RETURNING *;

-- name: CreateOnboardingPolicy :one
-- When none is active: the default policy, reactivated if it exists.
INSERT INTO onboarding_policies (id, default_cluster_id, entitlement_template, grant_template, active)
VALUES ('default', $1, $2, $3, true)
ON CONFLICT (id) DO UPDATE
SET default_cluster_id = EXCLUDED.default_cluster_id, entitlement_template = EXCLUDED.entitlement_template,
    grant_template = EXCLUDED.grant_template, active = true, updated_at = now()
RETURNING *;

-- name: LockClusterGrant :one
-- Held while a grant is edited or deleted (PRODUCT-PLAN §5.5).
SELECT * FROM organization_cluster_grants WHERE org_id = $1 AND cluster_id = $2 FOR UPDATE;

-- name: CountOrganizationGrants :one
SELECT count(*)::int FROM organization_cluster_grants WHERE org_id = $1;

-- name: UpdateClusterGrant :one
UPDATE organization_cluster_grants
SET max_workspaces = $2, max_namespaces_total = $3, requests_cpu_milli = $4, requests_memory_bytes = $5,
    limits_cpu_milli = $6, limits_memory_bytes = $7, pods = $8, requests_storage_bytes = $9,
    allowed_storage_classes = $10, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearGrantPools :exec
WITH s AS (DELETE FROM cluster_grant_shared_pools WHERE cluster_grant_shared_pools.grant_id = $1)
DELETE FROM cluster_grant_network_pools WHERE cluster_grant_network_pools.grant_id = $1;

-- name: DeleteClusterGrant :exec
DELETE FROM organization_cluster_grants WHERE id = $1;

-- name: PromoteOldestGrant :exec
-- After the default grant is deleted, the oldest grant left becomes the default.
UPDATE organization_cluster_grants SET is_default = true, updated_at = now()
WHERE id = (SELECT g.id FROM organization_cluster_grants g WHERE g.org_id = $1 ORDER BY g.created_at, g.id LIMIT 1);

-- name: CountLiveDedicatedNodes :one
SELECT count(*)::int FROM dedicated_node_grants WHERE org_id = $1 AND cluster_id = $2 AND revoked_at IS NULL;

-- name: ListAllClusterGrants :many
-- Every grant of every organization, in id order (paged by id).
SELECT sqlc.embed(g), o.slug AS org_slug, o.display_name AS org_display_name,
  array(SELECT pool_id FROM cluster_grant_shared_pools WHERE grant_id = g.id ORDER BY pool_id)::text[] AS shared_pool_ids,
  array(SELECT pool_id FROM cluster_grant_network_pools WHERE grant_id = g.id ORDER BY pool_id)::text[] AS network_pool_ids
FROM organization_cluster_grants g JOIN organizations o ON o.id = g.org_id
WHERE (sqlc.narg(cluster_id)::text IS NULL OR g.cluster_id = sqlc.narg(cluster_id))
  AND (sqlc.narg(cursor)::text IS NULL OR g.id > sqlc.narg(cursor))
ORDER BY g.id
LIMIT @page_size;

-- name: ListOperationsForAdmin :many
-- Operations of every organization and of clusters, newest first.
SELECT * FROM operations
WHERE (sqlc.narg(state)::text IS NULL OR state = sqlc.narg(state))
  AND (sqlc.narg(cursor)::text IS NULL OR id < sqlc.narg(cursor))
ORDER BY id DESC
LIMIT @page_size;

-- name: TouchClusterHealth :exec
UPDATE clusters SET last_health_check = now() WHERE id = $1;
