-- What the provisioning worker copies back from the cluster
-- (IMPLEMENTATION-PLAN §5.1).

-- name: RecordWorkspaceStatus :one
-- The controller's report on a spec version, and the state it leads to.
UPDATE workspaces
SET conditions = @conditions, observed_generation = @observed_generation, state = @state, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: RecordNamespace :exec
UPDATE workspace_namespaces SET state = @state, usage_snapshot = @usage_snapshot, updated_at = now() WHERE id = @id;

-- name: CreateObservedNamespace :exec
-- A Namespace the controller found that the portal did not ask for (kubectl, Headlamp).
INSERT INTO workspace_namespaces (id, workspace_id, cluster_id, name, source, state, usage_snapshot)
VALUES (@id, @workspace_id, @cluster_id, @name, @source, 'Active', @usage_snapshot);

-- name: TimedOutOperations :many
-- Workspace operations delivered (Running) longer ago than the timeout, on a
-- cluster that is not Unavailable: time does not count while it is
-- (04-state-machines §6).
SELECT op.* FROM operations op
JOIN workspaces w ON w.id = op.resource_id AND op.resource_type = 'Workspace'
JOIN clusters c ON c.id = w.cluster_id
WHERE op.state = 'Running' AND w.cluster_id = @cluster_id AND c.status <> 'Unavailable'
  AND op.updated_at < now() - make_interval(secs => @timeout_seconds::float8)
ORDER BY op.id;

-- name: RecordObservedNode :exec
-- A node as the operator reports it (PlatformNodeInventory); the pool an
-- Admin put it in stays.
INSERT INTO cluster_nodes (cluster_id, node_name, kind, ready, schedulable,
    allocatable_cpu_milli, allocatable_memory_bytes, allocatable_pods, observed_at)
VALUES (@cluster_id, @node_name, @kind, @ready, @schedulable,
    @allocatable_cpu_milli, @allocatable_memory_bytes, @allocatable_pods, now())
ON CONFLICT (cluster_id, node_name) DO UPDATE
SET kind = EXCLUDED.kind, ready = EXCLUDED.ready, schedulable = EXCLUDED.schedulable,
    allocatable_cpu_milli = EXCLUDED.allocatable_cpu_milli, allocatable_memory_bytes = EXCLUDED.allocatable_memory_bytes,
    allocatable_pods = EXCLUDED.allocatable_pods, observed_at = now(),
    updated_at = CASE WHEN (cluster_nodes.kind, cluster_nodes.ready, cluster_nodes.schedulable, cluster_nodes.allocatable_cpu_milli,
                            cluster_nodes.allocatable_memory_bytes, cluster_nodes.allocatable_pods)
                         IS DISTINCT FROM (EXCLUDED.kind, EXCLUDED.ready, EXCLUDED.schedulable, EXCLUDED.allocatable_cpu_milli,
                            EXCLUDED.allocatable_memory_bytes, EXCLUDED.allocatable_pods)
                      THEN now() ELSE cluster_nodes.updated_at END;

-- name: ForgetNodesNotIn :execrows
-- Nodes the cluster no longer has.
DELETE FROM cluster_nodes WHERE cluster_id = @cluster_id AND NOT (node_name = ANY(@names::text[]));

-- name: RecordNodeGrantNode :exec
-- What the dedicated node of a grant offers and how it is (04-state-machines §5).
UPDATE dedicated_node_grants
SET allocatable_cpu_milli = @allocatable_cpu_milli, allocatable_memory_bytes = @allocatable_memory_bytes,
    allocatable_pods = @allocatable_pods, node_state = @node_state, capacity_state = @capacity_state, updated_at = now()
WHERE id = @id;

-- name: RecordPoolSnapshot :exec
UPDATE shared_compute_pools SET capacity_snapshot = @capacity_snapshot WHERE id = @id AND cluster_id = @cluster_id;
