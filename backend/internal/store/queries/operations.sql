-- name: CreateOperation :one
INSERT INTO operations (id, org_id, resource_type, resource_id, type, correlation_id, created_by)
VALUES (@id, @org_id, @resource_type, @resource_id, @type, @correlation_id, sqlc.narg(created_by))
RETURNING *;

-- name: GetOperationForCaller :one
-- Resolves the organization from the operation itself, never from the
-- client (threat case §10.1). A non-member gets no row, hence 404, unless
-- the caller is a Platform Admin; role is NULL when the caller is not a member.
SELECT sqlc.embed(op), m.role
FROM operations op
LEFT JOIN organization_members m ON m.org_id = op.org_id AND m.user_id = @user_id
WHERE op.id = @id
  AND (m.user_id IS NOT NULL OR @platform_admin::bool);

-- name: MoveOperation :one
-- Applies a state change only if the current state is one it may come from;
-- no row means the change was not allowed (or the operation is gone).
UPDATE operations
SET state         = sqlc.arg(to_state)::text,
    error_code    = sqlc.arg(error_code)::text,
    error_message = sqlc.arg(error_message)::text,
    finished_at   = CASE WHEN sqlc.arg(to_state)::text IN ('Succeeded', 'Failed', 'Blocked') THEN now() END,
    updated_at    = now()
WHERE id = @id AND state = ANY(sqlc.arg(from_states)::text[])
RETURNING *;
