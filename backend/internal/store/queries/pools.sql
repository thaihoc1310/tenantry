-- Shared pools, network pools, the nodes of a cluster and dedicated nodes as
-- a Platform Admin manages them (M5, PRODUCT-PLAN §6.2–6.3, §7). Changes are
-- database only until the provisioning worker builds the CRDs from them (M6).

-- name: ListSharedPools :many
SELECT sqlc.embed(p),
  array(SELECT n.node_name FROM cluster_nodes n WHERE n.shared_pool_id = p.id ORDER BY n.node_name)::text[] AS nodes
FROM shared_compute_pools p WHERE p.cluster_id = $1 ORDER BY p.id;

-- name: LockSharedPool :one
SELECT * FROM shared_compute_pools WHERE id = $1 FOR UPDATE;

-- name: CreateSharedPool :one
INSERT INTO shared_compute_pools (id, cluster_id, name, allocation_overcommit_ratio,
    system_reserve_cpu_milli, system_reserve_memory_bytes, system_reserve_pods)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateSharedPool :one
UPDATE shared_compute_pools
SET name = $2, allocation_overcommit_ratio = $3, system_reserve_cpu_milli = $4,
    system_reserve_memory_bytes = $5, system_reserve_pods = $6, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: LockClusterNodes :many
-- The nodes named, locked before they join a pool or an organization (02 §3:
-- a node is in one pool or dedicated, never both), with the organization a
-- node is dedicated to, if any.
SELECT sqlc.embed(n), d.org_id AS dedicated_org_id
FROM cluster_nodes n
LEFT JOIN dedicated_node_grants d ON d.cluster_id = n.cluster_id AND d.node_name = n.node_name AND d.revoked_at IS NULL
WHERE n.cluster_id = @cluster_id AND n.node_name = ANY(@names::text[])
ORDER BY n.node_name
FOR UPDATE OF n;

-- name: LeavePool :exec
-- Nodes of the pool that are no longer listed leave it.
UPDATE cluster_nodes SET shared_pool_id = NULL, updated_at = now()
WHERE shared_pool_id = @pool_id AND NOT (node_name = ANY(@keep::text[]));

-- name: JoinPool :exec
UPDATE cluster_nodes SET shared_pool_id = @pool_id, updated_at = now()
WHERE cluster_id = @cluster_id AND node_name = ANY(@names::text[]);

-- name: PoolNodeCapacities :many
SELECT node_name, allocatable_cpu_milli, allocatable_memory_bytes, allocatable_pods
FROM cluster_nodes WHERE shared_pool_id = $1 ORDER BY node_name;

-- name: ListClusterNodes :many
SELECT sqlc.embed(n), d.org_id AS dedicated_org_id, o.slug AS dedicated_org_slug, o.display_name AS dedicated_org_name
FROM cluster_nodes n
LEFT JOIN dedicated_node_grants d ON d.cluster_id = n.cluster_id AND d.node_name = n.node_name AND d.revoked_at IS NULL
LEFT JOIN organizations o ON o.id = d.org_id
WHERE n.cluster_id = $1
ORDER BY n.node_name;

-- name: SeedClusterNode :execrows
-- Only a node the cluster does not know yet: from M6 the operator reports nodes.
INSERT INTO cluster_nodes (cluster_id, node_name, kind, ready, schedulable,
    allocatable_cpu_milli, allocatable_memory_bytes, allocatable_pods, shared_pool_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (cluster_id, node_name) DO NOTHING;

-- name: SeedReservedCIDRs :execrows
-- Only while the cluster has none: an Admin's later change stays.
UPDATE clusters SET reserved_cidrs = $2, updated_at = now() WHERE id = $1 AND reserved_cidrs = '{}';

-- name: SeedPoolSystemReserve :execrows
-- Only while the pool reserves nothing yet.
UPDATE shared_compute_pools
SET system_reserve_cpu_milli = $2, system_reserve_memory_bytes = $3, system_reserve_pods = $4, updated_at = now()
WHERE id = $1 AND system_reserve_cpu_milli = 0 AND system_reserve_memory_bytes = 0 AND system_reserve_pods = 0;

-- name: ListNetworkPools :many
SELECT * FROM network_pools WHERE cluster_id = $1 ORDER BY id;

-- name: CreateNetworkPool :one
INSERT INTO network_pools (id, cluster_id, org_id, cidrs, default_prefix_length, allowed_prefix_lengths)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: LockNetworkPool :one
SELECT * FROM network_pools WHERE id = $1 FOR UPDATE;

-- name: UpdateNetworkPool :one
UPDATE network_pools SET cidrs = $2, default_prefix_length = $3, allowed_prefix_lengths = $4
WHERE id = $1
RETURNING *;

-- name: ListLiveDedicatedNodes :many
SELECT * FROM dedicated_node_grants
WHERE org_id = $1 AND cluster_id = $2 AND revoked_at IS NULL
ORDER BY node_name;

-- name: CreateDedicatedNodeGrant :one
-- Starts NotReady until the operator has checked the node (M8); the
-- allocatable snapshot is the node's as last reported.
INSERT INTO dedicated_node_grants (id, org_id, cluster_id, node_name,
    allocatable_cpu_milli, allocatable_memory_bytes, allocatable_pods,
    system_reserve_cpu_milli, system_reserve_memory_bytes, system_reserve_pods, capacity_state)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: UpdateDedicatedNodeReserve :one
UPDATE dedicated_node_grants
SET system_reserve_cpu_milli = $2, system_reserve_memory_bytes = $3, system_reserve_pods = $4,
    capacity_state = $5, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: RevokeDedicatedNodeGrant :exec
UPDATE dedicated_node_grants SET revoked_at = now(), updated_at = now() WHERE id = $1;

-- name: ListLiveNodeGrantsOfCluster :many
SELECT * FROM dedicated_node_grants WHERE cluster_id = $1 AND revoked_at IS NULL ORDER BY id;
