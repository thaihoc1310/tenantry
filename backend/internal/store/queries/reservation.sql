-- name: GetActiveOnboardingPolicy :one
SELECT * FROM onboarding_policies WHERE active;

-- name: ShareLockCluster :one
-- Held while a grant on the cluster is written, so the cluster cannot leave
-- Ready (or disappear) half-way through.
SELECT * FROM clusters WHERE id = $1 FOR SHARE;

-- name: GetSharedPools :many
SELECT id, cluster_id FROM shared_compute_pools WHERE id = ANY(@ids::text[]);

-- name: GetNetworkPools :many
SELECT id, cluster_id, org_id FROM network_pools WHERE id = ANY(@ids::text[]);

-- name: CreateClusterGrant :one
INSERT INTO organization_cluster_grants (
    id, org_id, cluster_id, is_default, max_workspaces, max_namespaces_total,
    requests_cpu_milli, requests_memory_bytes, limits_cpu_milli, limits_memory_bytes,
    pods, requests_storage_bytes, allowed_storage_classes)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: AddGrantSharedPools :exec
INSERT INTO cluster_grant_shared_pools (grant_id, pool_id)
SELECT @grant_id, unnest(@pool_ids::text[]);

-- name: AddGrantNetworkPools :exec
INSERT INTO cluster_grant_network_pools (grant_id, pool_id)
SELECT @grant_id, unnest(@pool_ids::text[]);
