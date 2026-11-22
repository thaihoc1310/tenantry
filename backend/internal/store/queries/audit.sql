-- name: InsertAuditEvent :exec
INSERT INTO audit_events (actor_user_id, org_id, action, target_type, target_id, result, detail)
VALUES ($1, $2, $3, $4, $5, $6, $7);
