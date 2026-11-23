-- What a Platform Admin changes about organizations and users (M5,
-- PRODUCT-PLAN §16.5).

-- name: CreateActiveOrganization :one
-- An organization an Admin creates for a customer is Active from the start.
INSERT INTO organizations (id, slug, display_name, description, owner_user_id, status)
VALUES (@id, @slug, @display_name, @description, @owner_user_id, 'Active')
RETURNING *;

-- name: UserByEmail :one
-- Keycloak keeps emails unique (duplicateEmails off), so at most one user has it.
SELECT * FROM users WHERE lower(email) = @email::text ORDER BY created_at LIMIT 1;

-- name: GetOrganizationForAdmin :one
SELECT sqlc.embed(o), u.email AS owner_email, u.display_name AS owner_display_name
FROM organizations o JOIN users u ON u.id = o.owner_user_id
WHERE o.id = $1 AND o.status <> 'Deleted';

-- name: UpdateEntitlement :exec
UPDATE organization_entitlements
SET max_members = $2, max_groups = $3, max_workspaces_total = $4, updated_at = now()
WHERE org_id = $1;

-- name: CountLiveGroupsOf :one
-- Groups of the organization, tombstones left out.
SELECT count(*)::int FROM groups WHERE org_id = $1 AND deleted_at IS NULL;

-- name: ListUsersForAdmin :many
-- By email, then id (users without an email first). q matches email or
-- display name; status, when given, keeps Active or Disabled accounts.
SELECT * FROM users
WHERE (sqlc.narg(q)::text IS NULL OR email ILIKE '%' || sqlc.narg(q) || '%' OR display_name ILIKE '%' || sqlc.narg(q) || '%')
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(cursor)::text IS NULL OR (coalesce(email, ''), id) > (sqlc.narg(cursor_email)::text, sqlc.narg(cursor)::text))
ORDER BY coalesce(email, ''), id
LIMIT @page_size;

-- name: LockUser :one
SELECT * FROM users WHERE id = $1 FOR UPDATE;

-- name: SetUserStatus :one
-- The account's new state goes to Keycloak through the group sync worker.
UPDATE users
SET status = @status, sync_status = 'Pending', sync_attempts = 0, next_sync_at = now(), last_error = '', updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteUserSessions :execrows
DELETE FROM sessions WHERE user_id = $1;
