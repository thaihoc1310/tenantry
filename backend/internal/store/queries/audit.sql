-- name: InsertAuditEvent :exec
INSERT INTO audit_events (actor_user_id, org_id, action, target_type, target_id, result, detail)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListOrganizationAudit :many
-- An organization's audit trail, newest first, with who acted.
SELECT e.id, e.at, e.action, e.target_type, e.target_id, e.result, e.detail,
       e.actor_user_id, u.display_name AS actor_name, u.email AS actor_email
FROM audit_events e LEFT JOIN users u ON u.id = e.actor_user_id
WHERE e.org_id = @org_id AND (sqlc.narg(cursor)::bigint IS NULL OR e.id < sqlc.narg(cursor))
ORDER BY e.id DESC
LIMIT @page_size;
