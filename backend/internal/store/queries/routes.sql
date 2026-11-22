-- name: ListWorkspaceRoutes :many
SELECT * FROM workspace_routes WHERE workspace_id = $1 AND state <> 'Deleted' ORDER BY namespace, name;

-- name: GetWorkspaceRoute :one
SELECT * FROM workspace_routes WHERE id = $1 AND workspace_id = $2 AND state <> 'Deleted';

-- name: CountWorkspaceRoutes :one
SELECT count(*)::int FROM workspace_routes WHERE workspace_id = $1 AND state <> 'Deleted';

-- name: CreateWorkspaceRoute :one
INSERT INTO workspace_routes (id, workspace_id, namespace, name, hostname, service_name, service_port, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: SetRouteState :one
UPDATE workspace_routes SET state = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: RecordRoute :exec
UPDATE workspace_routes SET state = $2, message = $3, updated_at = now() WHERE id = $1;

-- name: DeleteRoutesInNamespace :exec
-- The Routes of a Namespace go with it (04-state-machines §9.3).
UPDATE workspace_routes SET state = 'Deleted', updated_at = now()
WHERE workspace_id = $1 AND namespace = $2 AND state <> 'Deleted';

-- name: DeleteWorkspaceRoutes :exec
UPDATE workspace_routes SET state = 'Deleted', updated_at = now() WHERE workspace_id = $1 AND state <> 'Deleted';
