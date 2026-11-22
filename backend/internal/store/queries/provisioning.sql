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
